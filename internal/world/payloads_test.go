// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

func TestMovePayload_JSON(t *testing.T) {
	tests := []struct {
		name    string
		payload world.MovePayload
		json    string
	}{
		{
			name: "character move with exit",
			payload: world.MovePayload{
				EntityType: "character",
				EntityID:   "char-123",
				FromType:   "location",
				FromID:     "room-1",
				ToType:     "location",
				ToID:       "room-2",
				ExitID:     "exit-123",
				ExitName:   "north",
			},
			json: `{"entity_type":"character","entity_id":"char-123","from_type":"location","from_id":"room-1","to_type":"location","to_id":"room-2","exit_id":"exit-123","exit_name":"north"}`,
		},
		{
			name: "object move to character",
			payload: world.MovePayload{
				EntityType: "object",
				EntityID:   "obj-456",
				FromType:   "location",
				FromID:     "room-1",
				ToType:     "character",
				ToID:       "char-789",
			},
			json: `{"entity_type":"object","entity_id":"obj-456","from_type":"location","from_id":"room-1","to_type":"character","to_id":"char-789"}`,
		},
		{
			name: "object move to container",
			payload: world.MovePayload{
				EntityType: "object",
				EntityID:   "obj-456",
				FromType:   "character",
				FromID:     "char-123",
				ToType:     "object",
				ToID:       "container-999",
			},
			json: `{"entity_type":"object","entity_id":"obj-456","from_type":"character","from_id":"char-123","to_type":"object","to_id":"container-999"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.payload)
			require.NoError(t, err)
			assert.JSONEq(t, tt.json, string(data))

			// Test unmarshaling
			var unmarshaled world.MovePayload
			err = json.Unmarshal([]byte(tt.json), &unmarshaled)
			require.NoError(t, err)
			assert.Equal(t, tt.payload, unmarshaled)
		})
	}
}

func TestObjectGivePayload_JSON(t *testing.T) {
	tests := []struct {
		name    string
		payload world.ObjectGivePayload
		json    string
	}{
		{
			name: "simple give",
			payload: world.ObjectGivePayload{
				ObjectID:        "obj-123",
				ObjectName:      "Sword",
				FromCharacterID: "char-1",
				ToCharacterID:   "char-2",
			},
			json: `{"object_id":"obj-123","object_name":"Sword","from_character_id":"char-1","to_character_id":"char-2"}`,
		},
		{
			name: "give with special characters",
			payload: world.ObjectGivePayload{
				ObjectID:        "obj-456",
				ObjectName:      "Silver Dagger of the Moon",
				FromCharacterID: "char-abc",
				ToCharacterID:   "char-xyz",
			},
			json: `{"object_id":"obj-456","object_name":"Silver Dagger of the Moon","from_character_id":"char-abc","to_character_id":"char-xyz"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.payload)
			require.NoError(t, err)
			assert.JSONEq(t, tt.json, string(data))

			// Test unmarshaling
			var unmarshaled world.ObjectGivePayload
			err = json.Unmarshal([]byte(tt.json), &unmarshaled)
			require.NoError(t, err)
			assert.Equal(t, tt.payload, unmarshaled)
		})
	}
}

func TestMovePayload_OmitEmptyFields(t *testing.T) {
	payload := world.MovePayload{
		EntityType: "character",
		EntityID:   "char-123",
		FromType:   "location",
		FromID:     "room-1",
		ToType:     "location",
		ToID:       "room-2",
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	// Empty exit fields should be omitted
	assert.NotContains(t, string(data), "exit_id")
	assert.NotContains(t, string(data), "exit_name")
}

func TestPayloads_RoundTrip(t *testing.T) {
	t.Run("move payload round trip", func(t *testing.T) {
		original := world.MovePayload{
			EntityType: "object",
			EntityID:   "obj-999",
			FromType:   "character",
			FromID:     "char-555",
			ToType:     "location",
			ToID:       "room-777",
			ExitID:     "exit-888",
			ExitName:   "south",
		}

		// Marshal to JSON
		jsonData, err := json.Marshal(original)
		require.NoError(t, err)

		// Unmarshal back
		var restored world.MovePayload
		err = json.Unmarshal(jsonData, &restored)
		require.NoError(t, err)

		// Should be identical
		assert.Equal(t, original, restored)
	})

	t.Run("object give payload round trip", func(t *testing.T) {
		original := world.ObjectGivePayload{
			ObjectID:        "obj-111",
			ObjectName:      "Golden Ring",
			FromCharacterID: "char-aaa",
			ToCharacterID:   "char-bbb",
		}

		// Marshal to JSON
		jsonData, err := json.Marshal(original)
		require.NoError(t, err)

		// Unmarshal back
		var restored world.ObjectGivePayload
		err = json.Unmarshal(jsonData, &restored)
		require.NoError(t, err)

		// Should be identical
		assert.Equal(t, original, restored)
	})
}
