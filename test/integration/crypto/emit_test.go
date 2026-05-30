// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package crypto_test holds the Phase 3a end-to-end integration tests
// for sensitive event emit. The tests stand up a real PostgreSQL
// container, an embedded NATS+JetStream bus, the audit projection
// worker, and a real DEK manager wired to an env-backed KEK, then
// assert that a sensitive plugin emit produces ciphertext on the bus
// and a byte-equal events_audit row (INV-21).
package crypto_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

// fixedJS / fixedPool satisfy audit.JSProvider / audit.PoolProvider
// from already-started resources.
type fixedJS struct{ js jetstream.JetStream }

func (f fixedJS) JS() jetstream.JetStream { return f.js }

type fixedPool struct{ pool *pgxpool.Pool }

func (f fixedPool) Pool() *pgxpool.Pool { return f.pool }

// TestSensitiveEmitProducesCiphertextOnBusAndInAudit lands the
// end-to-end Phase 3a behavior: a manifest=may + Sensitive=true emit
// produces ciphertext on the bus and a byte-equal events_audit row
// (INV-21). AAD-bind tamper detection (INV-25) is unit-tested in
// internal/eventbus/codec/xchacha20poly1305_test.go; full decrypt
// round-trip is Phase 3b's job (subscribe path).
var _ = Describe("Sensitive emit produces ciphertext on bus and in audit (INV-21)", func() {
	It("emits encrypted envelope to JetStream and matching events_audit row", func() {
		ctx := context.Background()

		// PG: shared container + per-test migrated DB. NewPGPool/StartPostgres
		// shorthands cited in the plan don't exist in test/testutil; use the
		// existing SharedPostgres + FreshDatabase + pgxpool pattern that the
		// rest of test/integration uses.
		shared := testutil.SharedPostgres(suiteT)
		connStr := testutil.FreshDatabase(suiteT, shared)
		pool := newPool(suiteT, connStr)

		bus := testutil.StartEmbeddedJetStream(suiteT)

		// KEK source: env-backed test KEK (32 random bytes hex-encoded).
		kekHex := testutil.RandomKEKHex(suiteT)
		suiteT.Setenv("HOLOMUSH_TEST_KEK", kekHex)
		kekSource := kek.NewEnvSource("HOLOMUSH_TEST_KEK", false)

		provider, err := kek.NewLocalAEADProvider(ctx, kekSource, pool)
		Expect(err).NotTo(HaveOccurred())

		// dek.NewStore(pool) / dek.NewCache(cfg) / dek.NewManager(...)
		// signatures verified at:
		//   internal/eventbus/crypto/dek/store.go:77
		//   internal/eventbus/crypto/dek/cache.go:74
		//   internal/eventbus/crypto/dek/manager.go:49
		dekStore := dek.NewStore(pool)
		dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
		dekPartCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64})
		dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache,
			func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
			&dekBindingStub{bindingID: "bind-emit"})
		Expect(err).NotTo(HaveOccurred())

		// Stand up the audit projection so events_audit gets populated.
		// audit.Config{} (no OwnerMap) means host owns every subject.
		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
		Expect(hostSub.Start(ctx)).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		manifest := &plugins.Manifest{
			Name:                "test-plugin",
			Emits:               []string{"scene"},
			ActorKindsClaimable: []string{"plugin"},
			Crypto: &plugins.CryptoSection{
				Emits: []plugins.CryptoEmit{
					{EventType: "test-plugin:whisper", Sensitivity: plugins.SensitivityMay},
				},
			},
		}
		manifestLookup := func(name string) *plugins.Manifest {
			if name == "test-plugin" {
				return manifest
			}
			return nil
		}
		// Post-w9ml: Actor.ID MUST be a ULID string (strict-gate
		// coreActorToEventbusActor rejects non-ULID IDs).
		testPluginActorID := plugintest.PluginULIDFromName("test-plugin").String()
		actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
			return core.Actor{Kind: core.ActorPlugin, ID: testPluginActorID}, nil
		}

		// Verb registry: register test-plugin:whisper so RenderingPublisher
		// can stamp App-Rendering (the audit projection rejects messages
		// without it). Uses BootstrapVerbRegistry to pick up the host
		// builtins, then RegisterWithSource for our plugin verb.
		registry, err := core.BootstrapVerbRegistry("test")
		Expect(err).NotTo(HaveOccurred())
		Expect(registry.RegisterWithSource(core.VerbRegistration{
			Type:          "test-plugin:whisper",
			Category:      "communication",
			Format:        "speech",
			Label:         "whispers",
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
			Source:        "test-plugin",
		}, "1.0.0")).NotTo(HaveOccurred())

		// NewJetStreamPublisher signature verified at publisher.go:151:
		//   func NewJetStreamPublisher(js jetstream.JetStream, cfg Config, opts ...PublishOption) *JetStreamPublisher
		// bus.JS is the public field on eventbustest.Embedded (embedded.go:50).
		rawPub := eventbus.NewJetStreamPublisher(
			bus.JS,
			eventbus.Config{}.Defaults(),
			eventbus.WithDEKManager(dekMgr),
		)
		// RenderingPublisher stamps the App-Rendering header that the audit
		// projection requires; it is the single writer of that header.
		hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

		// Phase 3a sensitivity fence is gated behind WithCryptoEnabled —
		// this E2E exercises the enabled path end-to-end (encrypt + audit).
		emitter := plugins.NewPluginEventEmitter(
			hostPub, manifestLookup, actorResolver,
			plugins.WithCryptoEnabled(true),
		)

		const plaintext = `{"text":"hello, secret world"}`
		intent := pluginsdk.EmitIntent{
			Subject:   "scene.01HXXXTESTSCENE000000000",
			Type:      pluginsdk.EventType("test-plugin:whisper"),
			Payload:   plaintext,
			Sensitive: true,
		}
		Expect(emitter.Emit(ctx, "test-plugin", intent)).NotTo(HaveOccurred())

		// 1. Bus assertion. The emitter's eventbus.Qualify translates
		// scene.<id> → events.main.scene.<id>; subscribe to events.> to
		// avoid coupling to the exact qualified subject shape.
		msg := testutil.WaitForOneJetStreamMsg(suiteT, bus, "events.>", testutil.DefaultWait)
		headers := msg.Headers()
		Expect(headers.Get(eventbus.HeaderCodec)).To(Equal("xchacha20poly1305-v1"))
		dekRefHdr := headers.Get(eventbus.HeaderDekRef)
		dekVerHdr := headers.Get(eventbus.HeaderDekVersion)
		Expect(dekRefHdr).NotTo(BeEmpty())
		Expect(dekVerHdr).NotTo(BeEmpty())
		Expect(msg.Data()).NotTo(Equal([]byte(plaintext)), "payload must be ciphertext")

		// 2. Audit row mirrors bus (INV-21). Wait for the projection to
		// drain so the INSERT lands before we query.
		hostSub.AwaitDrained(suiteT, 10*time.Second)

		natsMsgID := headers.Get("Nats-Msg-Id")
		Expect(natsMsgID).NotTo(BeEmpty())
		idBytes := testutil.MustParseULID(suiteT, natsMsgID).Bytes()
		row := testutil.QueryEventsAuditByID(suiteT, pool, idBytes)
		Expect(row.Codec).To(Equal("xchacha20poly1305-v1"))
		Expect(row.DekRef).NotTo(BeNil())
		gotRef, err := strconv.ParseInt(dekRefHdr, 10, 64)
		Expect(err).NotTo(HaveOccurred())
		Expect(gotRef).To(Equal(*row.DekRef))
		Expect(row.DekVersion).NotTo(BeNil())
		gotVer, err := strconv.ParseInt(dekVerHdr, 10, 32)
		Expect(err).NotTo(HaveOccurred())
		Expect(int32(gotVer)).To(Equal(*row.DekVersion)) //nolint:gosec // G115: ParseInt with bitSize=32 already bounds the value to int32.
		Expect(msg.Data()).To(Equal(row.Envelope), "INV-21: bus and audit envelope bytes must be byte-equal")

		// Decision 0: msg.Data is the marshaled envelope (cleartext metadata
		// fields + ciphertext payload field), NOT a single ciphertext blob.
		var wireEnvelope eventbusv1.Event
		Expect(proto.Unmarshal(msg.Data(), &wireEnvelope)).NotTo(HaveOccurred(), "msg.Data MUST unmarshal as eventbusv1.Event")
		Expect(wireEnvelope.Payload).NotTo(Equal([]byte(plaintext)), "envelope.Payload MUST be ciphertext, not plaintext")
		Expect(wireEnvelope.Subject).To(Equal("events.main.scene.01HXXXTESTSCENE000000000"), "envelope.Subject MUST be cleartext on the wire")
		Expect(wireEnvelope.Type).To(Equal("test-plugin:whisper"), "envelope.Type MUST be cleartext on the wire")
		Expect(wireEnvelope.Timestamp).NotTo(BeNil(), "envelope.Timestamp MUST be cleartext on the wire")
		Expect(wireEnvelope.Actor).NotTo(BeNil(), "envelope.Actor MUST be cleartext on the wire")

		// AAD-bind verification (INV-25 round-trip) is unit-tested at
		// internal/eventbus/codec/xchacha20poly1305_test.go::TestXChaCha20Poly1305DetectsAADTamper.
		// Decrypt-on-fanout E2E (full plaintext recovery via the subscriber
		// path) is Phase 3b.
	})
})

// newPool opens a pgxpool against a caller-supplied connection string.
// t.Cleanup handles Close — callers do not.
func newPool(t *testing.T, connStr string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("newPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
