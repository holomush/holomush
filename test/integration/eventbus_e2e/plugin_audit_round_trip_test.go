// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

// dekBindingStubE2E satisfies dek.BindingResolver for round-trip tests
// that don't depend on a wizard binding store.
type dekBindingStubE2E struct{ id string }

func (s *dekBindingStubE2E) Current(_ context.Context, _ string) (string, error) {
	return s.id, nil
}

// Scene log ciphertext and audit header round-trip specs — INV-P7-6 + INV-P7-12.
//
// Emits one sensitive event under a plugin-owned subject and asserts:
//
//   - The plugin's scene_log row holds CIPHERTEXT, not plaintext (INV-P7-12).
//   - The plugin row's payload bytes are byte-equal to the bus envelope
//     payload (INV-P7-6: plugin storage MUST mirror the bus byte-for-byte).
//   - The plugin row carries dek_ref + dek_version populated from the
//     bus headers (INV-EVENTBUS-25 column shape, INV-P7-1 wire shape).
var _ = Describe("Scene log preserves ciphertext and audit headers (INV-P7-6, INV-P7-12)", func() {
	It("plugin scene_log row is ciphertext byte-equal to bus envelope payload", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		pool := freshPool()
		bus := freshBus()

		// scene_log schema. Matches plugins/core-scenes/migrations/000004 +
		// 000005 (Phase 7 dek_ref + dek_version).
		ensurePluginSchema(ctx, suiteT, pool, "plugin_core_scenes", `
			CREATE TABLE IF NOT EXISTS plugin_core_scenes.scene_log (
				id          BYTEA PRIMARY KEY,
				subject     TEXT NOT NULL,
				type        TEXT NOT NULL,
				timestamp   TIMESTAMPTZ NOT NULL,
				actor_kind  TEXT NOT NULL,
				actor_id    BYTEA,
				payload     BYTEA NOT NULL,
				schema_ver  SMALLINT NOT NULL,
				codec       TEXT NOT NULL,
				js_seq      BIGINT,
				dek_ref     BIGINT,
				dek_version INTEGER,
				inserted_at TIMESTAMPTZ NOT NULL DEFAULT now()
			);
		`)

		// KEK + DEK manager (mirrors test/integration/crypto/emit_test.go setup).
		kekHex := testutil.RandomKEKHex(suiteT)
		// Per-spec env var with restore (NOT suiteT.Setenv — that's suite-scoped
		// and would leak across specs through the shared *testing.T).
		const kekEnv = "HOLOMUSH_TEST_ROUND_TRIP_KEK"
		prevKek, prevKekSet := os.LookupEnv(kekEnv)
		Expect(os.Setenv(kekEnv, kekHex)).To(Succeed())
		DeferCleanup(func() {
			if prevKekSet {
				_ = os.Setenv(kekEnv, prevKek)
			} else {
				_ = os.Unsetenv(kekEnv)
			}
		})
		provider, err := kek.NewLocalAEADProvider(ctx,
			kek.NewEnvSource("HOLOMUSH_TEST_ROUND_TRIP_KEK", false), pool)
		Expect(err).NotTo(HaveOccurred())
		dekMgr, err := dek.NewManager(provider,
			dek.NewStore(pool),
			dek.NewCache(dek.CacheConfig{Capacity: 64}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64}),
			func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
			&dekBindingStubE2E{id: "bind-roundtrip"})
		Expect(err).NotTo(HaveOccurred())

		// Encrypting publisher — Sensitive=true events flow through the DEK
		// manager and end up encrypted on the bus.
		encryptingPub := eventbus.NewJetStreamPublisher(
			bus.JS,
			eventbus.Config{}.Defaults(),
			eventbus.WithDEKManager(dekMgr),
		)

		// OwnerMap + per-plugin consumer (ack-and-skip in host projection;
		// scene subjects flow through the per-plugin consumer client).
		owners, err := audit.NewOwnerMap([]audit.SubjectOwner{
			{PluginName: "core-scenes", Pattern: "events.*.scene.>"},
		})
		Expect(err).NotTo(HaveOccurred())
		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{
			Owners: owners,
		})
		pluginMgr := audit.NewPluginConsumerManager(bus.JS)
		Expect(pluginMgr.Add(ctx, audit.PluginConsumerConfig{
			PluginName: "core-scenes",
			Subjects:   []string{"events.*.scene.>"},
			Client:     &pgSceneLogClient{pool: pool},
		})).To(Succeed())
		hostSub.SetLateInitProvider(func() (*audit.OwnerMap, *audit.PluginConsumerManager) {
			return owners, pluginMgr
		})
		Expect(hostSub.Start(ctx)).To(Succeed())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		// Publish one sensitive event on a plugin-owned subject.
		const plaintext = `{"text":"the original plaintext"}`
		eventID := ulid.Make()
		subject := eventbus.Subject("events.main.scene.01ABC.ic")
		Expect(encryptingPub.Publish(ctx, eventbus.Event{
			ID:        eventID,
			Subject:   subject,
			Type:      eventbus.Type("scene.whisper"),
			Timestamp: time.Now().UTC(),
			Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
			Payload:   []byte(plaintext),
			Sensitive: true,
		})).To(Succeed())

		// Wait for the plugin's per-plugin consumer to land the row.
		waitForRowInSceneLog(suiteT, pool, eventID.Bytes(), 10*time.Second)

		// Read back the persisted row.
		pluginRow := readSceneLogRow(suiteT, pool, eventID.Bytes())

		// INV-P7-12: plugin row payload MUST be ciphertext, not plaintext.
		Expect(pluginRow.codec).To(Equal("xchacha20poly1305-v1"))
		Expect(pluginRow.payload).NotTo(Equal([]byte(plaintext)),
			"INV-P7-12: plugin row MUST hold ciphertext for sensitive events")
		Expect(pluginRow.payload).NotTo(BeEmpty())

		// INV-EVENTBUS-25: dek_ref + dek_version present.
		Expect(pluginRow.dekRef).NotTo(BeNil())
		Expect(pluginRow.dekVersion).NotTo(BeNil())

		// INV-P7-6: plugin payload byte-equal to the bus envelope payload.
		busPayload := lookupBusEnvelopePayload(suiteT, bus, eventID)
		Expect(pluginRow.payload).To(Equal(busPayload),
			"INV-P7-6: plugin storage MUST be byte-equal to bus envelope payload")

		// Cross-check: bus payload itself MUST be ciphertext (sanity).
		Expect(busPayload).NotTo(Equal([]byte(plaintext)),
			"sanity: bus envelope payload MUST be ciphertext for sensitive events")
	})
})

