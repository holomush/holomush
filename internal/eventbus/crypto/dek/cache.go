// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"container/list"
	"sync"
	"time"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

// CacheKey identifies an entry by KeyID + version. Per master spec §5.8,
// the cache is version-aware so rotation works correctly: after Rotate,
// the old version stays cacheable for in-flight reads while new emits
// hit the new version.
type CacheKey struct {
	KeyID   codec.KeyID
	Version uint32
}

// CacheConfig parameterizes the cache. Defaults per master spec §5.8:
// capacity=1024, ttl=5m. Phase 2 ships these defaults but tests pass
// smaller capacity for LRU eviction and shorter TTL for expiry tests.
type CacheConfig struct {
	Capacity int
	TTL      time.Duration
}

// Cache holds unwrapped DEK Material in process memory with LRU
// eviction and TTL safety net. INV-27: MUST NOT live in NATS KV, PG,
// disk, or logs.
//
// The cache is internally synchronized for concurrent use. Phase 2's
// callers (DEKManager.GetOrCreate / Resolve) are concurrent.
type Cache struct {
	cap   int
	ttl   time.Duration
	clock func() time.Time

	mu    sync.Mutex
	list  *list.List
	byKey map[CacheKey]*list.Element
}

type cacheEntry struct {
	key       CacheKey
	material  *Material
	expiresAt time.Time
}

// NewCache constructs a cache using time.Now as the clock.
func NewCache(cfg CacheConfig) *Cache {
	return NewCacheWithClock(cfg, time.Now)
}

// NewCacheWithClock allows tests to inject a deterministic clock.
func NewCacheWithClock(cfg CacheConfig, clock func() time.Time) *Cache {
	return &Cache{
		cap:   cfg.Capacity,
		ttl:   cfg.TTL,
		clock: clock,
		list:  list.New(),
		byKey: make(map[CacheKey]*list.Element, cfg.Capacity),
	}
}

// Get returns the Material for key. Returns false on miss or
// TTL-expired entry.
func (c *Cache) Get(key CacheKey) (*Material, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.byKey[key]
	if !ok {
		return nil, false
	}
	entry, ok := elem.Value.(*cacheEntry)
	if !ok {
		return nil, false
	}
	if c.clock().After(entry.expiresAt) {
		// Expired: remove and return miss.
		c.list.Remove(elem)
		delete(c.byKey, key)
		return nil, false
	}
	// LRU touch.
	c.list.MoveToFront(elem)
	return entry.material, true
}

// Put inserts or updates an entry. Evicts the LRU entry if over
// capacity.
func (c *Cache) Put(key CacheKey, material *Material) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.byKey[key]; ok {
		// Update in place.
		if entry, ok := elem.Value.(*cacheEntry); ok {
			entry.material = material
			entry.expiresAt = c.clock().Add(c.ttl)
		}
		c.list.MoveToFront(elem)
		return
	}

	entry := &cacheEntry{key: key, material: material, expiresAt: c.clock().Add(c.ttl)}
	elem := c.list.PushFront(entry)
	c.byKey[key] = elem

	if c.list.Len() > c.cap {
		// Evict LRU.
		oldest := c.list.Back()
		if oldest != nil {
			c.list.Remove(oldest)
			if oldEntry, ok := oldest.Value.(*cacheEntry); ok {
				delete(c.byKey, oldEntry.key)
			}
		}
	}
}

// Invalidate removes an entry. Used by Phase 4+ for cross-replica
// invalidation (Phase 2 only exposes the local-side primitive).
func (c *Cache) Invalidate(key CacheKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.byKey[key]; ok {
		c.list.Remove(elem)
		delete(c.byKey, key)
	}
}
