// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package clustertest_test

import (
	"context"
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

// TestWithEvictAfterMissedKeepsSyntheticMemberPastDefaultWindow is the
// regression guard for holomush-kz7tb. A one-shot synthetic heartbeat
// keeps a member present only for EvictAfterMissed × HeartbeatInterval
// before the sweeper reaps it (default 3×100ms = 300ms). Tests that
// inject a synthetic peer and then exercise it across an unbounded
// scheduling gap (the coordinator rate-limit assertion under parallel
// -race) raced that 300ms window: when the gap stretched past 300ms the
// member was swept, RequestInvalidation saw a single-member snapshot,
// self-acked, and returned nil instead of the expected rate-limited
// error. WithEvictAfterMissed widens the window so the member survives
// well past any plausible inter-step latency.
func TestWithEvictAfterMissedKeepsSyntheticMemberPastDefaultWindow(t *testing.T) {
	t.Parallel()

	// 100 × 100ms = 10s eviction window — far beyond the 400ms sleep
	// below, which itself exceeds the DEFAULT 300ms window.
	h := clustertest.New(t, "test-game", 1, clustertest.WithEvictAfterMissed(100))
	target := cluster.MemberID("01HSYNTHETIC0EVICTWINDOW001")

	h.PublishSyntheticHeartbeat(t, "test-game", target, "test")
	h.AwaitMemberPresent(t, 0, target, 5*time.Second)

	// Sleep well past the default ~300ms nominal window but far under the
	// configured window (10s). Determinism here comes from the 10s window,
	// not the sleep length: with the default EvictAfterMissed=3 the member
	// would be a sweep candidate by now, whereas at 100 it cannot be reaped
	// for ~10s, so the member MUST still be present.
	time.Sleep(400 * time.Millisecond)

	_, ok := h.Members[0].Registry.Member(target)
	require.True(t, ok,
		"synthetic member evicted within 400ms; WithEvictAfterMissed(100) must extend the window to 10s")
}

// TestAwaitMemberAbsentReturnsAfterGracefulBye covers the happy path of
// the new helper: after a peer's Registry.Stop publishes its graceful bye,
// member 0 eventually deletes the peer, and AwaitMemberAbsent returns once
// it observes that departure (via OnMemberLeft or the post-Subscribe
// Member() check). Regression guard for holomush-o7k0p.
func TestAwaitMemberAbsentReturnsAfterGracefulBye(t *testing.T) {
	t.Parallel()

	h := clustertest.New(t, "test-game", 2)
	h.AwaitConverged(t, 2*time.Second)
	target := h.Members[1].MemberID

	_, ok := h.Members[0].Registry.Member(target)
	require.True(t, ok, "precondition: member 1 present in member 0's view after convergence")

	require.NoError(t, h.Members[1].Registry.Stop(context.Background()))
	h.AwaitMemberAbsent(t, 0, target, 5*time.Second)

	_, ok = h.Members[0].Registry.Member(target)
	require.False(t, ok, "member 1 must be absent once AwaitMemberAbsent returns")
}

// TestAwaitMemberAbsentEarlyReturnsWhenAlreadyAbsent covers the
// post-Subscribe absence check: a target that is not in member 0's view
// MUST return immediately via the Member()-absent check rather than
// blocking until deadline. Mirror of
// TestAwaitMemberPresentEarlyReturnsWhenAlreadyPresent. A never-injected
// MemberID is absent by construction, so a 5s deadline would only be
// reached if the early-return path regressed.
func TestAwaitMemberAbsentEarlyReturnsWhenAlreadyAbsent(t *testing.T) {
	t.Parallel()

	h := clustertest.New(t, "test-game", 1)
	target := cluster.MemberID("01HSYNTHETIC0NEVERPRESENT1")

	h.AwaitMemberAbsent(t, 0, target, 5*time.Second)
}

// TestAwaitMemberAbsentFatalsOnDeadlineWhenMemberStaysPresent covers the
// deadline path: the target remains present, so the helper Fatalfs with a
// useful diagnostic. Mirrors the AwaitMemberPresent deadline test. The 10ms
// deadline fires well before the default ~300ms eviction window, so the
// member cannot depart on its own within the fixture.
func TestAwaitMemberAbsentFatalsOnDeadlineWhenMemberStaysPresent(t *testing.T) {
	t.Parallel()

	h := clustertest.New(t, "test-game", 1)
	target := cluster.MemberID("01HSYNTHETIC0STAYSPRESENT01")

	h.PublishSyntheticHeartbeat(t, "test-game", target, "test")
	h.AwaitMemberPresent(t, 0, target, 5*time.Second)

	mock := &mockTB{T: t}
	h.AwaitMemberAbsent(mock, 0, target, 10*time.Millisecond)

	require.True(t, mock.fataled,
		"AwaitMemberAbsent MUST Fatalf when deadline expires while the member remains present")
}

// TestSyntheticHeartbeatAfterByeIsObservedWhenAbsenceAwaited is the
// regression guard for holomush-o7k0p. It reproduces the
// Stop → (await absent) → synthetic → await-present sequence the coordinator
// rate-limit test relies on. Without awaiting the departure first, the
// synthetic heartbeat races member 1's graceful bye on member 0's two
// independent subscriptions: when handleAlive runs while the real (converged)
// entry is still present, the synthetic's differing StartedAt trips the
// duplicate-MemberID guard and is dropped, then handleBye deletes member 1,
// leaving it permanently absent. Awaiting absence serializes the bye so the
// re-injected synthetic lands as a fresh add that sticks.
//
// WithEvictAfterMissed(100) (10s window) keeps the one-shot synthetic member
// alive for the whole test: without it the sweeper would reap the synthetic
// after the default ~300ms window and reintroduce flakiness (holomush-kz7tb).
func TestSyntheticHeartbeatAfterByeIsObservedWhenAbsenceAwaited(t *testing.T) {
	t.Parallel()

	h := clustertest.New(t, "test-game", 2, clustertest.WithEvictAfterMissed(100))
	h.AwaitConverged(t, 2*time.Second)
	target := h.Members[1].MemberID

	require.NoError(t, h.Members[1].Registry.Stop(context.Background()))
	h.AwaitMemberAbsent(t, 0, target, 5*time.Second)
	h.PublishSyntheticHeartbeat(t, "test-game", target, "")
	h.AwaitMemberPresent(t, 0, target, 5*time.Second)

	_, ok := h.Members[0].Registry.Member(target)
	require.True(t, ok,
		"member must be present after awaiting absence then re-injecting the synthetic heartbeat")
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
