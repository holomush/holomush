// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package history

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/test/testutil"
)

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
			payload, schema_ver, codec, js_seq
		) VALUES ($1, $2, 'test.event', $4, 'system', NULL, '\x00'::bytea, 1, 'identity', $3)
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
	out, err := tier.Read(ctx, q, time.Time{}, 10)
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
	_, err := tier.Read(ctx, q, time.Time{}, 10)
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
	_, err := tier.Read(ctx, q, time.Time{}, 10)
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
	out, err := tier.Read(ctx, q, edge, 10)
	require.NoError(t, err)
	// Cursor echo is validated and discarded; idAfterEdge is filtered by edge.
	assert.Empty(t, out)
}
