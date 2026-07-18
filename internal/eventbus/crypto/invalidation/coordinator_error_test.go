// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/cluster/clustertest"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubRegistry returns canned answers for cluster.Registry calls used
// by the invalidation Coordinator. Field hooks override behavior so a
// single struct shape covers every error-branch scenario.
type stubRegistry struct {
	self         cluster.MemberID
	liveMembers  []cluster.Member
	probeAndPill func(ctx context.Context, id cluster.MemberID, reason cluster.PillReason) error
	subscribe    func(cluster.MemberObserver) func()
}

func (s *stubRegistry) ID() lifecycle.SubsystemID          { return lifecycle.SubsystemCluster }
func (s *stubRegistry) DependsOn() []lifecycle.SubsystemID { return nil }
func (s *stubRegistry) Prepare(_ context.Context) error    { return nil }
func (s *stubRegistry) Activate(_ context.Context) error   { return nil }
func (s *stubRegistry) Stop(_ context.Context) error       { return nil }
func (s *stubRegistry) Self() cluster.MemberID             { return s.self }
func (s *stubRegistry) LiveMembers() []cluster.Member {
	out := make([]cluster.Member, len(s.liveMembers))
	copy(out, s.liveMembers)
	return out
}

func (s *stubRegistry) Member(id cluster.MemberID) (cluster.Member, bool) {
	for _, m := range s.liveMembers {
		if m.ID == id {
			return m, true
		}
	}
	return cluster.Member{}, false
}

func (s *stubRegistry) LiveCount() int { return len(s.liveMembers) }

func (s *stubRegistry) ProbeAndPill(ctx context.Context, id cluster.MemberID, reason cluster.PillReason) error {
	if s.probeAndPill != nil {
		return s.probeAndPill(ctx, id, reason)
	}
	return cluster.ErrPillProbeSucceeded
}

func (s *stubRegistry) Subscribe(o cluster.MemberObserver) func() {
	if s.subscribe != nil {
		return s.subscribe(o)
	}
	return func() {}
}

// newCoordinatorWithStub builds a Coordinator wired to the supplied stub
// Registry but pointed at a real embedded NATS connection (the
// publish/subscribe path requires a real *nats.Conn).
//
// Also installs a silent NATS subscriber on the cache_invalidate
// wildcard so PublishRequest doesn't trip the embedded server's
// no-responders detection (which would surface as
// INVALIDATION_INBOX_READ_FAILED instead of letting the inbox loop
// time out cleanly). The silent subscriber receives messages but
// never replies, so the Coordinator's collect window times out
// naturally and falls through to the probe-and-pill phase.
func newCoordinatorWithStub(t *testing.T, stub *stubRegistry) invalidation.Coordinator {
	t.Helper()
	h := clustertest.New(t, "test-game", 1)
	silentSub, err := h.Embedded.Conn.Subscribe(
		invalidation.SubjectCacheInvalidateWildcard("test-game"),
		func(_ *nats.Msg) { /* no-op: receive and drop */ },
	)
	if err != nil {
		t.Fatalf("subscribe silent listener: %v", err)
	}
	t.Cleanup(func() {
		if uerr := silentSub.Unsubscribe(); uerr != nil {
			t.Errorf("silentSub.Unsubscribe: %v", uerr)
		}
	})

	cache := dek.NewCache(dek.CacheConfig{})
	partCache := dek.NewParticipantsCache(dek.CacheConfig{})
	coord, err := invalidation.New(invalidation.Config{
		ClusterID:         "test-game",
		InvalidateTimeout: 100 * time.Millisecond,
	}, invalidation.Deps{
		Conn:      h.Embedded.Conn,
		Registry:  stub,
		DEKCache:  cache,
		PartCache: partCache,
		Logger:    slog.Default(),
	})
	if err != nil {
		t.Fatalf("invalidation.New: %v", err)
	}
	t.Cleanup(func() {
		if err := coord.Stop(context.Background()); err != nil {
			t.Errorf("coord.Stop: %v", err)
		}
	})
	return coord
}

