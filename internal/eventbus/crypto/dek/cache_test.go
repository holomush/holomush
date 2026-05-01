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

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, m)
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

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, m1)
	cache.Put(dek.CacheKey{KeyID: 2, Version: 1}, m2)
	// Touch key 1 so key 2 is the LRU.
	_, _ = cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	cache.Put(dek.CacheKey{KeyID: 3, Version: 1}, m3)

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

	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, m)
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
	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, m)

	cache.Invalidate(dek.CacheKey{KeyID: 1, Version: 1})
	_, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1})
	assert.False(t, ok)
}
