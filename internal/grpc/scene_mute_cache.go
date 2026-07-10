// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"sync"
	"time"
)

// SceneMuteChecker decides whether a non-focused member's SCENE_ACTIVITY badge
// downgrade should be suppressed for a given character. It is consulted at the
// single badge-downgrade chokepoint in dispatchDelivery so that a character's
// GLOBAL notify preference (SetSceneNotifyPref) and per-scene mute state
// actually silence the web badge and telnet nudge.
//
// The check is a PREFERENCE, not access control: callers MUST fail OPEN (deliver
// the frame) on a nil checker or any returned error. The downgraded frame is
// already content-free (INV-SCENE-62), so dropping it never leaks and delivering
// it on error only degrades the mute UX, never privacy.
type SceneMuteChecker interface {
	// ShouldSuppress reports whether the SCENE_ACTIVITY frame for (characterID,
	// sceneID) should be dropped. playerID is threaded to the backing loader so
	// it can build the host-vouched plugin dispatch (it is NOT part of the
	// cache key — state is cached per character). A non-nil error means the
	// state could not be resolved; the caller treats that as "deliver".
	ShouldSuppress(ctx context.Context, characterID, playerID, sceneID string) (bool, error)
}

// SceneMuteLoader fetches a character's notify state in one round-trip:
// globalEnabled is the character's global notify preference; mutedScenes is the
// set of scene ids the character has muted. The loader dispatches to the scene
// plugin with the host-vouched actor+ownerPlayerID (see cmd/holomush/sub_grpc.go).
type SceneMuteLoader = func(ctx context.Context, characterID, playerID string) (globalEnabled bool, mutedScenes []string, err error)

// NewSceneMuteChecker builds the per-character TTL cache that memoizes a
// character's {globalNotifyEnabled, mutedSet} for ttl. It is the ONLY
// construction seam callers outside this package use — the concrete cache type
// is unexported, so cmd/holomush (importing internal/grpc as holoGRPC) reaches
// it through this constructor and the SceneMuteChecker interface. now is
// injectable for tests; pass time.Now in production.
func NewSceneMuteChecker(loader SceneMuteLoader, ttl time.Duration, now func() time.Time) SceneMuteChecker {
	if now == nil {
		now = time.Now
	}
	return &sceneMuteCache{
		loader:  loader,
		ttl:     ttl,
		now:     now,
		entries: make(map[string]muteEntry),
	}
}

// muteEntry is a character's memoized notify state plus its fetch time.
type muteEntry struct {
	globalNotifyEnabled bool
	mutedSet            map[string]struct{}
	fetchedAt           time.Time
}

// sceneMuteCache is the per-character TTL cache implementing SceneMuteChecker.
// It keeps the hot delivery loop free of a per-event RPC: at most one loader
// refresh per character per TTL window.
type sceneMuteCache struct {
	loader SceneMuteLoader
	ttl    time.Duration
	now    func() time.Time

	mu        sync.Mutex
	entries   map[string]muteEntry
	lastSweep time.Time
}

// ShouldSuppress answers from the memoized entry when it is fresh, otherwise
// refreshes via the loader. The loader is never called under the lock, so a slow
// plugin dispatch cannot block concurrent deliveries for other characters. On a
// loader error the answer is (false, err) — fail-open — and the prior entry is
// left untouched (the error is never cached).
func (c *sceneMuteCache) ShouldSuppress(ctx context.Context, characterID, playerID, sceneID string) (bool, error) {
	c.mu.Lock()
	entry, ok := c.entries[characterID]
	fresh := ok && c.now().Sub(entry.fetchedAt) < c.ttl
	c.mu.Unlock()

	if fresh {
		return decideSuppress(entry, sceneID), nil
	}

	globalEnabled, mutedScenes, err := c.loader(ctx, characterID, playerID)
	if err != nil {
		// Fail-open: surface the error, deliver the frame, do not poison.
		return false, err
	}

	mutedSet := make(map[string]struct{}, len(mutedScenes))
	for _, id := range mutedScenes {
		mutedSet[id] = struct{}{}
	}
	refreshed := muteEntry{
		globalNotifyEnabled: globalEnabled,
		mutedSet:            mutedSet,
		fetchedAt:           c.now(),
	}

	c.mu.Lock()
	c.entries[characterID] = refreshed
	c.evictExpiredLocked(refreshed.fetchedAt)
	c.mu.Unlock()

	return decideSuppress(refreshed, sceneID), nil
}

// evictExpiredLocked prunes entries older than the TTL so the cache stays bounded
// to characters seen within one TTL window, rather than growing for the process
// lifetime with every distinct character that ever hit ShouldSuppress. It is
// rate-limited to at most one sweep per TTL window (the refresh path already
// holds the lock, and expired entries are re-fetched anyway). Any character's
// refresh cleans up ALL expired entries, so a one-shot character's stale entry is
// reclaimed by the next refresh of any character. Caller MUST hold c.mu.
func (c *sceneMuteCache) evictExpiredLocked(now time.Time) {
	if now.Sub(c.lastSweep) < c.ttl {
		return
	}
	for id, e := range c.entries {
		if now.Sub(e.fetchedAt) >= c.ttl {
			delete(c.entries, id)
		}
	}
	c.lastSweep = now
}

// decideSuppress composes the two suppression signals in the pinned order:
// global-notify-off suppresses every scene; otherwise a scene in the muted set
// is suppressed; otherwise the frame is delivered.
func decideSuppress(entry muteEntry, sceneID string) bool {
	if !entry.globalNotifyEnabled {
		return true
	}
	_, muted := entry.mutedSet[sceneID]
	return muted
}