// TestRequestInvalidationByErrorClass exercises every error-discrimination
// branch of Coordinator.RequestInvalidation by injecting a stubRegistry
// whose ProbeAndPill returns the canned error class for each case.
//
// All cases share: ctxID = scene/01HSCENE, action = ActionRekey,
// version=1, successorVersion=2. They diverge on liveMembers shape,
// the probeAndPill closure, and the expected oops error class observed
// at the public RequestInvalidation boundary.
//
// Note on samber/oops Code() semantics (holomush-ojw1.3.22): the
// Coordinator's default-branch wrap path used to call
// oops.Code(OUTER).Wrap(innerOopsErr), which silently surfaced the
// INNER code because OopsError.Code() walks to the deepest code in
// the chain (getDeepestErrorCode in samber/oops@v1.21.0+). The fix
// switches that path to Errorf so the OUTER code
// (INVALIDATION_PROBE_AND_PILL_FAILED) is what callers see, with the
// inner code preserved in the With("inner_code") context. The
// "propagates outer ... with inner_code" case below pins this contract.
func TestRequestInvalidationByErrorClass(t *testing.T) {
	const (
		self  = cluster.MemberID("01HSELFAAAAAAAAAAAAAAAAAA")
		other = cluster.MemberID("01HOTHERAAAAAAAAAAAAAAAAA")
	)
	twoMember := []cluster.Member{
		{ID: self, Status: cluster.StatusAlive},
		{ID: other, Status: cluster.StatusAlive},
	}

	cases := []struct {
		// name is an ACE-style sentence describing the error path.
		name string

		// liveMembers seeds stubRegistry's snapshot. nil triggers
		// the ErrNoLiveMembers branch; twoMember triggers the publish
		// flow that probes `other` after the silent listener never
		// acks.
		liveMembers []cluster.Member

		// probeAndPill is the canned response for every probe attempt.
		// nil means stubRegistry returns ErrPillProbeSucceeded by
		// default (same as a real, alive, fast-responding peer).
		probeAndPill func(context.Context, cluster.MemberID, cluster.PillReason) error

		// wantCode is the surfaced oops error code at the public
		// RequestInvalidation boundary (deepest code per oops v1.21+).
		wantCode string

		// wantCtx asserts on oops With() context keys added by the
		// Coordinator's wrapping path. nil = no context assertions.
		wantCtx map[string]any
	}{
		{
			name:        "returns INVALIDATION_NO_LIVE_MEMBERS when LiveMembers snapshot is empty",
			liveMembers: nil,
			wantCode:    "INVALIDATION_NO_LIVE_MEMBERS",
		},
		{
			name:        "wraps a non-oops ProbeAndPill error as INVALIDATION_PROBE_AND_PILL_FAILED",
			liveMembers: twoMember,
			probeAndPill: func(_ context.Context, _ cluster.MemberID, _ cluster.PillReason) error {
				return errors.New("nats wedged") //nolint:err113 // test-only sentinel for non-oops branch
			},
			wantCode: "INVALIDATION_PROBE_AND_PILL_FAILED",
		},
		{
			name:        "returns INVALIDATION_RATE_LIMITED when ProbeAndPill surfaces CLUSTER_PILL_RATE_LIMITED",
			liveMembers: twoMember,
			probeAndPill: func(_ context.Context, _ cluster.MemberID, _ cluster.PillReason) error {
				return cluster.ErrPillRateLimited
			},
			wantCode: "INVALIDATION_RATE_LIMITED",
		},
		{
			// Stub returns CLUSTER_PILL_PROBE_SUCCEEDED for the missing
			// peer: the probe-and-pill loop MUST `continue`, then the
			// retry phase re-snapshots and re-publishes. The retry also
			// times out (silent listener never acks), surfacing
			// INVALIDATION_PARTIAL_FAILURE. The PARTIAL_FAILURE outcome
			// (not RATE_LIMITED, not PROBE_AND_PILL_FAILED) proves the
			// `continue` branch ran rather than the early-return cases.
			name:        "continues on CLUSTER_PILL_PROBE_SUCCEEDED and falls through to PARTIAL_FAILURE on retry timeout",
			liveMembers: twoMember,
			probeAndPill: func(_ context.Context, _ cluster.MemberID, _ cluster.PillReason) error {
				return cluster.ErrPillProbeSucceeded
			},
			wantCode: "INVALIDATION_PARTIAL_FAILURE",
		},
		{
			// Default branch: the OUTER code
			// INVALIDATION_PROBE_AND_PILL_FAILED is what callers see; the
			// inner CLUSTER_* code lives in `inner_code` context. Pre-fix
			// (3.22) the surfaced code was the inner one because
			// oops.Code(OUTER).Wrap(innerOopsErr) traverses to deepest;
			// the fix switched the wrap site to Errorf to break the
			// chain walk. inner_code + probe_target context keys prove
			// the default-wrap branch ran (vs. continue / rate-limited).
			name:        "surfaces outer INVALIDATION_PROBE_AND_PILL_FAILED with inner_code + probe_target context",
			liveMembers: twoMember,
			probeAndPill: func(_ context.Context, _ cluster.MemberID, _ cluster.PillReason) error {
				return oops.Code("CLUSTER_PROBE_AND_PILL_PROBE_FAILED").
					With("target", string(other)).
					Errorf("nats failure")
			},
			wantCode: "INVALIDATION_PROBE_AND_PILL_FAILED",
			wantCtx: map[string]any{
				"inner_code":   "CLUSTER_PROBE_AND_PILL_PROBE_FAILED",
				"probe_target": string(other),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRegistry{
				self:         self,
				liveMembers:  tc.liveMembers,
				probeAndPill: tc.probeAndPill,
			}
			coord := newCoordinatorWithStub(t, stub)

			err := coord.RequestInvalidation(
				context.Background(),
				dek.ContextID{Type: "scene", ID: "01HSCENE"},
				invalidation.ActionRekey, 1, 2,
			)

			errutil.AssertErrorCode(t, err, tc.wantCode)
			for k, v := range tc.wantCtx {
				errutil.AssertErrorContext(t, err, k, v)
			}
		})
	}
}
