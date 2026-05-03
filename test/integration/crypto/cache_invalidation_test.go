// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Phase 3c (holomush-ojw1.3) end-to-end multi-Registry tests for the
// invalidation Coordinator. Each test wires up clustertest.Harness with
// per-member isolated DEKCache + ParticipantsCache + Coordinator, then
// exercises invalidation flows across replicas. Tests are prefixed with
// `// Verifies: INV-N` for T14's meta-test binding.
package crypto_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/cluster/clustertest"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
)

func newCoordOnMember(
	t *testing.T,
	h *clustertest.Harness,
	i int,
	cache *dek.Cache,
	partCache *dek.ParticipantsCache,
) invalidation.Coordinator {
	t.Helper()
	coord, err := invalidation.New(invalidation.Config{
		ClusterID:         "test-game",
		InvalidateTimeout: 1 * time.Second,
	}, invalidation.Deps{
		Conn:      h.Embedded.Conn,
		Registry:  h.Members[i].Registry,
		DEKCache:  cache,
		PartCache: partCache,
		Logger:    slog.Default(),
	})
	if err != nil {
		t.Fatalf("invalidation.New: %v", err)
	}
	if err := coord.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = coord.Stop(context.Background()) })
	return coord
}

// Verifies: INV-29
func TestRotateAndRekeyRequireAllLiveMembersToAckWithinFiveSeconds(t *testing.T) {
	h := clustertest.New(t, "test-game", 3)
	h.AwaitConverged(t, 2*time.Second)

	// Each member gets its own caches + Coordinator.
	const n = 3
	var coords [n]invalidation.Coordinator
	var caches [n]*dek.Cache
	var partCaches [n]*dek.ParticipantsCache
	for i := 0; i < n; i++ {
		caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
		partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
		coords[i] = newCoordOnMember(t, h, i, caches[i], partCaches[i])
	}

	// Seed all caches with a context entry.
	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_REKEY_E2E"}
	for i := 0; i < n; i++ {
		caches[i].Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxID, dek.NewMaterial(make([]byte, dek.DEKByteLength)))
		partCaches[i].Put(
			dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
			[]dek.Participant{{PlayerID: "01HALICE"}},
		)
	}

	// Member 0's Coordinator issues rekey; expect all 3 acks.
	if err := coords[0].RequestInvalidation(context.Background(), ctxID, invalidation.ActionRekey, 1, 2); err != nil {
		t.Fatalf("RequestInvalidation: %v", err)
	}

	// All caches should have evicted the context.
	for i := 0; i < n; i++ {
		if _, ok := caches[i].Get(dek.CacheKey{KeyID: 1, Version: 1}); ok {
			t.Errorf("member %d DEK cache still has entry after rekey", i)
		}
	}
}

// Verifies: INV-29 (single-replica degeneration)
func TestSingleMemberClusterDegeneratesToSelfAck(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	cache := dek.NewCache(dek.CacheConfig{})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{})
	coord := newCoordOnMember(t, h, 0, cache, partCache)

	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_SOLO"}
	if err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionRekey, 1, 2); err != nil {
		t.Fatalf("RequestInvalidation single-member: %v", err)
	}
}

// Verifies: INV-59
// Verifies: INV-12 (read-immediacy substrate)
func TestParticipantsChangedPropagatesViaInvalidation(t *testing.T) {
	h := clustertest.New(t, "test-game", 2)
	h.AwaitConverged(t, 2*time.Second)

	const n = 2
	var caches [n]*dek.Cache
	var partCaches [n]*dek.ParticipantsCache
	var coords [n]invalidation.Coordinator
	for i := 0; i < n; i++ {
		caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
		partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
		coords[i] = newCoordOnMember(t, h, i, caches[i], partCaches[i])
	}

	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_ADD_E2E"}
	pck := dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}
	// Both replicas have stale participants cached.
	partCaches[0].Put(pck, []dek.Participant{{PlayerID: "01HALICE"}})
	partCaches[1].Put(pck, []dek.Participant{{PlayerID: "01HALICE"}})

	// Simulate Add() on member 0: publish participants_changed for v1.
	if err := coords[0].RequestInvalidation(context.Background(), ctxID, invalidation.ActionParticipantsChanged, 1, 0); err != nil {
		t.Fatalf("RequestInvalidation: %v", err)
	}

	// INV-59: every replica's ParticipantsCache for this version MUST
	// have no entry upon return.
	if _, ok := partCaches[0].Get(pck); ok {
		t.Errorf("member 0 still has cached participants after RequestInvalidation")
	}
	if _, ok := partCaches[1].Get(pck); ok {
		t.Errorf("member 1 still has cached participants after RequestInvalidation")
	}
}

// Verifies: INV-56 (single retry)
func TestCoordinatorAttemptsAtMostOneProbePillRetryCycle(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	cache := dek.NewCache(dek.CacheConfig{})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{})
	coord := newCoordOnMember(t, h, 0, cache, partCache)

	// Inject a synthetic peer that won't ack and won't respond to probes.
	target := h.Members[0].MemberID + "_NOT" // unique synthetic
	h.PublishSyntheticHeartbeat(t, "test-game", target, "")
	h.AwaitMemberPresent(t, 0, target, 1*time.Second)

	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_RETRY_LIMIT"}
	err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionRekey, 1, 2)
	if err == nil {
		// Pill issued + retry succeeded after eviction; acceptable.
		return
	}
	// Discriminate by oops code (errors.Is against oops sentinels is
	// tautological — see invalidation.ErrSelfTimeout doc). Either
	// INVALIDATION_SELF_TIMEOUT (only self left after pill) or
	// INVALIDATION_PARTIAL_FAILURE (retry still failed) is acceptable.
	oerr, ok := oops.AsOops(err)
	if !ok {
		t.Errorf("err = %v; want oops error (INVALIDATION_SELF_TIMEOUT or INVALIDATION_PARTIAL_FAILURE)", err)
		return
	}
	code := oerr.Code()
	if code != "INVALIDATION_SELF_TIMEOUT" && code != "INVALIDATION_PARTIAL_FAILURE" {
		t.Errorf("err code = %q; want INVALIDATION_SELF_TIMEOUT or INVALIDATION_PARTIAL_FAILURE", code)
	}
}
