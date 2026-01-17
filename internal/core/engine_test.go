package core

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestEngine_HandleSay(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	// Emit say event
	err := engine.HandleSay(ctx, charID, locationID, "Hello, world!")
	if err != nil {
		t.Fatalf("HandleSay failed: %v", err)
	}

	// Verify event was stored
	stream := "location:" + locationID.String()
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventTypeSay {
		t.Errorf("Expected say event, got %v", events[0].Type)
	}
}

func TestEngine_HandlePose(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	// Emit pose event
	err := engine.HandlePose(ctx, charID, locationID, "waves hello")
	if err != nil {
		t.Fatalf("HandlePose failed: %v", err)
	}

	// Verify event was stored
	stream := "location:" + locationID.String()
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventTypePose {
		t.Errorf("Expected pose event, got %v", events[0].Type)
	}
}
