// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/holomush/holomush/internal/core"
)

// setupPostgresContainer starts a PostgreSQL container for testing.
func setupPostgresContainer(t *testing.T) (*PostgresEventStore, func()) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("holomush"),
		postgres.WithPassword("holomush"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	store, err := NewPostgresEventStore(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create event store: %v", err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	cleanup := func() {
		store.Close()
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}

	return store, cleanup
}

func TestPostgresEventStore_Append(t *testing.T) {
	store, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	event := core.Event{
		ID:        core.NewULID(),
		Stream:    "location:test-room",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
		Payload:   []byte(`{"message":"Hello, world!"}`),
	}

	err := store.Append(ctx, event)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Verify event was stored
	events, err := store.Replay(ctx, "location:test-room", ulid.ULID{}, 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ID != event.ID {
		t.Errorf("event ID mismatch: got %v, want %v", events[0].ID, event.ID)
	}
}

func TestPostgresEventStore_Replay(t *testing.T) {
	store, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	stream := "location:replay-test"

	// Append multiple events
	ids := make([]ulid.ULID, 5)
	for i := range 5 {
		ids[i] = core.NewULID()
		event := core.Event{
			ID:        ids[i],
			Stream:    stream,
			Type:      core.EventTypeSay,
			Timestamp: time.Now(),
			Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
			Payload:   []byte(`{"message":"test"}`),
		}
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
		time.Sleep(time.Millisecond) // Ensure ULID ordering
	}

	t.Run("replay all from beginning", func(t *testing.T) {
		events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
		if err != nil {
			t.Fatalf("Replay failed: %v", err)
		}
		if len(events) != 5 {
			t.Errorf("expected 5 events, got %d", len(events))
		}
	})

	t.Run("replay with afterID", func(t *testing.T) {
		events, err := store.Replay(ctx, stream, ids[1], 10)
		if err != nil {
			t.Fatalf("Replay failed: %v", err)
		}
		if len(events) != 3 {
			t.Errorf("expected 3 events after id[1], got %d", len(events))
		}
	})

	t.Run("replay with limit", func(t *testing.T) {
		events, err := store.Replay(ctx, stream, ulid.ULID{}, 2)
		if err != nil {
			t.Fatalf("Replay failed: %v", err)
		}
		if len(events) != 2 {
			t.Errorf("expected 2 events with limit, got %d", len(events))
		}
	})

	t.Run("replay empty stream", func(t *testing.T) {
		events, err := store.Replay(ctx, "nonexistent:stream", ulid.ULID{}, 10)
		if err != nil {
			t.Fatalf("Replay failed: %v", err)
		}
		if len(events) != 0 {
			t.Errorf("expected 0 events for nonexistent stream, got %d", len(events))
		}
	})
}

func TestPostgresEventStore_LastEventID(t *testing.T) {
	store, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	stream := "location:last-id-test"

	t.Run("empty stream returns ErrStreamEmpty", func(t *testing.T) {
		_, err := store.LastEventID(ctx, stream)
		if err != core.ErrStreamEmpty {
			t.Errorf("expected ErrStreamEmpty, got %v", err)
		}
	})

	// Append some events
	var lastID ulid.ULID
	for i := range 3 {
		lastID = core.NewULID()
		event := core.Event{
			ID:        lastID,
			Stream:    stream,
			Type:      core.EventTypeSay,
			Timestamp: time.Now(),
			Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
			Payload:   []byte(`{}`),
		}
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
		time.Sleep(time.Millisecond)
	}

	t.Run("returns last event ID", func(t *testing.T) {
		id, err := store.LastEventID(ctx, stream)
		if err != nil {
			t.Fatalf("LastEventID failed: %v", err)
		}
		if id != lastID {
			t.Errorf("expected %v, got %v", lastID, id)
		}
	})
}

func TestPostgresEventStore_EventTypes(t *testing.T) {
	store, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	stream := "location:event-types-test"

	eventTypes := []core.EventType{
		core.EventTypeSay,
		core.EventTypePose,
		core.EventTypeArrive,
		core.EventTypeLeave,
		core.EventTypeSystem,
	}

	for _, et := range eventTypes {
		event := core.Event{
			ID:        core.NewULID(),
			Stream:    stream,
			Type:      et,
			Timestamp: time.Now(),
			Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
			Payload:   []byte(`{}`),
		}
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append %s event failed: %v", et, err)
		}
	}

	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	if len(events) != len(eventTypes) {
		t.Fatalf("expected %d events, got %d", len(eventTypes), len(events))
	}

	for i, et := range eventTypes {
		if events[i].Type != et {
			t.Errorf("event %d: expected type %s, got %s", i, et, events[i].Type)
		}
	}
}

