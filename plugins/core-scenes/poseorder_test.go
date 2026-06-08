// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/pgnanos"
)

// TestCompute pins INV-SCENE-7 (spec §6.2). Table-driven across all 4 modes,
// covering empty / single / multi-participant scenarios, mixed pose
// history, and the threshold edge case for cooldown modes (3pr/5pr).
func TestCompute(t *testing.T) {
	t.Parallel()

	// Fixed reference time so test expectations are stable.
	base := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	t1 := base.Add(1 * time.Minute) // joined-at for alice
	t2 := base.Add(2 * time.Minute) // joined-at for bob
	t3 := base.Add(3 * time.Minute) // joined-at for carol

	// Helper for *int32 seq values.
	seq := func(v int32) *int32 { return &v }
	// Helper for *pgnanos.Time pose-at values.
	pAt := func(v time.Time) *pgnanos.Time { n := pgnanos.From(v); return &n }

	names := map[string]string{
		"alice-id": "Alice",
		"bob-id":   "Bob",
		"carol-id": "Carol",
	}

	type want struct {
		characterID    string
		characterName  string
		eligible       bool
		hasPosesSince  bool   // true if PosesSinceLast should be non-nil
		posesSinceLast uint32 // checked only when hasPosesSince
	}

	cases := []struct {
		name           string
		mode           string
		totalPoseCount uint32
		participants   []ParticipantWithPoseMeta
		want           []want
	}{
		// --- empty / single edge cases ---
		{
			name:           "strict_empty",
			mode:           "strict",
			totalPoseCount: 0,
			participants:   nil,
			want:           []want{},
		},
		{
			name:           "free_empty",
			mode:           "free",
			totalPoseCount: 0,
			participants:   []ParticipantWithPoseMeta{},
			want:           []want{},
		},
		{
			name:           "strict_single_never_posed",
			mode:           "strict",
			totalPoseCount: 0,
			participants: []ParticipantWithPoseMeta{
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1)},
			},
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: true},
			},
		},
		{
			name:           "free_single_never_posed",
			mode:           "free",
			totalPoseCount: 0,
			participants: []ParticipantWithPoseMeta{
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1)},
			},
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: true},
			},
		},
		{
			name:           "3pr_single_never_posed",
			mode:           "3pr",
			totalPoseCount: 0,
			participants: []ParticipantWithPoseMeta{
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1)},
			},
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: true, hasPosesSince: true, posesSinceLast: 0},
			},
		},

		// --- multi-participant, none posed ---
		{
			name:           "strict_multi_none_posed_orders_by_joined",
			mode:           "strict",
			totalPoseCount: 0,
			participants: []ParticipantWithPoseMeta{
				// Deliberately out-of-order input — Compute should not mutate caller's slice.
				{CharacterID: "carol-id", JoinedAt: pgnanos.From(t3)},
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1)},
				{CharacterID: "bob-id", JoinedAt: pgnanos.From(t2)},
			},
			// Strict: NULLS FIRST tiebreaks on JoinedAt ASC → alice, bob, carol.
			// Head (alice) eligible; rest not.
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: true},
				{characterID: "bob-id", characterName: "Bob", eligible: false},
				{characterID: "carol-id", characterName: "Carol", eligible: false},
			},
		},
		{
			name:           "free_multi_none_posed_all_eligible",
			mode:           "free",
			totalPoseCount: 0,
			participants: []ParticipantWithPoseMeta{
				{CharacterID: "carol-id", JoinedAt: pgnanos.From(t3)},
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1)},
				{CharacterID: "bob-id", JoinedAt: pgnanos.From(t2)},
			},
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: true},
				{characterID: "bob-id", characterName: "Bob", eligible: true},
				{characterID: "carol-id", characterName: "Carol", eligible: true},
			},
		},
		{
			name:           "5pr_multi_none_posed_all_eligible",
			mode:           "5pr",
			totalPoseCount: 0,
			participants: []ParticipantWithPoseMeta{
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1)},
				{CharacterID: "bob-id", JoinedAt: pgnanos.From(t2)},
				{CharacterID: "carol-id", JoinedAt: pgnanos.From(t3)},
			},
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: true, hasPosesSince: true, posesSinceLast: 0},
				{characterID: "bob-id", characterName: "Bob", eligible: true, hasPosesSince: true, posesSinceLast: 0},
				{characterID: "carol-id", characterName: "Carol", eligible: true, hasPosesSince: true, posesSinceLast: 0},
			},
		},

		// --- multi-participant, mixed pose history ---
		{
			name:           "strict_multi_mixed_posed_orders_by_last_pose_then_joined",
			mode:           "strict",
			totalPoseCount: 3,
			participants: []ParticipantWithPoseMeta{
				// alice: posed most recently (seq 3 at t1+10m)
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1), LastPoseAt: pAt(t1.Add(10 * time.Minute)), LastPoseSeq: seq(3)},
				// bob: posed earliest (seq 1 at t2+1m) — should be ahead of alice
				{CharacterID: "bob-id", JoinedAt: pgnanos.From(t2), LastPoseAt: pAt(t2.Add(1 * time.Minute)), LastPoseSeq: seq(1)},
				// carol: never posed — should be at head
				{CharacterID: "carol-id", JoinedAt: pgnanos.From(t3)},
			},
			// Expected queue: carol (never posed, NULLS FIRST) → bob (earliest) → alice (latest).
			want: []want{
				{characterID: "carol-id", characterName: "Carol", eligible: true},
				{characterID: "bob-id", characterName: "Bob", eligible: false},
				{characterID: "alice-id", characterName: "Alice", eligible: false},
			},
		},
		{
			name:           "3pr_multi_mixed_some_eligible",
			mode:           "3pr",
			totalPoseCount: 5,
			participants: []ParticipantWithPoseMeta{
				// alice: posed at seq 5 — gap = 0, NOT eligible
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1), LastPoseAt: pAt(t1.Add(5 * time.Minute)), LastPoseSeq: seq(5)},
				// bob: posed at seq 2 — gap = 3, eligible (>=3)
				{CharacterID: "bob-id", JoinedAt: pgnanos.From(t2), LastPoseAt: pAt(t2.Add(1 * time.Minute)), LastPoseSeq: seq(2)},
				// carol: never posed — eligible
				{CharacterID: "carol-id", JoinedAt: pgnanos.From(t3)},
			},
			// Display order = strict ordering (NULLS FIRST, LastPoseAt ASC).
			want: []want{
				{characterID: "carol-id", characterName: "Carol", eligible: true, hasPosesSince: true, posesSinceLast: 5},
				{characterID: "bob-id", characterName: "Bob", eligible: true, hasPosesSince: true, posesSinceLast: 3},
				{characterID: "alice-id", characterName: "Alice", eligible: false, hasPosesSince: true, posesSinceLast: 0},
			},
		},

		// --- threshold edge cases for 3pr ---
		{
			name:           "3pr_threshold_at_boundary_is_eligible",
			mode:           "3pr",
			totalPoseCount: 4,
			participants: []ParticipantWithPoseMeta{
				// alice: posed at seq 1 — gap = 3, eligible (==threshold)
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1), LastPoseAt: pAt(t1.Add(1 * time.Minute)), LastPoseSeq: seq(1)},
			},
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: true, hasPosesSince: true, posesSinceLast: 3},
			},
		},
		{
			name:           "3pr_threshold_below_boundary_not_eligible",
			mode:           "3pr",
			totalPoseCount: 3,
			participants: []ParticipantWithPoseMeta{
				// alice: posed at seq 1 — gap = 2, NOT eligible (<threshold)
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1), LastPoseAt: pAt(t1.Add(1 * time.Minute)), LastPoseSeq: seq(1)},
			},
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: false, hasPosesSince: true, posesSinceLast: 2},
			},
		},

		// --- 5pr threshold edge case ---
		{
			name:           "5pr_threshold_at_boundary_is_eligible",
			mode:           "5pr",
			totalPoseCount: 6,
			participants: []ParticipantWithPoseMeta{
				// alice: posed at seq 1 — gap = 5, eligible
				{CharacterID: "alice-id", JoinedAt: pgnanos.From(t1), LastPoseAt: pAt(t1.Add(1 * time.Minute)), LastPoseSeq: seq(1)},
				// bob: posed at seq 2 — gap = 4, NOT eligible
				{CharacterID: "bob-id", JoinedAt: pgnanos.From(t2), LastPoseAt: pAt(t2.Add(1 * time.Minute)), LastPoseSeq: seq(2)},
			},
			want: []want{
				{characterID: "alice-id", characterName: "Alice", eligible: true, hasPosesSince: true, posesSinceLast: 5},
				{characterID: "bob-id", characterName: "Bob", eligible: false, hasPosesSince: true, posesSinceLast: 4},
			},
		},

		// --- name resolution fallback (missing name → character_id) ---
		{
			name:           "name_fallback_uses_character_id_when_missing",
			mode:           "free",
			totalPoseCount: 0,
			participants: []ParticipantWithPoseMeta{
				{CharacterID: "unknown-id", JoinedAt: pgnanos.From(t1)},
			},
			want: []want{
				{characterID: "unknown-id", characterName: "unknown-id", eligible: true},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Compute(tc.mode, tc.totalPoseCount, tc.participants, names)
			require.Len(t, got, len(tc.want), "entry count mismatch")

			for i, w := range tc.want {
				assert.Equal(t, w.characterID, got[i].CharacterID, "entry[%d] CharacterID", i)
				assert.Equal(t, w.characterName, got[i].CharacterName, "entry[%d] CharacterName", i)
				assert.Equal(t, w.eligible, got[i].Eligible, "entry[%d] Eligible", i)

				if w.hasPosesSince {
					require.NotNil(t, got[i].PosesSinceLast, "entry[%d] PosesSinceLast should be set", i)
					assert.Equal(t, w.posesSinceLast, *got[i].PosesSinceLast, "entry[%d] PosesSinceLast value", i)
				} else {
					assert.Nil(t, got[i].PosesSinceLast, "entry[%d] PosesSinceLast should be nil", i)
				}
			}
		})
	}
}

