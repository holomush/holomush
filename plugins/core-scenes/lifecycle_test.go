// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "testing"

func TestIsValidTransitionAllowsActiveToPaused(t *testing.T) {
	if !IsValidTransition(SceneStateActive, SceneStatePaused) {
		t.Error("active -> paused should be valid")
	}
}

func TestIsValidTransitionAllowsActiveToEnded(t *testing.T) {
	if !IsValidTransition(SceneStateActive, SceneStateEnded) {
		t.Error("active -> ended should be valid")
	}
}

func TestIsValidTransitionAllowsPausedToActive(t *testing.T) {
	if !IsValidTransition(SceneStatePaused, SceneStateActive) {
		t.Error("paused -> active should be valid (resume)")
	}
}

func TestIsValidTransitionAllowsPausedToEnded(t *testing.T) {
	if !IsValidTransition(SceneStatePaused, SceneStateEnded) {
		t.Error("paused -> ended should be valid")
	}
}

func TestIsValidTransitionAllowsEndedToArchived(t *testing.T) {
	if !IsValidTransition(SceneStateEnded, SceneStateArchived) {
		t.Error("ended -> archived should be valid (publish vote resolves)")
	}
}

func TestIsValidTransitionRejectsBackwardTransitions(t *testing.T) {
	// Per spec section 1.2: a scene MUST NOT transition backward.
	cases := []struct {
		from SceneState
		to   SceneState
	}{
		{SceneStatePaused, SceneStatePaused},   // self-transition
		{SceneStateEnded, SceneStateActive},    // ended cannot reanimate
		{SceneStateEnded, SceneStatePaused},    // ended cannot reanimate
		{SceneStateArchived, SceneStateActive}, // archived is terminal
		{SceneStateArchived, SceneStatePaused}, // archived is terminal
		{SceneStateArchived, SceneStateEnded},  // archived is terminal
		{SceneStateActive, SceneStateActive},   // self-transition
		{SceneStateActive, SceneStateArchived}, // skip ended state
	}
	for _, c := range cases {
		if IsValidTransition(c.from, c.to) {
			t.Errorf("transition %s -> %s should be rejected", c.from, c.to)
		}
	}
}

func TestIsValidTransitionRejectsUnknownStates(t *testing.T) {
	if IsValidTransition(SceneState("bogus"), SceneStateActive) {
		t.Error("bogus -> active should be rejected")
	}
	if IsValidTransition(SceneStateActive, SceneState("bogus")) {
		t.Error("active -> bogus should be rejected")
	}
}

func TestCanEndReturnsTrueForActiveAndPaused(t *testing.T) {
	if !CanEnd(SceneStateActive) {
		t.Error("active should be endable")
	}
	if !CanEnd(SceneStatePaused) {
		t.Error("paused should be endable")
	}
}

func TestCanEndReturnsFalseForEndedAndArchived(t *testing.T) {
	if CanEnd(SceneStateEnded) {
		t.Error("ended should not be endable")
	}
	if CanEnd(SceneStateArchived) {
		t.Error("archived should not be endable")
	}
}

func TestCanPauseReturnsTrueOnlyForActive(t *testing.T) {
	if !CanPause(SceneStateActive) {
		t.Error("active should be pausable")
	}
	if CanPause(SceneStatePaused) {
		t.Error("paused should not be re-pausable")
	}
	if CanPause(SceneStateEnded) {
		t.Error("ended should not be pausable")
	}
}

func TestCanResumeReturnsTrueOnlyForPaused(t *testing.T) {
	if !CanResume(SceneStatePaused) {
		t.Error("paused should be resumable")
	}
	if CanResume(SceneStateActive) {
		t.Error("active should not be resumable (already active)")
	}
	if CanResume(SceneStateEnded) {
		t.Error("ended should not be resumable")
	}
}

func TestCanUpdateReturnsFalseForEndedOrArchived(t *testing.T) {
	if CanUpdate(SceneStateEnded) {
		t.Error("ended should not be updatable")
	}
	if CanUpdate(SceneStateArchived) {
		t.Error("archived should not be updatable")
	}
	if !CanUpdate(SceneStateActive) {
		t.Error("active should be updatable")
	}
	if !CanUpdate(SceneStatePaused) {
		t.Error("paused should be updatable")
	}
}
