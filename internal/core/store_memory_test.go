// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package core

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryEventStoreAppendSucceedsWithValidEvent(t *testing.T) {
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

func TestMemoryEventStoreReplayReturnsEventsWithLimitAndAfterID(t *testing.T) {
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

func TestMemoryEventStoreLastEventIDReturnsIDOfMostRecentEvent(t *testing.T) {
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

func TestMemoryEventStoreReplayEmptyStreamReturnsNil(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	events, err := store.Replay(ctx, "nonexistent", ulid.ULID{}, 10)
	require.NoError(t, err)
	assert.Nil(t, events, "Expected nil for empty stream")
}

func TestMemoryEventStoreReplayMissingCursorReturnsEmpty(t *testing.T) {
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

	// Replay with an afterID that doesn't exist in the stream.
	// A missing cursor means the client's position is unknown — returning
	// nothing is safer than replaying the entire stream from the beginning.
	nonExistentID := NewULID()
	events, err := store.Replay(ctx, "location:test", nonExistentID, 10)
	require.NoError(t, err)
	assert.Empty(t, events, "missing afterID should return empty slice")
}

func TestMemoryEventStoreReplayLimitHigherThanEventCountReturnsAll(t *testing.T) {
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

func TestMemoryEventStoreSubscribeNotifiesWhenEventAppended(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	eventCh, errCh, err := store.Subscribe(ctx, "location:test")
	require.NoError(t, err)
	require.NotNil(t, eventCh)
	require.NotNil(t, errCh)

	event := Event{
		ID:        NewULID(),
		Stream:    "location:test",
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}
	err = store.Append(ctx, event)
	require.NoError(t, err)

	select {
	case id, ok := <-eventCh:
		require.True(t, ok, "channel should be open")
		assert.Equal(t, event.ID, id)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event notification")
	}
}

func TestMemoryEventStoreSubscribeDoesNotNotifyForOtherStreams(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	eventCh, _, err := store.Subscribe(ctx, "location:stream-a")
	require.NoError(t, err)

	event := Event{
		ID:        NewULID(),
		Stream:    "location:stream-b",
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
		Payload:   []byte(`{}`),
	}
	err = store.Append(ctx, event)
	require.NoError(t, err)

	select {
	case id := <-eventCh:
		t.Fatalf("unexpected notification for stream-b event: %s", id)
	case <-time.After(100 * time.Millisecond):
		// Expected: no notification for other streams
	}
}

func TestMemoryEventStoreReplayZeroLimitReturnsEmpty(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()

	// Append an event so the stream is non-empty
	event := Event{
		ID:        NewULID(),
		Stream:    "location:test",
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
		Payload:   []byte(`{}`),
	}
	require.NoError(t, store.Append(ctx, event))

	events, err := store.Replay(ctx, "location:test", ulid.ULID{}, 0)
	require.NoError(t, err)
	assert.Empty(t, events, "zero limit should return empty slice")
}

func TestMemoryEventStoreSubscribeClosesChannelOnContextCancel(t *testing.T) {
	store := NewMemoryEventStore()
	ctx, cancel := context.WithCancel(context.Background())

	eventCh, _, err := store.Subscribe(ctx, "location:test")
	require.NoError(t, err)

	cancel()

	// Channel should close after cancel
	select {
	case _, ok := <-eventCh:
		assert.False(t, ok, "channel should be closed after context cancel")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close")
	}

	// Subsequent Append should not panic
	event := Event{
		ID:        NewULID(),
		Stream:    "location:test",
		Type:      EventTypeSay,
		Timestamp: time.Now(),
		Actor:     Actor{Kind: ActorCharacter, ID: "char1"},
		Payload:   []byte(`{}`),
	}
	require.NotPanics(t, func() {
		_ = store.Append(context.Background(), event)
	})
}

func TestMemoryEventStore_ReplayTail(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()
	stream := "channel:test"
	now := time.Now()

	ids := make([]ulid.ULID, 5)
	for i := range 5 {
		ids[i] = NewULID()
		err := store.Append(ctx, Event{
			ID:        ids[i],
			Stream:    stream,
			Type:      EventType("channel_say"),
			Timestamp: now.Add(time.Duration(i) * 10 * time.Millisecond),
			Actor:     Actor{Kind: ActorCharacter, ID: "char-1"},
			Payload:   []byte(`{"message":"msg"}`),
		})
		require.NoError(t, err)
	}

	t.Run("returns last N events in chronological order", func(t *testing.T) {
		events, err := store.ReplayTail(ctx, stream, 3, time.Time{})
		require.NoError(t, err)
		assert.Len(t, events, 3)
		assert.Equal(t, ids[2], events[0].ID)
		assert.Equal(t, ids[3], events[1].ID)
		assert.Equal(t, ids[4], events[2].ID)
	})

	t.Run("respects notBefore filter", func(t *testing.T) {
		cutoff := now.Add(25 * time.Millisecond)
		events, err := store.ReplayTail(ctx, stream, 10, cutoff)
		require.NoError(t, err)
		assert.Len(t, events, 2)
		assert.Equal(t, ids[3], events[0].ID)
		assert.Equal(t, ids[4], events[1].ID)
	})

	t.Run("returns empty slice for unknown stream", func(t *testing.T) {
		events, err := store.ReplayTail(ctx, "channel:nonexistent", 10, time.Time{})
		require.NoError(t, err)
		assert.Empty(t, events)
	})

	t.Run("count larger than available returns all events", func(t *testing.T) {
		events, err := store.ReplayTail(ctx, stream, 100, time.Time{})
		require.NoError(t, err)
		assert.Len(t, events, 5)
	})
}
