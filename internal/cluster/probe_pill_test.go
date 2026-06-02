// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster_test

import (
	"context"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/cluster/clustertest"
	"github.com/holomush/holomush/pkg/errutil"
)

// Verifies: INV-CLUSTER-10
func TestProbeAndPillRefusesSelfPerINV60(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	r := h.Members[0].Registry
	err := r.ProbeAndPill(context.Background(), r.Self(), cluster.PillReasonMissedInvalidationAck)
	if err == nil {
		t.Fatal("ProbeAndPill(self) returned nil; want CLUSTER_CANNOT_PILL_SELF")
	}
	// Discriminate by oops code: errors.Is against an OopsError sentinel
	// is tautological (matches any OopsError) — see ErrCannotPillSelf doc.
	errutil.AssertErrorCode(t, err, "CLUSTER_CANNOT_PILL_SELF")
}

func TestProbeAndPillSucceedsAgainstResponsivePeer(t *testing.T) {
	h := clustertest.New(t, "test-game", 2)
	h.AwaitConverged(t, 2*time.Second)

	err := h.Members[0].Registry.ProbeAndPill(
		context.Background(),
		h.Members[1].MemberID,
		cluster.PillReasonMissedInvalidationAck,
	)
	if err == nil {
		t.Fatal("ProbeAndPill returned nil; want CLUSTER_PILL_PROBE_SUCCEEDED")
	}
	errutil.AssertErrorCode(t, err, "CLUSTER_PILL_PROBE_SUCCEEDED")
	select {
	case ev := <-h.Members[1].PillEvents:
		t.Fatalf("unexpected pill event on responsive peer: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestProbeAndPillTriggersPillOnUnresponsivePeer(t *testing.T) {
	h := clustertest.New(t, "test-game", 2)
	h.AwaitConverged(t, 2*time.Second)

	// Stop member 1's Registry so it no longer responds to probes.
	// Stop publishes a graceful bye, which evicts member 1 from member
	// 0's view; that's expected. ProbeAndPill does NOT gate on member
	// presence — it publishes a probe regardless, the probe times out
	// (no responder), and a pill is published. The synchronous
	// eviction in issuePill is a no-op delete on an already-evicted
	// key (safe).
	if err := h.Members[1].Registry.Stop(context.Background()); err != nil {
		t.Fatalf("h.Members[1].Registry.Stop: %v (test precondition: target must be stopped to be unresponsive)", err)
	}

	err := h.Members[0].Registry.ProbeAndPill(
		context.Background(),
		h.Members[1].MemberID,
		cluster.PillReasonMissedInvalidationAck,
	)
	if err != nil {
		t.Fatalf("ProbeAndPill returned %v; want nil after pill issued", err)
	}
	if _, ok := h.Members[0].Registry.Member(h.Members[1].MemberID); ok {
		t.Errorf("pilled member still in registry; expected synchronous eviction")
	}
}

// Verifies: INV-CLUSTER-7
func TestPillRateLimitBlocksSecondPillWithinWindow(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	r := h.Members[0].Registry

	target := cluster.MemberID("01HSYNTHETIC0VICTIM00000000")
	h.PublishSyntheticHeartbeat(t, "test-game", target, "test")
	h.AwaitMemberPresent(t, 0, target, 5*time.Second)

	if err := r.ProbeAndPill(context.Background(), target, cluster.PillReasonMissedInvalidationAck); err != nil {
		t.Fatalf("first pill returned %v; want nil", err)
	}

	// No need to re-inject presence between pills: probeAndPill does
	// not gate on r.members containing the target, and the rate-limit
	// claim is anchored to the first attempt's wall-clock time
	// regardless of membership state.
	err := r.ProbeAndPill(context.Background(), target, cluster.PillReasonMissedInvalidationAck)
	if err == nil {
		t.Fatal("second pill returned nil; want CLUSTER_PILL_RATE_LIMITED")
	}
	errutil.AssertErrorCode(t, err, "CLUSTER_PILL_RATE_LIMITED")
}

// Verifies: INV-CLUSTER-5
func TestPillReceivedOnPoisonSubjectInvokesPillTrigger(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)

	payload := cluster.PoisonPayload{
		ClusterID:           "test-game",
		CoordinatorMemberID: cluster.MemberID("01HSOURCEINV5500000000000A"),
		Reason:              cluster.PillReasonMissedInvalidationAck,
		IssuedAt:            time.Now(),
	}
	body, err := cluster.MarshalPoison(payload)
	if err != nil {
		t.Fatalf("MarshalPoison: %v", err)
	}
	if err := h.Embedded.Conn.Publish(
		cluster.SubjectPoison("test-game", h.Members[0].MemberID),
		body,
	); err != nil {
		t.Fatalf("publish poison: %v", err)
	}
	if err := h.Embedded.Conn.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	select {
	case ev := <-h.Members[0].PillEvents:
		if ev.Reason != cluster.PillReasonMissedInvalidationAck {
			t.Errorf("event Reason = %q; want %q", ev.Reason, cluster.PillReasonMissedInvalidationAck)
		}
		if ev.SourceID != cluster.MemberID("01HSOURCEINV5500000000000A") {
			t.Errorf("event SourceID = %q; want '01HSOURCEINV5500000000000A'", ev.SourceID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Pill.Trigger not invoked within 2s after poison publish")
	}
}

// TestProbeAndPillReturnsCtxCanceledOnParentCtxCancel covers the
// round-2 fix branch: when parent ctx is cancelled before/while the
// probe runs, ProbeAndPill MUST return CLUSTER_PROBE_AND_PILL_CTX_CANCELED
// rather than silently issuing a pill against a healthy peer.
func TestProbeAndPillReturnsCtxCanceledOnParentCtxCancel(t *testing.T) {
	h := clustertest.New(t, "test-game", 2)
	h.AwaitConverged(t, 2*time.Second)

	// Stop member 1 so the probe will not get a reply.
	if err := h.Members[1].Registry.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Parent ctx already cancelled before the call.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := h.Members[0].Registry.ProbeAndPill(
		ctx,
		h.Members[1].MemberID,
		cluster.PillReasonMissedInvalidationAck,
	)
	if err == nil {
		t.Fatal("ProbeAndPill returned nil; want CLUSTER_PROBE_AND_PILL_CTX_CANCELED")
	}
	errutil.AssertErrorCode(t, err, "CLUSTER_PROBE_AND_PILL_CTX_CANCELED")
}
