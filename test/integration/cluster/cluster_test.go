// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package cluster_test holds the Phase 3c (holomush-ojw1.3) multi-Registry
// integration tests. Each test exercises the cluster substrate end-to-end
// against multiple cluster.Registry instances on a shared embedded NATS
// server (via clustertest.Harness). Tests are prefixed with
// `// Verifies: INV-N` so T14's meta-test can bind invariant numbers to
// test names.
//
// INV-55 (production Pill os.Exit(125)) is exercised by a TestPill
// substitute in internal/cluster/probe_pill_test.go; a real subprocess
// harness for the production-Pill path is deferred to a follow-up.
package cluster_test

import (
	"context"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/cluster/clustertest"
	"github.com/holomush/holomush/pkg/errutil"
)

// Verifies: INV-53
func TestRegistryRejectsDuplicateMemberIDFromDifferentStartedAt(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	h.AwaitConverged(t, 1*time.Second)

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
	if err != nil {
		t.Fatalf("MarshalHeartbeat (first): %v", err)
	}
	if err := h.Embedded.Conn.Publish(cluster.SubjectAlive("test-game", target), b1); err != nil {
		t.Fatalf("publish first heartbeat: %v", err)
	}
	if err := h.Embedded.Conn.Flush(); err != nil {
		t.Fatalf("flush after first heartbeat: %v", err)
	}
	h.AwaitMemberPresent(t, 0, target, 1*time.Second)

	// Sample initial state.
	first, ok := h.Members[0].Registry.Member(target)
	if !ok {
		t.Fatal("first heartbeat did not register target member")
	}
	if !first.StartedAt.Equal(t1) {
		t.Errorf("StartedAt[1] = %v; want %v", first.StartedAt, t1)
	}

	// Inject a duplicate heartbeat with a DIFFERENT StartedAt (T2).
	// Per INV-53, the duplicate MUST be rejected: registry's view of
	// StartedAt MUST stay at T1.
	t2 := t1.Add(10 * time.Second)
	p2 := cluster.HeartbeatPayload{
		ClusterID:       "test-game",
		MemberID:        target,
		StartedAt:       t2,
		PublishedAt:     time.Now(),
		HolomushVersion: "duplicate",
	}
	b2, err := cluster.MarshalHeartbeat(p2)
	if err != nil {
		t.Fatalf("MarshalHeartbeat (duplicate): %v", err)
	}
	if err := h.Embedded.Conn.Publish(cluster.SubjectAlive("test-game", target), b2); err != nil {
		t.Fatalf("publish duplicate heartbeat: %v", err)
	}
	if err := h.Embedded.Conn.Flush(); err != nil {
		t.Fatalf("flush after duplicate heartbeat: %v", err)
	}

	// Allow time for receive processing.
	time.Sleep(200 * time.Millisecond)

	after, ok := h.Members[0].Registry.Member(target)
	if !ok {
		t.Fatal("target evicted unexpectedly after duplicate heartbeat")
	}
	if !after.StartedAt.Equal(t1) {
		t.Errorf("StartedAt = %v after duplicate; want %v (first-seen preserved)", after.StartedAt, t1)
	}
	if after.HolomushVersion != "first" {
		t.Errorf("HolomushVersion = %q after duplicate; want 'first' (first-seen preserved)",
			after.HolomushVersion)
	}
	// A future Phase MAY also assert cluster_duplicate_member_id_total
	// metric incremented; requires injecting DuplicateMemberIDMetrics
	// into the harness and reading the counter. Out of scope for the
	// Phase 3c test scaffold; the structured-log emission is the primary
	// operator-visible signal.
}

// Verifies: INV-54
func TestRegistryDropsMessagesForOtherClusterID(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	h.AwaitConverged(t, 1*time.Second)

	// Publish a heartbeat from "OTHER-CLUSTER" with a peer member id.
	// The local registry MUST drop it: cluster_id namespace isolation.
	foreignID := cluster.MemberID("01HFOREIGN_PEER")
	h.PublishSyntheticHeartbeat(t, "OTHER-CLUSTER", foreignID, "")

	time.Sleep(200 * time.Millisecond)
	if _, ok := h.Members[0].Registry.Member(foreignID); ok {
		t.Errorf("foreign-cluster member appeared in local registry")
	}
}

// Verifies: INV-57
func TestPillRateLimitBlocksDuplicateWithinWindow(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	target := cluster.MemberID("01HSYN_RATE_LIMIT")
	h.PublishSyntheticHeartbeat(t, "test-game", target, "")
	h.AwaitMemberPresent(t, 0, target, 1*time.Second)

	// First pill: succeeds (probe times out, pill issued).
	if err := h.Members[0].Registry.ProbeAndPill(context.Background(), target,
		cluster.PillReasonMissedInvalidationAck); err != nil {
		t.Fatalf("first pill returned %v; want nil", err)
	}

	// No need to re-inject presence between pills: probeAndPill does not
	// gate on r.members containing the target, and the rate-limit claim
	// is anchored to wall-clock regardless of membership state.
	err := h.Members[0].Registry.ProbeAndPill(context.Background(), target,
		cluster.PillReasonMissedInvalidationAck)
	// Discriminate by oops code: errors.Is against an OopsError sentinel
	// is tautological — see ErrPillRateLimited doc.
	errutil.AssertErrorCode(t, err, "CLUSTER_PILL_RATE_LIMITED")
}

// Verifies: INV-60
func TestProbeAndPillRefusesSelfTarget(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	err := h.Members[0].Registry.ProbeAndPill(context.Background(), h.Members[0].MemberID,
		cluster.PillReasonMissedInvalidationAck)
	errutil.AssertErrorCode(t, err, "CLUSTER_CANNOT_PILL_SELF")
}
