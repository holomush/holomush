// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

// Round 3 coverage: selectStartTier has 5 primary branches and was only at
// 66.7% after rounds 1+2. Table-driven tests drive every decision path so
// a future change to the routing logic lands with explicit coverage.

func TestSelectStartTierEveryBranch(t *testing.T) {
	t.Parallel()
	edge := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	// afterOnHot: ULID whose Timestamp is after edge (>= edge) → hot.
	afterOnHot, err := ulid.New(ulid.Timestamp(edge.Add(24*time.Hour)), nil)
	require.NoError(t, err)
	// afterOnCold: ULID whose Timestamp is before edge → cold.
	afterOnCold, err := ulid.New(ulid.Timestamp(edge.Add(-24*time.Hour)), nil)
	require.NoError(t, err)

	tests := []struct {
		name string
		q    eventbus.HistoryQuery
		now  time.Time
		want Tier
	}{
		{
			name: "after ulid after edge routes hot",
			q:    eventbus.HistoryQuery{After: afterOnHot, Direction: eventbus.DirectionForward},
			now:  now,
			want: TierJetStream,
		},
		{
			name: "after ulid before edge routes cold",
			q:    eventbus.HistoryQuery{After: afterOnCold, Direction: eventbus.DirectionForward},
			now:  now,
			want: TierPostgres,
		},
		{
			name: "forward unbounded NotBefore routes cold",
			q:    eventbus.HistoryQuery{Direction: eventbus.DirectionForward},
			now:  now,
			want: TierPostgres,
		},
		{
			name: "forward NotBefore before edge routes cold",
			q: eventbus.HistoryQuery{
				NotBefore: edge.Add(-10 * time.Hour),
				Direction: eventbus.DirectionForward,
			},
			now:  now,
			want: TierPostgres,
		},
		{
			name: "forward NotBefore at edge routes hot (tie goes to JS)",
			q: eventbus.HistoryQuery{
				NotBefore: edge,
				Direction: eventbus.DirectionForward,
			},
			now:  now,
			want: TierJetStream,
		},
		{
			name: "forward NotBefore after edge routes hot",
			q: eventbus.HistoryQuery{
				NotBefore: edge.Add(time.Hour),
				Direction: eventbus.DirectionForward,
			},
			now:  now,
			want: TierJetStream,
		},
		{
			name: "direction zero defaults to forward",
			q:    eventbus.HistoryQuery{}, // Direction == 0 → forward
			now:  now,
			want: TierPostgres, // zero NotBefore → unbounded → cold
		},
		{
			name: "backward unbounded NotAfter routes hot when now >= edge",
			q:    eventbus.HistoryQuery{Direction: eventbus.DirectionBackward},
			now:  now,
			want: TierJetStream,
		},
		{
			name: "backward unbounded NotAfter routes cold when now before edge (clock skew)",
			q:    eventbus.HistoryQuery{Direction: eventbus.DirectionBackward},
			now:  edge.Add(-time.Hour),
			want: TierPostgres,
		},
		{
			name: "backward NotAfter before edge routes cold",
			q: eventbus.HistoryQuery{
				NotAfter:  edge.Add(-time.Hour),
				Direction: eventbus.DirectionBackward,
			},
			now:  now,
			want: TierPostgres,
		},
		{
			name: "backward NotAfter at edge routes hot (tie goes to JS)",
			q: eventbus.HistoryQuery{
				NotAfter:  edge,
				Direction: eventbus.DirectionBackward,
			},
			now:  now,
			want: TierJetStream,
		},
		{
			name: "backward NotAfter after edge routes hot",
			q: eventbus.HistoryQuery{
				NotAfter:  edge.Add(time.Hour),
				Direction: eventbus.DirectionBackward,
			},
			now:  now,
			want: TierJetStream,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := selectStartTier(tc.q, edge, tc.now)
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
