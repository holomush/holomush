// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

// SessionEndedPayload is the JSON payload for session_ended events.
//
// Emitted on the character's own stream (character:{ID}) when a session
// terminates for any reason. Subscribers filter on SessionID to determine
// whether the termination is for their own session; a non-matching
// session_ended is forwarded verbatim for audit/UX value but does NOT
// terminate the Subscribe stream.
//
// See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md
// for the full design rationale and load-bearing invariants.
type SessionEndedPayload struct {
	SessionID   string `json:"session_id"`   // ULID of the ended session
	CharacterID string `json:"character_id"` // ULID of the character whose session ended
	Cause       string `json:"cause"`        // quit|logout|guest_end|kicked|reaped|evicted
	Reason      string `json:"reason"`       // human-readable; delivered to client as STREAM_CLOSED message
}

// Cause constants for SessionEndedPayload.Cause.
const (
	SessionEndedCauseQuit     = "quit"
	SessionEndedCauseLogout   = "logout"
	SessionEndedCauseGuestEnd = "guest_end"
	SessionEndedCauseKicked   = "kicked"
	SessionEndedCauseReaped   = "reaped"
	SessionEndedCauseEvicted  = "evicted"
)
