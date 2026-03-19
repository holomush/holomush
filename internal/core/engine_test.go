// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"encoding/json"
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
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	// Emit say event
	err := engine.HandleSay(ctx, char, "Hello, world!")
	require.NoError(t, err)

	// Verify event was stored
	stream := "location:" + locationID.String()
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, EventTypeSay, events[0].Type)

	// Verify payload includes character_name
	var payload SayPayload
	require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
	assert.Equal(t, "TestChar", payload.CharacterName)
	assert.Equal(t, "Hello, world!", payload.Message)
}

func TestEngine_HandlePose(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	engine := NewEngine(store, sessions, nil)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	// Emit pose event
	err := engine.HandlePose(ctx, char, "waves hello")
	require.NoError(t, err)

	// Verify event was stored
	stream := "location:" + locationID.String()
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, EventTypePose, events[0].Type)

	// Verify payload includes character_name
	var payload PosePayload
	require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
	assert.Equal(t, "TestChar", payload.CharacterName)
	assert.Equal(t, "waves hello", payload.Action)
}

func TestEngine_HandleSay_BroadcastsEvent(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	broadcaster := NewBroadcaster()
	engine := NewEngine(store, sessions, broadcaster)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	// Subscribe to the location stream before the event
	stream := "location:" + locationID.String()
	ch := broadcaster.Subscribe(stream)
	defer broadcaster.Unsubscribe(stream, ch)

	// Emit say event
	err := engine.HandleSay(ctx, char, "Hello, world!")
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
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	// Subscribe to the location stream before the event
	stream := "location:" + locationID.String()
	ch := broadcaster.Subscribe(stream)
	defer broadcaster.Unsubscribe(stream, ch)

	// Emit pose event
	err := engine.HandlePose(ctx, char, "waves")
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
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	// These should not panic even with nil broadcaster
	err := engine.HandleSay(ctx, char, "Hello")
	require.NoError(t, err)

	err = engine.HandlePose(ctx, char, "waves")
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
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	// Create some events
	for i := 0; i < 5; i++ {
		err := engine.HandleSay(ctx, char, "message")
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
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	// Connect to create session
	sessions.Connect(charID, connID)

	// Create some events
	for i := 0; i < 5; i++ {
		err := engine.HandleSay(ctx, char, "message")
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

func (f *failingEventStore) Subscribe(_ context.Context, _ string) (<-chan ulid.ULID, <-chan error, error) {
	return nil, nil, errStoreFailure
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
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	err := engine.HandleSay(ctx, char, "Hello")
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
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	err := engine.HandlePose(ctx, char, "waves")
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

func TestEngine_HandleConnect(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	broadcaster := NewBroadcaster()
	engine := NewEngine(store, sessions, broadcaster)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "Alyssa", LocationID: locationID}

	stream := "location:" + locationID.String()
	ch := broadcaster.Subscribe(stream)
	defer broadcaster.Unsubscribe(stream, ch)

	err := engine.HandleConnect(ctx, char)
	require.NoError(t, err)

	// Verify event was stored with correct type, stream, actor
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, EventTypeArrive, events[0].Type)
	assert.Equal(t, stream, events[0].Stream)
	assert.Equal(t, ActorCharacter, events[0].Actor.Kind)
	assert.Equal(t, charID.String(), events[0].Actor.ID)

	// Verify payload
	var payload ArrivePayload
	require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
	assert.Equal(t, "Alyssa", payload.CharacterName)

	// Verify broadcast
	select {
	case event := <-ch:
		assert.Equal(t, EventTypeArrive, event.Type)
		assert.Equal(t, stream, event.Stream)
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for broadcast event")
	}
}

func TestEngine_HandleDisconnect(t *testing.T) {
	store := NewMemoryEventStore()
	sessions := NewSessionManager()
	broadcaster := NewBroadcaster()
	engine := NewEngine(store, sessions, broadcaster)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "Alyssa", LocationID: locationID}

	stream := "location:" + locationID.String()
	ch := broadcaster.Subscribe(stream)
	defer broadcaster.Unsubscribe(stream, ch)

	err := engine.HandleDisconnect(ctx, char, "quit")
	require.NoError(t, err)

	// Verify event was stored with correct type, stream, actor
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, EventTypeLeave, events[0].Type)
	assert.Equal(t, stream, events[0].Stream)
	assert.Equal(t, ActorCharacter, events[0].Actor.Kind)
	assert.Equal(t, charID.String(), events[0].Actor.ID)

	// Verify payload
	var payload LeavePayload
	require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
	assert.Equal(t, "Alyssa", payload.CharacterName)
	assert.Equal(t, "quit", payload.Reason)

	// Verify broadcast
	select {
	case event := <-ch:
		assert.Equal(t, EventTypeLeave, event.Type)
		assert.Equal(t, stream, event.Stream)
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for broadcast event")
	}
}
