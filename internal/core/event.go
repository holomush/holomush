// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package core contains the core game engine types and logic.
package core

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// EventType identifies the kind of event.
type EventType string

// Event types for game events.
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

	// UI state event types
	EventTypeLocationState EventType = "location_state"
	EventTypeExitUpdate    EventType = "exit_update"
)

// LocationStatePayload is the JSON payload for location_state events, providing
// a full snapshot of the character's current location.
type LocationStatePayload struct {
	Location LocationStateInfo `json:"location"`
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

// Actor represents who or what caused an event.
type Actor struct {
	Kind ActorKind
	ID   string // Character ID, plugin name, or "system"
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