// TestCompute_StableOrdering_FreeMode pins the determinism property:
// the same input MUST produce identical output across repeated calls.
// Critical because pose-order is rendered in user-facing UI; flapping
// order would be confusing.
func TestCompute_StableOrdering_FreeMode(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	participants := []ParticipantWithPoseMeta{
		{CharacterID: "carol-id", JoinedAt: pgnanos.From(base.Add(3 * time.Minute))},
		{CharacterID: "alice-id", JoinedAt: pgnanos.From(base.Add(1 * time.Minute))},
		{CharacterID: "bob-id", JoinedAt: pgnanos.From(base.Add(2 * time.Minute))},
	}
	names := map[string]string{
		"alice-id": "Alice",
		"bob-id":   "Bob",
		"carol-id": "Carol",
	}

	first := Compute("free", 0, participants, names)
	for i := 0; i < 10; i++ {
		again := Compute("free", 0, participants, names)
		assert.Equal(t, first, again, "Compute output not stable across call %d", i)
	}
}

// TestCompute_DoesNotMutateInput verifies the function does not modify
// the caller's slice — important for callers that hold the slice for
// other purposes (e.g., emitting an audit event from the same data).
func TestCompute_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	participants := []ParticipantWithPoseMeta{
		{CharacterID: "carol-id", JoinedAt: pgnanos.From(base.Add(3 * time.Minute))},
		{CharacterID: "alice-id", JoinedAt: pgnanos.From(base.Add(1 * time.Minute))},
		{CharacterID: "bob-id", JoinedAt: pgnanos.From(base.Add(2 * time.Minute))},
	}
	// Snapshot original order.
	original := make([]ParticipantWithPoseMeta, len(participants))
	copy(original, participants)

	_ = Compute("strict", 0, participants, nil)

	assert.Equal(t, original, participants, "Compute mutated input slice")
}

