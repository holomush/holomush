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

// DefaultCacheCapacity and DefaultCacheTTL are applied when CacheConfig
// fields are zero or negative. Per master spec §5.8.
const (
	DefaultCacheCapacity = 1024
	DefaultCacheTTL      = 5 * time.Minute
)

// applyDefaults returns cfg with non-positive fields replaced by the
// master-spec defaults. A negative Capacity would otherwise panic in
// make(...,Capacity); zero would silently discard every Put.
func (cfg CacheConfig) applyDefaults() CacheConfig {
	if cfg.Capacity <= 0 {
		cfg.Capacity = DefaultCacheCapacity
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultCacheTTL
	}
	return cfg
}

// Cache holds unwrapped DEK Material in process memory with LRU
// eviction and TTL safety net. INV-CRYPTO-16: MUST NOT live in NATS KV, PG,
// disk, or logs.
//
// The cache is internally synchronized for concurrent use. Phase 2's
// callers (DEKManager.GetOrCreate / Resolve) are concurrent.
//
// Phase 3c (holomush-ojw1.3) added the byContext reverse index so
// InvalidateContext can evict every (KeyID, Version) belonging to a
// ContextID in O(entries-for-context). The reverse index is part of
// the in-process state — INV-CRYPTO-16 is preserved (no serialization).
type Cache struct {
	cap   int
	ttl   time.Duration
	clock func() time.Time

	mu        sync.Mutex
	list      *list.List
	byKey     map[CacheKey]*list.Element
	byContext map[ContextID]map[CacheKey]struct{}
}

// cacheEntry now carries the contextID so eviction can clean the
// reverse index in O(1).
type cacheEntry struct {
	key       CacheKey
	contextID ContextID
	material  *Material
	expiresAt time.Time
}

// NewCache constructs a cache using time.Now as the clock.
func NewCache(cfg CacheConfig) *Cache {
	return NewCacheWithClock(cfg, time.Now)
}

// NewCacheWithClock allows tests to inject a deterministic clock.
// A nil clock falls back to time.Now so misconfigured callers get a
// working cache (Get/Put dereference clock unconditionally) instead of
// a runtime panic.
func NewCacheWithClock(cfg CacheConfig, clock func() time.Time) *Cache {
	cfg = cfg.applyDefaults()
	if clock == nil {
		clock = time.Now
	}
	return &Cache{
		cap:       cfg.Capacity,
		ttl:       cfg.TTL,
		clock:     clock,
		list:      list.New(),
		byKey:     make(map[CacheKey]*list.Element, cfg.Capacity),
		byContext: make(map[ContextID]map[CacheKey]struct{}),
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
		if set, sok := c.byContext[entry.contextID]; sok {
			delete(set, key)
			if len(set) == 0 {
				delete(c.byContext, entry.contextID)
			}
		}
		return nil, false
	}
	// LRU touch.
	c.list.MoveToFront(elem)
	return entry.material, true
}

// Put inserts or updates an entry. Evicts the LRU entry if over
// capacity. The ctxID parameter is recorded in the reverse index so
// InvalidateContext can evict every (KeyID, Version) for a ContextID
// in O(entries-for-context). Phase 3c (holomush-ojw1.3) added ctxID;
// callers (manager.go) thread the row's ContextID through.
func (c *Cache) Put(key CacheKey, ctxID ContextID, material *Material) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.byKey[key]; ok {
		// Update in place.
		if entry, ok := elem.Value.(*cacheEntry); ok {
			if entry.contextID != ctxID {
				// Defensive: the same (KeyID, Version) should not
				// move between contexts — crypto_keys.id is unique
				// per (context_type, context_id, version). But if a
				// test or future caller does it, keep the reverse
				// index consistent.
				if set, sok := c.byContext[entry.contextID]; sok {
					delete(set, key)
					if len(set) == 0 {
						delete(c.byContext, entry.contextID)
					}
				}
				entry.contextID = ctxID
				c.indexContextLocked(ctxID, key)
			}
			entry.material = material
			entry.expiresAt = c.clock().Add(c.ttl)
		}
		c.list.MoveToFront(elem)
		return
	}

	entry := &cacheEntry{
		key:       key,
		contextID: ctxID,
		material:  material,
		expiresAt: c.clock().Add(c.ttl),
	}
	elem := c.list.PushFront(entry)
	c.byKey[key] = elem
	c.indexContextLocked(ctxID, key)

	if c.list.Len() > c.cap {
		c.evictOldestLocked()
	}
}

// indexContextLocked records key under ctxID in the reverse index.
// Caller MUST hold c.mu.
func (c *Cache) indexContextLocked(ctxID ContextID, key CacheKey) {
	set, ok := c.byContext[ctxID]
	if !ok {
		set = make(map[CacheKey]struct{})
		c.byContext[ctxID] = set
	}
	set[key] = struct{}{}
}

// evictOldestLocked removes the LRU entry from the list, byKey, and
// byContext reverse index. Caller MUST hold c.mu.
func (c *Cache) evictOldestLocked() {
	oldest := c.list.Back()
	if oldest == nil {
		return
	}
	c.list.Remove(oldest)
	if entry, ok := oldest.Value.(*cacheEntry); ok {
		delete(c.byKey, entry.key)
		if set, sok := c.byContext[entry.contextID]; sok {
			delete(set, entry.key)
			if len(set) == 0 {
				delete(c.byContext, entry.contextID)
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
		if entry, ok := elem.Value.(*cacheEntry); ok {
			if set, sok := c.byContext[entry.contextID]; sok {
				delete(set, key)
				if len(set) == 0 {
					delete(c.byContext, entry.contextID)
				}
			}
		}
	}
}

// InvalidateContext removes every cached entry whose ContextID matches
// ctxID. Phase 3c (holomush-ojw1.3) added this method; the rekey
// action in invalidation.Coordinator (lands in T9/T10) calls it when
// a peer publishes a rekey for ctxID, ensuring stale unwrapped
// material does not survive a key rotation.
func (c *Cache) InvalidateContext(ctxID ContextID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	set, ok := c.byContext[ctxID]
	if !ok {
		return
	}
	for key := range set {
		if elem, ok := c.byKey[key]; ok {
			c.list.Remove(elem)
			delete(c.byKey, key)
		}
	}
	delete(c.byContext, ctxID)
}

// Len returns the number of entries currently in the cache. Used by
// Phase 3c tests to assert reverse-index cleanup after eviction.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list.Len()
}

// contextIndexLen returns the number of distinct ContextIDs currently
// indexed in the reverse map. Test-only: exposed so tests can verify
// the reverse index is cleaned by every eviction path (LRU,
// Invalidate, InvalidateContext, TTL-expiry). Production code MUST
// NOT depend on this — InvalidateContext's defensive byKey guard
// makes byContext leaks invisible at the public API.
func (c *Cache) contextIndexLen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.byContext)
}
