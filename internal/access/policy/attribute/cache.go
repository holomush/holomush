// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"container/list"
	"context"
	"sync"
)

type contextKey string

const (
	cacheContextKey contextKey = "attribute_cache"
	inResolutionKey contextKey = "in_resolution"
)

// cacheEntry represents a cached attribute map
type cacheEntry struct {
	key   string
	value map[string]any
}

// attributeCache is a simple LRU cache for attribute resolution
type attributeCache struct {
	mu       sync.RWMutex
	capacity int
	items    map[string]*list.Element
	lru      *list.List
}

// newAttributeCache creates a new LRU cache with the given capacity
func newAttributeCache(capacity int) *attributeCache {
	return &attributeCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		lru:      list.New(),
	}
}

// Get retrieves a value from the cache.
// Uses a full write lock because MoveToFront mutates the LRU list.
func (c *attributeCache) Get(key string) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		return nil, false
	}

	// Move to front (most recently used) — mutates list, requires write lock
	c.lru.MoveToFront(elem)

	entry, ok := elem.Value.(*cacheEntry)
	if !ok {
		return nil, false
	}
	return entry.value, true
}

// Put adds a value to the cache
func (c *attributeCache) Put(key string, value map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if elem, exists := c.items[key]; exists {
		c.lru.MoveToFront(elem)
		if entry, ok := elem.Value.(*cacheEntry); ok {
			entry.value = value
		}
		return
	}

	// Add new entry
	entry := &cacheEntry{key: key, value: value}
	elem := c.lru.PushFront(entry)
	c.items[key] = elem

	// Evict oldest if over capacity
	if c.lru.Len() > c.capacity {
		oldest := c.lru.Back()
		if oldest != nil {
			c.lru.Remove(oldest)
			if oldEntry, ok := oldest.Value.(*cacheEntry); ok {
				delete(c.items, oldEntry.key)
			}
		}
	}
}

// getCacheFromContext retrieves the cache from context, or creates a new one
func getCacheFromContext(ctx context.Context) *attributeCache {
	if cache, ok := ctx.Value(cacheContextKey).(*attributeCache); ok {
		return cache
	}
	return newAttributeCache(100)
}

// withCache returns a new context with a cache attached
func withCache(ctx context.Context) context.Context {
	cache := newAttributeCache(100)
	return context.WithValue(ctx, cacheContextKey, cache)
}

// isInResolution checks if resolution is already in progress
func isInResolution(ctx context.Context) bool {
	if inResolution, ok := ctx.Value(inResolutionKey).(bool); ok {
		return inResolution
	}
	return false
}

// markInResolution marks the context as being in resolution
func markInResolution(ctx context.Context) context.Context {
	return context.WithValue(ctx, inResolutionKey, true)
}
