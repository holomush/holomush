// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package core contains the core game engine types and logic.
package core

import (
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// StreamPrefixCharacter is the stream name prefix for character-scoped event
// streams (e.g., "character:01ABC"). Use this constant instead of the raw
// string literal to avoid magic values.
const StreamPrefixCharacter = "character:"

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

// Event types for game events.
//
// TODO(holomush-k18g): Migrate plugin-owned EventType constants out of
// internal/core/. The majority of the constants below are plugin-domain
// (communication, world movement, object interactions) but currently
// live here because the target Go plugin packages (core-world, core-comm)
// do not yet exist. Host-only types that stay: EventTypeSystem,
// EventTypeCommandResponse, EventTypeCommandError, EventTypeSessionEnded.
// F5 (holomush-1tvn.12) landed the audit-routing infrastructure that
// unblocks this migration.
const (
	EventTypeSay    EventType = "say"
	EventTypePose   EventType = "pose"
	EventTypeArrive EventType = "arrive"
	EventTypeLeave  EventType = "leave"
	EventTypeSystem EventType = "system"

	// World event types
	EventTypeMove          EventType = "move"
	EventTypeObjectCreate  EventType = "object_create"
	EventTypeObjectDestroy EventType = "object_destroy"
	EventTypeObjectUse     EventType = "object_use"
	EventTypeObjectExamine EventType = "object_examine"
	EventTypeObjectGive    EventType = "object_give"

	// Private communication event types
	EventTypePage    EventType = "page"
	EventTypeWhisper EventType = "whisper"
	EventTypeOOC     EventType = "ooc"
	EventTypePemit   EventType = "pemit"

	// Whisper notice (location-broadcast when someone whispers)
	EventTypeWhisperNotice EventType = "whisper_notice"

	// Command response event types
	EventTypeCommandResponse EventType = "command_response"
	EventTypeCommandError    EventType = "command_error"

	// UI state event types
	EventTypeLocationState EventType = "location_state"
	EventTypeExitUpdate    EventType = "exit_update"

	// Session lifecycle
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
type LocationStateChar struct {
	Name string `json:"name"`
	Idle bool   `json:"idle"`
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

// ActorSystemID is the well-known ID used for Actor{Kind: ActorSystem} events.
const ActorSystemID = "system"

// Actor represents who or what caused an event.
type Actor struct {
	Kind ActorKind
	ID   string // Character ID, plugin name, or ActorSystemID
}

// Event represents something that happened in the game world.
type Event struct {
	ID        ulid.ULID
	Stream    string // e.g., "location:01ABC", "character:01XYZ"
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
