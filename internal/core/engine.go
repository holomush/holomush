// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// SayPayload is the JSON payload for say events.
type SayPayload struct {
	Message string `json:"message"`
}

// PosePayload is the JSON payload for pose events.
type PosePayload struct {
	Action string `json:"action"`
}

// Engine is the core game engine.
type Engine struct {
	store       EventStore
	sessions    *SessionManager
	broadcaster *Broadcaster
}

// NewEngine creates a new game engine.
func NewEngine(store EventStore, sessions *SessionManager, broadcaster *Broadcaster) *Engine {
	return &Engine{
		store:       store,
		sessions:    sessions,
		broadcaster: broadcaster,
	}
}

// HandleSay processes a say command.
func (e *Engine) HandleSay(ctx context.Context, charID, locationID ulid.ULID, message string) error {
	payload, err := json.Marshal(SayPayload{Message: message})
	if err != nil {
		return fmt.Errorf("failed to marshal say payload: %w", err)
	}

	event := Event{
		ID:        NewULID(),
		Stream:    "location:" + locationID.String(),
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: charID.String()},
		Payload:   payload,
	}

	if err := e.store.Append(ctx, event); err != nil {
		return fmt.Errorf("failed to append say event: %w", err)
	}

	// Broadcast to subscribers (nil-safe)
	if e.broadcaster != nil {
		e.broadcaster.Broadcast(event)
	}

	return nil
}

// HandlePose processes a pose command.
func (e *Engine) HandlePose(ctx context.Context, charID, locationID ulid.ULID, action string) error {
	payload, err := json.Marshal(PosePayload{Action: action})
	if err != nil {
		return fmt.Errorf("failed to marshal pose payload: %w", err)
	}

	event := Event{
		ID:        NewULID(),
		Stream:    "location:" + locationID.String(),
		Type:      EventTypePose,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: charID.String()},
		Payload:   payload,
	}

	if err := e.store.Append(ctx, event); err != nil {
		return fmt.Errorf("failed to append pose event: %w", err)
	}

	// Broadcast to subscribers (nil-safe)
	if e.broadcaster != nil {
		e.broadcaster.Broadcast(event)
	}

	return nil
}

// ReplayEvents returns missed events for a character.
func (e *Engine) ReplayEvents(ctx context.Context, charID ulid.ULID, stream string, limit int) ([]Event, error) {
	session := e.sessions.GetSession(charID)
	var afterID ulid.ULID
	if session != nil {
		afterID = session.EventCursors[stream]
	}
	events, err := e.store.Replay(ctx, stream, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to replay events: %w", err)
	}
	return events, nil
}
