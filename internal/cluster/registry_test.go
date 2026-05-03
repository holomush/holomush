// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster_test

import (
	"context"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/cluster/clustertest"
)

func TestRegistryStartIncludesSelfInLiveMembers(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)
	if got := h.Members[0].Registry.LiveCount(); got != 1 {
		t.Fatalf("LiveCount = %d; want 1 (self)", got)
	}
	if h.Members[0].Registry.Self() == cluster.MemberID("") {
		t.Fatal("Self() returned empty MemberID")
	}
}

func TestThreeMemberClusterConvergesViaHeartbeat(t *testing.T) {
	h := clustertest.New(t, "test-game", 3)
	h.AwaitConverged(t, 2*time.Second)
	for i, m := range h.Members {
		if m.Registry.LiveCount() != 3 {
			t.Errorf("member %d LiveCount = %d; want 3", i, m.Registry.LiveCount())
		}
	}
}

func TestMemberLeftCallbackFiresOnGracefulStop(t *testing.T) {
	h := clustertest.New(t, "test-game", 2)
	h.AwaitConverged(t, 2*time.Second)

	type leaveEvent struct {
		id     cluster.MemberID
		reason cluster.LeaveReason
	}
	leaveCh := make(chan leaveEvent, 4)
	h.Members[0].Registry.Subscribe(testObserver{
		onLeft: func(id cluster.MemberID, reason cluster.LeaveReason) {
			leaveCh <- leaveEvent{id: id, reason: reason}
		},
	})

	// Stop member 1; member 0 should observe the bye.
	if err := h.Members[1].Registry.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case ev := <-leaveCh:
		if ev.id != h.Members[1].MemberID {
			t.Errorf("leave id = %q; want %q", ev.id, h.Members[1].MemberID)
		}
		if ev.reason != cluster.LeaveReasonGracefulBye {
			t.Errorf("leave reason = %v; want LeaveReasonGracefulBye", ev.reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no OnMemberLeft callback within 2s")
	}
}

func TestPeerStopRemovesMemberViaByePath(t *testing.T) {
	// Exercises the bye path: peer.Stop() publishes a graceful bye,
	// and the surviving member observes it via handleBye and removes
	// the peer immediately (NOT via the eviction sweeper).
	// Sweeper-only path is covered by
	// TestEvictionSweeperRemovesMemberAfterMissedHeartbeats.
	h := clustertest.New(t, "test-game", 2)
	h.AwaitConverged(t, 2*time.Second)

	if err := h.Members[1].Registry.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.Members[0].Registry.LiveCount() == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("member 0 LiveCount = %d; want 1 after peer Stop()", h.Members[0].Registry.LiveCount())
}

// TestEvictionSweeperRemovesMemberAfterMissedHeartbeats exercises the
// eviction-sweeper path specifically: a synthetic peer publishes one
// heartbeat (no bye), then never publishes again. The sweeper should
// evict it after EvictAfterMissed × HeartbeatInterval (3 × 100ms =
// 300ms in the harness).
func TestEvictionSweeperRemovesMemberAfterMissedHeartbeats(t *testing.T) {
	h := clustertest.New(t, "test-game", 2)
	h.AwaitConverged(t, 2*time.Second)

	syntheticID := cluster.MemberID("01HSYNTHETIC0123456789ABCD")
	h.PublishSyntheticHeartbeat(t, "test-game", syntheticID, "synthetic")

	// Wait until the synthetic peer is visible to all real members.
	seenDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(seenDeadline) {
		ok := true
		for _, m := range h.Members {
			if _, present := m.Registry.Member(syntheticID); !present {
				ok = false
				break
			}
		}
		if ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, m := range h.Members {
		if _, present := m.Registry.Member(syntheticID); !present {
			t.Fatalf("synthetic %q never observed by member %q",
				string(syntheticID), string(m.MemberID))
		}
	}

	// No further heartbeats. Sweeper should evict after
	// EvictAfterMissed × HeartbeatInterval = 3 × 100ms = 300ms,
	// plus up to one sweeper-tick (100ms) of latency. Allow 1.5s slack.
	evictDeadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(evictDeadline) {
		evictedFromAll := true
		for _, m := range h.Members {
			if _, present := m.Registry.Member(syntheticID); present {
				evictedFromAll = false
				break
			}
		}
		if evictedFromAll {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, m := range h.Members {
		if _, present := m.Registry.Member(syntheticID); present {
			t.Errorf("synthetic %q still present in member %q after sweeper deadline",
				string(syntheticID), string(m.MemberID))
		}
	}
}

// testObserver implements cluster.MemberObserver for tests; only the
// OnMemberLeft callback is wired.
type testObserver struct {
	onJoined func(cluster.Member)
	onLeft   func(cluster.MemberID, cluster.LeaveReason)
	onStatus func(cluster.MemberID, cluster.MemberStatus)
}

func (o testObserver) OnMemberJoined(m cluster.Member) {
	if o.onJoined != nil {
		o.onJoined(m)
	}
}

func (o testObserver) OnMemberLeft(id cluster.MemberID, r cluster.LeaveReason) {
	if o.onLeft != nil {
		o.onLeft(id, r)
	}
}

func (o testObserver) OnMemberStatusChanged(id cluster.MemberID, s cluster.MemberStatus) {
	if o.onStatus != nil {
		o.onStatus(id, s)
	}
}
