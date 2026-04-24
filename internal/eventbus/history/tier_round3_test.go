// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus"
)

// Round 3 coverage: selectStartTier has 5 primary branches and was only at
// 66.7% after rounds 1+2. Table-driven tests drive every decision path so
// a future change to the routing logic lands with explicit coverage.

// TestSelectStartTierEveryBranch drives every decision path via snapshot-based
// seq routing (the primary path) and time-bound fallback (when no cursor).
func TestSelectStartTierEveryBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	edge := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)

	// snapWith builds a pre-populated snapshot for routing tests.
	// firstSeq=10, lastSeq=1000 simulates a healthy stream.
	snapWith := func(firstSeq, lastSeq uint64) *StreamStateSnapshot {
		return newSnapshotForTest(firstSeq, lastSeq)
	}

	tests := []struct {
		name string
		q    eventbus.HistoryQuery
		now  time.Time
		snap *StreamStateSnapshot
		want Tier
	}{
		{
			name: "cursor seq >= firstSeq routes hot",
			q:    eventbus.HistoryQuery{AfterSeq: 500, Direction: eventbus.DirectionForward},
			now:  now,
			snap: snapWith(10, 1000),
			want: TierJetStream,
		},
		{
			name: "cursor seq < firstSeq routes cold",
			q:    eventbus.HistoryQuery{AfterSeq: 5, Direction: eventbus.DirectionForward},
			now:  now,
			snap: snapWith(10, 1000),
			want: TierPostgres,
		},
		{
			name: "cursor seq == firstSeq routes hot (inclusive boundary)",
			q:    eventbus.HistoryQuery{AfterSeq: 10, Direction: eventbus.DirectionForward},
			now:  now,
			snap: snapWith(10, 1000),
			want: TierJetStream,
		},
		{
			name: "backward cursor seq >= firstSeq routes hot",
			q:    eventbus.HistoryQuery{BeforeSeq: 500, Direction: eventbus.DirectionBackward},
			now:  now,
			snap: snapWith(10, 1000),
			want: TierJetStream,
		},
		{
			name: "backward cursor seq < firstSeq routes cold",
			q:    eventbus.HistoryQuery{BeforeSeq: 5, Direction: eventbus.DirectionBackward},
			now:  now,
			snap: snapWith(10, 1000),
			want: TierPostgres,
		},
		// Fallback paths (no cursor or no/nil snapshot)
		{
			name: "no cursor, nil snap, forward unbounded NotBefore routes cold",
			q:    eventbus.HistoryQuery{Direction: eventbus.DirectionForward},
			now:  now,
			snap: nil,
			want: TierPostgres,
		},
		{
			name: "no cursor, nil snap, forward NotBefore before edge routes cold",
			q: eventbus.HistoryQuery{
				NotBefore: edge.Add(-10 * time.Hour),
				Direction: eventbus.DirectionForward,
			},
			now:  now,
			snap: nil,
			want: TierPostgres,
		},
		{
			name: "no cursor, nil snap, forward NotBefore at edge routes hot (tie goes to JS)",
			q: eventbus.HistoryQuery{
				NotBefore: edge,
				Direction: eventbus.DirectionForward,
			},
			now:  now,
			snap: nil,
			want: TierJetStream,
		},
		{
			name: "no cursor, nil snap, forward NotBefore after edge routes hot",
			q: eventbus.HistoryQuery{
				NotBefore: edge.Add(time.Hour),
				Direction: eventbus.DirectionForward,
			},
			now:  now,
			snap: nil,
			want: TierJetStream,
		},
		{
			name: "no cursor, nil snap, direction zero defaults to forward",
			q:    eventbus.HistoryQuery{}, // Direction == 0 → forward
			now:  now,
			snap: nil,
			want: TierPostgres, // zero NotBefore → unbounded → cold
		},
		{
			name: "no cursor, nil snap, backward unbounded NotAfter routes hot when now >= edge",
			q:    eventbus.HistoryQuery{Direction: eventbus.DirectionBackward},
			now:  now,
			snap: nil,
			want: TierJetStream,
		},
		{
			name: "no cursor, nil snap, backward unbounded NotAfter routes cold when now before edge (clock skew)",
			q:    eventbus.HistoryQuery{Direction: eventbus.DirectionBackward},
			now:  edge.Add(-time.Hour),
			snap: nil,
			want: TierPostgres,
		},
		{
			name: "no cursor, nil snap, backward NotAfter before edge routes cold",
			q: eventbus.HistoryQuery{
				NotAfter:  edge.Add(-time.Hour),
				Direction: eventbus.DirectionBackward,
			},
			now:  now,
			snap: nil,
			want: TierPostgres,
		},
		{
			name: "no cursor, nil snap, backward NotAfter at edge routes hot (tie goes to JS)",
			q: eventbus.HistoryQuery{
				NotAfter:  edge,
				Direction: eventbus.DirectionBackward,
			},
			now:  now,
			snap: nil,
			want: TierJetStream,
		},
		{
			name: "no cursor, nil snap, backward NotAfter after edge routes hot",
			q: eventbus.HistoryQuery{
				NotAfter:  edge.Add(time.Hour),
				Direction: eventbus.DirectionBackward,
			},
			now:  now,
			snap: nil,
			want: TierJetStream,
		},
		{
			name: "snap with firstSeq=0 falls back to time routing (no NotBefore → cold)",
			q: eventbus.HistoryQuery{
				AfterSeq:  100,
				Direction: eventbus.DirectionForward,
			},
			now:  now,
			snap: newSnapshotForTest(0, 0), // firstSeq=0 → can't route by seq → time fallback
			want: TierPostgres,             // no NotBefore → unbounded forward → cold
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := selectStartTier(ctx, tc.q, edge, tc.now, tc.snap)
			assert.Equal(t, tc.want, got, "selectStartTier decision")
		})
	}
}

// TestOtherTier exercises the helper that flips hot↔cold. Tiny function
// but the 100% line reading is visible in coverage deltas.
func TestOtherTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Tier
		want Tier
	}{
		{name: "jetstream maps to postgres", in: TierJetStream, want: TierPostgres},
		{name: "postgres maps to jetstream", in: TierPostgres, want: TierJetStream},
		{name: "unknown tier falls back to jetstream", in: Tier(255), want: TierJetStream},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, otherTier(tc.in))
		})
	}
}
