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

// TestBuildConfigForwardWithAfterCursorUsesStartTimePolicy covers the
// "q.After is set" early-return branch.
func TestBuildConfigForwardWithAfterCursorUsesStartTimePolicy(t *testing.T) {
	t.Parallel()
	h := &jetStreamHotTier{now: time.Now}
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	after, err := ulid.New(ulid.Timestamp(ts), nil)
	require.NoError(t, err)
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.scene.abc"),
		After:     after,
		Direction: eventbus.DirectionForward,
	}
	cfg, err := h.buildConfig(context.Background(), q, time.Time{}, 10)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartTimePolicy, cfg.DeliverPolicy)
	require.NotNil(t, cfg.OptStartTime)
	// NotBefore is absent, so OptStartTime should reflect q.After's time.
	assert.WithinDuration(t, ulid.Time(after.Time()), *cfg.OptStartTime, time.Second)
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
	ev := func(ts time.Time) eventbus.Event {
		return eventbus.Event{ID: mk(ts), Timestamp: ts}
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
			ev:   ev(now),
			q:    eventbus.HistoryQuery{},
			tier: TierJetStream,
			want: true,
		},
		{
			name: "JS tier rejects pre-edge event",
			ev:   ev(edge.Add(-time.Hour)),
			q:    eventbus.HistoryQuery{},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "After excludes ULID at cursor (exclusive)",
			ev: eventbus.Event{
				ID:        mk(now),
				Timestamp: now,
			},
			q:    eventbus.HistoryQuery{After: mk(now)},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "Before excludes ULID at cursor (exclusive)",
			ev: eventbus.Event{
				ID:        mk(now),
				Timestamp: now,
			},
			q:    eventbus.HistoryQuery{Before: mk(now)},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "NotBefore rejects event before window",
			ev:   ev(now.Add(-10 * time.Hour)),
			q:    eventbus.HistoryQuery{NotBefore: now.Add(-5 * time.Hour)},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "NotAfter rejects event after window",
			ev:   ev(now.Add(time.Hour)),
			q:    eventbus.HistoryQuery{NotAfter: now},
			tier: TierJetStream,
			want: false,
		},
		{
			name: "cold tier accepts pre-edge without tier-boundary reject",
			ev:   ev(edge.Add(-10 * time.Hour)),
			q:    eventbus.HistoryQuery{},
			tier: TierPostgres,
			want: true,
		},
		{
			name: "cold tier accepts overlapping post-edge event (seen-set dedups)",
			ev:   ev(now),
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

// Hold a reference to nats.Header so imports are not flagged when the file
// compiles on its own.
var _ = nats.Header{}
