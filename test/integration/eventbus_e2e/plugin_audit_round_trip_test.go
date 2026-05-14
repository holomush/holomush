// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestSceneLogPreservesCiphertextAndAuditHeaders — INV-P7-6 + INV-P7-12.
//
// Emits one sensitive event under a plugin-owned subject and asserts:
//
//   - The plugin's scene_log row holds CIPHERTEXT, not plaintext (INV-P7-12).
//   - The plugin row's payload bytes are byte-equal to the bus envelope
//     payload (INV-P7-6: plugin storage MUST mirror the bus byte-for-byte).
//   - The plugin row carries dek_ref + dek_version populated from the
//     bus headers (INV-P7-3 column shape, INV-P7-1 wire shape).
func TestSceneLogPreservesCiphertextAndAuditHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	pool := freshPool(t)
	bus := testutil.StartEmbeddedJetStream(t)

	// scene_log schema. Matches plugins/core-scenes/migrations/000004 +
	// 000005 (Phase 7 dek_ref + dek_version).
	ensurePluginSchema(ctx, t, pool, "plugin_core_scenes", `
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
	kekHex := testutil.RandomKEKHex(t)
	t.Setenv("HOLOMUSH_TEST_ROUND_TRIP_KEK", kekHex)
	provider, err := kek.NewLocalAEADProvider(ctx,
		kek.NewEnvSource("HOLOMUSH_TEST_ROUND_TRIP_KEK", false), pool)
	require.NoError(t, err)
	dekMgr, err := dek.NewManager(provider,
		dek.NewStore(pool),
		dek.NewCache(dek.CacheConfig{Capacity: 64}),
		dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64}),
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&dekBindingStubE2E{id: "bind-roundtrip"})
	require.NoError(t, err)

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
	require.NoError(t, err)
	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{
		Owners: owners,
	})
	pluginMgr := audit.NewPluginConsumerManager(bus.JS)
	require.NoError(t, pluginMgr.Add(ctx, audit.PluginConsumerConfig{
		PluginName: "core-scenes",
		Subjects:   []string{"events.*.scene.>"},
		Client:     &pgSceneLogClient{pool: pool},
	}))
	hostSub.SetLateInitProvider(func() (*audit.OwnerMap, *audit.PluginConsumerManager) {
		return owners, pluginMgr
	})
	require.NoError(t, hostSub.Start(ctx))
	t.Cleanup(func() { _ = hostSub.Stop(context.Background()) })

	// Publish one sensitive event on a plugin-owned subject.
	const plaintext = `{"text":"the original plaintext"}`
	eventID := ulid.Make()
	subject := eventbus.Subject("events.main.scene.01ABC.ic")
	require.NoError(t, encryptingPub.Publish(ctx, eventbus.Event{
		ID:        eventID,
		Subject:   subject,
		Type:      eventbus.Type("scene.whisper"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(plaintext),
		Sensitive: true,
	}))

	// Wait for the plugin's per-plugin consumer to land the row.
	waitForRowInSceneLog(t, pool, eventID.Bytes(), 10*time.Second)

	// Read back the persisted row.
	pluginRow := readSceneLogRow(t, pool, eventID.Bytes())

	// INV-P7-12: plugin row payload MUST be ciphertext, not plaintext.
	assert.Equal(t, "xchacha20poly1305-v1", pluginRow.codec)
	assert.NotEqual(t, []byte(plaintext), pluginRow.payload,
		"INV-P7-12: plugin row MUST hold ciphertext for sensitive events")
	assert.NotEmpty(t, pluginRow.payload)

	// INV-P7-3: dek_ref + dek_version present.
	require.NotNil(t, pluginRow.dekRef)
	require.NotNil(t, pluginRow.dekVersion)

	// INV-P7-6: plugin payload byte-equal to the bus envelope payload.
	busPayload := lookupBusEnvelopePayload(t, bus, eventID)
	assert.Equal(t, busPayload, pluginRow.payload,
		"INV-P7-6: plugin storage MUST be byte-equal to bus envelope payload")

	// Cross-check: bus payload itself MUST be ciphertext (sanity).
	assert.NotEqual(t, []byte(plaintext), busPayload,
		"sanity: bus envelope payload MUST be ciphertext for sensitive events")
}

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
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT codec, payload, dek_ref, dek_version
		FROM plugin_core_scenes.scene_log
		WHERE id = $1`, eventID).Scan(&r.codec, &r.payload, &r.dekRef, &r.dekVersion))
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
		require.NoError(t, proto.Unmarshal(msg.Data(), &env))
		// Sanity: assert the headers carry the crypto fields too.
		require.NotEmpty(t, msg.Headers().Get(eventbus.HeaderDekRef))
		require.NotEmpty(t, msg.Headers().Get(eventbus.HeaderDekVersion))
		_, err := strconv.ParseInt(msg.Headers().Get(eventbus.HeaderDekRef), 10, 64)
		require.NoError(t, err)
		return env.GetPayload()
	}
	t.Fatalf("timed out waiting for bus envelope for event %s", idStr)
	return nil
}
