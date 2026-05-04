// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package tests for the dek.ParticipantsCache reverse index.
// These tests need the test-only contextIndexLen accessor (lowercase,
// package-private) to verify byContext cleanup on LRU eviction —
// invisible to the public API because InvalidateContext's defensive
// byKey guard would silently skip leaked entries (same shape as
// dek.Cache; see cache_internal_test.go).

package dek

import (
	"testing"
	"time"
)

// TestParticipantsCacheEvictOldestCleansReverseIndexAcrossContexts guards
// against a regression where Put's LRU eviction removes the entry from
// the list and byKey but leaks it into byContext. Because
// ParticipantsCacheKey embeds (ContextType, ContextID) by construction,
// a single key cannot be re-used across contexts (unlike CacheKey which
// is just (KeyID, Version)). The leak therefore manifests as an empty
// byContext entry surviving for the evicted ctxA after its only entry
// is LRU-evicted. contextIndexLen catches this.
func TestParticipantsCacheEvictOldestCleansReverseIndexAcrossContexts(t *testing.T) {
	c := NewParticipantsCache(CacheConfig{Capacity: 2, TTL: 5 * time.Minute})

	kA := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
	kB1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_B", Version: 1}
	kB2 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_B", Version: 2}

	c.Put(kA, newParticipants("01HALICE"))  // byContext[A]={kA}
	c.Put(kB1, newParticipants("01HBOB"))   // list=[kB1, kA]
	c.Put(kB2, newParticipants("01HCAROL")) // evicts kA (LRU)

	if got := c.contextIndexLen(); got != 1 {
		t.Errorf("contextIndexLen = %d; want 1 (only ctxB should remain after LRU eviction of ctxA's only entry)", got)
	}
	if got := c.Len(); got != 2 {
		t.Errorf("Len = %d; want 2", got)
	}
}
