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

func TestEngine_HandleSay(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	// Emit say event
	err := engine.HandleSay(ctx, charID, locationID, "Hello, world!")
	require.NoError(t, err)

	// Verify event was stored
	stream := "location:" + locationID.String()
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, EventTypeSay, events[0].Type)
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
	require.NoError(t, err)

	// Verify event was stored
	stream := "location:" + locationID.String()
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, EventTypePose, events[0].Type)
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
	require.NoError(t, err)

	// Verify event was broadcast
	select {
	case event := <-ch:
		assert.Equal(t, EventTypeSay, event.Type)
		assert.Equal(t, stream, event.Stream)
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
	require.NoError(t, err)

	// Verify event was broadcast
	select {
	case event := <-ch:
		assert.Equal(t, EventTypePose, event.Type)
		assert.Equal(t, stream, event.Stream)
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
	require.NoError(t, err)

	err = engine.HandlePose(ctx, charID, locationID, "waves")
	require.NoError(t, err)
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
		require.NoError(t, err)
	}

	// Replay without session (no cursor)
	events, err := engine.ReplayEvents(ctx, charID, stream, 10)
	require.NoError(t, err)
	assert.Len(t, events, 5)
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
		require.NoError(t, err)
	}

	// Get events and set cursor to third event
	allEvents, _ := store.Replay(ctx, stream, ulid.ULID{}, 10)
	sessions.UpdateCursor(charID, stream, allEvents[2].ID)

	// Replay should return only events after cursor
	events, err := engine.ReplayEvents(ctx, charID, stream, 10)
	require.NoError(t, err)
	assert.Len(t, events, 2, "Expected 2 events after cursor")
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
	require.Error(t, err, "Expected error from failing store")
	assert.ErrorIs(t, err, errStoreFailure, "Should wrap store error")
}

func TestEngine_HandlePose_StoreError(t *testing.T) {
	store := &failingEventStore{}
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()

	err := engine.HandlePose(ctx, charID, locationID, "waves")
	require.Error(t, err, "Expected error from failing store")
	assert.ErrorIs(t, err, errStoreFailure, "Should wrap store error")
}

func TestEngine_ReplayEvents_StoreError(t *testing.T) {
	store := &failingEventStore{}
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()

	_, err := engine.ReplayEvents(ctx, charID, "location:test", 10)
	require.Error(t, err, "Expected error from failing store")
	assert.ErrorIs(t, err, errStoreFailure, "Should wrap store error")
}
