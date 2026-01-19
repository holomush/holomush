// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

func TestEngine_ReplayEvents(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	stream := "location:" + locationID.String()

	// Create some events
	for i := 0; i < 5; i++ {
		err := engine.HandleSay(ctx, charID, locationID, "message")
		if err != nil {
			t.Fatalf("HandleSay failed: %v", err)
		}
	}

	// Replay without session (no cursor)
	events, err := engine.ReplayEvents(ctx, charID, stream, 10)
	if err != nil {
		t.Fatalf("ReplayEvents failed: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("Expected 5 events, got %d", len(events))
	}
}

func TestEngine_ReplayEvents_WithCursor(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()
	connID := NewULID()
	locationID := NewULID()
	stream := "location:" + locationID.String()

	// Connect to create session
	sessions.Connect(charID, connID)

	// Create some events
	for i := 0; i < 5; i++ {
		err := engine.HandleSay(ctx, charID, locationID, "message")
		if err != nil {
			t.Fatalf("HandleSay failed: %v", err)
		}
	}

	// Get events and set cursor to third event
	allEvents, _ := store.Replay(ctx, stream, ulid.ULID{}, 10)
	sessions.UpdateCursor(charID, stream, allEvents[2].ID)

	// Replay should return only events after cursor
	events, err := engine.ReplayEvents(ctx, charID, stream, 10)
	if err != nil {
		t.Fatalf("ReplayEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events after cursor, got %d", len(events))
	}
}

// failingEventStore is a mock that returns errors for testing error paths.
type failingEventStore struct{}

func (f *failingEventStore) Append(_ context.Context, _ Event) error {
	return errStoreFailure
}

func (f *failingEventStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]Event, error) {
	return nil, errStoreFailure
}

func (f *failingEventStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, errStoreFailure
}

var errStoreFailure = &storeError{msg: "store failure"}

type storeError struct {
	msg string
}

func (e *storeError) Error() string {
	return e.msg
}

func TestEngine_HandleSay_StoreError(t *testing.T) {
	store := &failingEventStore{}
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	err := engine.HandleSay(ctx, charID, locationID, "Hello")
	if err == nil {
		t.Fatal("Expected error from failing store")
	}
	if err.Error() != "failed to append say event: store failure" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestEngine_HandlePose_StoreError(t *testing.T) {
	store := &failingEventStore{}
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	err := engine.HandlePose(ctx, charID, locationID, "waves")
	if err == nil {
		t.Fatal("Expected error from failing store")
	}
	if err.Error() != "failed to append pose event: store failure" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestEngine_ReplayEvents_StoreError(t *testing.T) {
	store := &failingEventStore{}
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()

	_, err := engine.ReplayEvents(ctx, charID, "location:test", 10)
	if err == nil {
		t.Fatal("Expected error from failing store")
	}
	if err.Error() != "failed to replay events: store failure" {
		t.Errorf("Unexpected error message: %v", err)
	}
}
