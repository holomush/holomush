// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import "time"

// NewCacheWithPoliciesForTest creates a Cache with pre-compiled policies for testing.
// This is a test helper that bypasses the normal store/compile flow and directly
// sets the cache snapshot. It marks the cache as non-stale by setting lastUpdate.
//
// This function is intended for use in migration equivalence tests and other
// tests that need to compare static and policy engine behavior without database setup.
func NewCacheWithPoliciesForTest(policies []CachedPolicy) *Cache {
	cache := NewCache(nil, nil)

	// Manually set the snapshot
	cache.mu.Lock()
	cache.snapshot = &Snapshot{
		Policies:  policies,
		CreatedAt: time.Now(),
	}
	cache.mu.Unlock()

	// Mark as non-stale
	cache.lastUpdate.Store(time.Now().UnixNano())

	return cache
}
