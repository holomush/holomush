// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/core/coretest"
	"github.com/holomush/holomush/internal/eventvocab"
)

func TestEngineHandleConnectStoresArriveEventWithCharacterPayload(t *testing.T) {
	store := coretest.NewMemoryEventStore()
	engine := core.NewEngine(store)

	ctx := context.Background()
	charID := core.NewULID()
	locationID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Alyssa", LocationID: locationID}

	stream := "location." + locationID.String()

	err := engine.HandleConnect(ctx, char)
	require.NoError(t, err)

	// Verify event was stored with correct type, stream, actor
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, eventvocab.EventTypeArrive, events[0].Type)
	assert.Equal(t, stream, events[0].Stream)
	assert.Equal(t, core.ActorCharacter, events[0].Actor.Kind)
	assert.Equal(t, charID.String(), events[0].Actor.ID)

	// Verify payload
	var payload core.ArrivePayload
	require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
	assert.Equal(t, "Alyssa", payload.CharacterName)
}

func TestEngineHandleDisconnectStoresLeaveEventWithReasonPayload(t *testing.T) {
	store := coretest.NewMemoryEventStore()
	engine := core.NewEngine(store)

	ctx := context.Background()
	charID := core.NewULID()
	locationID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Alyssa", LocationID: locationID}

	stream := "location." + locationID.String()

	err := engine.HandleDisconnect(ctx, char, "quit")
	require.NoError(t, err)

	// Verify event was stored with correct type, stream, actor
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, eventvocab.EventTypeLeave, events[0].Type)
	assert.Equal(t, stream, events[0].Stream)
	assert.Equal(t, core.ActorCharacter, events[0].Actor.Kind)
	assert.Equal(t, charID.String(), events[0].Actor.ID)

	// Verify payload
	var payload core.LeavePayload
	require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
	assert.Equal(t, "Alyssa", payload.CharacterName)
	assert.Equal(t, "quit", payload.Reason)
}

func TestNewEngineAcceptsMemoryStore(t *testing.T) {
	store := coretest.NewMemoryEventStore()
	e := core.NewEngine(store)
	assert.NotNil(t, e)
}

func TestNewEnginePanicsOnNilAppender(t *testing.T) {
	assert.Panics(t, func() {
		core.NewEngine(nil)
	}, "NewEngine must reject a nil EventAppender so callers fail fast at construction")
}

func TestNewEnginePanicsOnTypedNilAppender(t *testing.T) {
	// A typed-nil (*coretest.MemoryEventStore)(nil) is NOT caught by a naive
	// `== nil` guard because the interface wraps a non-nil type
	// descriptor. The constructor uses reflection (isNilEventAppender)
	// to detect this so misconfiguration surfaces at construction time
	// rather than on first Handle* call.
	var nilStore *coretest.MemoryEventStore
	assert.Panics(t, func() {
		_ = core.NewEngine(nilStore)
	}, "typed-nil store must panic at construction, not on first use")
}
