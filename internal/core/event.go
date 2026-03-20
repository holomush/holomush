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
	EventTypeRoomState  EventType = "room_state"
	EventTypeExitUpdate EventType = "exit_update"
)

// RoomStatePayload is the JSON payload for room_state events, providing a
// full snapshot of the character's current location.
type RoomStatePayload struct {
	Location RoomStateLocation `json:"location"`
	Exits    []RoomStateExit   `json:"exits"`
	Present  []RoomStateChar   `json:"present"`
}

// RoomStateLocation describes the location in a room_state event.
type RoomStateLocation struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// RoomStateExit describes an exit visible from the current location.
type RoomStateExit struct {
	Direction string `json:"direction"`
	Name      string `json:"name"`
	Locked    bool   `json:"locked"`
}

// RoomStateChar describes a character present in the current location.
type RoomStateChar struct {
	Name string `json:"name"`
	Idle bool   `json:"idle"`
}

// ExitUpdatePayload is the JSON payload for exit_update events, providing a
// delta update to the exits in the current location.
type ExitUpdatePayload struct {
	Exits []RoomStateExit `json:"exits"`
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
