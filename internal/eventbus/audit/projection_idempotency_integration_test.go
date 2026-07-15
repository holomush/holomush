// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package audit

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/test/testutil"
)

// auditIdemPool returns a pgxpool on a fresh, fully-migrated database (schema
// at HEAD — includes the partitioned events_audit from 000052).
func auditIdemPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	pool, err := pgxpool.New(context.Background(), testutil.FreshDatabase(t, shared))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func countAllAuditRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events_audit`).Scan(&n))
	return n
}

func idemStubMsg(t *testing.T, h nats.Header, subject string, storeTime time.Time, streamSeq uint64) *stubMsg {
	t.Helper()
	return &stubMsg{
		headers: h,
		subject: subject,
		data:    []byte{0x00},
		meta: &jetstream.MsgMetadata{
			Timestamp: storeTime,
			Sequence:  jetstream.SequencePair{Stream: streamSeq},
		},
	}
}

// TestWriteAuditRowDedupsSameEventAcrossStoreTimes proves the composite-PK
// dedup: the SAME event written twice with DIFFERENT JetStream store-times
// (live path then DLQ-replay path) yields exactly ONE row, because event_ms is
// derived from the immutable event ULID (identical on both calls). The
// persisted timestamp column equals the FIRST write's store-time — proof that
// timestamp keeps its store-time meaning and the second write is dropped.
func TestWriteAuditRowDedupsSameEventAcrossStoreTimes(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()

	id := ulid.Make()
	h := validHeaders(t)
	h.Set(headerMsgID, id.String())

	t1 := time.Now().Add(-time.Hour).UTC()
	t2 := time.Now().UTC()
	msg1 := idemStubMsg(t, h, "events.main.dedup", t1, 10)
	msg2 := idemStubMsg(t, h, "events.main.dedup", t2, 20)

	require.NoError(t, writeAuditRow(ctx, pool, "events.main.dedup", msg1))
	require.NoError(t, writeAuditRow(ctx, pool, "events.main.dedup", msg2))

	require.Equal(t, 1, countAllAuditRows(t, pool), "same event must dedup to exactly one row")

	var ts, gotEventMs int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT timestamp, event_ms FROM events_audit WHERE id=$1`, id[:]).Scan(&ts, &gotEventMs))
	require.Equal(t, t1.UnixNano(), ts, "persisted timestamp must be the FIRST write's store-time")
	require.Equal(t, eventMsFromULID(id), gotEventMs, "event_ms must be the ULID-derived value")
}

// TestWriteAuditRowDistinctEventsProduceTwoRows proves no false dedup: two
// distinct ULIDs produce two rows.
func TestWriteAuditRowDistinctEventsProduceTwoRows(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()

	h1 := validHeaders(t)
	id1 := ulid.Make()
	h1.Set(headerMsgID, id1.String())

	h2 := validHeaders(t)
	id2 := ulid.Make()
	h2.Set(headerMsgID, id2.String())

	now := time.Now().UTC()
	require.NoError(t, writeAuditRow(ctx, pool, "events.main.a", idemStubMsg(t, h1, "events.main.a", now, 1)))
	require.NoError(t, writeAuditRow(ctx, pool, "events.main.b", idemStubMsg(t, h2, "events.main.b", now, 2)))

	require.Equal(t, 2, countAllAuditRows(t, pool), "two distinct events must produce two rows")
}

// TestWriteAuditRowConflictTargetMatchesCompositePK proves the ON CONFLICT
// target matches the composite PK — a repeat write raises no "no unique or
// exclusion constraint matching the ON CONFLICT specification" error.
func TestWriteAuditRowConflictTargetMatchesCompositePK(t *testing.T) {
	pool := auditIdemPool(t)
	ctx := context.Background()

	id := ulid.Make()
	h := validHeaders(t)
	h.Set(headerMsgID, id.String())
	now := time.Now().UTC()

	require.NoError(t, writeAuditRow(ctx, pool, "events.main.pk", idemStubMsg(t, h, "events.main.pk", now, 1)))
	// A second identical write must succeed (ON CONFLICT (id, event_ms) DO NOTHING),
	// not error on a mismatched conflict target.
	require.NoError(t, writeAuditRow(ctx, pool, "events.main.pk", idemStubMsg(t, h, "events.main.pk", now, 1)))
}
