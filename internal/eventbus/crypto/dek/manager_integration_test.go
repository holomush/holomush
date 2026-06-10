//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// noopInvalidator is a no-op Invalidator for tests that exercise
// GetOrCreate / Resolve / Participants but never Add / Rotate.
var noopInvalidator = func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil }

// newTestPGPool returns a connection string for a fresh, fully-migrated
// database, backed by a process-wide shared postgres container.
//
// Each call yields a new database created from a pre-migrated template
// via CREATE DATABASE … WITH TEMPLATE (fast — typically <100ms per call),
// so the 13 specs that need their own schema-isolated DB no longer pay
// the 5–10s container-startup cost each. The shared container is
// initialized once per test binary via testutil.SharedPostgres; database
// cleanup is registered by testutil.FreshDatabase via t.Cleanup. The
// returned no-op closure preserves the previous (connStr, teardown)
// caller contract.
func newTestPGPool(t *testing.T) (string, func()) {
	t.Helper()
	env := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, env)
	return connStr, func() {}
}

func newTestProvider(t *testing.T) kek.Provider {
	t.Helper()
	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	if err != nil {
		t.Fatalf("failed to read random bytes: %v", err)
	}
	name := "TEST_KEK_" + sanitizeEnvName(t.Name())
	t.Setenv(name, hex.EncodeToString(kekBytes))
	src := kek.NewEnvSource(name, false)
	p, err := kek.NewLocalAEADProviderForUnitTest(context.Background(), src)
	if err != nil {
		t.Fatalf("failed to create test provider: %v", err)
	}
	return p
}

// preInsertBarrier coordinates N goroutines so they all reach the
// pre-insert hook before any is allowed to proceed. Used by the
// concurrent-mint race test to guarantee the unique-violation recovery
// path is exercised — without the barrier, the scheduler could
// serialize the goroutines and the second call would observe the
// already-inserted row in selectActive instead of racing on INSERT.
type preInsertBarrier struct {
	t       *testing.T
	wg      sync.WaitGroup
	mu      sync.Mutex
	arrived int
}

func newPreInsertBarrier(t *testing.T, n int) *preInsertBarrier {
	t.Helper()
	b := &preInsertBarrier{t: t}
	b.wg.Add(n)
	return b
}

// Wait records that one goroutine has reached the hook, then blocks
// until N goroutines have all arrived.
func (b *preInsertBarrier) Wait() {
	b.mu.Lock()
	b.arrived++
	b.mu.Unlock()
	b.wg.Done()
	b.wg.Wait()
}

// ArrivalCount returns how many goroutines reached Wait. Tests assert
// this equals the expected fan-out so the test cannot pass when the
// scheduler serialized the goroutines and only one called INSERT.
func (b *preInsertBarrier) ArrivalCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.arrived
}

// sanitizeEnvName strips characters that env var names disallow.
func sanitizeEnvName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// testIntegrationPool creates a testcontainer-backed *pgxpool.Pool with
// migrations applied. Used by Add, Rotate, and INV integration tests.
func testIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	connStr, cleanup := newTestPGPool(t)
	t.Cleanup(cleanup)
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// stubInvalidator records invalidation calls for assertions.
type stubInvalidator struct {
	calls []invalidationCall
}

type invalidationCall struct {
	ctxID            dek.ContextID
	action           string
	version          uint32
	successorVersion uint32
}

func (s *stubInvalidator) call() dek.Invalidator {
	return func(_ context.Context, ctxID dek.ContextID, action string, v, sv uint32) error {
		s.calls = append(s.calls, invalidationCall{
			ctxID: ctxID, action: action, version: v, successorVersion: sv,
		})
		return nil
	}
}