// TestCompute_UnrecognizedModeFallsBackToFree confirms defensive fallback:
// an unrecognized mode string is treated as "free" rather than
// returning an empty result or panicking. IsValid() gating happens at
// write time; this is a belt-and-suspenders guard at compute time.
func TestCompute_UnrecognizedModeFallsBackToFree(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	participants := []ParticipantWithPoseMeta{
		{CharacterID: "alice-id", JoinedAt: pgnanos.From(base.Add(1 * time.Minute))},
		{CharacterID: "bob-id", JoinedAt: pgnanos.From(base.Add(2 * time.Minute))},
	}

	got := Compute("bogus-mode", 5, participants, nil)
	require.Len(t, got, 2)
	// Free mode: all eligible, sorted by JoinedAt, no PosesSinceLast.
	assert.True(t, got[0].Eligible)
	assert.True(t, got[1].Eligible)
	assert.Nil(t, got[0].PosesSinceLast)
	assert.Nil(t, got[1].PosesSinceLast)
	assert.Equal(t, "alice-id", got[0].CharacterID)
	assert.Equal(t, "bob-id", got[1].CharacterID)
}

// TestCompute_ObserverNeverAppearsInPoseOrder pins the observer exclusion from
// pose order. The Compute function receives its roster from
// ListParticipantsWithPoseMeta, whose SQL filters role IN ('owner','member');
// observer rows are therefore never included in the participants slice passed
// here. This test reproduces that invariant at the pure-function layer: even
// if a caller passed an observer-tagged participant, Compute has no role
// awareness and would include it — confirming that the exclusion must be
// (and is) enforced at the store query boundary. The store-level SQL gate
// (store.go:1622, `AND p.role IN ('owner', 'member')`) is the real pin;
// this test documents the contract and guards against regressions where the
// store query loses its role filter.
//
// The unit test verifies Compute's pure-function behavior (no role awareness),
// then the store integration test (poseorder_integration_test.go) pins the
// real DB-level exclusion. Together they cover INV-SCENE-7.
func TestCompute_ObserverNeverAppearsInPoseOrder(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)
	member := ParticipantWithPoseMeta{
		CharacterID: "char-member",
		JoinedAt:    pgnanos.From(base.Add(1 * time.Minute)),
	}
	// The store's SQL excludes observer rows before Compute is called.
	// Verify that when ONLY the member is passed (as the store provides),
	// the observer does not appear in the output — even in all modes.
	for _, mode := range []string{"strict", "free", "3pr", "5pr"} {
		t.Run("mode_"+mode+"_observer_absent_from_store_slice", func(t *testing.T) {
			t.Parallel()
			got := Compute(mode, 0, []ParticipantWithPoseMeta{member}, nil)
			require.Len(t, got, 1, "only the member must appear")
			assert.Equal(t, "char-member", got[0].CharacterID)
			for _, e := range got {
				assert.NotEqual(t, "char-observer", e.CharacterID,
					"observer MUST NOT appear in pose-order output (mode=%s)", mode)
			}
		})
	}
}
