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
	"testing"
	"time"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/idgen"
)

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

// New constructs a Harness with n Registry members on a shared NATS
// connection. All members use cluster_id=clusterID.
func New(t *testing.T, clusterID string, n int) *Harness {
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
		if err := reg.Start(context.Background()); err != nil {
			t.Fatalf("reg[%d].Start: %v", i, err)
		}
		t.Cleanup(func() { _ = reg.Stop(context.Background()) }) //nolint:errcheck // best-effort cleanup

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
func (h *Harness) AwaitConverged(t *testing.T, deadline time.Duration) {
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
func (h *Harness) PublishSyntheticHeartbeat(t *testing.T, clusterID string, memberID cluster.MemberID, holomushVersion string) {
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
