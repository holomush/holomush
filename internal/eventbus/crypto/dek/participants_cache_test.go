// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"testing"
	"time"
)

func newParticipants(playerIDs ...string) []Participant {
	out := make([]Participant, len(playerIDs))
	for i, p := range playerIDs {
		out[i] = Participant{PlayerID: p, JoinedAt: time.Now()}
	}
	return out
}

func TestParticipantsCachePutAndGetRoundTrip(t *testing.T) {
	c := NewParticipantsCache(CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
	key := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
	in := newParticipants("01HALICE", "01HBOB")

	c.Put(key, in)
	out, ok := c.Get(key)
	if !ok {
		t.Fatal("Get returned not-found after Put")
	}
	if len(out) != 2 {
		t.Errorf("len(out) = %d; want 2", len(out))
	}
}

func TestParticipantsCacheInvalidateContextRemovesAllVersions(t *testing.T) {
	c := NewParticipantsCache(CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
	ctxA1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
	ctxA2 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 2}
	ctxB1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_B", Version: 1}

	c.Put(ctxA1, newParticipants("01HALICE"))
	c.Put(ctxA2, newParticipants("01HALICE", "01HBOB"))
	c.Put(ctxB1, newParticipants("01HCAROL"))

	c.InvalidateContext(ContextID{Type: "scene", ID: "01HSCENE_A"})

	if _, ok := c.Get(ctxA1); ok {
		t.Errorf("ctxA v1 still present")
	}
	if _, ok := c.Get(ctxA2); ok {
		t.Errorf("ctxA v2 still present")
	}
	if _, ok := c.Get(ctxB1); !ok {
		t.Errorf("ctxB v1 missing; only ctxA should be evicted")
	}
}

func TestParticipantsCacheInvalidateRemovesSingleVersion(t *testing.T) {
	c := NewParticipantsCache(CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
	k1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
	k2 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 2}

	c.Put(k1, newParticipants("01HALICE"))
	c.Put(k2, newParticipants("01HALICE", "01HBOB"))

	c.Invalidate(k1)

	if _, ok := c.Get(k1); ok {
		t.Errorf("k1 still present after Invalidate(k1)")
	}
	if _, ok := c.Get(k2); !ok {
		t.Errorf("k2 missing; Invalidate(k1) must not affect k2")
	}
}

func TestParticipantsCacheTTLExpiry(t *testing.T) {
	fakeNow := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fakeNow }
	c := NewParticipantsCacheWithClock(CacheConfig{Capacity: 10, TTL: 1 * time.Minute}, clock)

	key := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
	c.Put(key, newParticipants("01HALICE"))

	fakeNow = fakeNow.Add(2 * time.Minute) // past TTL
	if _, ok := c.Get(key); ok {
		t.Errorf("expired entry returned by Get")
	}
}

func TestParticipantsCacheLRUEviction(t *testing.T) {
	c := NewParticipantsCache(CacheConfig{Capacity: 2, TTL: 5 * time.Minute})
	k1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HA", Version: 1}
	k2 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HB", Version: 1}
	k3 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HC", Version: 1}

	c.Put(k1, newParticipants("01HALICE"))
	c.Put(k2, newParticipants("01HBOB"))
	c.Put(k3, newParticipants("01HCAROL")) // evicts k1 (LRU)

	if _, ok := c.Get(k1); ok {
		t.Errorf("k1 still present; expected LRU eviction")
	}
	if _, ok := c.Get(k2); !ok {
		t.Errorf("k2 missing")
	}
	if _, ok := c.Get(k3); !ok {
		t.Errorf("k3 missing")
	}
}
