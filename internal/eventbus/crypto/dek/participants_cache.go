// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"container/list"
	"sync"
	"time"
)

// ParticipantsCacheKey is the (context_type, context_id, version)
// composite key for the participant-set cache. Version is part of
// the key because Rotate creates vN+1 with a different participant
// list while vN's set stays unchanged; per-version pinning matches
// the per-event (KeyID, Version) pinning the AAD already provides.
type ParticipantsCacheKey struct {
	ContextType string
	ContextID   string
	Version     uint32
}

// ParticipantsCache holds per-version participant lists with LRU + TTL.
// Symmetric to dek.Cache; separate type because the value shape differs
// ([]Participant vs *Material) and because eviction semantics differ
// (participants invalidate per-version on Add via participants_changed
// action; DEK material invalidates per-context on Rekey).
//
// Phase 3c grounding doc Decision 3 + INV-CLUSTER-9 + INV-CRYPTO-16.
type ParticipantsCache struct {
	cap   int
	ttl   time.Duration
	clock func() time.Time

	mu        sync.Mutex
	list      *list.List
	byKey     map[ParticipantsCacheKey]*list.Element
	byContext map[ContextID]map[ParticipantsCacheKey]struct{}
}

type participantsEntry struct {
	key       ParticipantsCacheKey
	contextID ContextID
	list      []Participant
	expiresAt time.Time
}

// NewParticipantsCache constructs a ParticipantsCache with the master-spec
// defaults applied to zero/negative CacheConfig fields (capacity 1024,
// TTL 5min). Uses time.Now as the clock.
func NewParticipantsCache(cfg CacheConfig) *ParticipantsCache {
	return NewParticipantsCacheWithClock(cfg, time.Now)
}

// NewParticipantsCacheWithClock is the test seam for deterministic TTL
// expiry tests; production code uses NewParticipantsCache.
func NewParticipantsCacheWithClock(cfg CacheConfig, clock func() time.Time) *ParticipantsCache {
	cfg = cfg.applyDefaults()
	if clock == nil {
		clock = time.Now
	}
	return &ParticipantsCache{
		cap:       cfg.Capacity,
		ttl:       cfg.TTL,
		clock:     clock,
		list:      list.New(),
		byKey:     make(map[ParticipantsCacheKey]*list.Element, cfg.Capacity),
		byContext: make(map[ContextID]map[ParticipantsCacheKey]struct{}),
	}
}

// Get returns the participant list for key if present and unexpired.
// Promotes the entry to MRU on hit. Lazy-evicts on TTL expiry.
//
// Returns a defensive copy: the cache owns its slice, and callers MAY
// freely mutate the returned slice without corrupting cached state or
// racing with concurrent Put/Get callers.
func (c *ParticipantsCache) Get(key ParticipantsCacheKey) ([]Participant, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.byKey[key]
	if !ok {
		return nil, false
	}
	entry, ok := elem.Value.(*participantsEntry)
	if !ok {
		return nil, false
	}
	if c.clock().After(entry.expiresAt) {
		c.removeLocked(elem, entry)
		return nil, false
	}
	c.list.MoveToFront(elem)
	out := append([]Participant(nil), entry.list...)
	return out, true
}

// Put inserts or refreshes the participant list for key. Evicts the LRU
// entry when capacity is exceeded (cleaning byContext on the eviction
// path so InvalidateContext stays correct).
//
// Stores a defensive copy of the supplied slice: callers MAY continue
// to mutate `participants` after Put returns without corrupting cached
// state.
func (c *ParticipantsCache) Put(key ParticipantsCacheKey, participants []Participant) {
	ctxID := ContextID{Type: key.ContextType, ID: key.ContextID}
	stored := append([]Participant(nil), participants...)
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.byKey[key]; ok {
		if entry, ok := elem.Value.(*participantsEntry); ok {
			entry.list = stored
			entry.expiresAt = c.clock().Add(c.ttl)
		}
		c.list.MoveToFront(elem)
		return
	}

	entry := &participantsEntry{
		key:       key,
		contextID: ctxID,
		list:      stored,
		expiresAt: c.clock().Add(c.ttl),
	}
	elem := c.list.PushFront(entry)
	c.byKey[key] = elem
	set, ok := c.byContext[ctxID]
	if !ok {
		set = make(map[ParticipantsCacheKey]struct{})
		c.byContext[ctxID] = set
	}
	set[key] = struct{}{}

	if c.list.Len() > c.cap {
		oldest := c.list.Back()
		if oldest != nil {
			if oldEntry, ok := oldest.Value.(*participantsEntry); ok {
				c.removeLocked(oldest, oldEntry)
			}
		}
	}
}

// Invalidate removes a single (ContextType, ContextID, Version) entry.
// No-op if absent. Used by tests and selective invalidation paths.
func (c *ParticipantsCache) Invalidate(key ParticipantsCacheKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.byKey[key]; ok {
		if entry, ok := elem.Value.(*participantsEntry); ok {
			c.removeLocked(elem, entry)
		}
	}
}

// InvalidateContext removes every cached version for ctxID. This is the
// hook the rekey action calls when a participants_changed audit row
// lands; participants for vN are stable, but the active version moves
// to vN+1 with a different set, so the safe action is to drop all
// cached versions for the affected context.
func (c *ParticipantsCache) InvalidateContext(ctxID ContextID) {
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

// removeLocked drops elem from the LRU list, byKey, and byContext.
// Caller MUST hold c.mu. Cleans the byContext set for the entry's
// context, deleting the empty set so contextIndexLen reflects only
// live contexts.
func (c *ParticipantsCache) removeLocked(elem *list.Element, entry *participantsEntry) {
	c.list.Remove(elem)
	delete(c.byKey, entry.key)
	if set, ok := c.byContext[entry.contextID]; ok {
		delete(set, entry.key)
		if len(set) == 0 {
			delete(c.byContext, entry.contextID)
		}
	}
}

// Len returns the number of live cache entries. Primarily used by tests.
func (c *ParticipantsCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list.Len()
}

// contextIndexLen returns the number of distinct ContextIDs in the
// reverse index. Test-only — see participants_cache_internal_test.go.
// Mirrors dek.Cache.contextIndexLen so the LRU-eviction-leaks-byContext
// shape that's invisible at the public API can be regression-tested.
func (c *ParticipantsCache) contextIndexLen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.byContext)
}
