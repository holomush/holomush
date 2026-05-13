// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package readstream

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

// newTestPool opens a pgxpool against a fresh migrated test database.
func newColdReaderTestPool(t *testing.T) *pgxpool.Pool {
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

// testULIDAt returns a ULID with the given millisecond timestamp component.
func testULIDAt(t *testing.T, ms uint64) ulid.ULID {
	t.Helper()
	u, err := ulid.New(ms, rand.Reader)
	require.NoError(t, err)
	return u
}

// insertEncryptedAuditRow inserts an events_audit row with a non-NULL dek_ref
// (simulating an encrypted/sensitive event).
func insertEncryptedAuditRow(
	t *testing.T,
	pool *pgxpool.Pool,
	id ulid.ULID,
	subject eventbus.Subject,
	seq uint64,
	ts time.Time,
) {
	t.Helper()
	envelopeBytes, err := proto.Marshal(&eventbusv1.Event{
		Id:        id[:],
		Subject:   string(subject),
		Type:      "test.encrypted",
		Timestamp: timestamppb.New(ts),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	})
	require.NoError(t, err)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq,
			dek_ref, dek_version
		) VALUES ($1, $2, 'test.encrypted', $3, 'system', NULL,
		          $4, 1, 'aes256-gcm', $5,
		          42, 1)
	`, id[:], string(subject), ts, envelopeBytes, int64(seq))
	require.NoError(t, err)
}

// insertIdentityAuditRow inserts an events_audit row with NULL dek_ref
// (identity-codec, cleartext event — should be excluded by the filter).
func insertIdentityAuditRow(
	t *testing.T,
	pool *pgxpool.Pool,
	id ulid.ULID,
	subject eventbus.Subject,
	seq uint64,
	ts time.Time,
) {
	t.Helper()
	envelopeBytes, err := proto.Marshal(&eventbusv1.Event{
		Id:        id[:],
		Subject:   string(subject),
		Type:      "test.cleartext",
		Timestamp: timestamppb.New(ts),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	})
	require.NoError(t, err)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq
		) VALUES ($1, $2, 'test.cleartext', $3, 'system', NULL,
		          $4, 1, 'identity', $5)
	`, id[:], string(subject), ts, envelopeBytes, int64(seq))
	require.NoError(t, err)
}

// TestColdReader_MultiSubjectUnion: two subjects with 3 rows each → 6 rows returned.
func TestColdReader_MultiSubjectUnion(t *testing.T) {
	pool := newColdReaderTestPool(t)
	now := time.Now().UTC()

	subjectA := eventbus.Subject("events.game.scene.AAAAAAA.>")
	subjectB := eventbus.Subject("events.game.scene.BBBBBBB.>")

	for i := range 3 {
		insertEncryptedAuditRow(t, pool, testULIDAt(t, uint64(1000+i)), subjectA, uint64(1+i), now.Add(time.Duration(i)*time.Millisecond))
	}
	for i := range 3 {
		insertEncryptedAuditRow(t, pool, testULIDAt(t, uint64(2000+i)), subjectB, uint64(4+i), now.Add(time.Duration(3+i)*time.Millisecond))
	}

	cr := NewColdReader(pool)
	rows, err := cr.Read(context.Background(), ColdQuery{
		Subjects: []eventbus.Subject{subjectA, subjectB},
	})
	require.NoError(t, err)
	assert.Len(t, rows, 6, "both subjects combined should yield 6 rows")
}

