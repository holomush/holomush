// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package crypto_test — Phase 3b integration tests for plugin-decrypt
// AuthGuard invariants (INV-CRYPTO-9/10/11/12). These tests exercise the
// checkPlugin branch on the subscriber fan-out path using in-process
// stubs (no binary plugin processes required).
package crypto_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	guardaudit "github.com/holomush/holomush/internal/eventbus/authguard/audit"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

// mapManifestLookup implements authguard.ManifestLookup using a
// map of plugin name → Manifest. Inlines the same loop that
// Manager.PluginRequestsDecryption (manager.go:1227) uses, without
// requiring a Manager instance.
type mapManifestLookup map[string]*plugins.Manifest

func (m mapManifestLookup) PluginRequestsDecryption(name, eventType string) bool {
	manifest, ok := m[name]
	if !ok || manifest == nil || manifest.Crypto == nil {
		return false
	}
	for _, consume := range manifest.Crypto.Consumes {
		for _, ref := range consume.RequestsDecryption {
			if ref == eventType {
				return true
			}
		}
	}
	return false
}

func (m mapManifestLookup) PluginCanReadBack(name, eventType string) bool {
	manifest, ok := m[name]
	if !ok || manifest == nil || manifest.Crypto == nil {
		return false
	}
	for _, e := range manifest.Crypto.Emits {
		if e.EventType == eventType {
			return e.Readback
		}
	}
	return false
}

// auditPassthroughPublisher wraps an eventbus.Publisher and bypasses
// subject validation for audit.> subjects. The JetStreamPublisher rejects
// any subject that doesn't start with "events.", but audit events use
// "audit.<gameID>.plugin_decrypt.<pluginName>" subjects. In production,
// the emitter's drain goroutine silently drops the publish error (the
// EVENTS stream doesn't capture audit.> subjects anyway). For INV-CRYPTO-11,
// we need the audit event to actually land on JetStream so we can consume
// it from the AUDIT stream. This wrapper delegates events.> subjects to
// the wrapped publisher and publishes audit.> subjects directly to
// JetStream via nats.Msg, preserving headers and proto envelope format.
type auditPassthroughPublisher struct {
	inner eventbus.Publisher
	js    jetstream.JetStream
}

func (p *auditPassthroughPublisher) Publish(ctx context.Context, event eventbus.Event) error {
	if !strings.HasPrefix(string(event.Subject), "audit.") {
		return p.inner.Publish(ctx, event)
	}
	// Build the same proto envelope the JetStreamPublisher would produce
	// for a non-sensitive event, then publish directly to JetStream.
	envelope := &eventbusv1.Event{
		Id:        event.ID.Bytes(),
		Subject:   string(event.Subject),
		Type:      string(event.Type),
		Timestamp: timestamppb.New(event.Timestamp),
		Actor:     eventbus.ActorToProto(event.Actor),
		Payload:   event.Payload,
	}
	plainBytes, err := proto.Marshal(envelope)
	if err != nil {
		return oops.Code("AUDIT_PASSTHROUGH_MARSHAL_FAILED").Wrap(err)
	}
	msg := &nats.Msg{
		Subject: string(event.Subject),
		Data:    plainBytes,
		Header:  nats.Header{},
	}
	msg.Header.Set(eventbus.HeaderMsgID, event.ID.String())
	msg.Header.Set(eventbus.HeaderSchemaVersion, eventbus.SchemaVersion)
	msg.Header.Set(eventbus.HeaderEventType, string(event.Type))
	msg.Header.Set(eventbus.HeaderCodec, string(codec.NameIdentity))
	msg.Header.Set(eventbus.HeaderActorKind, event.Actor.Kind.String())
	if event.Actor.ID != (ulid.ULID{}) {
		msg.Header.Set(eventbus.HeaderActorID, event.Actor.ID.String())
	}
	_, err = p.js.PublishMsg(ctx, msg)
	return err
}

