//go:build integration

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
)

func TestPostgresEventStore_Integration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()
	store, err := NewPostgresEventStore(ctx, dsn)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Run migrations
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	stream := "test:" + core.NewULID().String()

	// Test Append
	event := core.Event{
		ID:        core.NewULID(),
		Stream:    stream,
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	if err := store.Append(ctx, event); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Test Replay
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	// Test LastEventID
	lastID, err := store.LastEventID(ctx, stream)
	if err != nil {
		t.Fatalf("LastEventID failed: %v", err)
	}
	if lastID != event.ID {
		t.Errorf("Expected %v, got %v", event.ID, lastID)
	}
}
