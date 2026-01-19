// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

func TestMemoryEventStore_Append(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	event := Event{
		ID:        NewULID(),
		Stream:    "location:test",
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	err := store.Append(ctx, event)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}
}

func TestMemoryEventStore_Replay(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	// Append 5 events
	ids := make([]ulid.ULID, 0, 5)
	for range 5 {
		event := Event{
			ID:        NewULID(),
			Stream:    "location:test",
			Type:      EventTypeSay,
			Timestamp: time.Now(),
			Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
			Payload:   []byte(`{}`),
		}
		ids = append(ids, event.ID)
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	// Replay from beginning, limit 3
	events, err := store.Replay(ctx, "location:test", ulid.ULID{}, 3)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	// Replay after third event
	events, err = store.Replay(ctx, "location:test", ids[2], 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events after id[2], got %d", len(events))
	}
}

func TestMemoryEventStore_LastEventID(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	// Empty stream
	_, err := store.LastEventID(ctx, "empty")
	if err == nil {
		t.Error("Expected error for empty stream")
	}

	// Add event
	event := Event{
		ID:        NewULID(),
		Stream:    "location:test",
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorSystem, ID: "system"},
		Payload:   []byte(`{}`),
	}
	err = store.Append(ctx, event)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	lastID, err := store.LastEventID(ctx, "location:test")
	if err != nil {
		t.Fatalf("LastEventID failed: %v", err)
	}
	if lastID != event.ID {
		t.Errorf("Expected %v, got %v", event.ID, lastID)
	}
}

func TestMemoryEventStore_Replay_EmptyStream(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	events, err := store.Replay(ctx, "nonexistent", ulid.ULID{}, 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if events != nil {
		t.Errorf("Expected nil for empty stream, got %v", events)
	}
}

func TestMemoryEventStore_Replay_AfterIDNotFound(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	// Append some events
	for i := 0; i < 3; i++ {
		event := Event{
			ID:        NewULID(),
			Stream:    "location:test",
			Type:      EventTypeSay,
			Timestamp: time.Now(),
			Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
			Payload:   []byte(`{}`),
		}
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	// Replay with an afterID that doesn't exist in the stream
	// Should return all events from start (afterID not found = startIdx stays 0)
	nonExistentID := NewULID()
	events, err := store.Replay(ctx, "location:test", nonExistentID, 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	// When afterID is not found, startIdx stays at 0, so all events are returned
	if len(events) != 3 {
		t.Errorf("Expected 3 events when afterID not found, got %d", len(events))
	}
}

func TestMemoryEventStore_Replay_LimitExceedsEvents(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	// Append 2 events
	for i := 0; i < 2; i++ {
		event := Event{
			ID:        NewULID(),
			Stream:    "location:test",
			Type:      EventTypeSay,
			Timestamp: time.Now(),
			Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
			Payload:   []byte(`{}`),
		}
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Replay with limit higher than available events
	events, err := store.Replay(ctx, "location:test", ulid.ULID{}, 100)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}
}