// pluginDecryptHarness holds all components needed for a plugin-decrypt
// INV test. Each test function constructs its own harness via
// buildPluginDecryptHarness.
//
// Isolation contract — each test function MUST use:
//   - A unique scene ID (unique JetStream subject + DEK context)
//   - A unique session ID (JetStream durable consumers are per-session)
//   - A unique plugin name (AuthGuard decisions and audit records keyed by name)
//
// Since each test builds an independent harness, the isolation contract
// is trivially satisfied: no shared mutable state exists between tests.
type pluginDecryptHarness struct {
	publisher  eventbus.Publisher
	subscriber *eventbus.JetStreamSubscriber
	dekMgr     dek.Manager
	bus        *testutil.EmbeddedBus
	manifests  mapManifestLookup
	abacEngine *policytest.GrantEngine
}

// buildPluginDecryptHarness constructs the full crypto stack for the
// plugin-decrypt INV tests: PostgreSQL, embedded JetStream, DEK manager,
// AuthGuard with controllable stubs, audit emitter, and subscriber.
// The harness is self-contained: the publisher can emit sensitive events
// and the subscriber can open sessions and receive deliveries.
func buildPluginDecryptHarness(t *testing.T) *pluginDecryptHarness {
	t.Helper()
	ctx := context.Background()

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := newPool(t, connStr)

	bus := testutil.StartEmbeddedJetStream(t)

	// Create the AUDIT JetStream stream so guardaudit.Emitter publishes
	// succeed. The default EVENTS stream only captures events.>; audit
	// events are published to audit.> subjects which no stream matches
	// in production (emitter.go:215-218 silently drops the error). The
	// test creates this stream so INV-CRYPTO-11 can verify the event was emitted.
	_, err := bus.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "AUDIT",
		Subjects: []string{"audit.>"},
		Storage:  jetstream.MemoryStorage,
	})
	if err != nil {
		t.Fatalf("buildPluginDecryptHarness: CreateOrUpdateStream AUDIT: %v", err)
	}

	kekHex := testutil.RandomKEKHex(t)
	t.Setenv("HOLOMUSH_PLUGIN_DECRYPT_KEK", kekHex)
	kekSource := kek.NewEnvSource("HOLOMUSH_PLUGIN_DECRYPT_KEK", false)

	provider, err := kek.NewLocalAEADProvider(ctx, kekSource, pool)
	if err != nil {
		t.Fatalf("buildPluginDecryptHarness: NewLocalAEADProvider: %v", err)
	}

	dekStore := dek.NewStore(pool)
	dekCache := dek.NewCache(dek.CacheConfig{Capacity: 64})
	dekPartCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64})
	dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&dekBindingStub{bindingID: "bind-decrypt"})
	if err != nil {
		t.Fatalf("buildPluginDecryptHarness: NewManager: %v", err)
	}

	// Host audit subsystem: populates events_audit. Handle stays local —
	// no test calls AwaitDrained on it.
	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	if err := hostSub.Prepare(ctx); err != nil {
		t.Fatalf("buildPluginDecryptHarness: hostSub.Prepare: %v", err)
	}
	if err := hostSub.Activate(ctx); err != nil {
		t.Fatalf("buildPluginDecryptHarness: hostSub.Activate: %v", err)
	}
	t.Cleanup(func() { _ = hostSub.Stop(context.Background()) })

	registry, err := core.BootstrapVerbRegistry("test")
	if err != nil {
		t.Fatalf("buildPluginDecryptHarness: BootstrapVerbRegistry: %v", err)
	}
	if err := registry.RegisterWithSource(core.VerbRegistration{
		Type:          "test-plugin:whisper",
		Category:      "communication",
		Format:        "speech",
		Label:         "whispers",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "test-plugin",
	}, "1.0.0"); err != nil {
		t.Fatalf("buildPluginDecryptHarness: RegisterWithSource: %v", err)
	}

	rawPub := eventbus.NewJetStreamPublisher(
		bus.JS,
		eventbus.Config{}.Defaults(),
		eventbus.WithDEKManager(dekMgr),
	)
	hostPub := eventbus.NewRenderingPublisher(rawPub, registry)

	// Controllable stubs for AuthGuard.
	manifests := make(mapManifestLookup)
	abacEngine := policytest.NewGrantEngine()

	participantLookup := authguard.NewDEKParticipantLookup(dekMgr)
	guard, err := authguard.New(participantLookup, manifests, abacEngine, noopBackpressure{})
	if err != nil {
		t.Fatalf("buildPluginDecryptHarness: authguard.New: %v", err)
	}

	sessionGuard := authguard.NewSessionBridgeGuard(guard)

	// Audit emitter: publishes audit events to audit.<gameID>.plugin_decrypt.<plugin>.
	// The AUDIT stream (created above) captures these.
	// The JetStreamPublisher rejects audit.> subjects (validates events.> prefix),
	// so we wrap it in auditPassthroughPublisher which bypasses subject validation
	// for audit events and publishes directly to JetStream.
	auditPub := &auditPassthroughPublisher{inner: rawPub, js: bus.JS}
	auditEmitter, err := guardaudit.NewQueuedEmitter(auditPub)
	if err != nil {
		t.Fatalf("buildPluginDecryptHarness: NewQueuedEmitter: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = auditEmitter.Shutdown(shutCtx)
	})
	sessionAuditEmitter, err := guardaudit.NewSessionBridgeEmitter(auditEmitter)
	if err != nil {
		t.Fatalf("buildPluginDecryptHarness: NewSessionBridgeEmitter: %v", err)
	}

	sub := eventbus.NewJetStreamSubscriber(
		bus.JS,
		eventbus.WithSubscriberAuthGuard(sessionGuard),
		eventbus.WithSubscriberDEKManager(dekMgr),
		eventbus.WithSubscriberDecryptAuditEmitter(sessionAuditEmitter),
		eventbus.WithSessionAckWait(5*time.Second),
		eventbus.WithSessionInactiveThreshold(30*time.Second),
	)

	return &pluginDecryptHarness{
		publisher:  hostPub,
		subscriber: sub,
		dekMgr:     dekMgr,
		bus:        bus,
		manifests:  manifests,
		abacEngine: abacEngine,
	}
}

