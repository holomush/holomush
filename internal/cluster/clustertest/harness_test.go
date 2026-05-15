// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package clustertest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/cluster/clustertest"
)

// TestAwaitMemberPresentReturnsWhenHeartbeatArrives is the regression
// guard for holomush-1r0v.15: the previous polling implementation raced
// its 1s deadline against NATS deliver→handler latency on slow CI. The
// event-driven implementation wakes on the Registry's OnMemberJoined
// callback as soon as handleAlive stores the new member.
//
// No wall-clock assertion on elapsed time — the helper returns when
// the event fires, and the test passes if the helper returns without
// Fatalf-ing the test. A polling regression would not be caught here,
// but is blocked by code review (the helper's doc comment forbids
// time.Sleep and the unit test below covers the deadline path).
func TestAwaitMemberPresentReturnsWhenHeartbeatArrives(t *testing.T) {
	t.Parallel()

	h := clustertest.New(t, "test-game", 1)
	target := cluster.MemberID("01HSYNTHETIC0AWAIT000000001")

	h.PublishSyntheticHeartbeat(t, "test-game", target, "test")
	h.AwaitMemberPresent(t, 0, target, 5*time.Second)
}

// TestAwaitMemberPresentEarlyReturnsWhenAlreadyPresent covers the
// post-Subscribe presence check: a heartbeat that lands before the
// observer is registered MUST still be caught by the Member() check
// or the helper would block until deadline.
func TestAwaitMemberPresentEarlyReturnsWhenAlreadyPresent(t *testing.T) {
	t.Parallel()

	h := clustertest.New(t, "test-game", 1)
	target := cluster.MemberID("01HSYNTHETIC0AWAIT000000002")

	h.PublishSyntheticHeartbeat(t, "test-game", target, "test")
	// First call drains the heartbeat synchronously via the event
	// path; the second call MUST take the early-return path because
	// Member() finds the target in the map at the post-Subscribe
	// check.
	h.AwaitMemberPresent(t, 0, target, 5*time.Second)
	h.AwaitMemberPresent(t, 0, target, 5*time.Second)
}

// TestAwaitMemberPresentFatalsOnDeadlineWhenHeartbeatNeverArrives
// covers the deadline path: target never appears, the helper Fatalfs
// with a useful diagnostic. Uses a short (10ms) deadline as the test
// fixture — the wall-clock cost is bounded by that value, and there
// is no race because no event ever fires.
func TestAwaitMemberPresentFatalsOnDeadlineWhenHeartbeatNeverArrives(t *testing.T) {
	t.Parallel()

	h := clustertest.New(t, "test-game", 1)
	target := cluster.MemberID("01HSYNTHETIC0NEVERARRIVES01")

	mock := &mockTB{T: t}
	h.AwaitMemberPresent(mock, 0, target, 10*time.Millisecond)

	require.True(t, mock.fataled,
		"AwaitMemberPresent MUST Fatalf when deadline expires without a join")
}

// mockTB satisfies clustertest.TB while recording Fatalf so a test can
// assert the deadline path without aborting the outer test.
type mockTB struct {
	*testing.T
	fataled bool
}

func (m *mockTB) Fatalf(_ string, _ ...any) {
	m.fataled = true
}

// Helper is required by clustertest.TB; delegate to the embedded T.
func (m *mockTB) Helper() { m.T.Helper() }
