// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

func TestMovePayload_JSON(t *testing.T) {
	// Use fixed ULIDs for predictable JSON comparison
	entityID := ulid.MustParse("01HQ1234567890ABCDEFGH0001")
	fromID := ulid.MustParse("01HQ1234567890ABCDEFGH0002")
	toID := ulid.MustParse("01HQ1234567890ABCDEFGH0003")
	exitID := ulid.MustParse("01HQ1234567890ABCDEFGH0004")

	t.Run("character move with exit", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
			ExitID:     &exitID,
			ExitName:   "north",
		}
		expectedJSON := `{"entity_type":"character","entity_id":"01HQ1234567890ABCDEFGH0001","from_type":"location","from_id":"01HQ1234567890ABCDEFGH0002","to_type":"location","to_id":"01HQ1234567890ABCDEFGH0003","exit_id":"01HQ1234567890ABCDEFGH0004","exit_name":"north"}`

		// Test marshaling
		data, err := json.Marshal(payload)
		require.NoError(t, err)
		assert.JSONEq(t, expectedJSON, string(data))

		// Test unmarshaling
		var unmarshaled world.MovePayload
		err = json.Unmarshal([]byte(expectedJSON), &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, payload, unmarshaled)
	})

	t.Run("object move to character", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeCharacter,
			ToID:       toID,
		}
		expectedJSON := `{"entity_type":"object","entity_id":"01HQ1234567890ABCDEFGH0001","from_type":"location","from_id":"01HQ1234567890ABCDEFGH0002","to_type":"character","to_id":"01HQ1234567890ABCDEFGH0003"}`

		data, err := json.Marshal(payload)
		require.NoError(t, err)
		assert.JSONEq(t, expectedJSON, string(data))

		var unmarshaled world.MovePayload
		err = json.Unmarshal([]byte(expectedJSON), &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, payload, unmarshaled)
	})

	t.Run("object move to container", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeCharacter,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeObject,
			ToID:       toID,
		}
		expectedJSON := `{"entity_type":"object","entity_id":"01HQ1234567890ABCDEFGH0001","from_type":"character","from_id":"01HQ1234567890ABCDEFGH0002","to_type":"object","to_id":"01HQ1234567890ABCDEFGH0003"}`

		data, err := json.Marshal(payload)
		require.NoError(t, err)
		assert.JSONEq(t, expectedJSON, string(data))

		var unmarshaled world.MovePayload
		err = json.Unmarshal([]byte(expectedJSON), &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, payload, unmarshaled)
	})
}

func TestObjectGivePayload_JSON(t *testing.T) {
	// Use fixed ULIDs for predictable JSON comparison
	objID := ulid.MustParse("01HQ1234567890ABCDEFGH0001")
	fromCharID := ulid.MustParse("01HQ1234567890ABCDEFGH0002")
	toCharID := ulid.MustParse("01HQ1234567890ABCDEFGH0003")

	t.Run("simple give", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Sword",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
		}
		expectedJSON := `{"object_id":"01HQ1234567890ABCDEFGH0001","object_name":"Sword","from_character_id":"01HQ1234567890ABCDEFGH0002","to_character_id":"01HQ1234567890ABCDEFGH0003"}`

		data, err := json.Marshal(payload)
		require.NoError(t, err)
		assert.JSONEq(t, expectedJSON, string(data))

		var unmarshaled world.ObjectGivePayload
		err = json.Unmarshal([]byte(expectedJSON), &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, payload, unmarshaled)
	})

	t.Run("give with special characters in name", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Silver Dagger of the Moon",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
		}
		expectedJSON := `{"object_id":"01HQ1234567890ABCDEFGH0001","object_name":"Silver Dagger of the Moon","from_character_id":"01HQ1234567890ABCDEFGH0002","to_character_id":"01HQ1234567890ABCDEFGH0003"}`

		data, err := json.Marshal(payload)
		require.NoError(t, err)
		assert.JSONEq(t, expectedJSON, string(data))

		var unmarshaled world.ObjectGivePayload
		err = json.Unmarshal([]byte(expectedJSON), &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, payload, unmarshaled)
	})
}

