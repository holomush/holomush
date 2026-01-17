package core

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

func TestEngine_HandleSay(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

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
	engine := NewEngine(store, sessions, nil)

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

func TestEngine_HandleSay_BroadcastsEvent(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	broadcaster := NewBroadcaster()
	engine := NewEngine(store, sessions, broadcaster)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	// Subscribe to the location stream before the event
	stream := "location:" + locationID.String()
	ch := broadcaster.Subscribe(stream)
	defer broadcaster.Unsubscribe(stream, ch)

	// Emit say event
	err := engine.HandleSay(ctx, charID, locationID, "Hello, world!")
	if err != nil {
		t.Fatalf("HandleSay failed: %v", err)
	}

	// Verify event was broadcast
	select {
	case event := <-ch:
		if event.Type != EventTypeSay {
			t.Errorf("Expected say event, got %v", event.Type)
		}
		if event.Stream != stream {
			t.Errorf("Expected stream %s, got %s", stream, event.Stream)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for broadcast event")
	}
}

func TestEngine_HandlePose_BroadcastsEvent(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	broadcaster := NewBroadcaster()
	engine := NewEngine(store, sessions, broadcaster)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	// Subscribe to the location stream before the event
	stream := "location:" + locationID.String()
	ch := broadcaster.Subscribe(stream)
	defer broadcaster.Unsubscribe(stream, ch)

	// Emit pose event
	err := engine.HandlePose(ctx, charID, locationID, "waves")
	if err != nil {
		t.Fatalf("HandlePose failed: %v", err)
	}

	// Verify event was broadcast
	select {
	case event := <-ch:
		if event.Type != EventTypePose {
			t.Errorf("Expected pose event, got %v", event.Type)
		}
		if event.Stream != stream {
			t.Errorf("Expected stream %s, got %s", stream, event.Stream)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for broadcast event")
	}
}

func TestEngine_NilBroadcaster_DoesNotPanic(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	// Pass nil broadcaster - should not panic
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	// These should not panic even with nil broadcaster
	err := engine.HandleSay(ctx, charID, locationID, "Hello")
	if err != nil {
		t.Fatalf("HandleSay failed: %v", err)
	}

	err = engine.HandlePose(ctx, charID, locationID, "waves")
	if err != nil {
		t.Fatalf("HandlePose failed: %v", err)
	}
}