// readSceneLogRow scans the post-Phase-7 scene_log columns for a single id.
type roundTripRow struct {
	codec      string
	payload    []byte
	dekRef     *int64
	dekVersion *int32
}

func readSceneLogRow(t *testing.T, pool *pgxpool.Pool, eventID []byte) roundTripRow {
	t.Helper()
	var r roundTripRow
	if err := pool.QueryRow(t.Context(), `
		SELECT codec, payload, dek_ref, dek_version
		FROM plugin_core_scenes.scene_log
		WHERE id = $1`, eventID).Scan(&r.codec, &r.payload, &r.dekRef, &r.dekVersion); err != nil {
		t.Fatalf("readSceneLogRow: %v", err)
	}
	return r
}

// lookupBusEnvelopePayload subscribes to the stream and reads back the
// envelope for the given event ID, returning Event.payload (the encrypted
// bytes per INV-49).
func lookupBusEnvelopePayload(t *testing.T, bus *testutil.EmbeddedBus, eventID ulid.ULID) []byte {
	t.Helper()
	idStr := eventID.String()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// Wait for at least one delivery on the events.> subject.
		msg := testutil.WaitForOneJetStreamMsg(t, bus, "events.main.scene.>", testutil.DefaultWait)
		if msg.Headers().Get("Nats-Msg-Id") != idStr {
			continue
		}
		var env eventbusv1.Event
		if err := proto.Unmarshal(msg.Data(), &env); err != nil {
			t.Fatalf("lookupBusEnvelopePayload unmarshal: %v", err)
		}
		// Sanity: assert the headers carry the crypto fields too.
		if msg.Headers().Get(eventbus.HeaderDekRef) == "" {
			t.Fatalf("bus envelope missing HeaderDekRef")
		}
		if msg.Headers().Get(eventbus.HeaderDekVersion) == "" {
			t.Fatalf("bus envelope missing HeaderDekVersion")
		}
		if _, err := strconv.ParseInt(msg.Headers().Get(eventbus.HeaderDekRef), 10, 64); err != nil {
			t.Fatalf("lookupBusEnvelopePayload ParseInt: %v", err)
		}
		return env.GetPayload()
	}
	t.Fatalf("timed out waiting for bus envelope for event %s", idStr)
	return nil
}