func TestMovePayload_OmitEmptyFields(t *testing.T) {
	entityID := ulid.Make()
	fromID := ulid.Make()
	toID := ulid.Make()

	payload := world.MovePayload{
		EntityType: world.EntityTypeCharacter,
		EntityID:   entityID,
		FromType:   world.ContainmentTypeLocation,
		FromID:     &fromID,
		ToType:     world.ContainmentTypeLocation,
		ToID:       toID,
		// ExitID and ExitName not set - should be omitted
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	// Empty exit fields should be omitted
	assert.NotContains(t, string(data), "exit_id")
	assert.NotContains(t, string(data), "exit_name")
}

func TestMovePayload_Validate(t *testing.T) {
	// Pre-create ULIDs for test cases
	entityID := ulid.Make()
	fromID := ulid.Make()
	toID := ulid.Make()
	exitID := ulid.Make()

	t.Run("valid character move", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
		}
		require.NoError(t, payload.Validate())
	})

	t.Run("valid object move to character", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeCharacter,
			ToID:       toID,
		}
		require.NoError(t, payload.Validate())
	})

	t.Run("valid object move to container", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeCharacter,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeObject,
			ToID:       toID,
		}
		require.NoError(t, payload.Validate())
	})

	t.Run("valid first-time placement (from none)", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeNone,
			FromID:     nil, // nil is valid when FromType is "none"
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
		}
		require.NoError(t, payload.Validate())
	})

	t.Run("invalid entity type", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: "invalid",
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entity_type")
	})

	t.Run("empty entity type", func(t *testing.T) {
		payload := world.MovePayload{
			EntityID: entityID,
			FromType: world.ContainmentTypeLocation,
			FromID:   &fromID,
			ToType:   world.ContainmentTypeLocation,
			ToID:     toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entity_type")
	})

	t.Run("zero entity ID", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			// EntityID is zero value
			FromType: world.ContainmentTypeLocation,
			FromID:   &fromID,
			ToType:   world.ContainmentTypeLocation,
			ToID:     toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entity_id")
	})

	t.Run("empty from type", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			// FromType intentionally empty
			FromID: &fromID,
			ToType: world.ContainmentTypeLocation,
			ToID:   toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from_type")
	})

	t.Run("invalid from type", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			FromType:   "invalid",
			FromID:     &fromID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from_type")
	})

	t.Run("nil from ID when not none type", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     nil, // nil is invalid when FromType is not "none"
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from_id")
	})

	t.Run("empty to type", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			// ToType intentionally empty
			ToID: toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "to_type")
	})

	t.Run("invalid to type", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			ToType:     "invalid",
			ToID:       toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "to_type")
	})

	t.Run("zero to ID", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeLocation,
			// ToID is zero value
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "to_id")
	})

	t.Run("character move with exit (valid)", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeCharacter,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
			ExitID:     &exitID,
			ExitName:   "north",
		}
		require.NoError(t, payload.Validate())
	})
}

func TestObjectGivePayload_Validate(t *testing.T) {
	// Pre-create ULIDs for test cases
	objID := ulid.Make()
	fromCharID := ulid.Make()
	toCharID := ulid.Make()

	t.Run("valid give", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Sword",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
		}
		require.NoError(t, payload.Validate())
	})

	t.Run("zero object ID", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			// ObjectID is zero value
			ObjectName:      "Sword",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object_id")
	})

	t.Run("empty object name", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object_name")
	})

	t.Run("zero from character ID", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:      objID,
			ObjectName:    "Sword",
			ToCharacterID: toCharID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from_character_id")
	})

	t.Run("zero to character ID", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Sword",
			FromCharacterID: fromCharID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "to_character_id")
	})

	t.Run("self give not allowed", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Sword",
			FromCharacterID: fromCharID,
			ToCharacterID:   fromCharID, // Same as from
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "to_character_id")
	})
}

func TestEntityType_IsValid(t *testing.T) {
	tests := []struct {
		name    string
		et      world.EntityType
		isValid bool
	}{
		{"character is valid", world.EntityTypeCharacter, true},
		{"object is valid", world.EntityTypeObject, true},
		{"empty is invalid", world.EntityType(""), false},
		{"unknown is invalid", world.EntityType("unknown"), false},
		{"location is invalid", world.EntityType("location"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isValid, tt.et.IsValid())
		})
	}
}

func TestContainmentType_IsValid(t *testing.T) {
	tests := []struct {
		name    string
		ct      world.ContainmentType
		isValid bool
	}{
		{"location is valid", world.ContainmentTypeLocation, true},
		{"character is valid", world.ContainmentTypeCharacter, true},
		{"object is valid", world.ContainmentTypeObject, true},
		{"none is invalid for IsValid", world.ContainmentTypeNone, false},
		{"empty is invalid", world.ContainmentType(""), false},
		{"unknown is invalid", world.ContainmentType("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isValid, tt.ct.IsValid())
		})
	}
}

func TestContainmentType_IsValidOrNone(t *testing.T) {
	tests := []struct {
		name    string
		ct      world.ContainmentType
		isValid bool
	}{
		{"location is valid", world.ContainmentTypeLocation, true},
		{"character is valid", world.ContainmentTypeCharacter, true},
		{"object is valid", world.ContainmentTypeObject, true},
		{"none is valid for IsValidOrNone", world.ContainmentTypeNone, true},
		{"empty is invalid", world.ContainmentType(""), false},
		{"unknown is invalid", world.ContainmentType("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isValid, tt.ct.IsValidOrNone())
		})
	}
}

func TestPayloads_RoundTrip(t *testing.T) {
	t.Run("move payload round trip", func(t *testing.T) {
		entityID := ulid.Make()
		fromID := ulid.Make()
		toID := ulid.Make()
		exitID := ulid.Make()

		original := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeCharacter,
			FromID:     &fromID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
			ExitID:     &exitID,
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
		objID := ulid.Make()
		fromCharID := ulid.Make()
		toCharID := ulid.Make()

		original := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Golden Ring",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
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
