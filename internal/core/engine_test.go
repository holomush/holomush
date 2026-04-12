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

func TestEngineHandleSayStoresEventWithCharacterNameAndMessage(t *testing.T) {
	store := NewMemoryEventStore()
	engine := NewEngine(store)

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

func TestEngineHandlePoseStoresEventWithCharacterNameAndAction(t *testing.T) {
	store := NewMemoryEventStore()
	engine := NewEngine(store)

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

func TestEngineHandleSayAppendsEventToLocationStream(t *testing.T) {
	store := NewMemoryEventStore()
	engine := NewEngine(store)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	stream := "location:" + locationID.String()

	err := engine.HandleSay(ctx, char, "Hello, world!")
	require.NoError(t, err)

	// Verify event was appended to store
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, EventTypeSay, events[0].Type)
	assert.Equal(t, stream, events[0].Stream)
}

func TestEngineHandlePoseAppendsEventToLocationStream(t *testing.T) {
	store := NewMemoryEventStore()
	engine := NewEngine(store)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	stream := "location:" + locationID.String()

	err := engine.HandlePose(ctx, char, "waves")
	require.NoError(t, err)

	// Verify event was appended to store
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, EventTypePose, events[0].Type)
	assert.Equal(t, stream, events[0].Stream)
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

func (f *failingEventStore) ReplayTail(_ context.Context, _ string, _ int, _ time.Time) ([]Event, error) {
	return nil, errStoreFailure
}

func (f *failingEventStore) SubscribeSession(_ context.Context) (Subscription, error) {
	return nil, errStoreFailure
}

var errStoreFailure = &storeError{msg: "store failure"}

type storeError struct {
	msg string
}

func (e *storeError) Error() string {
	return e.msg
}

func TestEngineHandleSayPropagatesStoreError(t *testing.T) {
	store := &failingEventStore{}
	engine := NewEngine(store)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	err := engine.HandleSay(ctx, char, "Hello")
	require.Error(t, err, "Expected error from failing store")
	assert.ErrorIs(t, err, errStoreFailure, "Should wrap store error")
}

func TestEngineHandlePosePropagatesStoreError(t *testing.T) {
	store := &failingEventStore{}
	engine := NewEngine(store)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "TestChar", LocationID: locationID}

	err := engine.HandlePose(ctx, char, "waves")
	require.Error(t, err, "Expected error from failing store")
	assert.ErrorIs(t, err, errStoreFailure, "Should wrap store error")
}

func TestEngineHandleConnectStoresArriveEventWithCharacterPayload(t *testing.T) {
	store := NewMemoryEventStore()
	engine := NewEngine(store)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "Alyssa", LocationID: locationID}

	stream := "location:" + locationID.String()

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
}

func TestEngineHandleDisconnectStoresLeaveEventWithReasonPayload(t *testing.T) {
	store := NewMemoryEventStore()
	engine := NewEngine(store)

	ctx := context.Background()
	charID := NewULID()
	locationID := NewULID()
	char := CharacterRef{ID: charID, Name: "Alyssa", LocationID: locationID}

	stream := "location:" + locationID.String()

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
}
