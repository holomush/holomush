// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

// SessionEndedPayload is the JSON payload for session_ended events.
//
// Emitted on the character's own subject (events.<game_id>.character.<id>) when
// a session terminates for any reason. The publish site
// (internal/presence/session_ended.go) emits the domain-relative
// character.<id>, which eventbus.Qualify prefixes; the dot form is load-bearing
// for the retirement reactor's consumer filter and its positional aggregate
// parse. Colon-style subjects were eradicated (holomush-rops) — the only
// surviving colon usage is the ABAC policy DSL's type prefix.
//
// Subscribers filter on SessionID to determine
// whether the termination is for their own session; a non-matching
// session_ended is forwarded verbatim for audit/UX value but does NOT
// terminate the Subscribe stream.
//
// See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md
// for the full design rationale and load-bearing invariants.
type SessionEndedPayload struct {
	SessionID   string `json:"session_id"`   // ULID of the ended session
	CharacterID string `json:"character_id"` // ULID of the character whose session ended
	Cause       string `json:"cause"`        // quit|logout|guest_end|kicked|reaped|evicted|retired
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
	// SessionEndedCauseRetired is the character-retirement fanout's cause
	// (IDENT-04, D-36). It is distinct from evicted and kicked on purpose:
	// those are involuntary terminations of a character that remains playable,
	// whereas a retired character has left active play entirely and its
	// session will not be re-established until an admin un-retires it.
	SessionEndedCauseRetired = "retired"
)