var _ = Describe("Manager", func() {
	It("GetOrCreate mints and persists a DEK", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "01ABCDEF"}
		key1, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())
		Expect(key1.ID).NotTo(BeZero())
		Expect(key1.Bytes).To(HaveLen(32))

		// A second call returns the same key (idempotent for the same context).
		key2, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())
		Expect(key2.ID).To(Equal(key1.ID))
		Expect(key2.Bytes).To(Equal(key1.Bytes))

		// The crypto_keys table has exactly one row for this context.
		var rowCount int
		err = pool.QueryRow(ctx,
			"SELECT count(*) FROM crypto_keys WHERE context_type=$1 AND context_id=$2",
			"scene", "01ABCDEF").Scan(&rowCount)
		Expect(err).NotTo(HaveOccurred())
		Expect(rowCount).To(Equal(1))
	})

	It("Resolve returns key by ID and version", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "dm", ID: "01ABCDEF-01FFFFFF"}
		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())

		// Drop the cache so Resolve has to go through DB.
		cache.Invalidate(dek.CacheKey{KeyID: key.ID, Version: 1})

		resolved, err := mgr.Resolve(ctx, key.ID, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved.ID).To(Equal(key.ID))
		Expect(resolved.Bytes).To(Equal(key.Bytes))
	})

	It("Resolve not found returns DEK_NOT_FOUND", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		mgr, err := dek.NewManager(newTestProvider(suiteT), dek.NewStore(pool),
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}), noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		_, err = mgr.Resolve(ctx, codec.KeyID(99999), 1)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_NOT_FOUND")
	})

	It("Participants round-trip persists and retrieves participant list", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		// INV-STORE-1: deterministic ns-precision fixture so the JoinedAt round-trip
		// assertion below is load-bearing — sub-microsecond bits are preserved
		// through pgnanos.Time scan/insert paths.
		joinedAt1 := time.Date(2026, 5, 22, 12, 0, 0, 123456789, time.UTC)
		joinedAt2 := time.Date(2026, 5, 22, 12, 0, 0, 987654321, time.UTC)
		initial := []dek.Participant{
			{PlayerID: "01ABC", CharacterID: "01XYZ", BindingID: "01DEF", JoinedAt: joinedAt1},
			{PlayerID: "01GHI", CharacterID: "01JKL", BindingID: "01MNO", JoinedAt: joinedAt2},
		}
		key, err := mgr.GetOrCreate(ctx, dek.ContextID{Type: "scene", ID: "01HXX"}, initial)
		Expect(err).NotTo(HaveOccurred())

		parts, err := mgr.Participants(ctx, key.ID, key.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(2))
		Expect(parts[0].PlayerID).To(Equal("01ABC"))
		Expect(parts[0].CharacterID).To(Equal("01XYZ"))
		Expect(parts[0].BindingID).To(Equal("01DEF"))
		Expect(parts[0].JoinedAt.UnixNano()).To(Equal(joinedAt1.UnixNano()),
			"INV-STORE-1: JoinedAt ns precision MUST survive round-trip")
		Expect(parts[1].PlayerID).To(Equal("01GHI"))
		Expect(parts[1].CharacterID).To(Equal("01JKL"))
		Expect(parts[1].BindingID).To(Equal("01MNO"))
		Expect(parts[1].JoinedAt.UnixNano()).To(Equal(joinedAt2.UnixNano()),
			"INV-STORE-1: JoinedAt ns precision MUST survive round-trip")
	})

	It("resolves an empty BindingID on genesis via the BindingResolver", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		const wantBinding = "01RESOLVEDBINDING0000000"
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache,
			noopInvalidator, &stubBindingResolver{bindingID: wantBinding})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "character", ID: "01HRECIPIENTCHAR000000000"}
		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{{CharacterID: "01HRECIPIENTCHAR000000000"}})
		Expect(err).NotTo(HaveOccurred())

		parts, err := mgr.Participants(ctx, key.ID, key.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		Expect(parts[0].BindingID).To(Equal(wantBinding),
			"genesis must resolve empty BindingID via BindingResolver.Current")
	})

	It("Participants not found returns DEK_NOT_FOUND", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		mgr, err := dek.NewManager(newTestProvider(suiteT), dek.NewStore(pool),
			dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute}), noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		_, err = mgr.Participants(ctx, codec.KeyID(99999), 1)
		errutil.AssertErrorCode(suiteT, err, "DEK_NOT_FOUND")
	})

	It("Participants from unit-test stub returns DEK_MANAGER_NOT_CONFIGURED", func() {
		ctx := context.Background()
		mgr := dek.NewManagerForUnitTest()
		_, err := mgr.Participants(ctx, codec.KeyID(1), 1)
		errutil.AssertErrorCode(suiteT, err, "DEK_MANAGER_NOT_CONFIGURED")
	})

	It("GetOrCreate concurrent mint race converges on same DEK", func() {
		// Two goroutines call GetOrCreate(scene:X, ...) simultaneously.
		// One INSERT wins; the other raises unique-violation, re-SELECTs,
		// and returns the winner's row. Both callers see byte-equal Bytes.
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)

		// Use two managers backed by separate caches to simulate two
		// replicas; they share the underlying DB. The Stores share a
		// pre-insert barrier hook that forces both goroutines past
		// SelectActive (which sees no row) and through Wrap before either
		// is allowed to call INSERT — guaranteeing the unique-violation
		// recovery branch in GetOrCreate runs. Without the barrier, the
		// scheduler could serialize the two goroutines: the first would
		// successfully INSERT, the second would hit the row in
		// SelectActive on its next call and never test the loser path.
		storeA := dek.NewStore(pool)
		storeB := dek.NewStore(pool)
		barrier := newPreInsertBarrier(suiteT, 2)
		storeA.SetPreInsertHookForTest(barrier.Wait)
		storeB.SetPreInsertHookForTest(barrier.Wait)
		cacheA := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		cacheB := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCacheA := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCacheB := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgrA, err := dek.NewManager(provider, storeA, cacheA, partCacheA, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())
		mgrB, err := dek.NewManager(provider, storeB, cacheB, partCacheB, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "race-01"}

		var (
			wg   sync.WaitGroup
			keyA codec.Key
			keyB codec.Key
			errA error
			errB error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			keyA, errA = mgrA.GetOrCreate(ctx, ctxID, []dek.Participant{})
		}()
		go func() {
			defer wg.Done()
			keyB, errB = mgrB.GetOrCreate(ctx, ctxID, []dek.Participant{})
		}()
		wg.Wait()

		Expect(errA).NotTo(HaveOccurred())
		Expect(errB).NotTo(HaveOccurred())
		Expect(keyA.ID).To(Equal(keyB.ID), "both managers must converge on the same DEK row")
		Expect(keyA.Bytes).To(Equal(keyB.Bytes), "both managers must see byte-equal DEK bytes")
		// Verify the barrier was actually exercised: both goroutines must
		// have entered the pre-insert hook for the test to be meaningful.
		// (If only one called INSERT, the unique-violation path was not
		// exercised and the assertion above passed by accident.)
		Expect(barrier.ArrivalCount()).To(Equal(2),
			"both goroutines must reach the pre-insert hook for the race test to exercise the loser path")

		// Exactly one row exists.
		var rowCount int
		err = pool.QueryRow(ctx,
			"SELECT count(*) FROM crypto_keys WHERE context_type=$1 AND context_id=$2",
			"scene", "race-01").Scan(&rowCount)
		Expect(err).NotTo(HaveOccurred())
		Expect(rowCount).To(Equal(1))
	})

	// TestManagerParticipantsHitsCacheOnSecondCall verifies the Phase 3c
	// substrate: GetOrCreate seeds the participants cache via the mint
	// path; Participants returns the cached value; Invalidate forces a
	// fall-through to PG that re-seeds the cache. Without re-seeding,
	// Coordinator's rekey/participants_changed actions would leave the
	// cache stale across replicas.
	It("Participants hits cache on second call and re-seeds after invalidation", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_T7"}
		initial := []dek.Participant{{PlayerID: "01HALICE", JoinedAt: time.Now()}}

		// Mint a fresh DEK; the mint path seeds partCache directly.
		key, err := mgr.GetOrCreate(ctx, ctxID, initial)
		Expect(err).NotTo(HaveOccurred())

		// First Participants call — should be a cache hit (seeded by GetOrCreate).
		ps1, err := mgr.Participants(ctx, key.ID, key.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(ps1).To(HaveLen(1))
		Expect(ps1[0].PlayerID).To(Equal("01HALICE"))

		// Verify cache hit by checking direct cache state.
		pck := dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: key.Version}
		_, ok := partCache.Get(pck)
		Expect(ok).To(BeTrue(), "ParticipantsCache miss after GetOrCreate; expected mint-path seeding")

		// Invalidate the entry; next call falls through to PG.
		partCache.Invalidate(pck)
		_, ok = partCache.Get(pck)
		Expect(ok).To(BeFalse(), "ParticipantsCache hit after Invalidate; expected miss")

		// Second Participants call — fall-through path, must re-seed.
		ps2, err := mgr.Participants(ctx, key.ID, key.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(ps2).To(HaveLen(1))
		Expect(ps2[0].PlayerID).To(Equal("01HALICE"))

		// Cache should be repopulated by the fall-through.
		_, ok = partCache.Get(pck)
		Expect(ok).To(BeTrue(), "ParticipantsCache miss after fall-through; expected re-seed")
	})

	// TestManager_Add_AppendsParticipantAndPublishesInvalidation is INV-CRYPTO-7.
	It("Add appends participant and publishes invalidation (INV-CRYPTO-7)", func() {
		pool := testIntegrationPool(suiteT)
		st := dek.NewStore(pool)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

		ctxID := dek.ContextID{Type: "scene", ID: "add-test"}

		// Create a DEK via GetOrCreate first.
		mgr, err := dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			noopInvalidator, // invalidator — no-op during setup
			&stubBindingResolver{bindingID: "bind-1"},
		)
		Expect(err).NotTo(HaveOccurred())

		initial := []dek.Participant{
			{PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
		}
		dekKey, err := mgr.GetOrCreate(context.Background(), ctxID, initial)
		Expect(err).NotTo(HaveOccurred())

		// Now build a real stub invalidator and inject it into a new manager.
		invStub := &stubInvalidator{}
		mgr, err = dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			invStub.call(),
			&stubBindingResolver{bindingID: "bind-2"},
		)
		Expect(err).NotTo(HaveOccurred())

		err = mgr.Add(context.Background(), ctxID, dek.Participant{
			PlayerID: "p2", CharacterID: "c2", JoinedAt: time.Now().UTC(),
		})
		Expect(err).NotTo(HaveOccurred())

		// Verify invalidation was published.
		Expect(invStub.calls).To(HaveLen(1))
		Expect(invStub.calls[0].action).To(Equal("participants_changed"))
		Expect(invStub.calls[0].version).To(Equal(uint32(1)))
		Expect(invStub.calls[0].successorVersion).To(Equal(uint32(0)))

		// Verify the participant set was updated.
		parts, err := mgr.Participants(context.Background(), dekKey.ID, dekKey.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(2))
		Expect(parts[1].PlayerID).To(Equal("p2"))
		Expect(parts[1].BindingID).To(Equal("bind-2"))
	})

	// TestManager_Add_IdempotentOnBindingID verifies second Add is a no-op.
	It("Add is idempotent on same binding ID", func() {
		pool := testIntegrationPool(suiteT)
		st := dek.NewStore(pool)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

		ctxID := dek.ContextID{Type: "scene", ID: "idempotent-test"}

		mgr, err := dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			noopInvalidator,
			&stubBindingResolver{bindingID: "bind-1"},
		)
		Expect(err).NotTo(HaveOccurred())

		initial := []dek.Participant{
			{PlayerID: "p0", CharacterID: "c0", BindingID: "bind-0", JoinedAt: time.Now().UTC()},
		}
		dekKey, err := mgr.GetOrCreate(context.Background(), ctxID, initial)
		Expect(err).NotTo(HaveOccurred())

		invStub := &stubInvalidator{}
		mgr, err = dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			invStub.call(),
			&stubBindingResolver{bindingID: "bind-1"},
		)
		Expect(err).NotTo(HaveOccurred())

		// First Add should succeed.
		err = mgr.Add(context.Background(), ctxID, dek.Participant{
			PlayerID: "p1", CharacterID: "c1",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(invStub.calls).To(HaveLen(1))

		// Second Add with same (player_id, binding_id) should be no-op.
		err = mgr.Add(context.Background(), ctxID, dek.Participant{
			PlayerID: "p1", CharacterID: "c1",
		})
		Expect(err).NotTo(HaveOccurred())
		// Only one invalidation call total — second was a no-op.
		Expect(invStub.calls).To(HaveLen(1))

		// Participants should have both entries (initial + added).
		parts, err := mgr.Participants(context.Background(), dekKey.ID, dekKey.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(2))
	})

	// TestManager_Add_BindingMissingFails verifies BINDING_NOT_FOUND when
	// no active binding exists.
	It("Add with missing binding returns BINDING_NOT_FOUND", func() {
		pool := testIntegrationPool(suiteT)
		st := dek.NewStore(pool)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

		ctxID := dek.ContextID{Type: "scene", ID: "binding-missing-test"}

		mgr, err := dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			noopInvalidator,
			&stubBindingResolver{bindingID: "bind-1"},
		)
		Expect(err).NotTo(HaveOccurred())

		initial := []dek.Participant{
			{PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
		}
		_, err = mgr.GetOrCreate(context.Background(), ctxID, initial)
		Expect(err).NotTo(HaveOccurred())

		invStub := &stubInvalidator{}
		mgr, err = dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			invStub.call(),
			&stubBindingResolver{err: oops.Code("BINDING_NOT_FOUND").
				Errorf("no active binding for character c3")},
		)
		Expect(err).NotTo(HaveOccurred())

		err = mgr.Add(context.Background(), ctxID, dek.Participant{
			PlayerID: "p2", CharacterID: "c3",
		})
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "BINDING_NOT_FOUND")
		// No invalidation should have been published.
		Expect(invStub.calls).To(BeEmpty())
	})

	// TestManager_Rotate_MintsFreshDEKAndMarksOldRotated is INV-CRYPTO-8.
	It("Rotate mints fresh DEK and marks old rotated (INV-CRYPTO-8)", func() {
		pool := testIntegrationPool(suiteT)
		st := dek.NewStore(pool)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

		ctxID := dek.ContextID{Type: "scene", ID: "rotate-test"}

		mgr, err := dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			noopInvalidator,
			&stubBindingResolver{bindingID: "bind-1"},
		)
		Expect(err).NotTo(HaveOccurred())

		initial := []dek.Participant{
			{PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
		}
		v1Key, err := mgr.GetOrCreate(context.Background(), ctxID, initial)
		Expect(err).NotTo(HaveOccurred())
		v1Version := v1Key.Version

		invStub := &stubInvalidator{}
		mgr, err = dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			invStub.call(),
			&stubBindingResolver{bindingID: "bind-1"},
		)
		Expect(err).NotTo(HaveOccurred())

		newParticipants := []dek.Participant{
			{PlayerID: "p2", CharacterID: "c2", BindingID: "bind-2", JoinedAt: time.Now().UTC()},
		}
		err = mgr.Rotate(context.Background(), ctxID, newParticipants, "test departure")
		Expect(err).NotTo(HaveOccurred())

		// Verify invalidation was published.
		Expect(invStub.calls).To(HaveLen(1))
		Expect(invStub.calls[0].action).To(Equal("rotate"))
		Expect(invStub.calls[0].version).To(Equal(uint32(1)))
		Expect(invStub.calls[0].successorVersion).To(Equal(uint32(2)))

		// Old DEK (v1) should still be unwrappable (INV-CRYPTO-8).
		_, err = mgr.Resolve(context.Background(), v1Key.ID, v1Version)
		Expect(err).NotTo(HaveOccurred())

		// Fetch the active (v2) DEK via GetOrCreate — no mint since v2 exists.
		activeKey, err := mgr.GetOrCreate(context.Background(), ctxID, nil)
		Expect(err).NotTo(HaveOccurred())

		// New DEK (v2) should have incremented version.
		Expect(activeKey.Version).To(Equal(v1Version + 1))

		// New DEK should be resolvable.
		_, err = mgr.Resolve(context.Background(), activeKey.ID, activeKey.Version)
		Expect(err).NotTo(HaveOccurred())

		// New participants are on v2.
		parts, err := mgr.Participants(context.Background(), activeKey.ID, activeKey.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		Expect(parts[0].PlayerID).To(Equal("p2"))
	})

	// TestManager_Rotate_RollsBackOnInvalidationFailure is INV-CLUSTER-2.
	It("Rotate rolls back on invalidation failure (INV-CLUSTER-2)", func() {
		pool := testIntegrationPool(suiteT)
		st := dek.NewStore(pool)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

		ctxID := dek.ContextID{Type: "scene", ID: "rotate-fail-test"}

		mgr, err := dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			noopInvalidator,
			&stubBindingResolver{bindingID: "bind-1"},
		)
		Expect(err).NotTo(HaveOccurred())

		initial := []dek.Participant{
			{PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
		}
		key, err := mgr.GetOrCreate(context.Background(), ctxID, initial)
		Expect(err).NotTo(HaveOccurred())

		// Build a stub that fails on invalidation.
		mgr, err = dek.NewManager(
			newTestProvider(suiteT), st, cache, partCache,
			func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error {
				return oops.Code("INVALIDATION_PARTIAL_FAILURE").Errorf("simulated failure")
			},
			&stubBindingResolver{bindingID: "bind-1"},
		)
		Expect(err).NotTo(HaveOccurred())

		newParticipants := []dek.Participant{
			{PlayerID: "p2", CharacterID: "c2", BindingID: "bind-2", JoinedAt: time.Now().UTC()},
		}
		err = mgr.Rotate(context.Background(), ctxID, newParticipants, "test")
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "INVALIDATION_PARTIAL_FAILURE")

		// The original DEK should still be the active one.
		parts, err := mgr.Participants(context.Background(), key.ID, key.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		Expect(parts[0].PlayerID).To(Equal("p1"))

		// No new DEK version should exist.
		_, err = mgr.Resolve(context.Background(), codec.KeyID(uint64(key.ID)+1), key.Version+1)
		Expect(err).To(HaveOccurred())
	})

	// TestManager_Add_ConcurrentSameBindingIsIdempotent verifies that
	// concurrent Adds with the same (player_id, binding_id) are both
	// idempotent — one succeeds, the other detects the duplicate and
	// returns no-op. Final participant count is exactly 2 (initial + 1 added).
	It("Add concurrent same binding is idempotent", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		st := dek.NewStore(pool)
		provider := newTestProvider(suiteT)

		// Seed: create the DEK with one initial participant.
		ctxID := dek.ContextID{Type: "scene", ID: "concurrent-add"}
		setupMgr, err := dek.NewManager(
			provider, st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			noopInvalidator, &stubBindingResolver{bindingID: "bind-initial"},
		)
		Expect(err).NotTo(HaveOccurred())
		initial := []dek.Participant{
			{PlayerID: "p0", CharacterID: "c0", BindingID: "bind-0", JoinedAt: time.Now().UTC()},
		}
		dekKey, err := setupMgr.GetOrCreate(ctx, ctxID, initial)
		Expect(err).NotTo(HaveOccurred())

		// Two managers with separate caches but shared DB.
		invA := &stubInvalidator{}
		invB := &stubInvalidator{}
		mgrA, err := dek.NewManager(
			newTestProvider(suiteT), st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			invA.call(), &stubBindingResolver{bindingID: "bind-concurrent"},
		)
		Expect(err).NotTo(HaveOccurred())
		mgrB, err := dek.NewManager(
			newTestProvider(suiteT), st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			invB.call(), &stubBindingResolver{bindingID: "bind-concurrent"},
		)
		Expect(err).NotTo(HaveOccurred())

		// Concurrently add the same participant (same player_id, same resolved binding_id).
		var wg sync.WaitGroup
		var errA, errB error
		wg.Add(2)
		go func() {
			defer wg.Done()
			errA = mgrA.Add(ctx, ctxID, dek.Participant{
				PlayerID: "p1", CharacterID: "c1",
			})
		}()
		go func() {
			defer wg.Done()
			errB = mgrB.Add(ctx, ctxID, dek.Participant{
				PlayerID: "p1", CharacterID: "c1",
			})
		}()
		wg.Wait()

		Expect(errA).NotTo(HaveOccurred())
		Expect(errB).NotTo(HaveOccurred())

		// Exactly one must have published an invalidation — the second
		// goroutine serializes on the FOR UPDATE row lock and sees the
		// duplicate, returning added=false without publishing.
		totalCalls := len(invA.calls) + len(invB.calls)
		Expect(totalCalls).To(Equal(1),
			"exactly one concurrent Add must publish invalidation; got %d", totalCalls)

		// Query via mgrA (whose partCache was seeded by Add) rather than setupMgr
		// (whose cache is stale — it was seeded by GetOrCreate and never updated).
		parts, err := mgrA.Participants(ctx, dekKey.ID, dekKey.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(2))
		players := map[string]bool{"p0": true, "p1": true}
		for _, p := range parts {
			Expect(players[p.PlayerID]).To(BeTrue(), "unexpected player %s", p.PlayerID)
		}
	})

	// TestManager_Add_ConcurrentDistinctParticipantsPreservesBoth is the
	// regression test for holomush-fi0n.9 (TOCTOU race in updateParticipants).
	// Two goroutines concurrently add different participants to the same DEK.
	// With SELECT ... FOR UPDATE, the second goroutine blocks on the first's
	// row lock and reads the updated participant list; both entries persist.
	// Without the lock, the second read-modify-write can silently discard the
	// first write (final count would be 2 instead of 3).
	It("Add concurrent distinct participants preserves both (holomush-fi0n.9)", func() {
		ctx := context.Background()
		pool := testIntegrationPool(suiteT)
		st := dek.NewStore(pool)

		ctxID := dek.ContextID{Type: "scene", ID: "concurrent-distinct"}

		// Seed: create the DEK with one initial participant.
		setupMgr, err := dek.NewManager(
			newTestProvider(suiteT), st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			noopInvalidator, &stubBindingResolver{bindingID: "bind-0"},
		)
		Expect(err).NotTo(HaveOccurred())
		initial := []dek.Participant{
			{PlayerID: "p0", CharacterID: "c0", BindingID: "bind-0", JoinedAt: time.Now().UTC()},
		}
		dekKey, err := setupMgr.GetOrCreate(ctx, ctxID, initial)
		Expect(err).NotTo(HaveOccurred())

		// Two managers with separate caches but shared DB.
		invA := &stubInvalidator{}
		invB := &stubInvalidator{}
		mgrA, err := dek.NewManager(
			newTestProvider(suiteT), st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			invA.call(), &stubBindingResolver{bindingID: "bind-A"},
		)
		Expect(err).NotTo(HaveOccurred())
		mgrB, err := dek.NewManager(
			newTestProvider(suiteT), st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			invB.call(), &stubBindingResolver{bindingID: "bind-B"},
		)
		Expect(err).NotTo(HaveOccurred())

		// Concurrently add DISTINCT participants — both must persist.
		var wg sync.WaitGroup
		var errA, errB error
		readyCh := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-readyCh
			errA = mgrA.Add(ctx, ctxID, dek.Participant{
				PlayerID: "pA", CharacterID: "cA",
			})
		}()
		go func() {
			defer wg.Done()
			<-readyCh
			errB = mgrB.Add(ctx, ctxID, dek.Participant{
				PlayerID: "pB", CharacterID: "cB",
			})
		}()
		close(readyCh)
		wg.Wait()

		Expect(errA).NotTo(HaveOccurred())
		Expect(errB).NotTo(HaveOccurred())

		// Both must have published invalidation.
		Expect(invA.calls).To(HaveLen(1))
		Expect(invB.calls).To(HaveLen(1))

		// Query from a fresh manager with clean caches to avoid reading
		// stale cache state from mgrA/mgrB (which each seeded only their
		// own participant during Add).
		freshMgr, err := dek.NewManager(
			newTestProvider(suiteT), st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			noopInvalidator, &stubBindingResolver{bindingID: "fresh"},
		)
		Expect(err).NotTo(HaveOccurred())
		parts, err := freshMgr.Participants(ctx, dekKey.ID, dekKey.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(3), "initial p0 + pA + pB = 3")
		players := map[string]bool{"p0": true, "pA": true, "pB": true}
		for _, p := range parts {
			Expect(players[p.PlayerID]).To(BeTrue(), "unexpected player %s", p.PlayerID)
		}
	})

	// TestManager_Add_WithExplicitBindingIDSkipsResolver covers the code path
	// where Add is called with a pre-set BindingID (p.BindingID != ""), so
	// the BindingResolver is never invoked.
	It("Add with explicit BindingID skips resolver", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		st := dek.NewStore(pool)

		// This stub returns an error if called — the test fails if Add invokes it.
		invStub := &stubInvalidator{}
		mgr, err := dek.NewManager(
			newTestProvider(suiteT), st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			invStub.call(), &stubBindingResolver{bindingID: "should-not-be-used"},
		)
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "explicit-binding"}
		initial := []dek.Participant{
			{PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
		}
		dekKey, err := mgr.GetOrCreate(ctx, ctxID, initial)
		Expect(err).NotTo(HaveOccurred())

		// Add with an explicit BindingID — skips the resolver entirely.
		err = mgr.Add(ctx, ctxID, dek.Participant{
			PlayerID: "p2", CharacterID: "c2", BindingID: "bind-explicit",
			JoinedAt: time.Now().UTC(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(invStub.calls).To(HaveLen(1))
		Expect(invStub.calls[0].action).To(Equal("participants_changed"))

		parts, err := mgr.Participants(ctx, dekKey.ID, dekKey.Version)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(2))
		Expect(parts[1].BindingID).To(Equal("bind-explicit"))
	})

	It("EnsureParticipant genesises the DEK seeded with p when none exists", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "01GENESISFOCUS01"}
		p := dek.Participant{PlayerID: "01PLAYER0000000001", CharacterID: "01CHAR00000000001", AddedVia: "test.first_focus"}

		// No DEK exists yet — bare Add would fail with ErrNoRows; EnsureParticipant must genesis.
		Expect(mgr.EnsureParticipant(ctx, ctxID, p)).To(Succeed())

		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())
		parts, err := mgr.Participants(ctx, key.ID, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		Expect(parts[0].PlayerID).To(Equal(p.PlayerID))
	})

	It("EnsureParticipant appends p when an active DEK already exists without p", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "01GENESISFOCUS02"}
		// Publisher-style empty genesis (no participants), mirroring initialParticipantsForContext nil.
		_, err = mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())

		p := dek.Participant{PlayerID: "01PLAYER0000000002", CharacterID: "01CHAR00000000002", AddedVia: "test.focus_after_pose"}
		Expect(mgr.EnsureParticipant(ctx, ctxID, p)).To(Succeed())

		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())
		parts, err := mgr.Participants(ctx, key.ID, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		Expect(parts[0].PlayerID).To(Equal(p.PlayerID))
	})

	It("EnsureParticipant is an idempotent no-op when p already present", func() {
		ctx := context.Background()
		connStr, teardown := newTestPGPool(suiteT)
		DeferCleanup(teardown)
		pool, err := pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)

		provider := newTestProvider(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "01GENESISFOCUS03"}
		p := dek.Participant{PlayerID: "01PLAYER0000000003", CharacterID: "01CHAR00000000003", AddedVia: "test.idempotent"}
		Expect(mgr.EnsureParticipant(ctx, ctxID, p)).To(Succeed())
		Expect(mgr.EnsureParticipant(ctx, ctxID, p)).To(Succeed())

		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())
		parts, err := mgr.Participants(ctx, key.ID, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1), "duplicate EnsureParticipant must not double-append")
	})
})

