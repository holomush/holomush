// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func TestCache_PutGet_Roundtrip(t *testing.T) {
	cache := dek.NewCache(dek.CacheConfig{Capacity: 4, TTL: time.Minute})
	m := dek.NewMaterial([]byte("0123456789abcdef0123456789abcdef"))
	ctxTest := dek.ContextID{Type: "test", ID: "01HTEST"}

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxTest, m)
	got, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	require.True(t, ok)
	assert.Equal(t, m, got)
}

func TestCache_Get_MissReturnsFalse(t *testing.T) {
	cache := dek.NewCache(dek.CacheConfig{Capacity: 4, TTL: time.Minute})
	_, ok := cache.Get(dek.CacheKey{KeyID: 99, Version: 1})
	assert.False(t, ok)
}

func TestCache_LRUEviction(t *testing.T) {
	cache := dek.NewCache(dek.CacheConfig{Capacity: 2, TTL: time.Minute})
	m1 := dek.NewMaterial([]byte("11111111111111111111111111111111"))
	m2 := dek.NewMaterial([]byte("22222222222222222222222222222222"))
	m3 := dek.NewMaterial([]byte("33333333333333333333333333333333"))
	ctxTest := dek.ContextID{Type: "test", ID: "01HTEST"}

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxTest, m1)
	cache.Put(dek.CacheKey{KeyID: 2, Version: 1}, ctxTest, m2)
	// Touch key 1 so key 2 is the LRU.
	_, _ = cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	cache.Put(dek.CacheKey{KeyID: 3, Version: 1}, ctxTest, m3)

	_, ok := cache.Get(dek.CacheKey{KeyID: 2, Version: 1})
	assert.False(t, ok, "key 2 should have been evicted as LRU")
	_, ok = cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	assert.True(t, ok, "key 1 should remain (recently used)")
	_, ok = cache.Get(dek.CacheKey{KeyID: 3, Version: 1})
	assert.True(t, ok, "key 3 should remain (newly inserted)")
}

func TestCache_TTLExpiry(t *testing.T) {
	// Use a clock to test TTL deterministically; cache accepts a
	// clock function for testability.
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	cache := dek.NewCacheWithClock(dek.CacheConfig{Capacity: 4, TTL: 5 * time.Minute}, clock)
	m := dek.NewMaterial([]byte("0123456789abcdef0123456789abcdef"))
	ctxTest := dek.ContextID{Type: "test", ID: "01HTEST"}

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxTest, m)
	_, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	require.True(t, ok)

	// Advance past TTL.
	now = now.Add(6 * time.Minute)
	_, ok = cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	assert.False(t, ok, "entry should have expired after TTL")
}

func TestCache_Invalidate_RemovesEntry(t *testing.T) {
	cache := dek.NewCache(dek.CacheConfig{Capacity: 4, TTL: time.Minute})
	m := dek.NewMaterial([]byte("0123456789abcdef0123456789abcdef"))
	ctxTest := dek.ContextID{Type: "test", ID: "01HTEST"}
	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxTest, m)

	cache.Invalidate(dek.CacheKey{KeyID: 1, Version: 1})
	_, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	assert.False(t, ok)
}

// TestCache_ZeroConfig_AppliesDefaults verifies that an empty
// CacheConfig{} yields the master-spec defaults (Capacity=1024,
// TTL=5m) instead of silently discarding every Put.
func TestCache_ZeroConfig_AppliesDefaults(t *testing.T) {
	cache := dek.NewCache(dek.CacheConfig{}) // both fields zero
	m := dek.NewMaterial([]byte("0123456789abcdef0123456789abcdef"))
	ctxTest := dek.ContextID{Type: "test", ID: "01HTEST"}

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxTest, m)
	got, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	require.True(t, ok, "Put with zero CacheConfig must store (defaults applied), not silently drop")
	assert.Equal(t, m, got)
}

// TestCache_NegativeCapacity_AppliesDefaults verifies the defensive
// branch: a negative Capacity should fall back to the documented
// default rather than panic in `make(map..., negative)`.
func TestCache_NegativeCapacity_AppliesDefaults(t *testing.T) {
	cache := dek.NewCache(dek.CacheConfig{Capacity: -10, TTL: time.Minute})
	m := dek.NewMaterial([]byte("0123456789abcdef0123456789abcdef"))
	ctxTest := dek.ContextID{Type: "test", ID: "01HTEST"}

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxTest, m)
	got, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	require.True(t, ok)
	assert.Equal(t, m, got)
}

// TestCache_PutUpdate_UpdatesExisting verifies that a second Put for
// the same key replaces the entry's material in place (covering the
// "update existing" branch in Cache.Put).
func TestCache_PutUpdate_UpdatesExisting(t *testing.T) {
	cache := dek.NewCache(dek.CacheConfig{Capacity: 4, TTL: time.Minute})
	m1 := dek.NewMaterial([]byte("11111111111111111111111111111111"))
	m2 := dek.NewMaterial([]byte("22222222222222222222222222222222"))
	ctxTest := dek.ContextID{Type: "test", ID: "01HTEST"}

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxTest, m1)
	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxTest, m2) // update in place

	got, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	require.True(t, ok)
	assert.Equal(t, m2, got, "second Put must replace the first entry's material")
}

func TestCacheInvalidateContextRemovesAllVersionsForThatContext(t *testing.T) {
	c := dek.NewCache(dek.CacheConfig{Capacity: 100, TTL: 5 * time.Minute})

	ctxA := dek.ContextID{Type: "scene", ID: "01HSCENE_A"}
	ctxB := dek.ContextID{Type: "scene", ID: "01HSCENE_B"}

	c.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxA, dek.NewMaterial(make([]byte, dek.DEKByteLength)))
	c.Put(dek.CacheKey{KeyID: 1, Version: 2}, ctxA, dek.NewMaterial(make([]byte, dek.DEKByteLength)))
	c.Put(dek.CacheKey{KeyID: 2, Version: 1}, ctxB, dek.NewMaterial(make([]byte, dek.DEKByteLength)))

	c.InvalidateContext(ctxA)

	if _, ok := c.Get(dek.CacheKey{KeyID: 1, Version: 1}); ok {
		t.Errorf("ctxA v1 still present after InvalidateContext(ctxA)")
	}
	if _, ok := c.Get(dek.CacheKey{KeyID: 1, Version: 2}); ok {
		t.Errorf("ctxA v2 still present after InvalidateContext(ctxA)")
	}
	if _, ok := c.Get(dek.CacheKey{KeyID: 2, Version: 1}); !ok {
		t.Errorf("ctxB v1 missing after InvalidateContext(ctxA); only ctxA should be evicted")
	}
}

// TestCacheReverseIndexIsCleanedOnLRUEviction and the cross-context
// regression test live in cache_internal_test.go (package dek) so
// they can call the test-only contextIndexLen accessor and verify
// byContext integrity directly. The external (dek_test) surface only
// sees Len(), which would silently mask byContext leaks via
// InvalidateContext's defensive byKey guard.
