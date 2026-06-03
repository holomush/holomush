// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"sort"
	"time"
)

// PoseOrderEntry is the Go-side pose-order entry. Mirrors the proto
// type scene.v1.PoseOrderEntry; the GetPoseOrder handler (Task 20)
// maps this in-memory form to the proto wire shape.
//
// LastPosedAt is nil when the participant has never posed in this
// scene. PosesSinceLast is a pointer so the handler can omit it on the
// wire for modes where the value is not meaningful (strict, free).
type PoseOrderEntry struct {
	CharacterID    string
	CharacterName  string
	Eligible       bool
	LastPosedAt    *time.Time
	PosesSinceLast *uint32
}

// Compute returns the pose-order entries for the given scene mode +
// maintained metadata. Pure function: no DB, no globals, no I/O.
// Pins INV-SCENE-7 (spec §6.2).
//
// Parameters:
//   - mode: "strict" | "3pr" | "5pr" | "free" (scene's pose_order_mode).
//     An unrecognized mode is treated as "free" — defensive fallback;
//     IsValid() gating happens at write time.
//   - totalPoseCount: cumulative scene_pose count
//     (scenes.total_pose_count).
//   - participants: per-participant pose metadata from
//     ListParticipantsWithPoseMeta.
//   - names: character_id → character_name lookup. May be empty/nil;
//     missing entries render as the raw character_id (best-effort —
//     the handler is responsible for resolving names; this helper
//     never fails on missing names).
//
// Returns: ordered entries. strict/3pr/5pr sort by
// (LastPoseSeq NULLS FIRST, LastPoseAt ASC, JoinedAt ASC) for stable
// display. free sorts by JoinedAt ASC.
func Compute(
	mode string,
	totalPoseCount uint32,
	participants []ParticipantWithPoseMeta,
	names map[string]string,
) []PoseOrderEntry {
	if len(participants) == 0 {
		return []PoseOrderEntry{}
	}

	// Copy + sort per mode. Never mutate the caller's slice.
	sorted := make([]ParticipantWithPoseMeta, len(participants))
	copy(sorted, participants)

	switch PoseOrderMode(mode) {
	case PoseOrderModeStrict, PoseOrderMode3PR, PoseOrderMode5PR:
		sortStrictQueue(sorted)
	default: // free + unrecognized fallback
		sortByJoinedAt(sorted)
	}

	entries := make([]PoseOrderEntry, len(sorted))
	for i, p := range sorted {
		var lastPosedAt *time.Time
		if p.LastPoseAt != nil {
			t := p.LastPoseAt.Time()
			lastPosedAt = &t
		}
		entries[i] = PoseOrderEntry{
			CharacterID:   p.CharacterID,
			CharacterName: resolveName(p.CharacterID, names),
			Eligible:      eligibility(mode, totalPoseCount, p, i),
			LastPosedAt:   lastPosedAt,
		}
		// PosesSinceLast is only populated for cooldown modes.
		if PoseOrderMode(mode) == PoseOrderMode3PR || PoseOrderMode(mode) == PoseOrderMode5PR {
			gap := posesSinceLast(totalPoseCount, p.LastPoseSeq)
			entries[i].PosesSinceLast = &gap
		}
	}
	return entries
}

// sortStrictQueue sorts in-place by (LastPoseSeq NULLS FIRST,
// LastPoseAt ASC, JoinedAt ASC). Never-posed participants (LastPoseSeq
// == nil) appear at the head of the queue, then posed participants in
// order of oldest last-pose first. JoinedAt is the deterministic
// tiebreaker.
func sortStrictQueue(ps []ParticipantWithPoseMeta) {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]

		// NULLS FIRST on LastPoseSeq: never-posed (nil) sorts before posed.
		aNever := a.LastPoseSeq == nil
		bNever := b.LastPoseSeq == nil
		if aNever != bNever {
			return aNever // i never-posed → i first
		}
		if aNever && bNever {
			// Both never posed: tiebreak by JoinedAt ASC.
			return a.JoinedAt.Time().Before(b.JoinedAt.Time())
		}

		// Both have posed: oldest LastPoseAt first.
		if a.LastPoseAt != nil && b.LastPoseAt != nil && !a.LastPoseAt.Time().Equal(b.LastPoseAt.Time()) {
			return a.LastPoseAt.Time().Before(b.LastPoseAt.Time())
		}
		// Identical (or unexpectedly-nil) LastPoseAt: tiebreak by JoinedAt ASC.
		return a.JoinedAt.Time().Before(b.JoinedAt.Time())
	})
}