// dekCapturingProvider wraps a real kek.Provider and captures the
// plaintext byte slices flowing through Wrap (input) and Unwrap (output)
// so tests can inspect post-call zeroize behavior (holomush-e49r.4).
type dekCapturingProvider struct {
	inner       kek.Provider
	wrapPlain   [][]byte
	unwrapPlain [][]byte
}

func (p *dekCapturingProvider) Name() string {
	return p.inner.Name()
}

func (p *dekCapturingProvider) RotateKEK(ctx context.Context) (string, error) {
	return p.inner.RotateKEK(ctx) //nolint:wrapcheck // pass-through in spy
}

func (p *dekCapturingProvider) HealthCheck(ctx context.Context) error {
	return p.inner.HealthCheck(ctx) //nolint:wrapcheck // pass-through in spy
}

func (p *dekCapturingProvider) Wrap(ctx context.Context, plaintext []byte) ([]byte, string, error) {
	p.wrapPlain = append(p.wrapPlain, plaintext)
	return p.inner.Wrap(ctx, plaintext) //nolint:wrapcheck // pass-through in spy
}

func (p *dekCapturingProvider) Unwrap(ctx context.Context, wrapped []byte, kekKeyID string) ([]byte, error) {
	plaintext, err := p.inner.Unwrap(ctx, wrapped, kekKeyID)
	if err == nil {
		p.unwrapPlain = append(p.unwrapPlain, plaintext)
	}
	return plaintext, err //nolint:wrapcheck // pass-through in spy
}

