//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/pkg/errutil"
)

// testBindingStub implements dek.BindingResolver for tests.
type testBindingStub struct {
	bindingID string
	err       error
}

func (s *testBindingStub) Current(_ context.Context, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.bindingID, nil
}

// TestSoftDeletedDEKAppearsAsNoRowsForProductionReads asserts that
// rows with destroyed_at IS NOT NULL are invisible to the production
// read predicates: selectByID (exercised via Manager.Resolve, which
// translates pgx.ErrNoRows into DEK_NOT_FOUND) and selectActive
// (exercised via a direct SQL check of the same predicate the method
// uses). Phase 3c Decision 4 / INV-39.
//
// We don't assert via Manager.GetOrCreate because the unique
// (context_type, context_id, version) constraint on crypto_keys
// remains in force across soft-delete; minting a fresh v1 in the same
// context after a soft-delete is a Rekey-tenure concern out of scope
// for T8. The selectActive predicate behavior is the property under
// test, and we verify it directly.
func TestSoftDeletedDEKAppearsAsNoRowsForProductionReads(t *testing.T) {
	ctx := context.Background()
	connStr, teardown := newTestPGPool(t)
	defer teardown()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	provider := newTestProvider(t)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&testBindingStub{bindingID: "bind-test"})
	require.NoError(t, err)

	// Mint a DEK via the public API.
	ctxID := dek.ContextID{Type: "scene", ID: "01ABCDEF-SOFTDEL"}
	key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	require.NoError(t, err)

	// Soft-delete the row directly via SQL. Drop both caches so the
	// next read goes through the SQL filter rather than returning
	// the cached material.
	_, err = pool.Exec(
		ctx,
		`UPDATE crypto_keys SET destroyed_at = NOW() WHERE id = $1`,
		int64(key.ID), //nolint:gosec // G115: codec.KeyID values are positive BIGSERIAL ids.
	)
	require.NoError(t, err)
	cache.Invalidate(dek.CacheKey{KeyID: key.ID, Version: 1})
	partCache.Invalidate(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1})

	// Resolve must surface DEK_NOT_FOUND because selectByID filters the
	// soft-deleted row (selectByID returns pgx.ErrNoRows; Manager.Resolve
	// translates it to DEK_NOT_FOUND).
	_, err = mgr.Resolve(ctx, key.ID, 1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_NOT_FOUND")

	// selectActive's predicate (rotated_at IS NULL AND destroyed_at IS
	// NULL) must return zero rows for the soft-deleted context. We
	// query directly because selectActive is unexported; this asserts
	// the same predicate the method uses.
	var n int
	err = pool.QueryRow(ctx, `
        SELECT count(*) FROM crypto_keys
         WHERE context_type=$1 AND context_id=$2
           AND rotated_at IS NULL AND destroyed_at IS NULL
    `, ctxID.Type, ctxID.ID).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 0, n,
		"soft-deleted row must not match selectActive's predicate")
}

// TestSelectAnyByIDReturnsDestroyedRows asserts the forensic read path
// returns soft-deleted rows with DestroyedAt populated and the original
// ContextID intact, so Phase 5 Rekey audit emission can read previous-
// tenure rows. Phase 3c Decision 4.
func TestSelectAnyByIDReturnsDestroyedRows(t *testing.T) {
	ctx := context.Background()
	connStr, teardown := newTestPGPool(t)
	defer teardown()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	provider := newTestProvider(t)
	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgr, err := dek.NewManager(provider, store, cache, partCache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&testBindingStub{bindingID: "bind-test"})
	require.NoError(t, err)

	// Insert via Manager so the row matches production shape.
	ctxID := dek.ContextID{Type: "dm", ID: "01ABCDEF-FORENSIC"}
	key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	require.NoError(t, err)

	// Soft-delete the row.
	_, err = pool.Exec(
		ctx,
		`UPDATE crypto_keys SET destroyed_at = NOW() WHERE id = $1`,
		int64(key.ID), //nolint:gosec // G115: codec.KeyID values are positive BIGSERIAL ids.
	)
	require.NoError(t, err)

	// SelectAnyByID returns the destroyed row with DestroyedAt populated
	// and original ContextID intact.
	got, err := store.SelectAnyByID(ctx, key.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, ctxID.Type, got.ContextType)
	assert.Equal(t, ctxID.ID, got.ContextID)
	assert.Equal(t, uint32(1), got.Version)
	require.NotNil(t, got.DestroyedAt, "DestroyedAt must be populated for soft-deleted rows")
	assert.False(t, got.DestroyedAt.IsZero())

	// Sanity: a missing (keyID, version) still surfaces an error
	// (pgx.ErrNoRows wrapped through oops).
	_, err = store.SelectAnyByID(ctx, codec.KeyID(0), 999)
	require.Error(t, err)
}
