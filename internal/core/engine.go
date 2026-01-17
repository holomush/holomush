package core

import (
	"context"
	"encoding/json"
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
	store    EventStore
	sessions *SessionManager
}

// NewEngine creates a new game engine.
func NewEngine(store EventStore, sessions *SessionManager) *Engine {
	return &Engine{
		store:    store,
		sessions: sessions,
	}
}

// HandleSay processes a say command.
func (e *Engine) HandleSay(ctx context.Context, charID, locationID ulid.ULID, message string) error {
	payload, _ := json.Marshal(SayPayload{Message: message})

	event := Event{
		ID:        NewULID(),
		Stream:    "location:" + locationID.String(),
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: charID.String()},
		Payload:   payload,
	}

	return e.store.Append(ctx, event)
}

// HandlePose processes a pose command.
func (e *Engine) HandlePose(ctx context.Context, charID, locationID ulid.ULID, action string) error {
	payload, _ := json.Marshal(PosePayload{Action: action})

	event := Event{
		ID:        NewULID(),
		Stream:    "location:" + locationID.String(),
		Type:      EventTypePose,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: charID.String()},
		Payload:   payload,
	}

	return e.store.Append(ctx, event)
}

// ReplayEvents returns missed events for a character.
func (e *Engine) ReplayEvents(ctx context.Context, charID ulid.ULID, stream string, limit int) ([]Event, error) {
	session := e.sessions.GetSession(charID)
	var afterID ulid.ULID
	if session != nil {
		afterID = session.EventCursors[stream]
	}
	return e.store.Replay(ctx, stream, afterID, limit)
}
