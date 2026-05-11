// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package chain_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/test/testutil"
)

// newIntegrationPool opens a pgxpool against a fresh migrated test database
// and registers cleanup. Each test gets its own isolated database so rows
// from one test cannot interfere with another.
func newIntegrationPool(t *testing.T) *pgxpool.Pool {
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

// insertAuditEntry inserts a minimal events_audit row with the given js_seq,
// subject, and JSON payload. The envelope column carries the raw payload bytes
// (identity codec: no proto wrapping for these synthetic test rows).
func insertAuditEntry(t *testing.T, pool *pgxpool.Pool, jsSeq int64, subject, payloadJSON string) {
	t.Helper()
	// Use a deterministic fake event ID derived from jsSeq.
	fakeID := make([]byte, 16)
	fakeID[0] = byte(jsSeq >> 8)
	fakeID[1] = byte(jsSeq)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO events_audit
			(id, subject, type, timestamp, actor_kind, actor_id,
			 envelope, schema_ver, codec, js_seq, rendering)
		VALUES
			($1, $2, 'test.chain.event', now(), 'system', NULL,
			 $3, 1, 'identity', $4, '{}'::jsonb)
	`, fakeID, subject, []byte(payloadJSON), jsSeq)
	require.NoError(t, err)
}

// TestRepo_ChainInitialized_RoundTrip verifies the full read-mark-reread
// lifecycle of the chain-initialized signal in bootstrap_metadata.
//
// Invariants exercised:
//   - Fresh (chain_name, scope_key) pair returns false (no row present).
//   - MarkChainInitialized inserts the row; subsequent query returns true.
//   - MarkChainInitialized is idempotent (ON CONFLICT DO NOTHING).
func TestRepo_ChainInitialized_RoundTrip(t *testing.T) {
	pool := newIntegrationPool(t)
	repo := chain.NewPostgresRepo(pool)
	ctx := context.Background()

	// No row yet — must return false.
	initialized, err := repo.ChainInitialized(ctx, "test.chain", "scope1")
	require.NoError(t, err)
	require.False(t, initialized, "fresh (chain_name, scope_key) must not be initialized")

	// Mark initialized.
	require.NoError(t, repo.MarkChainInitialized(ctx, "test.chain", "scope1"))

	// Now it must be true.
	initialized, err = repo.ChainInitialized(ctx, "test.chain", "scope1")
	require.NoError(t, err)
	require.True(t, initialized, "after MarkChainInitialized the signal must be present")

	// Idempotent re-mark must not return an error.
	require.NoError(t, repo.MarkChainInitialized(ctx, "test.chain", "scope1"),
		"MarkChainInitialized must be idempotent (ON CONFLICT DO NOTHING)")

	// Distinct (chain_name, scope_key) pairs are independent.
	initialized2, err := repo.ChainInitialized(ctx, "test.chain", "scope2")
	require.NoError(t, err)
	require.False(t, initialized2, "a different scope must not inherit the initialized flag")
}

// TestRepo_LoadEntriesByScope_OrdersByJSSeq verifies that entries belonging
// to the given subject are returned in ascending js_seq order and that rows
// from a different scope (different subject) are excluded.
func TestRepo_LoadEntriesByScope_OrdersByJSSeq(t *testing.T) {
	pool := newIntegrationPool(t)
	repo := chain.NewPostgresRepo(pool)
	ctx := context.Background()

	subjectPrefix := "events.g1.system.example"

	// Insert three rows for scopeA in reverse seq order.
	insertAuditEntry(t, pool, 100, subjectPrefix+".scopeA", `{"scope":"scopeA","label":"third"}`)
	insertAuditEntry(t, pool, 50, subjectPrefix+".scopeA", `{"scope":"scopeA","label":"first"}`)
	insertAuditEntry(t, pool, 75, subjectPrefix+".scopeA", `{"scope":"scopeA","label":"second"}`)

	// Row for a different scope — must not appear in scopeA results.
	insertAuditEntry(t, pool, 60, subjectPrefix+".scopeB", `{"scope":"scopeB"}`)

	entries, err := repo.LoadEntriesByScope(ctx, subjectPrefix, "scopeA")
	require.NoError(t, err)
	require.Len(t, entries, 3, "exactly three rows for scopeA must be returned")

	// Must be ordered by js_seq ASC.
	require.Equal(t, int64(50), entries[0].JSSeq, "first entry must have the lowest js_seq")
	require.Equal(t, int64(75), entries[1].JSSeq, "second entry must be next by js_seq")
	require.Equal(t, int64(100), entries[2].JSSeq, "third entry must have the highest js_seq")

	// Subject field on each entry must match.
	for _, e := range entries {
		require.Equal(t, subjectPrefix+".scopeA", e.Subject)
	}

	// Payload field must be non-nil.
	require.NotEmpty(t, entries[0].Payload)
}

// TestRepo_DiscoverScopes_DistinctFromSubject verifies that DiscoverScopes
// returns one scope per distinct subject suffix, deduplicating multiple rows
// under the same subject.
func TestRepo_DiscoverScopes_DistinctFromSubject(t *testing.T) {
	pool := newIntegrationPool(t)
	repo := chain.NewPostgresRepo(pool)
	ctx := context.Background()

	subjectPrefix := "events.g2.system.example"

	// Two rows for scope "a", one for scope "b".
	insertAuditEntry(t, pool, 10, subjectPrefix+".a", `{"scope":"a"}`)
	insertAuditEntry(t, pool, 11, subjectPrefix+".a", `{"scope":"a"}`)
	insertAuditEntry(t, pool, 12, subjectPrefix+".b", `{"scope":"b"}`)

	// Row with a different prefix — must not appear.
	insertAuditEntry(t, pool, 13, "events.g2.system.other.c", `{"scope":"c"}`)

	scopes, err := repo.DiscoverScopes(ctx, subjectPrefix)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a", "b"}, scopes,
		"DiscoverScopes must return distinct scope suffixes for the given subjectPrefix")
}
