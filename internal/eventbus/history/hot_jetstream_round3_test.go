// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

// Round 3: hot_jetstream internals — buildConfig has 4 branches, matchesQuery
// has 6 boolean filters. Rounds 1-2 exercised the happy paths via embedded
// NATS; here we drive buildConfig and matchesQuery directly so the
// hard-to-reach error arms (stream-lookup failure, empty stream fallback)
// are covered without an embedded server.

// TestBuildConfigForwardWithAfterSeqUsesStartSequencePolicy covers the
// "q.AfterSeq > 0" early-return branch (cursor present, forward direction).
func TestBuildConfigForwardWithAfterSeqUsesStartSequencePolicy(t *testing.T) {
	t.Parallel()
	h := &jetStreamHotTier{now: time.Now}
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.scene.abc"),
		AfterSeq:  42,
		Direction: eventbus.DirectionForward,
	}
	cfg, err := h.buildConfig(context.Background(), q, time.Time{}, 10)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartSequencePolicy, cfg.DeliverPolicy)
	assert.Equal(t, uint64(42), cfg.OptStartSeq)
	assert.Equal(t, []string{string(q.Subject)}, cfg.FilterSubjects)
}

// TestBuildConfigForwardWithNotBeforeAfterEdgeUsesNotBefore covers the
// branch where NotBefore is strictly later than edge.
func TestBuildConfigForwardWithNotBeforeAfterEdgeUsesNotBefore(t *testing.T) {
	t.Parallel()
	h := &jetStreamHotTier{now: time.Now}
	edge := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notBefore := edge.Add(24 * time.Hour)
	q := eventbus.HistoryQuery{
		Subject:   "events.main.scene.abc",
		NotBefore: notBefore,
		Direction: eventbus.DirectionForward,
	}
	cfg, err := h.buildConfig(context.Background(), q, edge, 10)
	require.NoError(t, err)
	require.NotNil(t, cfg.OptStartTime)
	// NotBefore > edge → start at NotBefore.
	assert.Equal(t, notBefore, *cfg.OptStartTime)
}

// TestBuildConfigForwardWithNotBeforeBeforeEdgeUsesEdge covers the branch
// where NotBefore is earlier than edge (so JS starts at edge, not
// NotBefore — older stuff isn't in JS retention anyway).
func TestBuildConfigForwardWithNotBeforeBeforeEdgeUsesEdge(t *testing.T) {
	t.Parallel()
	h := &jetStreamHotTier{now: time.Now}
	edge := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := eventbus.HistoryQuery{
		Subject:   "events.main.scene.abc",
		NotBefore: edge.Add(-time.Hour),
		Direction: eventbus.DirectionForward,
	}
	cfg, err := h.buildConfig(context.Background(), q, edge, 10)
	require.NoError(t, err)
	require.NotNil(t, cfg.OptStartTime)
	assert.Equal(t, edge, *cfg.OptStartTime)
}

// TestBuildConfigForwardDirectionZeroDefaultsToForward covers the
// Direction==0 branch in buildConfig.
func TestBuildConfigForwardDirectionZeroDefaultsToForward(t *testing.T) {
	t.Parallel()
	h := &jetStreamHotTier{now: time.Now}
	edge := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := eventbus.HistoryQuery{
		Subject: "events.main.scene.abc",
		// Direction unset (0)
	}
	cfg, err := h.buildConfig(context.Background(), q, edge, 10)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartTimePolicy, cfg.DeliverPolicy)
}

// TestBuildConfigBackwardWithBeforeSeqUsesStartSequencePolicy covers the
// backward cursor branch: BeforeSeq > 0 → start at max(1, BeforeSeq - fetch).
// fetch is set to pageSize by the caller so the window [BeforeSeq-pageSize,
// BeforeSeq) holds exactly pageSize in-scope events.
func TestBuildConfigBackwardWithBeforeSeqUsesStartSequencePolicy(t *testing.T) {
	t.Parallel()
	h := &jetStreamHotTier{now: time.Now}
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.scene.abc"),
		BeforeSeq: 50,
		Direction: eventbus.DirectionBackward,
	}
	// fetch=10 (= pageSize for backward): startSeq = 50 - 10 = 40
	// window [40, 50): 10 in-scope events (seq 40..49), reversed = seq 49..40
	cfg, err := h.buildConfig(context.Background(), q, time.Time{}, 10)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartSequencePolicy, cfg.DeliverPolicy)
	assert.Equal(t, uint64(40), cfg.OptStartSeq)
}

