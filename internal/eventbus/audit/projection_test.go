// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/testsupport/quarantinetest"
	"github.com/holomush/holomush/test/testutil"
)

// Test subject and header values used in the integration tests. These
// are fixed canonical values — Phase A has no real publishers, so the
// test harness synthesizes messages shaped exactly as the real
// publishers will produce once Phase B lands.
const (
	testSubject    = "events.test.unit"
	testEventType  = "test.unit"
	testCodec      = "identity"
	testSchemaVer  = "1"
	testActorKind  = "system"
	awaitTimeout   = 3 * time.Second
	streamReadyCtx = 5 * time.Second
)

// fixedJS / fixedPool satisfy the Subsystem's provider interfaces by
// returning a value captured at construction time. Integration tests
// already have live JS / pool references before constructing the
// subsystem, so deferred resolution isn't needed here.
type fixedJS struct{ js jetstream.JetStream }

func (f fixedJS) JS() jetstream.JetStream { return f.js }

type fixedPool struct{ pool *pgxpool.Pool }

func (f fixedPool) Pool() *pgxpool.Pool { return f.pool }

// openPool opens a pgxpool against a fresh test database (connection
// string returned by FreshDatabase).
func openPool(t *testing.T, connStr string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), streamReadyCtx)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// publishTestMessage synthesizes a JS message with the headers the
// projection expects. Returns the ULID used as Nats-Msg-Id so the
// caller can assert persisted state.
func publishTestMessage(t *testing.T, js jetstream.JetStream) ulid.ULID {
	t.Helper()
	return publishTestMessageWithID(t, js, ulid.Make())
}

// publishTestMessageOnSubject publishes to a caller-chosen subject with
// a fresh msgID. Returns the ULID used as Nats-Msg-Id so callers can
// assert persisted rows. Used by the F2 plugin-ownership skip test to
// place messages on plugin-owned subjects without touching the default
// testSubject.
func publishTestMessageOnSubject(t *testing.T, js jetstream.JetStream, subject string) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	msg := &nats.Msg{
		Subject: subject,
		Header: nats.Header{
			"Nats-Msg-Id":        []string{id.String()},
			"App-Codec":          []string{testCodec},
			"App-Event-Type":     []string{testEventType},
			"App-Schema-Version": []string{testSchemaVer},
			"App-Actor-Kind":     []string{testActorKind},
			"App-Rendering":      []string{`{}`},
		},
		Data: []byte(`{"hello":"world"}`),
	}
	_, err := js.PublishMsg(t.Context(), msg)
	require.NoError(t, err)
	return id
}

// publishTestMessageWithID publishes a message with a caller-chosen
// msgID. Used by the idempotency test to publish duplicates.
func publishTestMessageWithID(t *testing.T, js jetstream.JetStream, id ulid.ULID) ulid.ULID {
	t.Helper()
	msg := &nats.Msg{
		Subject: testSubject,
		Header: nats.Header{
			"Nats-Msg-Id":        []string{id.String()},
			"App-Codec":          []string{testCodec},
			"App-Event-Type":     []string{testEventType},
			"App-Schema-Version": []string{testSchemaVer},
			"App-Actor-Kind":     []string{testActorKind},
			"App-Rendering":      []string{`{}`},
		},
		Data: []byte(`{"hello":"world"}`),
	}
	_, err := js.PublishMsg(t.Context(), msg)
	require.NoError(t, err)
	return id
}

// countAuditRows counts rows in events_audit (optionally filtered by id).
func countAuditRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	err := pool.QueryRow(t.Context(), `SELECT count(*) FROM events_audit`).Scan(&count)
	require.NoError(t, err)
	return count
}

// TestProjectionDrainsPublishedMessageToAuditTable publishes one event,
// waits for the projection to drain, then asserts the row fields match
// the headers and metadata we published.
func TestProjectionDrainsPublishedMessageToAuditTable(t *testing.T) {
	quarantinetest.Skip(t, "holomush-1nl7")
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := openPool(t, connStr)

	bus := eventbustest.New(t)

	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub.Prepare(t.Context()))
	require.NoError(t, sub.Activate(t.Context()))
	t.Cleanup(func() { _ = sub.Stop(context.Background()) })

	id := publishTestMessage(t, bus.JS)

	sub.AwaitDrained(t, awaitTimeout)

	// Verify exactly one row was written with the expected fields.
	var (
		subject, eventType, actorKind, codec string
		schemaVer                            int16
		jsSeq                                int64
		envelope                             []byte
		timestamp                            pgnanos.Time
		idBytes                              []byte
	)
	err := pool.QueryRow(t.Context(), `
		SELECT id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq
		FROM events_audit
	`).Scan(&idBytes, &subject, &eventType, &timestamp, &actorKind, &envelope, &schemaVer, &codec, &jsSeq)
	require.NoError(t, err)

	expectedID := id.Bytes()
	assert.Equal(t, expectedID, idBytes)
	assert.Equal(t, testSubject, subject)
	assert.Equal(t, testEventType, eventType)
	assert.Equal(t, testActorKind, actorKind)
	assert.Equal(t, testCodec, codec)
	assert.EqualValues(t, 1, schemaVer)
	assert.Equal(t, int64(1), jsSeq, "first published message should have js_seq=1")
	assert.JSONEq(t, `{"hello":"world"}`, string(envelope))
	assert.False(t, timestamp.IsZero())
}

