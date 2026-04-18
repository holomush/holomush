// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samber/oops"
)

// SayPayload is the JSON payload for say events.
type SayPayload struct {
	CharacterName string `json:"character_name"`
	Message       string `json:"message"`
}

// PosePayload is the JSON payload for pose events.
type PosePayload struct {
	CharacterName string `json:"character_name"`
	Action        string `json:"action"`
	NoSpace       bool   `json:"no_space,omitempty"`
}

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

// EngineOption configures a new Engine.
type EngineOption func(*engineConfig)

type engineConfig struct {
	productionGuardrail bool
}

// WithProductionGuardrail enables the runtime assertion that the engine's
// store is a *core.EventWriter. Enforces invariant I1 (EventWriter
// serialization) in production wiring. Test constructors typically omit
// this to allow lightweight in-memory stores for pure-logic tests.
func WithProductionGuardrail() EngineOption {
	return func(c *engineConfig) { c.productionGuardrail = true }
}

// Engine is the core game engine.
type Engine struct {
	store EventStore
}

// NewEngine creates a new game engine.
func NewEngine(store EventStore, opts ...EngineOption) *Engine {
	cfg := engineConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.productionGuardrail {
		if _, ok := store.(*EventWriter); !ok {
			panic(fmt.Sprintf(
				"core.NewEngine: production mode requires *EventWriter store (I1 guardrail). "+
					"Got %T. See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md Design Decision #8.",
				store))
		}
	}
	return &Engine{store: store}
}

// HandleSay processes a say command.
func (e *Engine) HandleSay(ctx context.Context, char CharacterRef, message string) error {
	payload, err := json.Marshal(SayPayload{CharacterName: char.Name, Message: message})
	if err != nil {
		return oops.With("operation", "marshal_say_payload").Wrap(err)
	}

	event := Event{
		ID:        NewULID(),
		Stream:    "location:" + char.LocationID.String(),
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: char.ID.String()},
		Payload:   payload,
	}

	if err := e.store.Append(ctx, event); err != nil {
		return oops.With("operation", "append_say_event").Wrap(err)
	}

	return nil
}

// HandlePose processes a pose command.
func (e *Engine) HandlePose(ctx context.Context, char CharacterRef, action string) error {
	payload, err := json.Marshal(PosePayload{CharacterName: char.Name, Action: action})
	if err != nil {
		return oops.With("operation", "marshal_pose_payload").Wrap(err)
	}

	event := Event{
		ID:        NewULID(),
		Stream:    "location:" + char.LocationID.String(),
		Type:      EventTypePose,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: char.ID.String()},
		Payload:   payload,
	}

	if err := e.store.Append(ctx, event); err != nil {
		return oops.With("operation", "append_pose_event").Wrap(err)
	}

	return nil
}

// HandleConnect processes a character connecting to a location.
func (e *Engine) HandleConnect(ctx context.Context, char CharacterRef) error {
	payload, err := json.Marshal(ArrivePayload{CharacterName: char.Name})
	if err != nil {
		return oops.With("operation", "marshal_arrive_payload").Wrap(err)
	}

	event := Event{
		ID:        NewULID(),
		Stream:    "location:" + char.LocationID.String(),
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
		Stream:    "location:" + char.LocationID.String(),
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
