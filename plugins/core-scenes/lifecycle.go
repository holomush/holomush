// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// IsValidTransition reports whether a scene can transition from `from` to `to`.
//
// Per spec section 1.2:
//
//	active  -> paused | ended
//	paused  -> active | ended
//	ended   -> archived
//
// A scene MUST NOT transition backward (e.g., ended -> active). Self
// transitions (active -> active) are also rejected — they're meaningless
// and would mask bugs in the calling code.
func IsValidTransition(from, to SceneState) bool {
	if !from.IsValid() || !to.IsValid() {
		return false
	}
	switch from {
	case SceneStateActive:
		return to == SceneStatePaused || to == SceneStateEnded
	case SceneStatePaused:
		return to == SceneStateActive || to == SceneStateEnded
	case SceneStateEnded:
		return to == SceneStateArchived
	case SceneStateArchived:
		return false // terminal
	}
	return false
}

// CanEnd reports whether a scene in the given state can be ended by the owner.
// End is allowed from active or paused; not from ended or archived.
func CanEnd(state SceneState) bool {
	return state == SceneStateActive || state == SceneStatePaused
}

// CanPause reports whether a scene in the given state can be paused by the owner.
// Pause is only allowed from active.
func CanPause(state SceneState) bool {
	return state == SceneStateActive
}

// CanResume reports whether a scene in the given state can be resumed.
// Resume is only allowed from paused; this function checks the state
// precondition only. The participant gate (any participant per spec D6) is
// enforced by the ABAC resume-scene-as-participant policy.
func CanResume(state SceneState) bool {
	return state == SceneStatePaused
}

// CanUpdate reports whether a scene in the given state accepts metadata
// updates (UpdateScene RPC). Ended and archived scenes are immutable.
func CanUpdate(state SceneState) bool {
	return state == SceneStateActive || state == SceneStatePaused
}
