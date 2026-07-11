// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package clustertest

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/testsupport/natstest"
)

// ExternalMember bundles a cluster.Registry with its OWN independent
// *nats.Conn to the external NATS container. Unlike HarnessMember (which
// shares one in-process eventbustest connection across every member), each
// ExternalMember dials its own connection — the multi-node shape that
// genuinely proves cross-replica coordination (CLUSTER-03, D-05a).
type ExternalMember struct {
	Registry   cluster.Registry
	Conn       *nats.Conn
	Pill       cluster.Pill
	PillEvents <-chan cluster.PillEvent
	MemberID   cluster.MemberID
}

// ExternalHarness wires n cluster.Registry members onto a single external
// NATS JetStream container (from natstest), each member on an INDEPENDENT
// *nats.Conn.
//
// This closes the shared-conn gap the embedded Harness leaves open: the
// embedded Harness hands every member the same eventbustest.Embedded.Conn,
// so a "multi-member" test never exercises real per-replica connections.
// NewExternal gives member i its own dial, so N-of-N ack collection,
// cluster_id filtering, and probe-and-pill are proven over the wire between
// distinct connections to one real broker (CLUSTER-03, D-05a).
type ExternalHarness struct {
	Env     *natstest.NATSEnv
	Members []ExternalMember
}

// NewExternal constructs an ExternalHarness with n Registry members, each on
// its OWN connection to env. All members use cluster_id=clusterID with
// accelerated heartbeats. Optional [Option] values override harness defaults
// (e.g. [WithPillRateLimit]).
//
// Accepts TB so callers can pass either *testing.T or ginkgo.GinkgoT().
func NewExternal(t TB, env *natstest.NATSEnv, clusterID string, n int, opts ...Option) *ExternalHarness {
	t.Helper()
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

	h := &ExternalHarness{Env: env}
	for i := 0; i < n; i++ {
		conn := h.dial(t)
		pill, events := cluster.NewTestPill()
		// Real ULIDs (production shape) catch accidental MemberID-format
		// coupling in tests and match production observability.
		memberID := cluster.MemberID(idgen.New().String())
		reg, err := cluster.NewSubsystem(cfg, cluster.Deps{
			Conn:          conn,
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
		regForCleanup := reg
		t.Cleanup(func() {
			stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelStop()
			// Best-effort: a hung-replica spec may deliberately Close a
			// member's conn, so a graceful-bye publish can fail here. That is
			// expected external-conn teardown noise, not a test failure.
			if stopErr := regForCleanup.Stop(stopCtx); stopErr != nil {
				t.Logf("external reg.Stop: %v", stopErr)
			}
		})

		h.Members = append(h.Members, ExternalMember{
			Registry:   reg,
			Conn:       conn,
			Pill:       pill,
			PillEvents: events,
			MemberID:   memberID,
		})
	}

	return h
}

// dial opens a NEW, independent connection to the container and registers its
// close on t.Cleanup. Registered BEFORE the owning registry's Stop so LIFO
// cleanup drains the registry (graceful bye) before the socket closes.
func (h *ExternalHarness) dial(t TB) *nats.Conn {
	t.Helper()
	conn, err := nats.Connect(h.Env.URL)
	if err != nil {
		t.Fatalf("natstest dial %s: %v", h.Env.URL, err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// AwaitConverged blocks until every member's LiveCount() == n (each sees all
// peers). Times out at deadline.
func (h *ExternalHarness) AwaitConverged(t TB, deadline time.Duration) {
	t.Helper()
	n := len(h.Members)
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("ExternalHarness.AwaitConverged timed out after %v; member views: %s", deadline, h.snapshot())
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

// PublishSyntheticHeartbeat publishes a single heartbeat for a synthetic
// (non-Registry-backed) MemberID on member 0's independent connection. Real
// members see it as a peer join; the synthetic source publishes once and
// never again, so the eviction sweeper reaps it after EvictAfterMissed ×
// HeartbeatInterval. Passing a foreign clusterID exercises the cluster_id
// namespace-isolation drop (INV-CLUSTER-4).
func (h *ExternalHarness) PublishSyntheticHeartbeat(t TB, clusterID string, memberID cluster.MemberID, holomushVersion string) {
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
	conn := h.Members[0].Conn
	if err := conn.Publish(cluster.SubjectAlive(clusterID, memberID), b); err != nil {
		t.Fatalf("Publish synthetic heartbeat: %v", err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

// AwaitMemberPresent blocks until member i sees `target` in its registry
// view, or fails the test on deadline. Event-driven and subscribe-then-check
// ordered, identical to the embedded harness's variant.
func (h *ExternalHarness) AwaitMemberPresent(t TB, i int, target cluster.MemberID, deadline time.Duration) {
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

func (h *ExternalHarness) snapshot() string {
	out := ""
	for i, m := range h.Members {
		out += " m" + string(rune('0'+i)) + "=" + memberIDsList(m.Registry.LiveMembers())
	}
	return out
}
