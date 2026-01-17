// Package core contains the core game engine types and logic.
package core

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// EventType identifies the kind of event.
type EventType string

const (
	EventTypeSay    EventType = "say"
	EventTypePose   EventType = "pose"
	EventTypeArrive EventType = "arrive"
	EventTypeLeave  EventType = "leave"
	EventTypeSystem EventType = "system"
)

// ActorKind identifies what type of entity caused an event.
type ActorKind uint8

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
	Stream    string    // e.g., "location:01ABC", "char:01XYZ"
	Type      EventType
	Timestamp time.Time
	Actor     Actor
	Payload   []byte // JSON
}
