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
	var ids []ulid.ULID
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
	store.Append(ctx, event)

	lastID, err := store.LastEventID(ctx, "location:test")
	if err != nil {
		t.Fatalf("LastEventID failed: %v", err)
	}
	if lastID != event.ID {
		t.Errorf("Expected %v, got %v", event.ID, lastID)
	}
}
