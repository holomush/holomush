// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package eventvocab is the dependency-free event-type vocabulary leaf: the
// wire-type discriminators and JSON payload shapes shared by every consumer
// of an event, regardless of which side of the process boundary it sits on.
// It is importable by both internal/eventbus and the gateway (internal/web,
// internal/telnet) per INV-EVENTBUS-1 — the package MUST have zero
// holomush/internal imports so importing it never pulls a gateway across the
// protocol-translation-only boundary.
package eventvocab

import (
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
// invoke this prior to publishing or accepting an event.
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

// CommandResponsePayload is the JSON payload for command_response and
// command_error events. The event type itself carries the error distinction.
type CommandResponsePayload struct {
	Text string `json:"text"`
}