// TestE49R4_GetOrCreateZeroizesPlaintextDEK verifies the defense-in-depth
// invariant (bead holomush-e49r.4): after GetOrCreate returns, the plaintext
// DEK byte slice the provider's Wrap call saw is zeroed, preventing
// long-lived heap-resident DEK material from surfacing in coredumps or
// heap dumps.
//
// TestE49R4_ResolveZeroizesUnwrappedDEK verifies the same defense-in-depth
// invariant on the unwrapAndCache path: after Resolve returns, the plaintext
// DEK returned by provider.Unwrap is zeroed.
var _ = Describe("Manager zeroize defense-in-depth (holomush-e49r.4)", func() {
	It("GetOrCreate zeroizes plaintext DEK after return", func() {
		ctx := context.Background()
		pool := testIntegrationPool(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

		spy := &dekCapturingProvider{inner: newTestProvider(suiteT)}
		mgr, err := dek.NewManager(spy, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "e49r4-getorcreate"}
		_, err = mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())

		Expect(spy.wrapPlain).To(HaveLen(1), "Wrap MUST have been called exactly once during fresh GetOrCreate")
		captured := spy.wrapPlain[0]
		Expect(captured).To(HaveLen(dek.DEKByteLength), "captured plaintext must be DEK-length")
		for i, b := range captured {
			Expect(b).To(BeZero(),
				"captured plaintext byte[%d] MUST be zeroed after GetOrCreate returns (e49r.4 defense-in-depth)", i)
		}
	})

	It("Resolve zeroizes unwrapped DEK after return", func() {
		ctx := context.Background()
		pool := testIntegrationPool(suiteT)
		cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
		partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

		spy := &dekCapturingProvider{inner: newTestProvider(suiteT)}
		mgr, err := dek.NewManager(spy, dek.NewStore(pool), cache, partCache, noopInvalidator, &stubBindingResolver{})
		Expect(err).NotTo(HaveOccurred())

		ctxID := dek.ContextID{Type: "scene", ID: "e49r4-resolve"}
		key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
		Expect(err).NotTo(HaveOccurred())

		// Drop cache so Resolve must call provider.Unwrap, then reset the
		// capture so we only inspect the unwrap-path plaintext.
		cache.Invalidate(dek.CacheKey{KeyID: key.ID, Version: 1})
		spy.unwrapPlain = nil

		_, err = mgr.Resolve(ctx, key.ID, 1)
		Expect(err).NotTo(HaveOccurred())

		Expect(spy.unwrapPlain).To(HaveLen(1), "Unwrap MUST have been called exactly once after cache eviction")
		captured := spy.unwrapPlain[0]
		Expect(captured).To(HaveLen(dek.DEKByteLength), "captured plaintext must be DEK-length")
		for i, b := range captured {
			Expect(b).To(BeZero(),
				"unwrapped plaintext byte[%d] MUST be zeroed after Resolve returns (e49r.4 defense-in-depth)", i)
		}
	})
})
