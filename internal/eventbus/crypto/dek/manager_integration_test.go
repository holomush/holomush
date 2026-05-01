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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

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
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2)),
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
	mgr := dek.NewManager(provider, dek.NewStore(pool), cache)

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
	mgr := dek.NewManager(provider, dek.NewStore(pool), cache)

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

	mgr := dek.NewManager(newTestProvider(t), dek.NewStore(pool),
		dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}))

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
	// replicas; they share the underlying DB.
	cacheA := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	cacheB := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	mgrA := dek.NewManager(provider, dek.NewStore(pool), cacheA)
	mgrB := dek.NewManager(provider, dek.NewStore(pool), cacheB)

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

	// Exactly one row exists.
	var rowCount int
	err = pool.QueryRow(ctx,
		"SELECT count(*) FROM crypto_keys WHERE context_type=$1 AND context_id=$2",
		"scene", "race-01").Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}
