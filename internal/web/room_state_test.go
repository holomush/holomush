// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// Compile-time check that *world.Service satisfies WorldQuerier.
var _ WorldQuerier = (*world.Service)(nil)

// mockWorldQuerier implements WorldQuerier for tests.
type mockWorldQuerier struct {
	location   *world.Location
	locErr     error
	exits      []*world.Exit
	exitsErr   error
	characters []*world.Character
	charsErr   error
}

func (m *mockWorldQuerier) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	return m.location, m.locErr
}

func (m *mockWorldQuerier) GetExitsByLocation(_ context.Context, _ string, _ ulid.ULID) ([]*world.Exit, error) {
	return m.exits, m.exitsErr
}

func (m *mockWorldQuerier) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return m.characters, m.charsErr
}

func TestBuildRoomState_Success(t *testing.T) {
	locID := ulid.Make()
	exitID := ulid.Make()
	charID := ulid.Make()
	destID := ulid.Make()

	wq := &mockWorldQuerier{
		location: &world.Location{
			ID:          locID,
			Name:        "Town Square",
			Description: "A bustling town square.",
		},
		exits: []*world.Exit{
			{
				ID:             exitID,
				FromLocationID: locID,
				ToLocationID:   destID,
				Name:           "north",
				Locked:         false,
			},
		},
		characters: []*world.Character{
			{ID: charID, Name: "Alice"},
		},
	}

	h := &Handler{worldService: wq}
	ev, err := h.buildRoomState(context.Background(), locID)
	require.NoError(t, err)
	require.NotNil(t, ev)

	assert.Equal(t, "room_state", ev.GetType())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_STATE, ev.GetChannel())
	assert.NotZero(t, ev.GetTimestamp())

	meta := ev.GetMetadata().AsMap()
	loc, ok := meta["location"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Town Square", loc["name"])
	assert.Equal(t, locID.String(), loc["id"])
	assert.Equal(t, "A bustling town square.", loc["description"])

	exits, ok := meta["exits"].([]interface{})
	require.True(t, ok)
	require.Len(t, exits, 1)
	exitMap := exits[0].(map[string]interface{})
	assert.Equal(t, "north", exitMap["direction"])
	assert.Equal(t, false, exitMap["locked"])

	present, ok := meta["present"].([]interface{})
	require.True(t, ok)
	require.Len(t, present, 1)
	charMap := present[0].(map[string]interface{})
	assert.Equal(t, "Alice", charMap["name"])
}

func TestBuildRoomState_EmptyExitsAndChars(t *testing.T) {
	locID := ulid.Make()

	wq := &mockWorldQuerier{
		location: &world.Location{
			ID:          locID,
			Name:        "Void",
			Description: "Empty.",
		},
		exits:      []*world.Exit{},
		characters: []*world.Character{},
	}

	h := &Handler{worldService: wq}
	ev, err := h.buildRoomState(context.Background(), locID)
	require.NoError(t, err)
	require.NotNil(t, ev)

	meta := ev.GetMetadata().AsMap()
	exits, ok := meta["exits"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, exits)

	present, ok := meta["present"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, present)
}

func TestBuildRoomState_LocationError(t *testing.T) {
	wq := &mockWorldQuerier{
		locErr: errors.New("location not found"),
	}

	h := &Handler{worldService: wq}
	_, err := h.buildRoomState(context.Background(), ulid.Make())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "location not found")
}

func TestBuildRoomState_ExitsError(t *testing.T) {
	wq := &mockWorldQuerier{
		location: &world.Location{ID: ulid.Make(), Name: "Room"},
		exitsErr: errors.New("exits query failed"),
	}

	h := &Handler{worldService: wq}
	_, err := h.buildRoomState(context.Background(), ulid.Make())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exits query failed")
}

func TestBuildRoomState_CharsError(t *testing.T) {
	wq := &mockWorldQuerier{
		location:   &world.Location{ID: ulid.Make(), Name: "Room"},
		exits:      []*world.Exit{},
		charsErr:   errors.New("chars query failed"),
	}

	h := &Handler{worldService: wq}
	_, err := h.buildRoomState(context.Background(), ulid.Make())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chars query failed")
}

func TestConvertExits(t *testing.T) {
	exits := []*world.Exit{
		{Name: "north", Locked: false},
		{Name: "south", Locked: true},
	}

	result := convertExits(exits)
	require.Len(t, result, 2)
	assert.Equal(t, core.RoomStateExit{Direction: "north", Name: "north", Locked: false}, result[0])
	assert.Equal(t, core.RoomStateExit{Direction: "south", Name: "south", Locked: true}, result[1])
}

func TestConvertExits_Nil(t *testing.T) {
	result := convertExits(nil)
	assert.Empty(t, result)
}

func TestConvertCharacters(t *testing.T) {
	chars := []*world.Character{
		{Name: "Alice"},
		{Name: "Bob"},
	}

	result := convertCharacters(chars)
	require.Len(t, result, 2)
	assert.Equal(t, core.RoomStateChar{Name: "Alice", Idle: false}, result[0])
	assert.Equal(t, core.RoomStateChar{Name: "Bob", Idle: false}, result[1])
}

func TestConvertCharacters_Nil(t *testing.T) {
	result := convertCharacters(nil)
	assert.Empty(t, result)
}

func TestRoomStateToGameEvent(t *testing.T) {
	payload := core.RoomStatePayload{
		Location: core.RoomStateLocation{
			ID:          "loc-123",
			Name:        "Hall",
			Description: "A hall.",
		},
		Exits: []core.RoomStateExit{
			{Direction: "north", Name: "Gate", Locked: false},
		},
		Present: []core.RoomStateChar{
			{Name: "Gandalf", Idle: false},
		},
	}

	ev, err := roomStateToGameEvent(payload)
	require.NoError(t, err)
	require.NotNil(t, ev)

	assert.Equal(t, "room_state", ev.GetType())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_STATE, ev.GetChannel())
	assert.NotZero(t, ev.GetTimestamp())

	meta := ev.GetMetadata().AsMap()
	loc, ok := meta["location"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Hall", loc["name"])
}
