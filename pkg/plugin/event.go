// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package pluginsdk provides the public API for HoloMUSH plugins.
//
// This package serves two purposes:
//   - Shared types (Event, EmitEvent, EventType, ActorKind) used by all plugin hosts
//   - SDK for building binary plugins (Handler, Serve, ServeConfig)
package pluginsdk

// EventType identifies the kind of event.
type EventType string

// Plugin-owned event-type constants live in the owning plugin's Go package
// (e.g., plugins/core-communication/events.go). The SDK exposes the
// EventType string type only. Cross-package event-type references use the
// qualified form "<plugin>:<event_type>" per spec §7.1.
//
// Host-owned event-type strings (e.g., "system" for system events) are
// constants in internal/core/event.go and are re-exported below as typed
// SDK constants so plugin code (which cannot import internal/core per the
// plugin-boundary discipline) can reference them without resorting to magic strings.

// Host-owned event-type constants. These name event types emitted by
// the host runtime — and by plugins emitting on behalf of the host
// (e.g., system messages from a service plugin). The string values are
// owned by internal/core/event.go; they are re-exported here as typed
// SDK constants so plugin code (which cannot import internal/core per
// the plugin-boundary discipline) can reference them without resorting
// to magic strings.
//
// Plugin-owned event-type constants (e.g., "core-communication:say")
// stay in their owning plugin's package, NOT here.
const (
	HostEventTypeSystem          EventType = "system"
	HostEventTypeSessionEnded    EventType = "session_ended"
	HostEventTypeCommandResponse EventType = "command_response"
	HostEventTypeCommandError    EventType = "command_error"
	HostEventTypeArrive          EventType = "arrive"
	HostEventTypeLeave           EventType = "leave"
	HostEventTypeMove            EventType = "move"
	HostEventTypeLocationState   EventType = "location_state"
	HostEventTypeExitUpdate      EventType = "exit_update"
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
	// Cursor is the opaque pagination token representing this event's
	// position in the stream. Pass this as QueryStreamHistoryRequest.Cursor
	// on the next call to page backward from this event. Treat as an
	// opaque blob — internal encoding may change between host versions.
	Cursor []byte
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

	// Sensitive declares per-event sensitivity at emit time. The host's
	// Manager.EmitPluginEvent copies this to EmitIntent.Sensitive, where
	// event_emitter.go::Emit's Phase 3a downgrade fence validates against
	// the manifest. Default false (zero value) for backwards-compat.
	Sensitive bool
}

// EmitIntent is a host-side request to emit an event on behalf of a plugin.
// Hosts use this to validate subject ownership and stamp system-owned fields
// before publishing the event.
//
// Subject names the JetStream subject the event will be published to (e.g.,
// "events.main.scene.01ABC.ic"). For the Phase B cutover (F1) the host
// accepts both legacy colon-delimited namespaces (e.g. "scene:01ABC") and
// JetStream-native dot-delimited subjects; legacy names are translated
// internally. F5 migrates plugins to emit JetStream-native subjects directly.
type EmitIntent struct {
	Subject string
	Type    EventType
	Payload string // JSON string

	// Sensitive declares per-event sensitivity at emit time.
	//
	// Phase 3a runtime semantics (host-side fence):
	//   - manifest sensitivity=never:  Sensitive=true rejected (INV-PLUGIN-29).
	//   - manifest sensitivity=may:    field decides (false → plaintext, true → encrypted).
	//   - manifest sensitivity=always: Sensitive=false rejected (INV-PLUGIN-30).
	//
	// Default false. Plugins that do not emit sensitive events leave
	// this zero.
	Sensitive bool
}
