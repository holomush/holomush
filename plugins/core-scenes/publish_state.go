// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// PublishTrigger names a state-machine event. The legal transition is
// (from, trigger) → to per spec §4.1.
type PublishTrigger string

const (
	TriggerAllYes         PublishTrigger = "all_yes"
	TriggerAllVotedAnyNo  PublishTrigger = "all_voted_any_no"
	TriggerTimeout        PublishTrigger = "timeout"
	TriggerWithdraw       PublishTrigger = "withdraw"
	TriggerCoolOffElapsed PublishTrigger = "cooloff_elapsed"
	TriggerFlipNo         PublishTrigger = "flip_no"
	TriggerSnapshotFailed PublishTrigger = "snapshot_failed"
)

// NextStatus returns the next status for (from, trigger) per spec §4.1, or
// (_, false) if the transition is illegal. It is a pure function for unit
// testability; the database layer applies the transition via
// SceneStore.TransitionStatus. PUBLISHED and ATTEMPT_FAILED are terminal —
// every trigger from them is rejected.
func NextStatus(from PublishedSceneStatus, t PublishTrigger) (PublishedSceneStatus, bool) {
	switch from {
	case StatusCollecting:
		switch t {
		case TriggerAllYes:
			return StatusCoolOff, true
		case TriggerAllVotedAnyNo, TriggerTimeout, TriggerWithdraw:
			return StatusAttemptFailed, true
		}
	case StatusCoolOff:
		switch t {
		case TriggerCoolOffElapsed:
			return StatusPublished, true
		case TriggerFlipNo:
			return StatusCollecting, true
		case TriggerWithdraw, TriggerSnapshotFailed:
			return StatusAttemptFailed, true
		}
	}
	return "", false
}

// FailureReasonForTrigger maps a terminal-causing trigger to its
// failure_reason, or (_, false) if the reason is not derivable from the trigger
// alone. TriggerSnapshotFailed is deliberately NOT mapped: per spec §4.1, a
// snapshot failure resolves to SNAPSHOT_DECRYPT_FAILED *or*
// SNAPSHOT_RENDER_FAILED depending on which pipeline step failed, so the
// concrete reason is supplied by the snapshot caller (C7), not inferred here.
func FailureReasonForTrigger(t PublishTrigger) (PublishFailureReason, bool) {
	switch t {
	case TriggerAllVotedAnyNo:
		return FailureAnyNo, true
	case TriggerTimeout:
		return FailureTimeout, true
	case TriggerWithdraw:
		return FailureWithdrawn, true
	}
	return "", false
}

// ResolveFromTally returns the trigger to apply based on a vote tally during
// COLLECTING, or ("", false) if no resolution applies yet. Resolution requires
// all roster members to have voted (Pending == 0) AND a non-empty roster: a
// zero-vote tally (Yes == No == 0) does NOT resolve to TriggerAllYes — that
// would silently auto-publish a roster-less attempt. (StartScenePublish already
// rejects empty rosters via SCENE_PUBLISH_NO_ELIGIBLE_VOTERS, so this is a
// defensive guard against an impossible-but-catastrophic input.) Otherwise:
// any no → TriggerAllVotedAnyNo; unanimous yes → TriggerAllYes.
func ResolveFromTally(t VoteTally) (PublishTrigger, bool) {
	if t.Pending > 0 {
		return "", false
	}
	if t.Yes == 0 && t.No == 0 {
		return "", false
	}
	if t.No > 0 {
		return TriggerAllVotedAnyNo, true
	}
	return TriggerAllYes, true
}

// IsEligibleVoterRole returns true for roles that may be on a publish roster
// per INV-SCENE-28: owner and member only, NOT invited (and not any other role).
func IsEligibleVoterRole(role string) bool {
	return role == "owner" || role == "member"
}