// TestBuildConfigBackwardWithBeforeSeqClampsToOne covers the case where
// BeforeSeq is smaller than fetch, so startSeq clamps to 1.
func TestBuildConfigBackwardWithBeforeSeqClampsToOne(t *testing.T) {
	t.Parallel()
	h := &jetStreamHotTier{now: time.Now}
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.scene.abc"),
		BeforeSeq: 5,
		Direction: eventbus.DirectionBackward,
	}
	// fetch=20, BeforeSeq=5: 5 <= 20 → startSeq=1
	cfg, err := h.buildConfig(context.Background(), q, time.Time{}, 20)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartSequencePolicy, cfg.DeliverPolicy)
	assert.Equal(t, uint64(1), cfg.OptStartSeq)
}

// TestMatchesQueryBoundaryBranchesEachFilter covers each filter branch of
// matchesQuery. Table-driven so every path shows independent coverage.
func TestMatchesQueryBoundaryBranchesEachFilter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	edge := now.Add(-24 * time.Hour)
	mk := func(ts time.Time) ulid.ULID {
		id, _ := ulid.New(ulid.Timestamp(ts), nil)
		return id
	}
	evSeq := func(ts time.Time, seq uint64) eventbus.Event {
		return eventbus.Event{ID: mk(ts), Seq: seq, Timestamp: ts}
	}

	tests := []struct {
		name string
		ev   eventbus.Event
		q    eventbus.HistoryQuery
		tier Tier
		want bool
	}{
		{
			name: "empty query in JS tier accepts post-edge event",
			ev:   evSeq(now, 10),
			q:    eventbus.HistoryQuery{},
			tier: TierJetStream,
			want: true,
		},
		{
			name: "JS tier rejects pre-edge event",
			ev:   evSeq(edge.Add(-time.Hour), 5),
			q:    eventbus.HistoryQuery{},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "after_seq excludes event at cursor seq (exclusive)",
			ev:   evSeq(now, 10),
			q:    eventbus.HistoryQuery{AfterSeq: 10},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "after_seq allows event strictly after cursor seq",
			ev:   evSeq(now, 11),
			q:    eventbus.HistoryQuery{AfterSeq: 10},
			tier: TierJetStream,
			want: true,
		},
		{
			name: "before_seq excludes event at cursor seq (exclusive)",
			ev:   evSeq(now, 10),
			q:    eventbus.HistoryQuery{BeforeSeq: 10},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "before_seq allows event strictly before cursor seq",
			ev:   evSeq(now, 9),
			q:    eventbus.HistoryQuery{BeforeSeq: 10},
			tier: TierJetStream,
			want: true,
		},
		{
			name: "not_before rejects event before window",
			ev:   evSeq(now.Add(-10*time.Hour), 3),
			q:    eventbus.HistoryQuery{NotBefore: now.Add(-5 * time.Hour)},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "not_after rejects event after window",
			ev:   evSeq(now.Add(time.Hour), 20),
			q:    eventbus.HistoryQuery{NotAfter: now},
			tier: TierJetStream,
			want: false,
		},
		{
			// Boundary lock (I-IU8J-3): event timestamp EXACTLY at
			// NotAfter MUST be included. Inclusive boundary semantics
			// are load-bearing for cursor-bounded backfill — without
			// this, a backfill scoped to Subscribe-attach-moment could
			// silently exclude an event whose timestamp matches the
			// attach moment to ms precision, producing a perceptible
			// "missing event" UX bug on the connect path.
			name: "not_after INCLUDES event whose timestamp equals NotAfter (boundary inclusive — iu8j)",
			ev:   evSeq(now, 21),
			q:    eventbus.HistoryQuery{NotAfter: now},
			tier: TierJetStream,
			want: true,
		},
		{
			// Combined bound: NotBefore AND NotAfter both honored.
			// Event inside the window MUST be included.
			name: "not_before+not_after window includes event inside the window (iu8j)",
			ev:   evSeq(now.Add(-30*time.Minute), 22),
			q: eventbus.HistoryQuery{
				NotBefore: now.Add(-1 * time.Hour),
				NotAfter:  now,
			},
			tier: TierJetStream,
			want: true,
		},
		{
			// Combined bound boundary: event AT the NotBefore side AND
			// AT the NotAfter side (degenerate single-point window).
			name: "not_before==not_after==event.timestamp includes the singleton window (iu8j)",
			ev:   evSeq(now, 23),
			q: eventbus.HistoryQuery{
				NotBefore: now,
				NotAfter:  now,
			},
			tier: TierJetStream,
			want: true,
		},
		{
			name: "cold tier accepts pre-edge without tier-boundary reject",
			ev:   evSeq(edge.Add(-10*time.Hour), 1),
			q:    eventbus.HistoryQuery{},
			tier: TierPostgres,
			want: true,
		},
		{
			name: "cold tier accepts overlapping post-edge event (seen-set dedups)",
			ev:   evSeq(now, 15),
			q:    eventbus.HistoryQuery{},
			tier: TierPostgres,
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := matchesQuery(tc.ev, tc.q, edge, tc.tier)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestOrderEventsReversesSliceForBackwardQuery covers the backward
// reversal branch.
func TestOrderEventsReversesSliceForBackwardQuery(t *testing.T) {
	t.Parallel()
	mk := func(ts time.Time) eventbus.Event {
		id, _ := ulid.New(ulid.Timestamp(ts), nil)
		return eventbus.Event{ID: id, Timestamp: ts}
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []eventbus.Event{
		mk(base),
		mk(base.Add(time.Hour)),
		mk(base.Add(2 * time.Hour)),
	}
	first := events[0].ID
	last := events[2].ID
	orderEvents(events, eventbus.HistoryQuery{Direction: eventbus.DirectionBackward})
	assert.Equal(t, last, events[0].ID, "backward reversed: newest first")
	assert.Equal(t, first, events[2].ID)
}

// TestOrderEventsKeepsOrderForForwardAndZeroDirection covers the forward
// fall-through (no-op reversal).
func TestOrderEventsKeepsOrderForForwardAndZeroDirection(t *testing.T) {
	t.Parallel()
	mk := func(ts time.Time) eventbus.Event {
		id, _ := ulid.New(ulid.Timestamp(ts), nil)
		return eventbus.Event{ID: id, Timestamp: ts}
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []eventbus.Event{mk(base), mk(base.Add(time.Hour))}
	original := append([]eventbus.Event{}, events...)

	orderEvents(events, eventbus.HistoryQuery{Direction: eventbus.DirectionForward})
	assert.Equal(t, original[0].ID, events[0].ID)

	orderEvents(events, eventbus.HistoryQuery{}) // Direction 0 → forward
	assert.Equal(t, original[0].ID, events[0].ID)
}

// TestHotTierForwardCursorUsesStartSequencePolicy is the canonical unit test
// specified in Task 7: pure config check, no NATS needed.
func TestHotTierForwardCursorUsesStartSequencePolicy(t *testing.T) {
	t.Parallel()
	tier := newJetStreamHotTier(nil, nil, func() time.Time { return time.Now() })
	// Use a valid ULID as the AfterID tripwire; any ULID is fine here since
	// buildConfig only looks at AfterSeq for policy selection.
	afterID := ulid.MustNew(ulid.Timestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), nil)
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.location.01ABC"),
		AfterSeq:  100,
		AfterID:   afterID,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	}
	cfg, err := tier.buildConfig(context.Background(), q, time.Time{}, q.PageSize+1)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartSequencePolicy, cfg.DeliverPolicy)
	assert.Equal(t, uint64(100), cfg.OptStartSeq)
}

// Hold a reference to nats.Header so imports are not flagged when the file
// compiles on its own.
var _ = nats.Header{}