// emitSensitiveWhisper publishes a sensitive test-plugin:whisper event
// to the given scene subject. The caller is responsible for creating
// the DEK context and participants beforehand.
func emitSensitiveWhisper(t *testing.T, h *pluginDecryptHarness, sceneID, plaintext string) {
	t.Helper()
	ctx := context.Background()

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
	actorResolver := func(_ context.Context, _ string) (core.Actor, error) {
		return core.Actor{Kind: core.ActorPlugin, ID: plugintest.PluginULIDFromName("test-plugin").String()}, nil
	}
	emitter := plugins.NewPluginEventEmitter(
		h.publisher, manifestLookup, actorResolver,
	)

	intent := pluginsdk.EmitIntent{
		Subject:   "scene." + sceneID,
		Type:      pluginsdk.EventType("test-plugin:whisper"),
		Payload:   plaintext,
		Sensitive: true,
	}
	if err := emitter.Emit(ctx, "test-plugin", intent); err != nil {
		t.Fatalf("emitSensitiveWhisper: %v", err)
	}
}

// TestPluginDecryptManifestGateDeniesWithoutDeclaration verifies INV-CRYPTO-9:
// a plugin without manifest requests_decryption for an event class MUST
// receive metadata-only delivery for events of that class, regardless of
// subject subscription. The manifest gate fires before the ABAC evaluation
// (guard.go:123), so even with an ABAC grant, the plugin is denied.
var _ = Describe("Plugin decrypt manifest gate denies without declaration (INV-CRYPTO-9)", func() {
	It("plugin without requests_decryption receives MetadataOnly=true", func() {
		h := buildPluginDecryptHarness(suiteT)

		const (
			sceneID              = "01HXXXINV17SCENE00000001"
			pluginName           = "inv17-no-manifest"
			sessionID            = "inv17-session"
			participantBindingID = "01INV17PARTICIPANTBIND00"
		)

		// Pre-create DEK with a participant so the event is encryptable.
		ctx := context.Background()
		ctxID := dek.ContextID{Type: "scene", ID: sceneID}
		_, err := h.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{
			{
				PlayerID:    "01INV17PLAYER00000000",
				CharacterID: "01INV17CHARACT00000000",
				BindingID:   participantBindingID,
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Register a grant for this plugin to prove the manifest gate fires
		// before ABAC evaluation — the ABAC engine would permit, but the
		// manifest gate denies first.
		h.abacEngine.Grant("plugin:"+pluginName, "decrypt", "dek:1:1")

		// Do NOT register the plugin in manifests — this is the INV-CRYPTO-9 condition.

		const plaintext = `{"text":"inv17 secret"}`
		emitSensitiveWhisper(suiteT, h, sceneID, plaintext)

		pluginID := eventbus.SessionIdentity{
			Kind:       eventbus.IdentityKindPlugin,
			PluginName: pluginName,
		}
		stream, err := h.subscriber.OpenSession(ctx, sessionID, pluginID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		receiveCtx, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()

		delivery, err := stream.Next(receiveCtx)
		Expect(err).NotTo(HaveOccurred(), "expected delivery from stream")
		Expect(delivery.Ack()).NotTo(HaveOccurred())

		// Verifies: INV-CRYPTO-9
		Expect(delivery.MetadataOnly()).To(BeTrue(), "INV-CRYPTO-9: plugin without requests_decryption must receive MetadataOnly=true")
		Expect(delivery.Event().Payload).To(BeEmpty(), "INV-CRYPTO-9: metadata-only delivery must have empty payload")
	})
})

// TestPluginDecryptABACGateDeniesWithoutGrant verifies INV-CRYPTO-10: a plugin
// with manifest declaration but without an active ABAC grant MUST receive
// metadata-only. The manifest gate passes (requests_decryption declared)
// but the ABAC evaluation denies (guard.go:143-144).
var _ = Describe("Plugin decrypt ABAC gate denies without grant (INV-CRYPTO-10)", func() {
	It("plugin with manifest but no ABAC grant receives MetadataOnly=true", func() {
		h := buildPluginDecryptHarness(suiteT)

		const (
			sceneID    = "01HXXXINV18SCENE00000002"
			pluginName = "inv18-has-manifest"
			sessionID  = "inv18-session"
		)

		// Pre-create DEK with a participant.
		ctx := context.Background()
		ctxID := dek.ContextID{Type: "scene", ID: sceneID}
		_, err := h.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{
			{
				PlayerID:    "01INV18PLAYER00000000",
				CharacterID: "01INV18CHARACT00000000",
				BindingID:   "01INV18PARTICIPANTBIND00",
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Register the plugin's manifest with requests_decryption — this
		// passes the manifest gate (guard.go:123) but ABAC will deny because
		// no grant is configured for this plugin in the GrantEngine.
		h.manifests[pluginName] = &plugins.Manifest{
			Name: pluginName,
			Crypto: &plugins.CryptoSection{
				Consumes: []plugins.CryptoConsume{
					{
						Subjects:           []string{"events.>"},
						RequestsDecryption: []string{"test-plugin:whisper"},
					},
				},
			},
		}

		// Do NOT call h.abacEngine.Grant — this is the INV-CRYPTO-10 condition.

		const plaintext = `{"text":"inv18 secret"}`
		emitSensitiveWhisper(suiteT, h, sceneID, plaintext)

		pluginID := eventbus.SessionIdentity{
			Kind:       eventbus.IdentityKindPlugin,
			PluginName: pluginName,
		}
		stream, err := h.subscriber.OpenSession(ctx, sessionID, pluginID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		receiveCtx, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()

		delivery, err := stream.Next(receiveCtx)
		Expect(err).NotTo(HaveOccurred(), "expected delivery from stream")
		Expect(delivery.Ack()).NotTo(HaveOccurred())

		// Verifies: INV-CRYPTO-10
		Expect(delivery.MetadataOnly()).To(BeTrue(), "INV-CRYPTO-10: plugin with manifest but no ABAC grant must receive MetadataOnly=true")
		Expect(delivery.Event().Payload).To(BeEmpty(), "INV-CRYPTO-10: metadata-only delivery must have empty payload")
	})
})

// TestPluginDecryptEmitsAuditOnPermitAndIsolation verifies INV-CRYPTO-11: every
// plugin decryption MUST emit an audit event on a subject the plugin cannot
// subscribe to. The audit event is published to
// audit.holomush.plugin_decrypt.<pluginName> (guardaudit.Emitter defaults
// gameID to "holomush" per emitter.go:29), which is outside the events.>
// subject namespace. The plugin session subscribes to events.> only, so
// the audit event is invisible by construction.
var _ = Describe("Plugin decrypt emits audit on permit and isolation (INV-CRYPTO-11)", func() {
	It("permitted plugin receives plaintext and audit event is isolated from plugin session", func() {
		h := buildPluginDecryptHarness(suiteT)

		const (
			sceneID    = "01HXXXINV19SCENE00000003"
			pluginName = "inv19-audit-plugin"
			sessionID  = "inv19-session"
		)

		ctx := context.Background()

		// Pre-create DEK with a participant and capture the key coordinates
		// for the ABAC grant (guard.go:127-129 constructs resource as
		// "dek:<keyID>:<version>").
		ctxID := dek.ContextID{Type: "scene", ID: sceneID}
		key, err := h.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{
			{
				PlayerID:    "01INV19PLAYER00000000",
				CharacterID: "01INV19CHARACT00000000",
				BindingID:   "01INV19PARTICIPANTBIND00",
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Register the manifest with requests_decryption.
		h.manifests[pluginName] = &plugins.Manifest{
			Name: pluginName,
			Crypto: &plugins.CryptoSection{
				Consumes: []plugins.CryptoConsume{
					{
						Subjects:           []string{"events.>"},
						RequestsDecryption: []string{"test-plugin:whisper"},
					},
				},
			},
		}

		// Grant the ABAC permission using the captured DEK coordinates.
		h.abacEngine.Grant(
			"plugin:"+pluginName,
			"decrypt",
			fmt.Sprintf("dek:%d:%d", key.ID, key.Version),
		)

		const plaintext = `{"text":"inv19 audit secret"}`
		emitSensitiveWhisper(suiteT, h, sceneID, plaintext)

		// Open the plugin session.
		pluginID := eventbus.SessionIdentity{
			Kind:       eventbus.IdentityKindPlugin,
			PluginName: pluginName,
		}
		stream, err := h.subscriber.OpenSession(ctx, sessionID, pluginID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		receiveCtx, cancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer cancel()

		delivery, err := stream.Next(receiveCtx)
		Expect(err).NotTo(HaveOccurred(), "expected delivery from stream")
		Expect(delivery.Ack()).NotTo(HaveOccurred())

		// Assertion 1: plugin received plaintext (permit path).
		// Verifies: INV-CRYPTO-11 (permit branch)
		Expect(delivery.MetadataOnly()).To(BeFalse(), "INV-CRYPTO-11: permitted plugin must receive MetadataOnly=false")
		Expect(delivery.Event().Payload).To(Equal([]byte(plaintext)), "INV-CRYPTO-11: permitted plugin must receive original plaintext")

		// Assertion 2: audit event exists on audit.holomush.plugin_decrypt.inv19-audit-plugin.
		// The guardaudit.Emitter publishes to the AUDIT stream (not events.>).
		// Verifies: INV-CRYPTO-11 (audit-on-permit)
		auditSubject := "audit.holomush.plugin_decrypt." + pluginName
		auditMsg := testutil.WaitForOneJetStreamMsgOnStream(suiteT, h.bus, "AUDIT", auditSubject, testutil.DefaultWait)
		Expect(auditMsg.Headers().Get("App-Event-Type")).To(Equal("audit:plugin_decrypt"),
			"INV-CRYPTO-11: audit event must have type audit:plugin_decrypt")

		// Assertion 3: the plugin session did NOT receive the audit event.
		// The session subscribes to events.>; the audit event is on audit.>,
		// which is outside the filter. We verify this by draining the session
		// with a short timeout — only the sensitive event should appear.
		//
		// The session already consumed the sensitive event above. If the audit
		// event were visible on events.>, stream.Next would return it.
		// Instead, we expect a timeout (no more messages).
		noMoreCtx, noMoreCancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer noMoreCancel()
		_, noMoreErr := stream.Next(noMoreCtx)
		Expect(noMoreErr).To(HaveOccurred(), "INV-CRYPTO-11: plugin session must not receive audit events (audit.> is outside events.> filter)")
	})
})

// TestPluginDecryptDenialDoesNotBlockFanout verifies INV-CRYPTO-12: a plugin
// authorization failure MUST NOT block fan-out to other recipients.
// Two sessions subscribe to the same events.> subject: a character
// participant (permitted) and a plugin without manifest (denied). The
// test asserts both receive the event — the character gets plaintext
// and the plugin gets metadata-only — proving that the plugin's deny
// did not block the character's delivery.
var _ = Describe("Plugin decrypt denial does not block fanout (INV-CRYPTO-12)", func() {
	It("character receives plaintext and denied plugin receives MetadataOnly without blocking each other", func() {
		h := buildPluginDecryptHarness(suiteT)

		const (
			sceneID              = "01HXXXINV20SCENE00000004"
			participantPlayerID  = "01INV20PLAYER00000000"
			participantCharID    = "01INV20CHARACT00000000"
			participantBindingID = "01INV20PARTICIPANTBIND00"
			deniedPluginName     = "inv20-denied-plugin"
			characterSessionID   = "inv20-character-session"
			pluginSessionID      = "inv20-plugin-session"
		)

		// Pre-create DEK with a participant (for the character session).
		ctx := context.Background()
		ctxID := dek.ContextID{Type: "scene", ID: sceneID}
		_, err := h.dekMgr.GetOrCreate(ctx, ctxID, []dek.Participant{
			{
				PlayerID:    participantPlayerID,
				CharacterID: participantCharID,
				BindingID:   participantBindingID,
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "test_setup",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Do NOT register the denied plugin's manifest — INV-CRYPTO-12 condition.

		const plaintext = `{"text":"inv20 fanout secret"}`

		// Open character session (participant — permitted).
		charID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    participantPlayerID,
			CharacterID: participantCharID,
			BindingID:   participantBindingID,
		}
		charStream, err := h.subscriber.OpenSession(ctx, characterSessionID, charID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = charStream.Close() })

		// Open plugin session (no manifest — denied).
		pluginID := eventbus.SessionIdentity{
			Kind:       eventbus.IdentityKindPlugin,
			PluginName: deniedPluginName,
		}
		pluginStream, err := h.subscriber.OpenSession(ctx, pluginSessionID, pluginID, []eventbus.Subject{"events.>"}, time.Time{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = pluginStream.Close() })

		// Publish the sensitive event after both sessions are open.
		emitSensitiveWhisper(suiteT, h, sceneID, plaintext)

		// Receive from character session.
		charRecvCtx, charCancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer charCancel()

		charDelivery, err := charStream.Next(charRecvCtx)
		Expect(err).NotTo(HaveOccurred(), "character session must receive the event")
		Expect(charDelivery.Ack()).NotTo(HaveOccurred())

		// Receive from plugin session.
		pluginRecvCtx, pluginCancel := context.WithTimeout(ctx, testutil.DefaultWait)
		defer pluginCancel()

		pluginDelivery, err := pluginStream.Next(pluginRecvCtx)
		Expect(err).NotTo(HaveOccurred(), "plugin session must receive the event (metadata-only)")
		Expect(pluginDelivery.Ack()).NotTo(HaveOccurred())

		// Verifies: INV-CRYPTO-12 — the character's plaintext delivery proves the
		// plugin's deny did not block fan-out to other recipients.
		Expect(charDelivery.MetadataOnly()).To(BeFalse(), "INV-CRYPTO-12: character participant must receive MetadataOnly=false (plaintext)")
		Expect(charDelivery.Event().Payload).To(Equal([]byte(plaintext)), "INV-CRYPTO-12: character must receive original plaintext")

		// The plugin's deny is expected (INV-CRYPTO-9 manifest gate).
		Expect(pluginDelivery.MetadataOnly()).To(BeTrue(), "INV-CRYPTO-12: denied plugin must receive MetadataOnly=true")
		Expect(pluginDelivery.Event().Payload).To(BeEmpty(), "INV-CRYPTO-12: denied plugin payload must be empty")
	})
})