// sortByJoinedAt sorts in-place by JoinedAt ASC. Used by `free` mode
// and as the natural display order for any non-queue mode.
func sortByJoinedAt(ps []ParticipantWithPoseMeta) {
	sort.SliceStable(ps, func(i, j int) bool {
		return ps[i].JoinedAt.Time().Before(ps[j].JoinedAt.Time())
	})
}

// eligibility evaluates whether a participant may pose next under the
// given mode. `index` is the participant's position in the
// post-sort entries slice (used by strict to identify the queue head).
func eligibility(
	mode string,
	totalPoseCount uint32,
	p ParticipantWithPoseMeta,
	index int,
) bool {
	switch PoseOrderMode(mode) {
	case PoseOrderModeStrict:
		// Only the queue head (index 0) is eligible.
		return index == 0
	case PoseOrderMode3PR:
		return eligibleByThreshold(totalPoseCount, p.LastPoseSeq, 3)
	case PoseOrderMode5PR:
		return eligibleByThreshold(totalPoseCount, p.LastPoseSeq, 5)
	default: // free + unrecognized fallback
		return true
	}
}

// eligibleByThreshold implements the 3pr/5pr cooldown check:
// `eligible = last_pose_seq IS NULL OR (total_pose_count - last_pose_seq) >= threshold`.
// Never-posed participants are always eligible.
func eligibleByThreshold(totalPoseCount uint32, lastPoseSeq *int32, threshold uint32) bool {
	if lastPoseSeq == nil {
		return true
	}
	// Guard against negative or unexpectedly-large seq values.
	// InsertScenePose writes last_pose_seq from the RETURNING value of
	// the same total_pose_count UPDATE that just bumped the counter,
	// so the (last_pose_seq <= total_pose_count) relationship holds
	// at commit time. This branch is defense-in-depth for operator
	// drift or future writers that don't share that transaction.
	if *lastPoseSeq < 0 {
		return true
	}
	// Safe conversion: the *lastPoseSeq < 0 guard above ensures the
	// value fits in uint32.
	seq := uint32(*lastPoseSeq)
	if seq > totalPoseCount {
		// Logically impossible per schema; treat as never-posed-equivalent.
		return true
	}
	return (totalPoseCount - seq) >= threshold
}

// posesSinceLast computes `total_pose_count - COALESCE(last_pose_seq, 0)`.
// Surfaced on PoseOrderEntry.PosesSinceLast for client UX rendering
// in 3pr/5pr modes ("Carol (2/3 since)").
func posesSinceLast(totalPoseCount uint32, lastPoseSeq *int32) uint32 {
	if lastPoseSeq == nil {
		return totalPoseCount
	}
	if *lastPoseSeq < 0 {
		return totalPoseCount
	}
	// Safe conversion: the *lastPoseSeq < 0 guard above ensures the
	// value fits in uint32.
	seq := uint32(*lastPoseSeq)
	if seq > totalPoseCount {
		return 0
	}
	return totalPoseCount - seq
}

// resolveName returns names[id] if present, else id. Best-effort: the
// handler may pass a nil/empty map (e.g., when the name resolver
// errors); falling back to the character_id keeps the response
// usable rather than failing the RPC.
func resolveName(id string, names map[string]string) string {
	if name, ok := names[id]; ok && name != "" {
		return name
	}
	return id
}
