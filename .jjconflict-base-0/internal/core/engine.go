// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
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
		return oops.With("operation", "marshal_say_payload").Wrap(err)
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
		return oops.With("operation", "append_say_event").Wrap(err)
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
		return oops.With("operation", "marshal_pose_payload").Wrap(err)
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
		return oops.With("operation", "append_pose_event").Wrap(err)
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
		return nil, oops.With("operation", "replay_events").With("stream", stream).Wrap(err)
	}
	return events, nil
}
