// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/samber/oops"
)

// CommandResponsePayload is the JSON payload for command_response and
// command_error events. The event type itself carries the error distinction.
type CommandResponsePayload struct {
	Text string `json:"text"`
}

// ArrivePayload is the JSON payload for arrive events.
type ArrivePayload struct {
	CharacterName string `json:"character_name"`
}

// LeavePayload is the JSON payload for leave events.
type LeavePayload struct {
	CharacterName string `json:"character_name"`
	Reason        string `json:"reason"`
}

// Engine is the core game engine.
type Engine struct {
	store EventAppender
}

// NewEngine creates a new game engine.
//
// Panics when store is nil so the misconfiguration surfaces at construction
// time rather than deferring to the first Handle* call (which would panic on
// a nil-receiver dereference of e.store). Detects both untyped nil and
// typed-nil interface values (e.g. a typed-nil concrete pointer) so callers
// truly fail fast at construction.
func NewEngine(store EventAppender) *Engine {
	if store == nil || isNilEventAppender(store) {
		panic("core.NewEngine: nil EventAppender")
	}
	return &Engine{store: store}
}

// isNilEventAppender detects typed-nil interface values whose underlying
// concrete kind is nilable (pointer, slice, map, chan, func, interface).
// Returns false for non-nilable kinds (struct, value-receiver fakes).
func isNilEventAppender(store EventAppender) bool {
	v := reflect.ValueOf(store)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// HandleConnect processes a character connecting to a location.
func (e *Engine) HandleConnect(ctx context.Context, char CharacterRef) error {
	payload, err := json.Marshal(ArrivePayload{CharacterName: char.Name})
	if err != nil {
		return oops.With("operation", "marshal_arrive_payload").Wrap(err)
	}

	event := Event{
		ID:        NewULID(),
		Stream:    "location." + char.LocationID.String(),
		Type:      EventTypeArrive,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: char.ID.String()},
		Payload:   payload,
	}

	if err := e.store.Append(ctx, event); err != nil {
		return oops.With("operation", "append_arrive_event").Wrap(err)
	}

	return nil
}

// HandleDisconnect processes a character disconnecting from a location.
func (e *Engine) HandleDisconnect(ctx context.Context, char CharacterRef, reason string) error {
	payload, err := json.Marshal(LeavePayload{CharacterName: char.Name, Reason: reason})
	if err != nil {
		return oops.With("operation", "marshal_leave_payload").Wrap(err)
	}

	event := Event{
		ID:        NewULID(),
		Stream:    "location." + char.LocationID.String(),
		Type:      EventTypeLeave,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: char.ID.String()},
		Payload:   payload,
	}

	if err := e.store.Append(ctx, event); err != nil {
		return oops.With("operation", "append_leave_event").Wrap(err)
	}

	return nil
}
