// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package core contains the core game engine types and logic.
package core

import (
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// MaxPayloadSize is the maximum allowed size (in bytes) of an event payload.
// Events exceeding this size are rejected before they reach the store to
// prevent DoS, disk-space, and bandwidth-amplification attacks originating
// from buggy or malicious plugins. 64 KiB comfortably accommodates every
// legitimate in-tree event payload.
const MaxPayloadSize = 64 * 1024

// ValidatePayload returns a structured oops error with code
// "EVENT_PAYLOAD_TOO_LARGE" when payload exceeds MaxPayloadSize, and nil
// otherwise. Call sites that accept plugin- or network-sourced events MUST
// invoke this prior to calling EventStore.Append.
func ValidatePayload(payload []byte) error {
	if len(payload) > MaxPayloadSize {
		return oops.Code("EVENT_PAYLOAD_TOO_LARGE").
			With("payload_size", len(payload)).
			With("max_payload_size", MaxPayloadSize).
			Errorf("event payload exceeds maximum size")
	}
	return nil
}

// EventType identifies the kind of event.
type EventType string

// Event types for game events. All constants here are host-owned and stay
// permanently. Plugin-owned event types have been migrated to their respective
// plugin packages with qualified <plugin>:<type> identifiers:
//   - core-communication: plugins/core-communication/events.go
//   - core-objects: plugins/core-objects/events.go
const (
	EventTypeArrive EventType = "arrive"
	EventTypeLeave  EventType = "leave"
	EventTypeSystem EventType = "system"

	// World movement (host-owned)
	EventTypeMove EventType = "move"

	// Command response event types (host-owned)
	EventTypeCommandResponse EventType = "command_response"
	EventTypeCommandError    EventType = "command_error"

	// UI state event types (host-owned)
	EventTypeLocationState EventType = "location_state"
	EventTypeExitUpdate    EventType = "exit_update"

	// Session lifecycle (host-owned)
	EventTypeSessionEnded EventType = "session_ended"
)

// LocationStatePayload is the JSON payload for location_state events, providing
// a full snapshot of the character's current location.
type LocationStatePayload struct {
	Location LocationStateInfo   `json:"location"`
	Exits    []LocationStateExit `json:"exits"`
	Present  []LocationStateChar `json:"present"`
}

// LocationStateInfo describes the location in a location_state event.
type LocationStateInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// LocationStateExit describes an exit visible from the current location.
type LocationStateExit struct {
	Direction string `json:"direction"`
	Name      string `json:"name"`
	Locked    bool   `json:"locked"`
}

// LocationStateChar describes a character present in the current location.
//
// CharacterID is the ULID identifying the character; it MUST be populated by
// the emit site (internal/grpc/location_follow.go) so client-side stores can
// key by stable identity rather than display name. The wire-format gap that
// pre-dated this field is documented in bead holomush-e4qo.
type LocationStateChar struct {
	CharacterID string `json:"character_id"`
	Name        string `json:"name"`
	Idle        bool   `json:"idle"`
}

// ExitUpdatePayload is the JSON payload for exit_update events, providing a
// delta update to the exits in the current location.
type ExitUpdatePayload struct {
	Exits []LocationStateExit `json:"exits"`
}

// PagePayload is the JSON payload for page events (private messages between characters).
type PagePayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Message    string `json:"message"`
	IsPose     bool   `json:"is_pose"`
}

// WhisperPayload is the JSON payload for whisper events (location-scoped private messages).
type WhisperPayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Message    string `json:"message"`
	IsPose     bool   `json:"is_pose"`
}

// WhisperNoticePayload is the JSON payload for whisper notice events, informing
// bystanders that a whisper occurred without revealing its content.
type WhisperNoticePayload struct {
	SenderName string `json:"sender_name"`
	TargetName string `json:"target_name"`
	Notice     string `json:"notice"`
}

// OOCPayload carries an OOC communication event.
type OOCPayload struct {
	CharacterName string `json:"character_name"`
	Message       string `json:"message"`
	Style         string `json:"style"` // "say", "pose", "semipose"
}

// PemitPayload carries a private emit event.
type PemitPayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	TargetID   string `json:"target_id"`
	Message    string `json:"message"`
}

// ActorKind identifies what type of entity caused an event.
type ActorKind uint8

// Actor kinds for event sources.
const (
	ActorCharacter ActorKind = iota
	ActorSystem
	ActorPlugin
)

