// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Phase 3c (holomush-ojw1.3) end-to-end multi-Registry specs for the
// invalidation Coordinator. Each spec wires up clustertest.Harness with
// per-member isolated DEKCache + ParticipantsCache + Coordinator, then
// exercises invalidation flows across replicas. Specs are prefixed with
// `// Verifies: INV-N` for T14's meta-test binding.
package crypto_test

import (
	"context"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/cluster/clustertest"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
)

// newCoordOnMember constructs an invalidation.Coordinator wired to
// member i of the harness. Lifecycle is anchored to GinkgoT() so
// t.Cleanup-style teardown runs at the spec boundary.
func newCoordOnMember(
	h *clustertest.Harness,
	i int,
	cache *dek.Cache,
	partCache *dek.ParticipantsCache,
) invalidation.Coordinator {
	GinkgoHelper()
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
	Expect(err).NotTo(HaveOccurred(), "invalidation.New")

	// Bound Start with a timeout so a wedged subscription doesn't
	// hang the spec deadline.
	startCtx, cancelStart := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelStart()
	Expect(coord.Start(startCtx)).To(Succeed(), "Coordinator.Start")

	// Surface Stop errors from cleanup; bound the call so a wedged
	// shutdown doesn't hang the spec runner.
	DeferCleanup(func() {
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelStop()
		Expect(coord.Stop(stopCtx)).To(Succeed(), "Coordinator.Stop")
	})
	return coord
}

var _ = Describe("Invalidation Coordinator", func() {
	// Verifies: INV-CLUSTER-2
	Describe("rekey requires all live members to ack within timeout", func() {
		It("evicts every replica's DEK cache for the context after a 3-member rekey", func() {
			h := clustertest.New(GinkgoT(), "test-game", 3)
			h.AwaitConverged(GinkgoT(), 2*time.Second)

			const n = 3
			coords := make([]invalidation.Coordinator, n)
			caches := make([]*dek.Cache, n)
			partCaches := make([]*dek.ParticipantsCache, n)
			for i := 0; i < n; i++ {
				caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				coords[i] = newCoordOnMember(h, i, caches[i], partCaches[i])
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
			Expect(coords[0].RequestInvalidation(
				context.Background(), ctxID, invalidation.ActionRekey, 1, 2,
			)).To(Succeed(), "RequestInvalidation rekey")

			// All caches should have evicted the context.
			for i := 0; i < n; i++ {
				_, ok := caches[i].Get(dek.CacheKey{KeyID: 1, Version: 1})
				Expect(ok).To(BeFalse(), "member %d DEK cache still has entry after rekey", i)
			}
		})
	})

	// Verifies: INV-CLUSTER-2 (single-replica degeneration)
	Describe("single-member cluster degenerates to self-ack", func() {
		It("returns nil after a Rekey publishes via NATS loopback to its own subscription", func() {
			h := clustertest.New(GinkgoT(), "test-game", 1)
			cache := dek.NewCache(dek.CacheConfig{})
			partCache := dek.NewParticipantsCache(dek.CacheConfig{})
			coord := newCoordOnMember(h, 0, cache, partCache)

			ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_SOLO"}
			Expect(coord.RequestInvalidation(
				context.Background(), ctxID, invalidation.ActionRekey, 1, 2,
			)).To(Succeed(), "single-member RequestInvalidation should self-ack via NATS loopback")
		})
	})

	// Verifies: INV-CLUSTER-9
	// Verifies: INV-CRYPTO-7 (read-immediacy substrate)
	Describe("participants_changed propagates within timeout", func() {
		It("evicts every replica's ParticipantsCache for (ctxType, ctxID, version) on Add", func() {
			h := clustertest.New(GinkgoT(), "test-game", 2)
			h.AwaitConverged(GinkgoT(), 2*time.Second)

			const n = 2
			caches := make([]*dek.Cache, n)
			partCaches := make([]*dek.ParticipantsCache, n)
			coords := make([]invalidation.Coordinator, n)
			for i := 0; i < n; i++ {
				caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				coords[i] = newCoordOnMember(h, i, caches[i], partCaches[i])
			}

			ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_ADD_E2E"}
			pck := dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}
			// Both replicas have stale participants cached.
			partCaches[0].Put(pck, []dek.Participant{{PlayerID: "01HALICE"}})
			partCaches[1].Put(pck, []dek.Participant{{PlayerID: "01HALICE"}})

			// Simulate Add() on member 0: publish participants_changed for v1.
			Expect(coords[0].RequestInvalidation(
				context.Background(), ctxID, invalidation.ActionParticipantsChanged, 1, 0,
			)).To(Succeed(), "RequestInvalidation participants_changed")

			// INV-CLUSTER-9: every replica's ParticipantsCache for this version
			// MUST have no entry upon return.
			_, ok0 := partCaches[0].Get(pck)
			Expect(ok0).To(BeFalse(), "member 0 still has cached participants after RequestInvalidation")
			_, ok1 := partCaches[1].Get(pck)
			Expect(ok1).To(BeFalse(), "member 1 still has cached participants after RequestInvalidation")
		})
	})

	// Verifies: INV-CLUSTER-6 (single retry)
	Describe("Coordinator attempts at most one probe-and-pill retry cycle", func() {
		It("returns nil OR a typed timeout error (SELF_TIMEOUT|PARTIAL_FAILURE) when a synthetic peer never acks", func() {
			h := clustertest.New(GinkgoT(), "test-game", 1)
			cache := dek.NewCache(dek.CacheConfig{})
			partCache := dek.NewParticipantsCache(dek.CacheConfig{})
			coord := newCoordOnMember(h, 0, cache, partCache)

			// Inject a synthetic peer that won't ack and won't respond to probes.
			target := h.Members[0].MemberID + "_NOT"
			h.PublishSyntheticHeartbeat(GinkgoT(), "test-game", target, "")
			h.AwaitMemberPresent(GinkgoT(), 0, target, 1*time.Second)

			ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_RETRY_LIMIT"}
			err := coord.RequestInvalidation(
				context.Background(), ctxID, invalidation.ActionRekey, 1, 2,
			)
			if err == nil {
				// Pill issued + retry succeeded after eviction; acceptable.
				return
			}
			// Discriminate by oops code; errors.Is against oops sentinels
			// is tautological — see invalidation.ErrSelfTimeout doc.
			oerr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue(),
				"err = %v; want oops error (INVALIDATION_SELF_TIMEOUT or INVALIDATION_PARTIAL_FAILURE)", err)
			Expect(oerr.Code()).To(SatisfyAny(
				Equal("INVALIDATION_SELF_TIMEOUT"),
				Equal("INVALIDATION_PARTIAL_FAILURE"),
			), "single-retry semantics: outcome must be self-timeout or partial-failure")
		})
	})
})