func TestPostgresEventStore_ActorKinds(t *testing.T) {
	store, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	stream := "location:actor-kinds-test"

	actorKinds := []core.ActorKind{
		core.ActorCharacter,
		core.ActorSystem,
		core.ActorPlugin,
	}

	for _, ak := range actorKinds {
		event := core.Event{
			ID:        core.NewULID(),
			Stream:    stream,
			Type:      core.EventTypeSay,
			Timestamp: time.Now(),
			Actor:     core.Actor{Kind: ak, ID: "test-actor"},
			Payload:   []byte(`{}`),
		}
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append event with actor kind %d failed: %v", ak, err)
		}
	}

	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	if len(events) != len(actorKinds) {
		t.Fatalf("expected %d events, got %d", len(actorKinds), len(events))
	}

	for i, ak := range actorKinds {
		if events[i].Actor.Kind != ak {
			t.Errorf("event %d: expected actor kind %d, got %d", i, ak, events[i].Actor.Kind)
		}
	}
}

func TestPostgresEventStore_SystemInfo(t *testing.T) {
	store, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("GetSystemInfo returns error for missing key", func(t *testing.T) {
		_, err := store.GetSystemInfo(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for missing key")
		}
	})

	t.Run("SetSystemInfo and GetSystemInfo", func(t *testing.T) {
		err := store.SetSystemInfo(ctx, "test_key", "test_value")
		if err != nil {
			t.Fatalf("SetSystemInfo() error = %v", err)
		}

		value, err := store.GetSystemInfo(ctx, "test_key")
		if err != nil {
			t.Fatalf("GetSystemInfo() error = %v", err)
		}
		if value != "test_value" {
			t.Errorf("GetSystemInfo() = %q, want %q", value, "test_value")
		}
	})

	t.Run("SetSystemInfo updates existing key", func(t *testing.T) {
		err := store.SetSystemInfo(ctx, "update_key", "original")
		if err != nil {
			t.Fatalf("SetSystemInfo() error = %v", err)
		}

		err = store.SetSystemInfo(ctx, "update_key", "updated")
		if err != nil {
			t.Fatalf("SetSystemInfo() update error = %v", err)
		}

		value, err := store.GetSystemInfo(ctx, "update_key")
		if err != nil {
			t.Fatalf("GetSystemInfo() error = %v", err)
		}
		if value != "updated" {
			t.Errorf("GetSystemInfo() = %q, want %q", value, "updated")
		}
	})
}

func TestPostgresEventStore_InitGameID(t *testing.T) {
	store, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("generates new game_id when none exists", func(t *testing.T) {
		gameID, err := store.InitGameID(ctx)
		if err != nil {
			t.Fatalf("InitGameID() error = %v", err)
		}
		if gameID == "" {
			t.Error("InitGameID() returned empty string")
		}
		// Verify it's a valid ULID (26 characters)
		if len(gameID) != 26 {
			t.Errorf("InitGameID() returned invalid ULID length: %d", len(gameID))
		}
	})

	t.Run("returns existing game_id", func(t *testing.T) {
		// Get current game_id
		firstID, err := store.InitGameID(ctx)
		if err != nil {
			t.Fatalf("InitGameID() first call error = %v", err)
		}

		// Call again should return same ID
		secondID, err := store.InitGameID(ctx)
		if err != nil {
			t.Fatalf("InitGameID() second call error = %v", err)
		}

		if firstID != secondID {
			t.Errorf("InitGameID() returned different IDs: %q vs %q", firstID, secondID)
		}
	})

	t.Run("game_id persists in database", func(t *testing.T) {
		gameID, err := store.InitGameID(ctx)
		if err != nil {
			t.Fatalf("InitGameID() error = %v", err)
		}

		// Verify via GetSystemInfo
		storedID, err := store.GetSystemInfo(ctx, "game_id")
		if err != nil {
			t.Fatalf("GetSystemInfo() error = %v", err)
		}
		if storedID != gameID {
			t.Errorf("stored game_id %q != returned game_id %q", storedID, gameID)
		}
	})
}