// TestProjectionIsIdempotentOnDuplicate publishes the same Nats-Msg-Id
// twice. JetStream's dedup window absorbs the second (no second
// delivery), and even if the server did deliver it the
// ON CONFLICT DO NOTHING on the INSERT would prevent a duplicate row.
func TestProjectionIsIdempotentOnDuplicate(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := openPool(t, connStr)

	bus := eventbustest.New(t)

	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub.Prepare(t.Context()))
	require.NoError(t, sub.Activate(t.Context()))
	t.Cleanup(func() { _ = sub.Stop(context.Background()) })

	id := ulid.Make()
	publishTestMessageWithID(t, bus.JS, id)
	// Second publish with the same msgID — JS dedup window absorbs it.
	publishTestMessageWithID(t, bus.JS, id)

	sub.AwaitDrained(t, awaitTimeout)

	assert.Equal(t, 1, countAuditRows(t, pool),
		"duplicate msgID must produce exactly one audit row")
}

// TestProjectionResumesAfterRestart publishes an event, drains it,
// stops the projection, then starts a *fresh* Subsystem instance with
// the same consumer name. The new instance should NOT re-insert the
// already-acked message. This verifies:
//   - Durable consumer resumes from the last-acked seq on restart.
//   - Even if the same message were redelivered, ON CONFLICT DO NOTHING
//     is the safety net.
func TestProjectionResumesAfterRestart(t *testing.T) {
	quarantinetest.Skip(t, "holomush-q55b")
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := openPool(t, connStr)

	bus := eventbustest.New(t)

	// First projection instance.
	sub1 := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub1.Prepare(t.Context()))
	require.NoError(t, sub1.Activate(t.Context()))
	publishTestMessage(t, bus.JS)
	sub1.AwaitDrained(t, awaitTimeout)
	require.NoError(t, sub1.Stop(context.Background()))

	require.Equal(t, 1, countAuditRows(t, pool),
		"first projection should have persisted exactly one row")

	// Second projection instance with the same durable name — resumes
	// from the same consumer on the server. No new messages should be
	// delivered (AckFloor == last published seq).
	sub2 := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub2.Prepare(t.Context()))
	require.NoError(t, sub2.Activate(t.Context()))
	t.Cleanup(func() { _ = sub2.Stop(context.Background()) })

	sub2.AwaitDrained(t, awaitTimeout)

	assert.Equal(t, 1, countAuditRows(t, pool),
		"restarted projection must not re-insert already-acked messages")
}

// TestProjectionAckSkipsPluginOwnedSubjects exercises the F2 contract:
// the host audit projection MUST ack-and-skip messages whose subject
// resolves to a plugin owner, and MUST still persist messages on
// host-owned subjects. Ack-and-skip (not just skip) is load-bearing —
// a plugin-owned message left unacked on the host consumer would
// redeliver forever until it hits MaxDeliver. Acking advances the host
// cursor; the per-plugin consumer (F5) has its own independent cursor.
func TestProjectionAckSkipsPluginOwnedSubjects(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := openPool(t, connStr)

	bus := eventbustest.New(t)

	// core-scenes owns `events.*.scene.>`; everything else falls back
	// to the host. testSubject ("events.test.unit") is host-owned.
	owners, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: "core-scenes", Pattern: "events.*.scene.>"},
	})
	require.NoError(t, err)

	sub := audit.NewSubsystem(
		fixedJS{js: bus.JS},
		fixedPool{pool: pool},
		audit.Config{Owners: owners},
	)
	require.NoError(t, sub.Prepare(t.Context()))
	require.NoError(t, sub.Activate(t.Context()))
	t.Cleanup(func() { _ = sub.Stop(context.Background()) })

	// Publish one plugin-owned message (scene) and one host-owned message.
	pluginID := publishTestMessageOnSubject(t, bus.JS, "events.main.scene.01ABC.ic")
	hostID := publishTestMessageOnSubject(t, bus.JS, testSubject)

	sub.AwaitDrained(t, awaitTimeout)

	// The plugin-owned message MUST NOT appear in events_audit.
	var pluginCount int
	err = pool.QueryRow(
		t.Context(),
		`SELECT count(*) FROM events_audit WHERE id = $1`,
		pluginID.Bytes(),
	).Scan(&pluginCount)
	require.NoError(t, err)
	assert.Equal(t, 0, pluginCount,
		"plugin-owned message must NOT be persisted by host projection")

	// The host-owned message MUST appear.
	var hostCount int
	err = pool.QueryRow(
		t.Context(),
		`SELECT count(*) FROM events_audit WHERE id = $1`,
		hostID.Bytes(),
	).Scan(&hostCount)
	require.NoError(t, err)
	assert.Equal(t, 1, hostCount,
		"host-owned message must be persisted by host projection")

	// AwaitDrained waits until NumPending and NumAckPending are both
	// zero, so a successful return already implies the host acked the
	// plugin-owned message — if it hadn't, NumAckPending would stay
	// >0 until AckWait expired.
}

