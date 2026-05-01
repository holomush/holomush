// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package history

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/test/testutil"
)

// newErrorSnapshotForTest builds a snapshot whose Get() returns the given
// error. Used to verify that infra failures propagate rather than being
// silently classified as ErrCursorStale or ErrCursorLag. Integration test only.
func newErrorSnapshotForTest(err error) *StreamStateSnapshot {
	s := &StreamStateSnapshot{err: err}
	s.once.Do(func() {}) // mark as populated (err is the result)
	return s
}

// newTestPool opens a pgxpool against a fresh migrated test database and
// registers cleanup. Each test gets its own isolated database so rows from
// one test cannot interfere with another.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// testULID returns a unique ULID for use in tests. The ms parameter is used
// as the timestamp component to create ordered ULIDs; entropy comes from
// crypto/rand.
func testULID(t *testing.T, ms uint64) ulid.ULID {
	t.Helper()
	u, err := ulid.New(ms, rand.Reader)
	require.NoError(t, err)
	return u
}

// insertAuditRow inserts a minimal events_audit row with the given id, subject,
// seq, and current timestamp.
func insertAuditRow(t *testing.T, pool *pgxpool.Pool, id ulid.ULID, subject eventbus.Subject, seq uint64) {
	t.Helper()
	insertAuditRowAt(t, pool, id, subject, seq, time.Now().UTC())
}

