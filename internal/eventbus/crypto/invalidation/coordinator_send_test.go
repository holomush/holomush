// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/cluster/clustertest"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestCoordinatorRequestInvalidationSucceedsWhenSelfIsOnlyMember(t *testing.T) {
	h := clustertest.New(t, "test-game", 1)

	// For the single-member case, the Coordinator publishes and the
	// local subscriber acks via NATS loopback (T10 wired the
	// receive-side handler body).

	cache := dek.NewCache(dek.CacheConfig{})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{})

	coord, err := invalidation.New(invalidation.Config{
		ClusterID:         "test-game",
		InvalidateTimeout: 500 * time.Millisecond,
	}, invalidation.Deps{
		Conn:      h.Embedded.Conn,
		Registry:  h.Members[0].Registry,
		DEKCache:  cache,
		PartCache: partCache,
		Logger:    slog.Default(),
	})
	if err != nil {
		t.Fatalf("invalidation.New: %v", err)
	}
	if startErr := coord.Start(context.Background()); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}
	t.Cleanup(func() { _ = coord.Stop(context.Background()) })

	err = coord.RequestInvalidation(
		context.Background(),
		dek.ContextID{Type: "scene", ID: "01HSCENE_TEST"},
		invalidation.ActionRekey,
		1, 2,
	)
	if err != nil {
		t.Errorf("RequestInvalidation single-member returned %v; want nil (self-ack via NATS loopback)", err)
	}
}

func TestCoordinatorRequestInvalidationReturnsRateLimitedWhenProbeAndPillRefuses(t *testing.T) {
	// Two-member harness; member 1 is unresponsive. Coordinator on
	// member 0 issues one pill against member 1. Coordinator's second
	// call within the rate-limit window returns ErrRateLimited via
	// the surfaced ErrPillRateLimited.
	//
	// PillRateLimit is bumped to 10s (default 1s) so the test's two
	// RequestInvalidation calls comfortably fit inside one rate-limit
	// window even on slow CI (holomush-ivnc). AwaitMemberPresent is now
	// event-driven (holomush-1r0v.15) — its deadline is a fast-fail
	// diagnostic, not a propagation timer. 5s is generous; healthy
	// runs return in <50ms.
	//
	// EvictAfterMissed is bumped to 100 (10s eviction window, default
	// 3×100ms = 300ms) so the synthetic member 1 — which heartbeats only
	// once per PublishSyntheticHeartbeat — stays in member 0's snapshot
	// across the whole test. Under parallel -race the gap between the
	// synthetic heartbeat and RequestInvalidation's LiveMembers snapshot
	// could exceed 300ms while the sweeper fired on schedule, reaping
	// member 1; RequestInvalidation then saw a single-member snapshot,
	// self-acked, and returned nil instead of the rate-limited error
	// (holomush-kz7tb).
	h := clustertest.New(t, "test-game", 2,
		clustertest.WithPillRateLimit(10*time.Second),
		clustertest.WithEvictAfterMissed(100))
	h.AwaitConverged(t, 2*time.Second)

	// Stop member 1 to make it unresponsive.
	_ = h.Members[1].Registry.Stop(context.Background())
	// Re-inject synthetic heartbeat so member 0 still sees it.
	h.PublishSyntheticHeartbeat(t, "test-game", h.Members[1].MemberID, "")
	h.AwaitMemberPresent(t, 0, h.Members[1].MemberID, 5*time.Second)

	cache := dek.NewCache(dek.CacheConfig{})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{})
	coord, err := invalidation.New(invalidation.Config{
		ClusterID:         "test-game",
		InvalidateTimeout: 200 * time.Millisecond,
	}, invalidation.Deps{
		Conn:      h.Embedded.Conn,
		Registry:  h.Members[0].Registry,
		DEKCache:  cache,
		PartCache: partCache,
		Logger:    slog.Default(),
	})
	if err != nil {
		t.Fatalf("invalidation.New: %v", err)
	}
	if startErr := coord.Start(context.Background()); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}
	t.Cleanup(func() { _ = coord.Stop(context.Background()) })

	// First call: pill issued, retry runs, depending on T10 may succeed
	// or partial-fail. We tolerate any outcome here; the next call is
	// the rate-limit assertion.
	_ = coord.RequestInvalidation(
		context.Background(),
		dek.ContextID{Type: "scene", ID: "01HSCENE_TEST"},
		invalidation.ActionRekey, 1, 2,
	)

	// Re-inject synthetic and verify second call hits rate limit.
	h.PublishSyntheticHeartbeat(t, "test-game", h.Members[1].MemberID, "")
	h.AwaitMemberPresent(t, 0, h.Members[1].MemberID, 5*time.Second)

	err = coord.RequestInvalidation(
		context.Background(),
		dek.ContextID{Type: "scene", ID: "01HSCENE_TEST"},
		invalidation.ActionRekey, 1, 2,
	)
	// Discriminate by oops code (NOT errors.Is — tautological for oops
	// sentinels). See feedback in CLAUDE.md and probe_pill.go.
	errutil.AssertErrorCode(t, err, "INVALIDATION_RATE_LIMITED")
}
