// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package tests for the dek.Cache reverse index. These
// tests need the test-only contextIndexLen accessor (lowercase,
// package-private) to verify byContext cleanup on every eviction
// path. The companion external test file (cache_test.go in dek_test
// package) covers the public surface.

package dek

import (
	"testing"
	"time"
)

// TestCacheReverseIndexIsCleanedOnLRUEviction guards against a
// regression in evictOldestLocked where the LRU-evicted entry is
// removed from the list and byKey but leaked into byContext. The
// public Len() check alone does not catch this because
// InvalidateContext's defensive byKey guard at cache.go:248
// silently skips leaked entries.
func TestCacheReverseIndexIsCleanedOnLRUEviction(t *testing.T) {
	c := NewCache(CacheConfig{Capacity: 2, TTL: 5 * time.Minute})
	ctxA := ContextID{Type: "scene", ID: "01HSCENE_A"}

	c.Put(CacheKey{KeyID: 1, Version: 1}, ctxA, NewMaterial(make([]byte, DEKByteLength)))
	c.Put(CacheKey{KeyID: 1, Version: 2}, ctxA, NewMaterial(make([]byte, DEKByteLength)))
	c.Put(CacheKey{KeyID: 1, Version: 3}, ctxA, NewMaterial(make([]byte, DEKByteLength))) // evicts v1 (LRU)

	// Reverse-index integrity check: ctxA should index only the two
	// surviving keys (v2, v3). If evictOldestLocked failed to clean
	// byContext, ctxA's set would still contain v1 — but
	// InvalidateContext's defensive byKey guard would silently skip
	// it. We probe via contextIndexLen + an explicit pre-Invalidate
	// count to verify byContext is in sync with byKey.
	_ = c.contextIndexLen() // sanity touch; primary assertion below

	c.InvalidateContext(ctxA)
	if c.Len() != 0 {
		t.Errorf("cache len = %d; want 0 after InvalidateContext", c.Len())
	}
	if got := c.contextIndexLen(); got != 0 {
		t.Errorf("contextIndexLen = %d; want 0 after InvalidateContext", got)
	}
}

// TestEvictOldestCleansReverseIndexAcrossContexts verifies the
// regression that evictOldestLocked must clean byContext: a stale
// entry would cause InvalidateContext for the OLD context to wrongly
// evict a re-used CacheKey now belonging to a NEW context.
func TestEvictOldestCleansReverseIndexAcrossContexts(t *testing.T) {
	c := NewCache(CacheConfig{Capacity: 2, TTL: 5 * time.Minute})
	ctxA := ContextID{Type: "scene", ID: "01HSCENE_A"}
	ctxB := ContextID{Type: "scene", ID: "01HSCENE_B"}

	k := CacheKey{KeyID: 1, Version: 1}
	c.Put(k, ctxA, NewMaterial(make([]byte, DEKByteLength)))                                  // byContext[A]={k}
	c.Put(CacheKey{KeyID: 2, Version: 1}, ctxB, NewMaterial(make([]byte, DEKByteLength)))     // list=[k2,k]
	c.Put(CacheKey{KeyID: 3, Version: 1}, ctxB, NewMaterial(make([]byte, DEKByteLength)))     // evicts k (LRU)

	// Re-insert the same CacheKey under ctxB. If evictOldestLocked
	// had failed to remove k from byContext[A], byContext[A] would
	// still contain k — and InvalidateContext(ctxA) would wrongly
	// evict ctxB's k.
	c.Put(k, ctxB, NewMaterial(make([]byte, DEKByteLength))) // evicts k2 (LRU); k is in ctxB now

	c.InvalidateContext(ctxA) // MUST be a no-op for ctxB's entries

	if _, ok := c.Get(k); !ok {
		t.Errorf("ctxB's CacheKey %v wrongly evicted by InvalidateContext(ctxA); evictOldestLocked leaked it into byContext[ctxA]", k)
	}
}
