// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"encoding/json"

	"github.com/holomush/holomush/pkg/plugin"
)

// Payload is a map of key-value pairs for event payloads.
// Values are JSON-encoded when building events.
type Payload map[string]any

// Emitter accumulates events for later emission.
// Use NewEmitter to create an instance, then Location/Character/Global
// to add events, and Flush to retrieve and clear the buffer.
type Emitter struct {
	events []plugin.EmitEvent
}

// NewEmitter creates a new event emitter with an empty buffer.
func NewEmitter() *Emitter {
	return &Emitter{}
}

// Location emits an event to a location stream ("location:<id>").
func (e *Emitter) Location(locationID string, eventType plugin.EventType, payload Payload) {
	e.emit("location:"+locationID, eventType, payload)
}

// Character emits an event to a character stream ("char:<id>").
func (e *Emitter) Character(characterID string, eventType plugin.EventType, payload Payload) {
	e.emit("char:"+characterID, eventType, payload)
}

// Global emits an event to the global stream.
func (e *Emitter) Global(eventType plugin.EventType, payload Payload) {
	e.emit("global", eventType, payload)
}

// Flush returns all accumulated events and clears the buffer.
// Returns nil if no events have been accumulated.
func (e *Emitter) Flush() []plugin.EmitEvent {
	if len(e.events) == 0 {
		return nil
	}
	events := e.events
	e.events = nil
	return events
}

// emit adds an event to the internal buffer.
// JSON encoding errors are silently ignored; the payload will be empty on error.
func (e *Emitter) emit(stream string, eventType plugin.EventType, payload Payload) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = []byte("{}")
	}
	e.events = append(e.events, plugin.EmitEvent{
		Stream:  stream,
		Type:    eventType,
		Payload: string(payloadJSON),
	})
}
