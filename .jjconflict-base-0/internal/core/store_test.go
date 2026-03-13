// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
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
		err := store.Append(ctx, event)
		require.NoError(t, err)
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	// Replay from beginning, limit 3
	events, err := store.Replay(ctx, "location:test", ulid.ULID{}, 3)
	require.NoError(t, err)
	assert.Len(t, events, 3)

	// Replay after third event
	events, err = store.Replay(ctx, "location:test", ids[2], 10)
	require.NoError(t, err)
	assert.Len(t, events, 2, "Expected 2 events after id[2]")
}

func TestMemoryEventStore_LastEventID(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	// Empty stream
	_, err := store.LastEventID(ctx, "empty")
	assert.Error(t, err, "Expected error for empty stream")

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
	require.NoError(t, err)

	lastID, err := store.LastEventID(ctx, "location:test")
	require.NoError(t, err)
	assert.Equal(t, event.ID, lastID)
}

func TestMemoryEventStore_Replay_EmptyStream(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	events, err := store.Replay(ctx, "nonexistent", ulid.ULID{}, 10)
	require.NoError(t, err)
	assert.Nil(t, events, "Expected nil for empty stream")
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
		err := store.Append(ctx, event)
		require.NoError(t, err)
		time.Sleep(time.Millisecond)
	}

	// Replay with an afterID that doesn't exist in the stream
	// Should return all events from start (afterID not found = startIdx stays 0)
	nonExistentID := NewULID()
	events, err := store.Replay(ctx, "location:test", nonExistentID, 10)
	require.NoError(t, err)
	// When afterID is not found, startIdx stays at 0, so all events are returned
	assert.Len(t, events, 3, "Expected 3 events when afterID not found")
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
		err := store.Append(ctx, event)
		require.NoError(t, err)
	}

	// Replay with limit higher than available events
	events, err := store.Replay(ctx, "location:test", ulid.ULID{}, 100)
	require.NoError(t, err)
	assert.Len(t, events, 2)
}
