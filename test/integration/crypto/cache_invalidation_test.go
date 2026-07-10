// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Multi-node end-to-end specs for the invalidation Coordinator. Each replica
// runs on its OWN *nats.Conn to a single external NATS container (via
// internal/testsupport/natstest), replacing the earlier shared in-process
// eventbustest connection — the exact gap CLUSTER-03 (D-05a) closes. Specs
// are prefixed with `// Verifies: INV-N` for the registry meta-test binding.
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
	"github.com/holomush/holomush/internal/testsupport/natstest"
)

// newCoordOnMember constructs an invalidation.Coordinator wired to member i
// of the external harness, using that member's OWN independent *nats.Conn
// (never a shared connection — Pitfall 2 / CLUSTER-03). Lifecycle is anchored
// to GinkgoT() so t.Cleanup-style teardown runs at the spec boundary.
func newCoordOnMember(
	h *clustertest.ExternalHarness,
	i int,
	cache *dek.Cache,
	partCache *dek.ParticipantsCache,
) invalidation.Coordinator {
	GinkgoHelper()
	coord, err := invalidation.New(invalidation.Config{
		ClusterID:         "test-game",
		InvalidateTimeout: 1 * time.Second,
	}, invalidation.Deps{
		Conn:      h.Members[i].Conn, // per-replica independent conn (was shared h.Embedded.Conn at :41)
		Registry:  h.Members[i].Registry,
		DEKCache:  cache,
		PartCache: partCache,
		Logger:    slog.Default(),
	})
	Expect(err).NotTo(HaveOccurred(), "invalidation.New")

	// Bound Start with a timeout so a wedged subscription doesn't hang the
	// spec deadline.
	startCtx, cancelStart := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelStart()
	Expect(coord.Start(startCtx)).To(Succeed(), "Coordinator.Start")

	// Surface Stop errors from cleanup; bound the call so a wedged shutdown
	// doesn't hang the spec runner.
	DeferCleanup(func() {
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelStop()
		// Best-effort: the hung-replica spec deliberately closes a member's
		// conn, so its Coordinator.Stop drain may fail — expected, not fatal.
		_ = coord.Stop(stopCtx)
	})
	return coord
}

