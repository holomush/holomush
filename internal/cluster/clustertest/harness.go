// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package clustertest provides multi-Registry test infrastructure on a
// shared in-process NATS connection. Used by cluster unit tests AND by
// the multi-Registry integration tests in test/integration/cluster/.
package clustertest

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/idgen"
)

// TB is the subset of testing.TB that clustertest needs. Composes
// eventbustest.TB (so callers can pass through to eventbustest.New)
// and adds Fatalf — the harness uses Fatalf for inline test bailouts.
//
// The narrow shape avoids dragging in testing.TB methods (like
// ArtifactDir, added in Go 1.24) that Ginkgo's GinkgoT() doesn't
// implement.
type TB interface {
	eventbustest.TB
	Fatalf(format string, args ...any)
}

// Harness wires up an embedded NATS server (via eventbustest) and a
// configurable number of cluster.Registry members on it. Cleanup is
// automatic via t.Cleanup.
type Harness struct {
	Embedded *eventbustest.Embedded
	Members  []HarnessMember
}

// HarnessMember bundles a Registry, its Pill (TestPill so trigger
// events are observable), and the channel of pill events.
type HarnessMember struct {
	Registry   cluster.Registry
	Pill       cluster.Pill
	PillEvents <-chan cluster.PillEvent
	MemberID   cluster.MemberID
}

// Option is a functional option for [New]. Use [WithPillRateLimit] etc. to
// override harness defaults from inside a specific test.
type Option func(*cluster.Config)

// WithPillRateLimit overrides the harness's default PillRateLimit (1s).
// Use this in rate-limit assertion tests where the intermediate work between
// pill calls can exceed the default window on slow CI runners — bumping the
// window beyond plausible inter-call latency eliminates the wall-clock race.
//
// Resolves holomush-ivnc (P1 flake).
func WithPillRateLimit(d time.Duration) Option {
	return func(cfg *cluster.Config) { cfg.PillRateLimit = d }
}

// WithEvictAfterMissed overrides the harness's default EvictAfterMissed (3).
// The eviction sweeper reaps a member whose last heartbeat is older than
// EvictAfterMissed × HeartbeatInterval (default 3 × 100ms = 300ms).
//
// A synthetic peer injected via PublishSyntheticHeartbeat heartbeats only
// once, so with the default window it becomes a sweep candidate after only
// ~300ms. Tests that inject a synthetic peer and then exercise it across
// an unbounded scheduling gap (e.g. a coordinator rate-limit assertion run
// under parallel -race, where the test goroutine can be descheduled past
// 300ms while the sweeper fires on schedule) race that window. Bumping it
// beyond plausible inter-step latency keeps the synthetic peer present for
// the whole test, eliminating the wall-clock race.
//
// Resolves holomush-kz7tb (P2 flake).
func WithEvictAfterMissed(n int) Option {
	return func(cfg *cluster.Config) { cfg.EvictAfterMissed = n }
}

// New constructs a Harness with n Registry members on a shared NATS
// connection. All members use cluster_id=clusterID. Optional [Option] values
// override the harness defaults (e.g. [WithPillRateLimit]).
//
// Accepts TB so callers can pass either *testing.T (plain Go
// tests) or ginkgo.GinkgoT() (Ginkgo specs); both satisfy the Helper /
// Fatalf / Cleanup methods this harness uses.
func New(t TB, clusterID string, n int, opts ...Option) *Harness {
	t.Helper()
	emb := eventbustest.New(t)

	h := &Harness{Embedded: emb}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	cfg := cluster.Config{
		ClusterID:         clusterID,
		HolomushVersion:   "test",
		HeartbeatInterval: 100 * time.Millisecond, // accelerated for tests
		EvictAfterMissed:  3,
		ProbeTimeout:      50 * time.Millisecond,
		PillRateLimit:     1 * time.Second,
		SkewWarnThreshold: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	for i := 0; i < n; i++ {
		pill, events := cluster.NewTestPill()
		// Use real ULIDs (production-shape) rather than synthetic
		// 01HMEMBERA-style strings. Real shape catches accidental
		// MemberID-format coupling in tests and matches what
		// production observability/metrics will see.
		memberID := cluster.MemberID(idgen.New().String())
		reg, err := cluster.NewSubsystem(cfg, cluster.Deps{
			Conn:          emb.Conn,
			Logger:        logger,
			Pill:          pill,
			SelfIDForTest: memberID,
		})
		if err != nil {
			t.Fatalf("NewSubsystem[%d]: %v", i, err)
		}
		startCtx, cancelStart := context.WithTimeout(context.Background(), 5*time.Second)
		err = reg.Start(startCtx)
		cancelStart()
		if err != nil {
			t.Fatalf("reg[%d].Start: %v", i, err)
		}
		// Loop-var capture is safe under Go 1.22+ semantics (project on
		// Go 1.26.2). Bound Stop with a timeout so a wedged shutdown
		// can't hang the test deadline; surface the error rather than
		// silently dropping it.
		regForCleanup := reg
		idx := i
		t.Cleanup(func() {
			stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelStop()
			if stopErr := regForCleanup.Stop(stopCtx); stopErr != nil {
				t.Errorf("reg[%d].Stop: %v", idx, stopErr)
			}
		})

		h.Members = append(h.Members, HarnessMember{
			Registry:   reg,
			Pill:       pill,
			PillEvents: events,
			MemberID:   memberID,
		})
	}

	return h
}

// AwaitConverged blocks until every member's LiveCount() == n (each
// sees all peers). Times out at deadline.
func (h *Harness) AwaitConverged(t TB, deadline time.Duration) {
	t.Helper()
	n := len(h.Members)
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("AwaitConverged timed out after %v; member views: %s", deadline, h.snapshot())
		case <-ticker.C:
			converged := true
			for _, m := range h.Members {
				if m.Registry.LiveCount() != n {
					converged = false
					break
				}
			}
			if converged {
				return
			}
		}
	}
}