// insertAuditRowAt inserts a minimal events_audit row with an explicit timestamp.
func insertAuditRowAt(t *testing.T, pool *pgxpool.Pool, id ulid.ULID, subject eventbus.Subject, seq uint64, ts time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec, js_seq, rendering
		) VALUES ($1, $2, 'test.event', $4, 'system', NULL, '\x00'::bytea, 1, 'identity', $3, '{}'::jsonb)
	`, id[:], string(subject), int64(seq), ts)
	require.NoError(t, err)
}

// TestColdReadValidatesAndDiscardsCursorEcho: forward read with a valid
// cursor returns rows after the cursor, having validated and discarded
// the cursor row from the result.
func TestColdReadValidatesAndDiscardsCursorEcho(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOCAAA0000000000A")
	id1 := testULID(t, 1000001)
	id2 := testULID(t, 1000002)
	id3 := testULID(t, 1000003)

	insertAuditRow(t, pool, id1, subject, 1)
	insertAuditRow(t, pool, id2, subject, 2)
	insertAuditRow(t, pool, id3, subject, 3)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  1,
		AfterID:   id1,
		Direction: eventbus.DirectionForward,
	}
	out, err := tier.Read(ctx, q, time.Time{}, 10, nil)
	require.NoError(t, err)
	require.Len(t, out, 2, "should return events after cursor; cursor row is discarded")
	assert.Equal(t, uint64(2), out[0].Seq)
	assert.Equal(t, id2, out[0].ID)
	assert.Equal(t, uint64(3), out[1].Seq)
	assert.Equal(t, id3, out[1].ID)
}

// TestColdReadReturnsStaleOnIDMismatch: cursor seq matches a row but the id
// does not — returns ErrCursorStale.
func TestColdReadReturnsStaleOnIDMismatch(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOCBBB0000000000B")
	idActual := testULID(t, 2000001)
	idCursor := testULID(t, 2000099) // different id for the same seq

	insertAuditRow(t, pool, idActual, subject, 1)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  1,
		AfterID:   idCursor, // mismatched id for seq 1
		Direction: eventbus.DirectionForward,
	}
	_, err := tier.Read(ctx, q, time.Time{}, 10, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrCursorStale)
}

// TestColdReadReturnsStaleOnMissingCursorSeq: cursor seq does not exist in
// events_audit — returns ErrCursorStale.
func TestColdReadReturnsStaleOnMissingCursorSeq(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOCCCC0000000000C")
	insertAuditRow(t, pool, testULID(t, 3000001), subject, 1)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  99, // seq 99 does not exist
		AfterID:   testULID(t, 3000099),
		Direction: eventbus.DirectionForward,
	}
	_, err := tier.Read(ctx, q, time.Time{}, 10, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrCursorStale)
}

// TestColdReadCursorAtEdgePassesEdgeFilter: when an edge time is supplied and
// the cursor row is AT or before the edge, the cursor echo is validated and
// discarded; rows after the edge are excluded. The page is empty because
// the only available post-cursor row falls after the edge timestamp.
func TestColdReadCursorAtEdgePassesEdgeFilter(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOCDDD0000000000D")
	idAtEdge := testULID(t, 4000005)
	idAfterEdge := testULID(t, 4000006)

	now := time.Now().UTC()
	insertAuditRowAt(t, pool, idAtEdge, subject, 5, now.Add(-1*time.Hour))
	insertAuditRowAt(t, pool, idAfterEdge, subject, 6, now)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  5,
		AfterID:   idAtEdge,
		Direction: eventbus.DirectionForward,
	}
	edge := now.Add(-30 * time.Minute)
	out, err := tier.Read(ctx, q, edge, 10, nil)
	require.NoError(t, err)
	// Cursor echo is validated and discarded; idAfterEdge is filtered by edge.
	assert.Empty(t, out)
}

// TestColdReadReturnsLagWhenCursorSeqMissingFromColdButPresentInJS uses a
// stubbed snapshot to simulate cursor.Seq=50 being in JS (LastSeq=100) but
// not yet projected into cold. Expect ErrCursorLag.
func TestColdReadReturnsLagWhenCursorSeqMissingFromColdButPresentInJS(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOCEEE0000000000E")
	// Insert seq=1 only; cursor will point to seq=50 which is absent.
	insertAuditRow(t, pool, testULID(t, 5000001), subject, 1)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  50,
		AfterID:   testULID(t, 5000050),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	// Snapshot: JS contains seqs 1..100; cursor.Seq=50 is within [1,100] → LAG.
	snap := newSnapshotForTest(1, 100)
	_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, snap)
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrCursorLag,
		"cursor seq within JS range must be LAG, not STALE")
}

// TestColdReadReturnsStaleWhenCursorSeqBeyondJS uses a stubbed snapshot where
// cursor.Seq exceeds the snapshot's LastSeq — the seq doesn't exist anywhere,
// so it's truly STALE.
func TestColdReadReturnsStaleWhenCursorSeqBeyondJS(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOCFFF0000000000F")
	// Insert seq=1 only; cursor will point to seq=200 which exceeds JS.
	insertAuditRow(t, pool, testULID(t, 6000001), subject, 1)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  200,
		AfterID:   testULID(t, 6000200),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	// Snapshot: JS only has seqs 1..100; cursor.Seq=200 > lastSeq=100 → STALE.
	snap := newSnapshotForTest(1, 100)
	_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, snap)
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrCursorStale,
		"cursor seq beyond JS range must be STALE")
}

// TestColdReadReturnsStaleWhenCursorBelowJSFirstSeq verifies that a cursor
// whose seq is below the snapshot's firstSeq (i.e., retention has aged it
// out of JS but it was never projected into cold) returns ErrCursorStale.
func TestColdReadReturnsStaleWhenCursorBelowJSFirstSeq(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	// Snapshot: JS has seq=10..100; cursor points to seq=5 which is below firstSeq.
	subject := eventbus.Subject("events.main.location.01HXTESTLOCHHH0000000000H")
	// No cold row for the cursor seq (absent from cold).
	insertAuditRow(t, pool, testULID(t, 8000001), subject, 99)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  5,
		AfterID:   testULID(t, 8000005),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	// Snapshot: firstSeq=10 > cursorSeq=5 → not in LAG range → STALE.
	snap := newSnapshotForTest(10, 100)
	_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, snap)
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrCursorStale,
		"cursor seq below JS firstSeq must be STALE (retention aged it out)")
}

// TestColdReadPropagatesSnapshotError verifies that an error returned by
// the snapshot's Get() propagates to the caller unchanged and is NOT
// classified as ErrCursorStale or ErrCursorLag.
func TestColdReadPropagatesSnapshotError(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	// Insert one row so the query reaches the cursor-validation branch.
	subject := eventbus.Subject("events.main.location.01HXTESTLOCIII0000000000I")
	insertAuditRow(t, pool, testULID(t, 9000001), subject, 1)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  50,
		AfterID:   testULID(t, 9000050),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	snapErr := oops.Code("EVENTBUS_HISTORY_STREAM_LOOKUP_FAILED").Errorf("js unreachable")
	snap := newErrorSnapshotForTest(snapErr)
	_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, snap)
	require.Error(t, err)
	assert.False(t, errors.Is(err, eventbus.ErrCursorStale),
		"snapshot error must not be classified as ErrCursorStale")
	assert.False(t, errors.Is(err, eventbus.ErrCursorLag),
		"snapshot error must not be classified as ErrCursorLag")
	// The infra error propagates as-is.
	assert.ErrorIs(t, err, snapErr)
}

// TestColdReadReturnsStaleWhenNoSnapshotAndCursorMissing verifies that without
// a snapshot (nil), a missing cursor seq always returns ErrCursorStale
// (can't distinguish LAG from STALE).
func TestColdReadReturnsStaleWhenNoSnapshotAndCursorMissing(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	subject := eventbus.Subject("events.main.location.01HXTESTLOCGGG0000000000G")
	insertAuditRow(t, pool, testULID(t, 7000001), subject, 1)

	tier := newPostgresColdTier(pool)
	q := eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  50,
		AfterID:   testULID(t, 7000050),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	// No snapshot — falls back to STALE.
	_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrCursorStale,
		"without snapshot, missing cursor must be STALE")
}
