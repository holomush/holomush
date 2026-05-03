// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/cluster/clustertest"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
)

func setupReceiveTest(t *testing.T) (*dek.Cache, *dek.ParticipantsCache, invalidation.Coordinator) {
	h := clustertest.New(t, "test-game", 1)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
	coord, err := invalidation.New(invalidation.Config{
		ClusterID:         "test-game",
		InvalidateTimeout: 500 * time.Millisecond,
	}, invalidation.Deps{
		Conn:      h.Embedded.Conn,
		Registry:  h.Members[0].Registry,
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
	return cache, partCache, coord
}

func TestRekeyActionEvictsBothCachesForContext(t *testing.T) {
	cache, partCache, coord := setupReceiveTest(t)

	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_REKEY"}
	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxID, dek.NewMaterial(make([]byte, dek.DEKByteLength)))
	partCache.Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
		[]dek.Participant{{PlayerID: "01HALICE"}})

	if err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionRekey, 1, 2); err != nil {
		t.Fatalf("RequestInvalidation: %v", err)
	}

	if _, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1}); ok {
		t.Errorf("DEK cache entry still present after rekey")
	}
	if _, ok := partCache.Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}); ok {
		t.Errorf("participants cache entry still present after rekey")
	}
}

func TestParticipantsChangedActionEvictsOnlyTheGivenVersion(t *testing.T) {
	_, partCache, coord := setupReceiveTest(t)

	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_ADD"}
	partCache.Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
		[]dek.Participant{{PlayerID: "01HALICE"}})
	partCache.Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 2},
		[]dek.Participant{{PlayerID: "01HALICE"}, {PlayerID: "01HBOB"}})

	if err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionParticipantsChanged, 1, 0); err != nil {
		t.Fatalf("RequestInvalidation: %v", err)
	}

	if _, ok := partCache.Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}); ok {
		t.Errorf("v1 still present; expected eviction")
	}
	if _, ok := partCache.Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 2}); !ok {
		t.Errorf("v2 missing; only v1 should be evicted")
	}
}

func TestRotateActionAcksWithoutEvicting(t *testing.T) {
	cache, partCache, coord := setupReceiveTest(t)

	ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_ROTATE"}
	cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxID, dek.NewMaterial(make([]byte, dek.DEKByteLength)))
	partCache.Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
		[]dek.Participant{{PlayerID: "01HALICE"}})

	if err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionRotate, 1, 2); err != nil {
		t.Fatalf("RequestInvalidation: %v", err)
	}

	if _, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1}); !ok {
		t.Errorf("DEK cache entry evicted on rotate; should have been no-op")
	}
	if _, ok := partCache.Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}); !ok {
		t.Errorf("participants cache entry evicted on rotate; should have been no-op")
	}
}
