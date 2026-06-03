// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/pgnanos"
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
// uses). Phase 3c Decision 4 / INV-CRYPTO-22.
//
// We don't assert via Manager.GetOrCreate because the unique
// (context_type, context_id, version) constraint on crypto_keys
// remains in force across soft-delete; minting a fresh v1 in the same
// context after a soft-delete is a Rekey-tenure concern out of scope
// for T8. The selectActive predicate behavior is the property under
// test, and we verify it directly.
var _ = Describe("Store soft-delete behaviour (Phase 3c Decision 4 / INV-CRYPTO-22)", func() {
	It("soft-deleted DEK appears as no rows for production reads", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache,
			func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
			&testBindingStub{bindingID: "bind-test"})
		Expect(err).NotTo(HaveOccurred())

		// Mint a DEK via the public API.
		ctxID := dek.ContextID{Type: "scene", ID: "01ABCDEF-SOFTDEL"}
		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())

		// Soft-delete the row directly via SQL. Drop both caches so the
		// next read goes through the SQL filter rather than returning
		// the cached material.
		_, err = pool.Exec(
			ctx,
			`UPDATE crypto_keys SET destroyed_at = $2 WHERE id = $1`,
			int64(key.ID), pgnanos.From(time.Now()), //nolint:gosec // G115: codec.KeyID values are positive BIGSERIAL ids.
		)
		Expect(err).NotTo(HaveOccurred())
		cache.Invalidate(dek.CacheKey{KeyID: key.ID, Version: 1})
		partCache.Invalidate(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1})

		// Resolve must surface DEK_NOT_FOUND because selectByID filters the
		// soft-deleted row (selectByID returns pgx.ErrNoRows; Manager.Resolve
		// translates it to DEK_NOT_FOUND).
		_, err = mgr.Resolve(ctx, key.ID, 1)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_NOT_FOUND")

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
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(0), "soft-deleted row must not match selectActive's predicate")
	})

	// TestSelectAnyByIDReturnsDestroyedRows asserts the forensic read path
	// returns soft-deleted rows with DestroyedAt populated and the original
	// ContextID intact, so Phase 5 Rekey audit emission can read previous-
	// tenure rows. Phase 3c Decision 4.
	It("SelectAnyByID returns destroyed rows for forensic reads", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		store := dek.NewStore(pool)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, store, cache, partCache,
			func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
			&testBindingStub{bindingID: "bind-test"})
		Expect(err).NotTo(HaveOccurred())

		// Insert via Manager so the row matches production shape.
		ctxID := dek.ContextID{Type: "dm", ID: "01ABCDEF-FORENSIC"}
		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())

		// Soft-delete the row.
		_, err = pool.Exec(
			ctx,
			`UPDATE crypto_keys SET destroyed_at = $2 WHERE id = $1`,
			int64(key.ID), pgnanos.From(time.Now()), //nolint:gosec // G115: codec.KeyID values are positive BIGSERIAL ids.
		)
		Expect(err).NotTo(HaveOccurred())

		// SelectAnyByID returns the destroyed row with DestroyedAt populated
		// and original ContextID intact.
		got, err := store.SelectAnyByID(ctx, key.ID, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.ContextType).To(Equal(ctxID.Type))
		Expect(got.ContextID).To(Equal(ctxID.ID))
		Expect(got.Version).To(Equal(uint32(1)))
		Expect(got.DestroyedAt).NotTo(BeNil(), "DestroyedAt must be populated for soft-deleted rows")
		Expect(got.DestroyedAt.IsZero()).To(BeFalse())

		// Sanity: a missing (keyID, version) still surfaces an error
		// (pgx.ErrNoRows wrapped through oops).
		_, err = store.SelectAnyByID(ctx, codec.KeyID(0), 999)
		Expect(err).To(HaveOccurred())
	})
})
