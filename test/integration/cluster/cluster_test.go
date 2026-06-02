// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package cluster_test holds the Phase 3c (holomush-ojw1.3) multi-Registry
// integration tests. Each spec exercises the cluster substrate end-to-end
// against multiple cluster.Registry instances on a shared embedded NATS
// server (via clustertest.Harness). Specs are prefixed with
// `// Verifies: INV-N` so T14's meta-test can bind invariant numbers to
// test names.
//
// INV-CLUSTER-5 (production Pill os.Exit(125)) is exercised by a TestPill
// substitute in internal/cluster/probe_pill_test.go; a real subprocess
// harness for the production-Pill path is deferred to a follow-up.
package cluster_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/cluster/clustertest"
)

var _ = Describe("Cluster Registry", func() {
	// Verifies: INV-CLUSTER-3
	Describe("first-seen StartedAt preservation under duplicate MemberID", func() {
		It("rejects a duplicate heartbeat carrying a different StartedAt and HolomushVersion", func(ctx SpecContext) {
			h := clustertest.New(GinkgoT(), "test-game", 1)
			h.AwaitConverged(GinkgoT(), 1*time.Second)

			target := cluster.MemberID("01HSYN_DUP_INV53")

			// Inject the first heartbeat at StartedAt = T1.
			t1 := time.Now()
			p1 := cluster.HeartbeatPayload{
				ClusterID:       "test-game",
				MemberID:        target,
				StartedAt:       t1,
				PublishedAt:     t1,
				HolomushVersion: "first",
			}
			b1, err := cluster.MarshalHeartbeat(p1)
			Expect(err).NotTo(HaveOccurred(), "MarshalHeartbeat (first)")
			Expect(h.Embedded.Conn.Publish(cluster.SubjectAlive("test-game", target), b1)).To(Succeed())
			Expect(h.Embedded.Conn.Flush()).To(Succeed())
			h.AwaitMemberPresent(GinkgoT(), 0, target, 1*time.Second)

			// Sample initial state.
			first, ok := h.Members[0].Registry.Member(target)
			Expect(ok).To(BeTrue(), "first heartbeat did not register target member")
			Expect(first.StartedAt.Equal(t1)).To(BeTrue(), "StartedAt[1] = %v; want %v", first.StartedAt, t1)

			// Inject a duplicate heartbeat with a DIFFERENT StartedAt (T2).
			t2 := t1.Add(10 * time.Second)
			p2 := cluster.HeartbeatPayload{
				ClusterID:       "test-game",
				MemberID:        target,
				StartedAt:       t2,
				PublishedAt:     time.Now(),
				HolomushVersion: "duplicate",
			}
			b2, err := cluster.MarshalHeartbeat(p2)
			Expect(err).NotTo(HaveOccurred(), "MarshalHeartbeat (duplicate)")
			Expect(h.Embedded.Conn.Publish(cluster.SubjectAlive("test-game", target), b2)).To(Succeed())
			Expect(h.Embedded.Conn.Flush()).To(Succeed())

			// firstSeenPreserved combines presence + StartedAt + version
			// into a single boolean so Eventually/Consistently can assert
			// against it. The rejection should land within the receive
			// window AND remain stable for a follow-up polling window
			// (Consistently catches a bug where the duplicate later "wins").
			firstSeenPreserved := func() bool {
				m, ok := h.Members[0].Registry.Member(target)
				return ok && m.StartedAt.Equal(t1) && m.HolomushVersion == "first"
			}
			Eventually(ctx, firstSeenPreserved).
				WithTimeout(2*time.Second).
				Should(BeTrue(), "first-seen StartedAt + HolomushVersion should remain after duplicate heartbeat")
			Consistently(ctx, firstSeenPreserved).
				WithTimeout(300*time.Millisecond).
				Should(BeTrue(), "rejection of duplicate must be stable, not a transient state")
		})
	})

	// Verifies: INV-CLUSTER-4
	Describe("cluster_id namespace isolation", func() {
		It("never observes a foreign-cluster heartbeat in the local registry", func(ctx SpecContext) {
			h := clustertest.New(GinkgoT(), "test-game", 1)
			h.AwaitConverged(GinkgoT(), 1*time.Second)

			foreignID := cluster.MemberID("01HFOREIGN_PEER")
			h.PublishSyntheticHeartbeat(GinkgoT(), "OTHER-CLUSTER", foreignID, "")

			// Consistently asserts the foreign member never appears: a
			// regression that drops the cluster-id check would let it
			// through eventually.
			Consistently(ctx, func() bool {
				_, ok := h.Members[0].Registry.Member(foreignID)
				return ok
			}).WithTimeout(500 * time.Millisecond).Should(BeFalse())
		})
	})

	// Verifies: INV-CLUSTER-7
	Describe("pill rate-limiting", func() {
		It("blocks a duplicate pill within the rate-limit window", func() {
			h := clustertest.New(GinkgoT(), "test-game", 1)
			target := cluster.MemberID("01HSYN_RATE_LIMIT")
			h.PublishSyntheticHeartbeat(GinkgoT(), "test-game", target, "")
			h.AwaitMemberPresent(GinkgoT(), 0, target, 1*time.Second)

			// First pill: succeeds (probe times out, pill issued).
			Expect(h.Members[0].Registry.ProbeAndPill(
				context.Background(), target, cluster.PillReasonMissedInvalidationAck,
			)).To(Succeed(), "first pill should succeed")

			// Second pill within the window: rate-limited. Discriminate
			// by oops code; errors.Is against an OopsError sentinel is
			// tautological — see cluster.ErrPillRateLimited doc.
			err := h.Members[0].Registry.ProbeAndPill(
				context.Background(), target, cluster.PillReasonMissedInvalidationAck,
			)
			Expect(err).To(HaveOccurred())
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue(), "err = %v; want oops.OopsError", err)
			Expect(oopsErr.Code()).To(Equal("CLUSTER_PILL_RATE_LIMITED"))
		})
	})

	// Verifies: INV-CLUSTER-10
	Describe("INV-CLUSTER-10 self-pill refusal", func() {
		It("returns CLUSTER_CANNOT_PILL_SELF when target is Self()", func() {
			h := clustertest.New(GinkgoT(), "test-game", 1)
			err := h.Members[0].Registry.ProbeAndPill(
				context.Background(), h.Members[0].MemberID, cluster.PillReasonMissedInvalidationAck,
			)
			Expect(err).To(HaveOccurred())
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue(), "err = %v; want oops.OopsError", err)
			Expect(oopsErr.Code()).To(Equal("CLUSTER_CANNOT_PILL_SELF"))
		})
	})
})