var _ = Describe("Invalidation Coordinator (multi-node external NATS)", Ordered, func() {
	var env *natstest.NATSEnv

	BeforeAll(func() {
		var err error
		env, err = natstest.StartNATS(context.Background())
		Expect(err).NotTo(HaveOccurred(), "natstest.StartNATS")
	})

	AfterAll(func() {
		if env != nil {
			Expect(env.Terminate(context.Background())).To(Succeed(), "natstest env.Terminate")
		}
	})

	// Verifies: INV-CLUSTER-1
	Describe("KEK rotation requires all live replicas to ack within the 30s budget", func() {
		It("returns nil after a 3-replica kek_rotation collects N-of-N acks over independent connections", func() {
			h := clustertest.NewExternal(GinkgoT(), env, "test-game", 3)
			h.AwaitConverged(GinkgoT(), 5*time.Second)

			const n = 3
			caches := make([]*dek.Cache, n)
			partCaches := make([]*dek.ParticipantsCache, n)
			coords := make([]invalidation.Coordinator, n)
			for i := 0; i < n; i++ {
				caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				coords[i] = newCoordOnMember(h, i, caches[i], partCaches[i])
			}

			// KEK rotation is a no-op eviction but MUST collect N-of-N acks
			// (INV-CLUSTER-1, 30s budget). A missing ack from any replica on
			// its own connection would time out and fail.
			ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_KEK_ROTATION_MULTINODE"}
			Expect(coords[0].RequestInvalidation(
				context.Background(), ctxID, invalidation.ActionKEKRotation, 1, 2,
			)).To(Succeed(), "kek_rotation must collect N-of-N acks across independent conns")
		})
	})

	// Verifies: INV-CLUSTER-2
	Describe("rekey requires all live members to ack within timeout", func() {
		It("evicts every replica's DEK cache for the context after a 3-member rekey over independent conns", func() {
			h := clustertest.NewExternal(GinkgoT(), env, "test-game", 3)
			h.AwaitConverged(GinkgoT(), 5*time.Second)

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
			ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_REKEY_MULTINODE"}
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

			// Every replica's DEK cache should have evicted the context.
			for i := 0; i < n; i++ {
				_, ok := caches[i].Get(dek.CacheKey{KeyID: 1, Version: 1})
				Expect(ok).To(BeFalse(), "member %d DEK cache still has entry after rekey", i)
			}
		})
	})

	// Verifies: INV-CLUSTER-2 (single-replica degeneration)
	Describe("single-member cluster degenerates to self-ack", func() {
		It("returns nil after a Rekey publishes via NATS loopback to its own subscription", func() {
			h := clustertest.NewExternal(GinkgoT(), env, "test-game", 1)
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
		It("evicts every replica's ParticipantsCache for (ctxType, ctxID, version) on Add over independent conns", func() {
			h := clustertest.NewExternal(GinkgoT(), env, "test-game", 2)
			h.AwaitConverged(GinkgoT(), 5*time.Second)

			const n = 2
			caches := make([]*dek.Cache, n)
			partCaches := make([]*dek.ParticipantsCache, n)
			coords := make([]invalidation.Coordinator, n)
			for i := 0; i < n; i++ {
				caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				coords[i] = newCoordOnMember(h, i, caches[i], partCaches[i])
			}

			ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_ADD_MULTINODE"}
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

	// A hung/dead replica must not stall the cluster: probe-and-pill evicts it
	// and the rotation completes with N-1 (D-08). No // Verifies: annotation —
	// this is a behavioral D-08 proof, not a registry invariant binding.
	Describe("a hung replica triggers probe-and-pill and the rotation completes with N-1", func() {
		It("closes one replica's connection mid-flight and still completes rekey after evicting it", func() {
			h := clustertest.NewExternal(GinkgoT(), env, "test-game", 3)
			h.AwaitConverged(GinkgoT(), 5*time.Second)

			const n = 3
			caches := make([]*dek.Cache, n)
			partCaches := make([]*dek.ParticipantsCache, n)
			coords := make([]invalidation.Coordinator, n)
			for i := 0; i < n; i++ {
				caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
				coords[i] = newCoordOnMember(h, i, caches[i], partCaches[i])
			}

			// Simulate a hung/dead replica: close member 2's connection so its
			// Coordinator can neither receive the invalidation nor ack it.
			h.Members[2].Conn.Close()

			// Member 0 issues rekey; member 2 never acks → probe-and-pill fires
			// → member 2 is evicted → the retry proceeds with N-1 and the
			// rotation completes (D-08). Eventually absorbs the eviction /
			// per-member pill rate-limit window.
			ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_HUNG_REPLICA_N_MINUS_1"}
			Eventually(func() error {
				return coords[0].RequestInvalidation(
					context.Background(), ctxID, invalidation.ActionRekey, 1, 2,
				)
			}).WithTimeout(20*time.Second).WithPolling(500*time.Millisecond).Should(Succeed(),
				"rekey must complete with N-1 after the hung replica is probe-pilled")

			// The dead replica MUST be gone from member 0's live view.
			Eventually(func() int {
				return h.Members[0].Registry.LiveCount()
			}).WithTimeout(5*time.Second).Should(Equal(2),
				"member 0 must observe the hung replica evicted (N-1 live)")
		})
	})

	// Verifies: INV-CLUSTER-6 (single retry)
	Describe("Coordinator attempts at most one probe-and-pill retry cycle", func() {
		It("returns nil OR a typed timeout error (SELF_TIMEOUT|PARTIAL_FAILURE) when a synthetic peer never acks", func() {
			h := clustertest.NewExternal(GinkgoT(), env, "test-game", 1)
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