// PublishSyntheticHeartbeat publishes a single heartbeat from a
// synthetic (non-Registry-backed) MemberID into the shared NATS
// connection. Real members will see it as a peer join. Useful for
// tests that need to assert sweeper-driven eviction without tearing
// down a real Registry (which would publish a graceful bye).
//
// The synthetic source publishes once and never again — the eviction
// sweeper on real members will then mark it stale and delete it after
// EvictAfterMissed × HeartbeatInterval.
func (h *Harness) PublishSyntheticHeartbeat(t TB, clusterID string, memberID cluster.MemberID, holomushVersion string) {
	t.Helper()
	now := time.Now()
	p := cluster.HeartbeatPayload{
		ClusterID:           clusterID,
		MemberID:            memberID,
		StartedAt:           now,
		PublishedAt:         now,
		HolomushVersion:     holomushVersion,
		LastInvalidationSeq: 0,
	}
	b, err := cluster.MarshalHeartbeat(p)
	if err != nil {
		t.Fatalf("MarshalHeartbeat: %v", err)
	}
	if err := h.Embedded.Conn.Publish(cluster.SubjectAlive(clusterID, memberID), b); err != nil {
		t.Fatalf("Publish synthetic heartbeat: %v", err)
	}
	if err := h.Embedded.Conn.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

// AwaitMemberPresent blocks until member i sees `target` in its
// registry view, or fails the test on deadline. Used by probe-and-pill
// tests that need to be sure the synthetic peer is present before
// invoking ProbeAndPill against it.
//
// The wait is event-driven: a MemberObserver is registered before the
// presence check, so the OnMemberJoined callback fires the wakeup
// channel as soon as Registry.handleAlive stores the new member and
// calls notifyJoined (internal/cluster/heartbeat.go:320). Replaces a
// 20ms polling loop whose tight 1s default deadline raced with NATS
// deliver→handler latency on slow CI (holomush-1r0v.15). The deadline
// is now a fast-fail diagnostic — heartbeat propagation never approaches
// it under healthy conditions, but a hung registry surfaces a useful
// error well before `go test -timeout` fires.
//
// Subscribe-then-check ordering is required to avoid the lost-wakeup
// race: a heartbeat that lands between Subscribe and the presence
// check still gets caught by the check (because handleAlive's store
// at heartbeat.go:313 precedes notifyJoined at :320, and Member()
// acquires the same r.mu the store wrote under).
func (h *Harness) AwaitMemberPresent(t TB, i int, target cluster.MemberID, deadline time.Duration) {
	t.Helper()
	if i < 0 || i >= len(h.Members) {
		t.Fatalf("AwaitMemberPresent: member index %d out of range [0,%d)", i, len(h.Members))
		return
	}
	reg := h.Members[i].Registry

	joined := make(chan struct{}, 1)
	cancel := reg.Subscribe(memberJoinSignaler{target: target, fire: joined})
	defer cancel()

	if _, ok := reg.Member(target); ok {
		return
	}

	select {
	case <-joined:
		return
	case <-time.After(deadline):
		t.Fatalf("AwaitMemberPresent: member %d did not observe %q within %v", i, target, deadline)
	}
}

// memberJoinSignaler is a MemberObserver that fires a non-blocking
// send on `fire` exactly once per OnMemberJoined for the configured
// target. The channel is buffered (1) so the signal is preserved if
// the receiver hasn't yet entered its select.
type memberJoinSignaler struct {
	target cluster.MemberID
	fire   chan<- struct{}
}

func (s memberJoinSignaler) OnMemberJoined(m cluster.Member) {
	if m.ID != s.target {
		return
	}
	select {
	case s.fire <- struct{}{}:
	default:
	}
}

func (s memberJoinSignaler) OnMemberLeft(cluster.MemberID, cluster.LeaveReason)           {}
func (s memberJoinSignaler) OnMemberStatusChanged(cluster.MemberID, cluster.MemberStatus) {}

func (h *Harness) snapshot() string {
	out := ""
	for i, m := range h.Members {
		out += " m" + string(rune('0'+i)) + "=" + memberIDsList(m.Registry.LiveMembers())
	}
	return out
}

func memberIDsList(ms []cluster.Member) string {
	s := "["
	for i := range ms {
		if i > 0 {
			s += ","
		}
		s += string(ms[i].ID)
	}
	return s + "]"
}
