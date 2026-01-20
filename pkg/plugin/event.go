// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugin defines the API for WASM plugins.
package plugin

// EventType identifies the kind of event.
type EventType string

// Event types that plugins can receive and emit.
const (
	EventTypeSay    EventType = "say"
	EventTypePose   EventType = "pose"
	EventTypeArrive EventType = "arrive"
	EventTypeLeave  EventType = "leave"
	EventTypeSystem EventType = "system"
)

// ActorKind identifies what type of entity caused an event.
type ActorKind uint8

// Actor kinds for event sources.
const (
	ActorCharacter ActorKind = iota
	ActorSystem
	ActorPlugin
)

// String returns the string representation of an ActorKind.
// Unrecognized kinds return "unknown".
func (k ActorKind) String() string {
	switch k {
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

// Event is the event structure passed to plugins.
// This is a simplified version of the internal Event type,
// serialized as JSON for WASM boundary crossing.
type Event struct {
	ID        string    `json:"id"`
	Stream    string    `json:"stream"`
	Type      EventType `json:"type"`
	Timestamp int64     `json:"timestamp"` // Unix milliseconds
	ActorKind ActorKind `json:"actor_kind"`
	ActorID   string    `json:"actor_id"`
	Payload   string    `json:"payload"` // JSON string
}

// Response is what plugins return after handling an event.
// Plugins can emit zero or more events in response.
type Response struct {
	// Events to emit. Each event will be published to its stream.
	Events []EmitEvent `json:"events,omitempty"`
}

// EmitEvent is an event that a plugin wants to emit.
type EmitEvent struct {
	Stream  string    `json:"stream"`
	Type    EventType `json:"type"`
	Payload string    `json:"payload"` // JSON string
}
