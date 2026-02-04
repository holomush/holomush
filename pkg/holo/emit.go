// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/holomush/holomush/pkg/plugin"
)

// Payload is a map of key-value pairs for event payloads.
// Values are JSON-encoded when building events.
type Payload map[string]any

// Emitter accumulates events for later emission.
// Use NewEmitter to create an instance, then Location/Character/Global
// to add events, and Flush to retrieve and clear the buffer.
//
// JSON encoding errors are tracked internally and returned from Flush().
// Use HasErrors() or ErrorCount() to check for errors before flushing.
type Emitter struct {
	events []plugin.EmitEvent
	errors []error
	logger *slog.Logger
}

// NewEmitter creates a new event emitter with an empty buffer.
// JSON encoding errors are tracked and returned from Flush().
// Use NewEmitterWithLogger to also log errors immediately when they occur.
func NewEmitter() *Emitter {
	return &Emitter{}
}

// NewEmitterWithLogger creates a new event emitter with logging support.
// When JSON encoding fails, errors are logged with context about the
// stream and event type to help diagnose plugin bugs or invalid payloads.
func NewEmitterWithLogger(logger *slog.Logger) *Emitter {
	return &Emitter{logger: logger}
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

// Flush returns all accumulated events and any JSON encoding errors, then clears both buffers.
// Returns (nil, nil) if no events or errors have been accumulated.
// The errors slice contains context about which streams and event types had encoding failures.
func (e *Emitter) Flush() ([]plugin.EmitEvent, []error) {
	if len(e.events) == 0 && len(e.errors) == 0 {
		return nil, nil
	}
	events := e.events
	errs := e.errors
	e.events = nil
	e.errors = nil
	return events, errs
}

// HasErrors returns true if any JSON encoding errors have occurred since the last Flush.
func (e *Emitter) HasErrors() bool {
	return len(e.errors) > 0
}

// ErrorCount returns the number of JSON encoding errors since the last Flush.
func (e *Emitter) ErrorCount() int {
	return len(e.errors)
}

// emit adds an event to the internal buffer.
// JSON encoding errors result in an empty payload and are tracked for retrieval
// via Flush(). If a logger is configured, errors are also logged immediately.
func (e *Emitter) emit(stream string, eventType plugin.EventType, payload Payload) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		e.errors = append(e.errors, fmt.Errorf(
			"json marshal failed: stream=%s type=%s: %w", stream, eventType, err,
		))
		if e.logger != nil {
			e.logger.Warn("json marshal failed",
				slog.String("stream", stream),
				slog.String("event_type", string(eventType)),
				slog.String("error", err.Error()),
			)
		}
		payloadJSON = []byte("{}")
	}
	e.events = append(e.events, plugin.EmitEvent{
		Stream:  stream,
		Type:    eventType,
		Payload: string(payloadJSON),
	})
}