// TestColdReader_TimeBoundsApplied: rows outside the Since/Until window are excluded.
func TestColdReader_TimeBoundsApplied(t *testing.T) {
	pool := newColdReaderTestPool(t)

	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	subject := eventbus.Subject("events.game.scene.TIMEBND.>")

	// Before window
	insertEncryptedAuditRow(t, pool, testULIDAt(t, 1001), subject, 1, base.Add(-2*time.Hour))
	// Inside window
	insertEncryptedAuditRow(t, pool, testULIDAt(t, 1002), subject, 2, base.Add(1*time.Hour))
	insertEncryptedAuditRow(t, pool, testULIDAt(t, 1003), subject, 3, base.Add(2*time.Hour))
	// After window
	insertEncryptedAuditRow(t, pool, testULIDAt(t, 1004), subject, 4, base.Add(5*time.Hour))

	cr := NewColdReader(pool)
	rows, err := cr.Read(context.Background(), ColdQuery{
		Subjects: []eventbus.Subject{subject},
		Since:    base,
		Until:    base.Add(3 * time.Hour),
	})
	require.NoError(t, err)
	assert.Len(t, rows, 2, "only rows within [Since, Until] are returned")
}

// TestColdReader_FiltersByDekRefNotNull: identity-codec rows are excluded.
func TestColdReader_FiltersByDekRefNotNull(t *testing.T) {
	pool := newColdReaderTestPool(t)
	now := time.Now().UTC()

	subject := eventbus.Subject("events.game.scene.DEKFILT.>")

	// Encrypted row (dek_ref NOT NULL) — should be returned
	insertEncryptedAuditRow(t, pool, testULIDAt(t, 3001), subject, 1, now)
	// Cleartext row (dek_ref NULL, codec=identity) — must NOT be returned
	insertIdentityAuditRow(t, pool, testULIDAt(t, 3002), subject, 2, now.Add(time.Millisecond))

	cr := NewColdReader(pool)
	rows, err := cr.Read(context.Background(), ColdQuery{
		Subjects: []eventbus.Subject{subject},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the encrypted row is returned")
	assert.Equal(t, "aes256-gcm", string(rows[0].Codec))
}

// TestColdReader_OrderByTimestampAsc: rows are returned in monotonically
// ascending timestamp order regardless of insertion order.
func TestColdReader_OrderByTimestampAsc(t *testing.T) {
	pool := newColdReaderTestPool(t)

	base := time.Now().UTC().Truncate(time.Millisecond)
	subject := eventbus.Subject("events.game.scene.ORDERBY.>")

	// Insert in reverse time order
	insertEncryptedAuditRow(t, pool, testULIDAt(t, 5003), subject, 3, base.Add(3*time.Second))
	insertEncryptedAuditRow(t, pool, testULIDAt(t, 5001), subject, 1, base.Add(1*time.Second))
	insertEncryptedAuditRow(t, pool, testULIDAt(t, 5002), subject, 2, base.Add(2*time.Second))

	cr := NewColdReader(pool)
	rows, err := cr.Read(context.Background(), ColdQuery{
		Subjects: []eventbus.Subject{subject},
	})
	require.NoError(t, err)
	require.Len(t, rows, 3)

	// Verify monotonically ascending timestamps
	for i := 1; i < len(rows); i++ {
		assert.True(t,
			!rows[i].Timestamp.Before(rows[i-1].Timestamp),
			"rows[%d].Timestamp=%v must be >= rows[%d].Timestamp=%v",
			i, rows[i].Timestamp, i-1, rows[i-1].Timestamp,
		)
	}
	// Verify ascending JsSeq as tiebreaker
	assert.Equal(t, uint64(1), rows[0].JsSeq)
	assert.Equal(t, uint64(2), rows[1].JsSeq)
	assert.Equal(t, uint64(3), rows[2].JsSeq)
}

// TestColdReader_EmptyResultEofTermination: a query that matches no rows
// returns an empty slice with nil error — not an EOF or other error.
func TestColdReader_EmptyResultEofTermination(t *testing.T) {
	pool := newColdReaderTestPool(t)

	cr := NewColdReader(pool)
	rows, err := cr.Read(context.Background(), ColdQuery{
		Subjects: []eventbus.Subject{"events.game.scene.EMPTY.>"},
		Since:    time.Now().Add(-time.Hour),
		Until:    time.Now(),
	})
	require.NoError(t, err)
	assert.Nil(t, rows, "empty result must be nil slice, not an error")
}
