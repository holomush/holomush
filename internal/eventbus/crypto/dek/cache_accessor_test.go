// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
)

// TestManagerSatisfiesCacheAccessor verifies that the concrete Manager
// returned by dek.NewManager satisfies dek.CacheAccessor — the interface
// production wiring uses to wire invalidation.Coordinator's eviction
// callbacks to the Manager's own caches (Phase 3c grounding doc Decision
// 5; the multi-replica forward-secrecy regression fix).
//
// Without this interface, cmd/holomush/core.go was constructing the
// Coordinator with NEW caches dedicated to the receive-side handler
// (different instances from those the Manager uses); peers would continue
// serving stale OLD DEK from the Manager's cache for up to the cache TTL
// (~5min) after a cross-replica Rekey completed.
func TestManagerSatisfiesCacheAccessor(t *testing.T) {
	noopInvalidator := func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error {
		return nil
	}

	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

	mgr, err := dek.NewManager(
		kek.NewNoneProviderForUnitTest(),
		&dek.Store{},
		cache,
		partCache,
		noopInvalidator,
		&stubBindingResolver{},
	)
	require.NoError(t, err)

	accessor, ok := mgr.(dek.CacheAccessor)
	require.True(t, ok, "dek.NewManager return value MUST satisfy dek.CacheAccessor — required for invalidation.Coordinator wiring per Phase 3c grounding Decision 5")

	assert.Same(t, cache, accessor.Cache(),
		"CacheAccessor.Cache() MUST return the SAME *Cache instance passed to NewManager — wiring relies on identity, not equality")
	assert.Same(t, partCache, accessor.PartCache(),
		"CacheAccessor.PartCache() MUST return the SAME *ParticipantsCache instance passed to NewManager")
}

// TestCacheAccessor_EnablesCrossReplicaEviction asserts the behaviour
// the accessor was added for: an external caller that holds a Manager
// can reach the underlying *Cache + *ParticipantsCache and observe
// InvalidateContext eviction. This mirrors what invalidation.Coordinator's
// receive-side handler does on a peer when a different replica drives a
// Rekey. The pre-fix wiring constructed the Coordinator with DEDICATED
// caches; this test would have passed against those caches yet the
// Manager's own caches stayed populated — surfacing the forward-secrecy
// regression. With the accessor wiring, the Coordinator and the Manager
// share cache identity, so this test exercises the production path.
func TestCacheAccessor_EnablesCrossReplicaEviction(t *testing.T) {
	noopInvalidator := func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error {
		return nil
	}

	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

	mgr, err := dek.NewManager(
		kek.NewNoneProviderForUnitTest(),
		&dek.Store{},
		cache,
		partCache,
		noopInvalidator,
		&stubBindingResolver{},
	)
	require.NoError(t, err)

	accessor := mgr.(dek.CacheAccessor)

	// Seed the Manager's caches as if a normal Resolve had populated
	// them. Then a peer-driven invalidation event would land in the
	// receive-side handler and invoke InvalidateContext on the SAME
	// cache pointer the accessor returns.
	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_FS_REGRESSION"}
	accessor.Cache().Put(
		dek.CacheKey{KeyID: codec.KeyID(42), Version: 1},
		ctxID,
		dek.NewMaterial(make([]byte, dek.DEKByteLength)),
	)
	accessor.PartCache().Put(
		dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
		[]dek.Participant{{PlayerID: "01HALICE"}},
	)

	// Simulate the Coordinator's receive callback (coordinator.go's
	// handleInvalidate calls these two methods on Deps.DEKCache/PartCache
	// for ActionRekey).
	accessor.Cache().InvalidateContext(ctxID)
	accessor.PartCache().InvalidateContext(ctxID)

	if _, ok := accessor.Cache().Get(dek.CacheKey{KeyID: codec.KeyID(42), Version: 1}); ok {
		t.Errorf("Manager's DEK cache entry STILL present after InvalidateContext — pre-fix wiring's forward-secrecy regression")
	}
	if _, ok := accessor.PartCache().Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}); ok {
		t.Errorf("Manager's participants cache entry STILL present after InvalidateContext — pre-fix wiring's forward-secrecy regression")
	}
}
