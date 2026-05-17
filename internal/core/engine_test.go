// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestNewEngineAcceptsMemoryStore(t *testing.T) {
	store := NewMemoryEventStore()
	e := NewEngine(store)
	assert.NotNil(t, e)
}

func TestNewEnginePanicsOnNilAppender(t *testing.T) {
	assert.Panics(t, func() {
		NewEngine(nil)
	}, "NewEngine must reject a nil EventAppender so callers fail fast at construction")
}

func TestNewEnginePanicsOnTypedNilAppender(t *testing.T) {
	// A typed-nil (*MemoryEventStore)(nil) is NOT caught by a naive
	// `== nil` guard because the interface wraps a non-nil type
	// descriptor. The constructor uses reflection (isNilEventAppender)
	// to detect this so misconfiguration surfaces at construction time
	// rather than on first Handle* call.
	var nilStore *MemoryEventStore
	assert.Panics(t, func() {
		_ = NewEngine(nilStore)
	}, "typed-nil store must panic at construction, not on first use")
}
