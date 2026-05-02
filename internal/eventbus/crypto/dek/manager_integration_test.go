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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

func newTestPGPool(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()
	pgContainer, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	migrator, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	require.NoError(t, migrator.Up())
	migrator.Close()
	return connStr, func() { _ = pgContainer.Terminate(ctx) }
}

func newTestProvider(t *testing.T) kek.Provider {
	t.Helper()
	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	require.NoError(t, err)
	name := "TEST_KEK_" + sanitizeEnvName(t.Name())
	t.Setenv(name, hex.EncodeToString(kekBytes))
	src := kek.NewEnvSource(name, false)
	p, err := kek.NewLocalAEADProviderForUnitTest(context.Background(), src)
	require.NoError(t, err)
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

func TestManager_GetOrCreate_MintsAndPersists(t *testing.T) {
	ctx := context.Background()
	connStr, teardown := newTestPGPool(t)
	defer teardown()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	provider := newTestProvider(t)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache)
	require.NoError(t, err)

	ctxID := dek.ContextID{Type: "scene", ID: "01ABCDEF"}
	key1, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	require.NoError(t, err)
	assert.NotZero(t, key1.ID)
	assert.Len(t, key1.Bytes, 32)

	// A second call returns the same key (idempotent for the same context).
	key2, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	require.NoError(t, err)
	assert.Equal(t, key1.ID, key2.ID)
	assert.Equal(t, key1.Bytes, key2.Bytes)

	// The crypto_keys table has exactly one row for this context.
	var rowCount int
	err = pool.QueryRow(ctx,
		"SELECT count(*) FROM crypto_keys WHERE context_type=$1 AND context_id=$2",
		"scene", "01ABCDEF").Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}

func TestManager_Resolve_ByKeyIDAndVersion(t *testing.T) {
	ctx := context.Background()
	connStr, teardown := newTestPGPool(t)
	defer teardown()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	provider := newTestProvider(t)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache)
	require.NoError(t, err)

	ctxID := dek.ContextID{Type: "dm", ID: "01ABCDEF-01FFFFFF"}
	key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
	require.NoError(t, err)

	// Drop the cache so Resolve has to go through DB.
	cache.Invalidate(dek.CacheKey{KeyID: key.ID, Version: 1})

	resolved, err := mgr.Resolve(ctx, key.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, key.ID, resolved.ID)
	assert.Equal(t, key.Bytes, resolved.Bytes)
}

func TestManager_Resolve_NotFound_ReturnsErrDEKNotFound(t *testing.T) {
	ctx := context.Background()
	connStr, teardown := newTestPGPool(t)
	defer teardown()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	mgr, err := dek.NewManager(newTestProvider(t), dek.NewStore(pool),
		dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}))
	require.NoError(t, err)

	_, err = mgr.Resolve(ctx, codec.KeyID(99999), 1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_NOT_FOUND")
}

func TestManager_GetOrCreate_ConcurrentMintRace(t *testing.T) {
	// Two goroutines call GetOrCreate(scene:X, ...) simultaneously.
	// One INSERT wins; the other raises unique-violation, re-SELECTs,
	// and returns the winner's row. Both callers see byte-equal Bytes.
	ctx := context.Background()
	connStr, teardown := newTestPGPool(t)
	defer teardown()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	provider := newTestProvider(t)

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
	barrier := newPreInsertBarrier(t, 2)
	storeA.SetPreInsertHookForTest(barrier.Wait)
	storeB.SetPreInsertHookForTest(barrier.Wait)
	cacheA := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	cacheB := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgrA, err := dek.NewManager(provider, storeA, cacheA)
	require.NoError(t, err)
	mgrB, err := dek.NewManager(provider, storeB, cacheB)
	require.NoError(t, err)

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

	require.NoError(t, errA)
	require.NoError(t, errB)
	assert.Equal(t, keyA.ID, keyB.ID, "both managers must converge on the same DEK row")
	assert.Equal(t, keyA.Bytes, keyB.Bytes, "both managers must see byte-equal DEK bytes")
	// Verify the barrier was actually exercised: both goroutines must
	// have entered the pre-insert hook for the test to be meaningful.
	// (If only one called INSERT, the unique-violation path was not
	// exercised and the assertion above passed by accident.)
	assert.Equal(t, 2, barrier.ArrivalCount(),
		"both goroutines must reach the pre-insert hook for the race test to exercise the loser path")

	// Exactly one row exists.
	var rowCount int
	err = pool.QueryRow(ctx,
		"SELECT count(*) FROM crypto_keys WHERE context_type=$1 AND context_id=$2",
		"scene", "race-01").Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}
