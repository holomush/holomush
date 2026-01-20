// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugin defines the shared API types for all plugin hosts (Lua, binary).
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
// This is a simplified version of the internal Event type.
type Event struct {
	ID        string
	Stream    string
	Type      EventType
	Timestamp int64 // Unix milliseconds
	ActorKind ActorKind
	ActorID   string
	Payload   string // JSON string
}

// Response is what plugins return after handling an event.
// Plugins can emit zero or more events in response.
type Response struct {
	// Events to emit. Each event will be published to its stream.
	Events []EmitEvent
}

// EmitEvent is an event that a plugin wants to emit.
type EmitEvent struct {
	Stream  string
	Type    EventType
	Payload string // JSON string
}