// publishTestMessageWithExtraHeaders publishes a message stamped with the
// canonical Phase A headers plus any caller-supplied extras (e.g. dek
// headers). The message is published on testSubject so the host projection
// persists it. Returns the ULID used as Nats-Msg-Id so callers can SELECT
// the resulting events_audit row.
func publishTestMessageWithExtraHeaders(t *testing.T, js jetstream.JetStream, codecName string, extra map[string]string) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	hdr := nats.Header{
		"Nats-Msg-Id":        []string{id.String()},
		"App-Codec":          []string{codecName},
		"App-Event-Type":     []string{testEventType},
		"App-Schema-Version": []string{testSchemaVer},
		"App-Actor-Kind":     []string{testActorKind},
		"App-Rendering":      []string{`{}`},
	}
	for k, v := range extra {
		hdr.Set(k, v)
	}
	msg := &nats.Msg{
		Subject: testSubject,
		Header:  hdr,
		Data:    []byte(`{"hello":"world"}`),
	}
	_, err := js.PublishMsg(t.Context(), msg)
	require.NoError(t, err)
	return id
}

// TestPersistWritesDekColumnsFromHeaders publishes a non-identity-codec
// message stamped with App-Dek-Ref and App-Dek-Version, then asserts the
// audit projection wrote both header values into events_audit.dek_ref and
// events_audit.dek_version. This is the column round-trip for Phase 3a's
// header-to-column wiring.
func TestPersistWritesDekColumnsFromHeaders(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := openPool(t, connStr)

	bus := eventbustest.New(t)

	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub.Prepare(t.Context()))
	require.NoError(t, sub.Activate(t.Context()))
	t.Cleanup(func() { _ = sub.Stop(context.Background()) })

	id := publishTestMessageWithExtraHeaders(t, bus.JS, "xchacha20poly1305-v1", map[string]string{
		eventbus.HeaderDekRef:     "42",
		eventbus.HeaderDekVersion: "3",
	})

	sub.AwaitDrained(t, awaitTimeout)

	var dekRef sql.NullInt64
	var dekVer sql.NullInt32
	err := pool.QueryRow(
		t.Context(),
		`SELECT dek_ref, dek_version FROM events_audit WHERE id = $1`,
		id.Bytes(),
	).Scan(&dekRef, &dekVer)
	require.NoError(t, err)
	require.True(t, dekRef.Valid, "dek_ref must be populated when App-Dek-Ref header is present")
	assert.Equal(t, int64(42), dekRef.Int64)
	require.True(t, dekVer.Valid, "dek_version must be populated when App-Dek-Version header is present")
	assert.Equal(t, int32(3), dekVer.Int32)
}

// TestPersistWritesNullDekColumnsForIdentityCodec verifies the negative
// case: identity-codec rows (no dek headers) MUST persist with SQL NULL in
// both dek_ref and dek_version. A non-NULL value here would corrupt the
// invariant that "identity row implies cleartext payload, no DEK."
func TestPersistWritesNullDekColumnsForIdentityCodec(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool := openPool(t, connStr)

	bus := eventbustest.New(t)

	sub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, sub.Prepare(t.Context()))
	require.NoError(t, sub.Activate(t.Context()))
	t.Cleanup(func() { _ = sub.Stop(context.Background()) })

	id := publishTestMessageWithExtraHeaders(t, bus.JS, testCodec, nil)

	sub.AwaitDrained(t, awaitTimeout)

	var dekRef sql.NullInt64
	var dekVer sql.NullInt32
	err := pool.QueryRow(
		t.Context(),
		`SELECT dek_ref, dek_version FROM events_audit WHERE id = $1`,
		id.Bytes(),
	).Scan(&dekRef, &dekVer)
	require.NoError(t, err)
	assert.False(t, dekRef.Valid, "identity codec must not populate dek_ref")
	assert.False(t, dekVer.Valid, "identity codec must not populate dek_version")
}