func (a ActorKind) String() string {
	switch a {
	case ActorCharacter:
		return "character"
	case ActorSystem:
		return "system"
	case ActorPlugin:
		return "plugin"
	default:
		return "unknown"
	}
}

// SystemActorULID is the canonical identity for the host's "system" actor —
// the categorical bucket for events emitted by the host itself rather than
// by a character, player, or plugin. Defined as a fixed byte pattern (not
// entropy-generated) so audit rows and history queries reliably round-trip
// the same identity. The all-zero leading 15 bytes plus a low-numbered tag
// byte make sentinels visually distinguishable from real entropy ULIDs in
// logs.
var SystemActorULID = ulid.ULID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01}

// WorldServiceActorULID is the identity for events emitted by the world
// service subsystem (location/object/exit lifecycle).
var WorldServiceActorULID = ulid.ULID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x02}

// ActorSystemID is the canonical Actor.ID value for the host's system
// actor. Pre-w9ml this was the literal "system"; post-w9ml it is the
// canonical ULID-string form of SystemActorULID. The 1 production + 4 test
// call sites compile unchanged — only the value flowing through them
// changes (string → ULID-string).
var ActorSystemID = SystemActorULID.String()

// SystemBroadcastSubject is the reserved stream (the `stream` argument to
// NewEvent) for grid-wide system broadcasts — server announcements and the
// admin `wall` command. The event appender qualifies it to
// events.<game_id>.system at the emit boundary. Both the command-layer
// broadcast path (command.Services.BroadcastSystemMessage / the shutdown
// command) and the plugin-host SessionAdmin broadcast backing
// (hostcap.NewSystemBroadcaster, decision holomush-t019a) MUST agree on this
// value so a plugin `wall` and a host announcement land on the same subject.
const SystemBroadcastSubject = "system"

// IsSentinelULID returns true iff id is a system actor sentinel ULID:
// first 15 bytes zero, last byte in [0x01, 0xFF]. Used by IdentityRegistry
// bootstrap (sentinel-collision detection on plugin row load) and by
// TestSentinelTagsUnique. Tag 0x00 is reserved as "no sentinel" — the
// all-zero ULID is the proto3 zero-value and would be wire-indistinguishable
// from absence-of-id.
//
// Tag-byte allocation policy: tags MUST be unique across the codebase and
// MUST be allocated via PR review of this file (single source of truth).
// Existing allocations: 0x01 = SystemActorULID, 0x02 = WorldServiceActorULID.
func IsSentinelULID(id ulid.ULID) bool {
	if id[15] == 0x00 {
		return false
	}
	for i := 0; i < 15; i++ {
		if id[i] != 0 {
			return false
		}
	}
	return true
}

// Actor represents who or what caused an event.
type Actor struct {
	Kind ActorKind
	// ID is the canonical ULID-string identity:
	//   Character: ULID from the user store (already in place).
	//   Plugin:    ULID from the plugin registry (resolved at stamp time
	//              via plugins.IdentityRegistry.IDByName, post-w9ml).
	//   System:    one of the sentinel ULID-strings above (SystemActorULID,
	//              WorldServiceActorULID, …) accessed via ActorSystemID or
	//              the typed sentinel constants.
	// core.Actor has no Unknown kind — empty ID is undefined behavior at
	// the core layer; the bus translation maps to ActorKindUnknown.
	ID string
}

// Event represents something that happened in the game world.
type Event struct {
	ID        ulid.ULID
	Stream    string // e.g., "location.01ABC", "character.01XYZ"
	Type      EventType
	Timestamp time.Time
	Actor     Actor
	Payload   []byte // JSON
}

// NewEvent constructs an Event with a monotonic ULID (from NewULID) and
// the current timestamp. This is the ONLY blessed construction path for
// events that will be appended to an EventStore.
//
// Invariant I-16 (Event ID Monotonicity): every event appended to
// EventStore MUST be constructed via NewEvent, which assigns the ID from
// NewULID() -- a monotonic-within-millisecond generator. idgen.New() and
// ulid.Make() are forbidden for event IDs because they produce
// non-monotonic IDs that silently break PostgresEventStore.Replay
// (WHERE id > afterID ORDER BY id) and cursor CAS advances.
//
// See docs/superpowers/specs/2026-04-11-focus-substrate-design.md section 3.1.
func NewEvent(stream string, eventType EventType, actor Actor, payload []byte) Event {
	return Event{
		ID:        NewULID(),
		Stream:    stream,
		Type:      eventType,
		Timestamp: time.Now(),
		Actor:     actor,
		Payload:   payload,
	}
}
