// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// allTriggers is every PublishTrigger, used to exhaustively probe rejection
// from terminal states.
var allTriggers = []PublishTrigger{
	TriggerAllYes, TriggerAllVotedAnyNo, TriggerTimeout, TriggerWithdraw,
	TriggerCoolOffElapsed, TriggerFlipNo, TriggerSnapshotFailed,
}

// TestPublishStateMachine_TransitionTable asserts every legal (from, trigger)
// transition and that every illegal pair is rejected (ok == false) — the pure
// predicate behind the store's SCENE_PUBLISH_INVALID_TRANSITION guard. Spec §4.1.
func TestPublishStateMachine_TransitionTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		from       PublishedSceneStatus
		trigger    PublishTrigger
		wantTo     PublishedSceneStatus
		wantReject bool
	}{
		{"collecting + all-yes transitions to cooloff", StatusCollecting, TriggerAllYes, StatusCoolOff, false},
		{"collecting + any-no transitions to attempt-failed", StatusCollecting, TriggerAllVotedAnyNo, StatusAttemptFailed, false},
		{"collecting + timeout transitions to attempt-failed", StatusCollecting, TriggerTimeout, StatusAttemptFailed, false},
		{"collecting + withdraw transitions to attempt-failed", StatusCollecting, TriggerWithdraw, StatusAttemptFailed, false},
		{"cooloff + window-elapsed transitions to published", StatusCoolOff, TriggerCoolOffElapsed, StatusPublished, false},
		{"cooloff + flip-no returns to collecting", StatusCoolOff, TriggerFlipNo, StatusCollecting, false},
		{"cooloff + withdraw transitions to attempt-failed", StatusCoolOff, TriggerWithdraw, StatusAttemptFailed, false},
		{"cooloff + snapshot-failed transitions to attempt-failed", StatusCoolOff, TriggerSnapshotFailed, StatusAttemptFailed, false},
		{"collecting + cooloff-elapsed is rejected", StatusCollecting, TriggerCoolOffElapsed, "", true},
		{"collecting + flip-no is rejected", StatusCollecting, TriggerFlipNo, "", true},
		{"collecting + snapshot-failed is rejected", StatusCollecting, TriggerSnapshotFailed, "", true},
		{"cooloff + all-yes is rejected", StatusCoolOff, TriggerAllYes, "", true},
		{"cooloff + timeout is rejected", StatusCoolOff, TriggerTimeout, "", true},
		{"cooloff + any-no is rejected", StatusCoolOff, TriggerAllVotedAnyNo, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			to, ok := NextStatus(tc.from, tc.trigger)
			if tc.wantReject {
				assert.False(t, ok, "transition must be rejected")
				return
			}
			assert.True(t, ok, "transition must be legal")
			assert.Equal(t, tc.wantTo, to)
		})
	}
}

// TestPublishStateMachine_RejectsBackwardFromTerminal asserts the terminal
// states PUBLISHED and ATTEMPT_FAILED reject EVERY trigger — no outbound
// transition exists. Spec §4.1.
func TestPublishStateMachine_RejectsBackwardFromTerminal(t *testing.T) {
	t.Parallel()
	for _, from := range []PublishedSceneStatus{StatusPublished, StatusAttemptFailed} {
		for _, trig := range allTriggers {
			_, ok := NextStatus(from, trig)
			assert.False(t, ok, "terminal %s must reject trigger %s", from, trig)
		}
	}
}

// TestPublishStateMachine_ResolutionTriggers asserts the vote-resolution
// transitions from the active states. Spec §4.1.
func TestPublishStateMachine_ResolutionTriggers(t *testing.T) {
	t.Parallel()

	to, ok := NextStatus(StatusCollecting, TriggerAllYes)
	assert.True(t, ok)
	assert.Equal(t, StatusCoolOff, to, "all-yes moves COLLECTING into the cool-off window")

	to, ok = NextStatus(StatusCollecting, TriggerAllVotedAnyNo)
	assert.True(t, ok)
	assert.Equal(t, StatusAttemptFailed, to, "any-no after all voted fails the attempt")

	to, ok = NextStatus(StatusCollecting, TriggerTimeout)
	assert.True(t, ok)
	assert.Equal(t, StatusAttemptFailed, to, "timeout with pending voters fails the attempt")

	// Owner withdraw fails the attempt from EITHER active state.
	for _, from := range []PublishedSceneStatus{StatusCollecting, StatusCoolOff} {
		to, ok = NextStatus(from, TriggerWithdraw)
		assert.True(t, ok)
		assert.Equal(t, StatusAttemptFailed, to, "owner withdraw fails the attempt from %s", from)
	}
}

// TestPublishStateMachine_CoolOffFlipBack asserts that a no-vote during COOLOFF
// (flip) returns the attempt to COLLECTING rather than failing it (INV-P6-2:
// once an attempt enters COOLOFF, votes may change only by voting no, which
// transitions the attempt back to COLLECTING). Spec §4.1.
// (Vote-state preservation across the flip is a store/handler concern verified
// in the B3 vote-handler tests, not this pure transition predicate.)
func TestPublishStateMachine_CoolOffFlipBack(t *testing.T) {
	t.Parallel()
	to, ok := NextStatus(StatusCoolOff, TriggerFlipNo)
	assert.True(t, ok)
	assert.Equal(t, StatusCollecting, to, "a flip-to-no during cool-off reopens COLLECTING")
}

// TestFailureReasonForTrigger maps each terminal-causing trigger to its
// failure_reason and rejects triggers whose reason is not derivable from the
// trigger alone (notably TriggerSnapshotFailed — see the function doc).
func TestFailureReasonForTrigger(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		trigger    PublishTrigger
		wantReason PublishFailureReason
		wantOK     bool
	}{
		{"any-no maps to ANY_NO", TriggerAllVotedAnyNo, FailureAnyNo, true},
		{"timeout maps to TIMEOUT", TriggerTimeout, FailureTimeout, true},
		{"withdraw maps to WITHDRAWN", TriggerWithdraw, FailureWithdrawn, true},
		{"snapshot-failed is not derivable (decrypt vs render decided by C7)", TriggerSnapshotFailed, "", false},
		{"all-yes is not a failure", TriggerAllYes, "", false},
		{"cooloff-elapsed is not a failure", TriggerCoolOffElapsed, "", false},
		{"flip-no is not a failure", TriggerFlipNo, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := FailureReasonForTrigger(tc.trigger)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantReason, reason)
			}
		})
	}
}
