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

	t.Run("non-nil from ID with none from type is invalid", func(t *testing.T) {
		// If FromType is "none" (first-time placement), FromID must be nil
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   entityID,
			FromType:   world.ContainmentTypeNone,
			FromID:     &fromID, // Invalid: should be nil when FromType is "none"
			ToType:     world.ContainmentTypeLocation,
			ToID:       toID,
		}
		err := payload.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from_id")
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

	t.Run("examine payload round trip", func(t *testing.T) {
		charID := ulid.Make()
		targetID := ulid.Make()
		locID := ulid.Make()

		original := world.ExaminePayload{
			CharacterID: charID,
			TargetType:  world.TargetTypeObject,
			TargetID:    targetID,
			TargetName:  "Mysterious Chest",
			LocationID:  locID,
		}

		// Marshal to JSON
		jsonData, err := json.Marshal(original)
		require.NoError(t, err)

		// Unmarshal back
		var restored world.ExaminePayload
		err = json.Unmarshal(jsonData, &restored)
		require.NoError(t, err)

		// Should be identical
		assert.Equal(t, original, restored)
	})
}

func TestTargetType_IsValid(t *testing.T) {
	tests := []struct {
		name    string
		tt      world.TargetType
		isValid bool
	}{
		{"location is valid", world.TargetTypeLocation, true},
		{"object is valid", world.TargetTypeObject, true},
		{"character is valid", world.TargetTypeCharacter, true},
		{"empty is invalid", world.TargetType(""), false},
		{"random is invalid", world.TargetType("random"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isValid, tt.tt.IsValid())
		})
	}
}

func TestNewMovePayload(t *testing.T) {
	t.Run("creates valid move payload", func(t *testing.T) {
		entityID := ulid.Make()
		fromID := ulid.Make()
		toID := ulid.Make()

		p, err := world.NewMovePayload(
			world.EntityTypeCharacter,
			entityID,
			world.ContainmentTypeLocation, &fromID,
			world.ContainmentTypeLocation, toID,
		)
		require.NoError(t, err)
		assert.Equal(t, world.EntityTypeCharacter, p.EntityType)
		assert.Equal(t, entityID, p.EntityID)
		assert.Equal(t, world.ContainmentTypeLocation, p.FromType)
		assert.Equal(t, fromID, *p.FromID)
		assert.Equal(t, world.ContainmentTypeLocation, p.ToType)
		assert.Equal(t, toID, p.ToID)
	})

	t.Run("returns error for zero entity ID", func(t *testing.T) {
		fromID := ulid.Make()
		toID := ulid.Make()
		_, err := world.NewMovePayload(
			world.EntityTypeCharacter,
			ulid.ULID{},
			world.ContainmentTypeLocation, &fromID,
			world.ContainmentTypeLocation, toID,
		)
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "entity_id", ve.Field)
	})

	t.Run("returns error for invalid entity type", func(t *testing.T) {
		_, err := world.NewMovePayload(
			world.EntityType("invalid"),
			ulid.Make(),
			world.ContainmentTypeLocation, ptrULID(ulid.Make()),
			world.ContainmentTypeLocation, ulid.Make(),
		)
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "entity_type", ve.Field)
	})

	t.Run("returns error for zero to_id", func(t *testing.T) {
		_, err := world.NewMovePayload(
			world.EntityTypeObject,
			ulid.Make(),
			world.ContainmentTypeLocation, ptrULID(ulid.Make()),
			world.ContainmentTypeLocation, ulid.ULID{},
		)
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "to_id", ve.Field)
	})
}

func TestNewFirstPlacement(t *testing.T) {
	t.Run("creates valid first placement payload", func(t *testing.T) {
		entityID := ulid.Make()
		toID := ulid.Make()

		p, err := world.NewFirstPlacement(
			world.EntityTypeObject,
			entityID,
			world.ContainmentTypeLocation, toID,
		)
		require.NoError(t, err)
		assert.Equal(t, world.ContainmentTypeNone, p.FromType)
		assert.Nil(t, p.FromID)
		assert.Equal(t, world.ContainmentTypeLocation, p.ToType)
		assert.Equal(t, toID, p.ToID)
	})

	t.Run("returns error for invalid entity type", func(t *testing.T) {
		entityID := ulid.Make()
		toID := ulid.Make()
		_, err := world.NewFirstPlacement(
			world.EntityType("invalid"),
			entityID,
			world.ContainmentTypeLocation, toID,
		)
		require.Error(t, err)
	})

	t.Run("returns error for zero entity ID", func(t *testing.T) {
		_, err := world.NewFirstPlacement(
			world.EntityTypeCharacter,
			ulid.ULID{},
			world.ContainmentTypeLocation, ulid.Make(),
		)
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "entity_id", ve.Field)
	})
}

func TestNewExaminePayload(t *testing.T) {
	t.Run("creates valid examine payload", func(t *testing.T) {
		charID := ulid.Make()
		targetID := ulid.Make()
		locID := ulid.Make()

		p, err := world.NewExaminePayload(charID, world.TargetTypeLocation, targetID, locID)
		require.NoError(t, err)
		assert.Equal(t, charID, p.CharacterID)
		assert.Equal(t, world.TargetTypeLocation, p.TargetType)
		assert.Equal(t, targetID, p.TargetID)
		assert.Equal(t, locID, p.LocationID)
	})

	t.Run("returns error for zero character ID", func(t *testing.T) {
		_, err := world.NewExaminePayload(ulid.ULID{}, world.TargetTypeLocation, ulid.Make(), ulid.Make())
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "character_id", ve.Field)
	})

	t.Run("returns error for invalid target type", func(t *testing.T) {
		_, err := world.NewExaminePayload(ulid.Make(), world.TargetType("invalid"), ulid.Make(), ulid.Make())
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "target_type", ve.Field)
	})

	t.Run("returns error for zero target ID", func(t *testing.T) {
		_, err := world.NewExaminePayload(ulid.Make(), world.TargetTypeCharacter, ulid.ULID{}, ulid.Make())
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "target_id", ve.Field)
	})

	t.Run("returns error for zero location ID", func(t *testing.T) {
		_, err := world.NewExaminePayload(ulid.Make(), world.TargetTypeObject, ulid.Make(), ulid.ULID{})
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "location_id", ve.Field)
	})
}

func TestNewObjectGivePayload(t *testing.T) {
	t.Run("creates valid object give payload", func(t *testing.T) {
		objID := ulid.Make()
		fromID := ulid.Make()
		toID := ulid.Make()

		p, err := world.NewObjectGivePayload(objID, fromID, toID, "sword")
		require.NoError(t, err)
		assert.Equal(t, objID, p.ObjectID)
		assert.Equal(t, fromID, p.FromCharacterID)
		assert.Equal(t, toID, p.ToCharacterID)
		assert.Equal(t, "sword", p.ObjectName)
	})

	t.Run("returns error when giving to self", func(t *testing.T) {
		charID := ulid.Make()
		_, err := world.NewObjectGivePayload(ulid.Make(), charID, charID, "sword")
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "to_character_id", ve.Field)
	})

	t.Run("returns error for zero object ID", func(t *testing.T) {
		_, err := world.NewObjectGivePayload(ulid.ULID{}, ulid.Make(), ulid.Make(), "sword")
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "object_id", ve.Field)
	})

	t.Run("returns error for empty object name", func(t *testing.T) {
		_, err := world.NewObjectGivePayload(ulid.Make(), ulid.Make(), ulid.Make(), "")
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "object_name", ve.Field)
	})

	t.Run("returns error for zero from character ID", func(t *testing.T) {
		_, err := world.NewObjectGivePayload(ulid.Make(), ulid.ULID{}, ulid.Make(), "sword")
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "from_character_id", ve.Field)
	})

	t.Run("returns error for zero to character ID", func(t *testing.T) {
		_, err := world.NewObjectGivePayload(ulid.Make(), ulid.Make(), ulid.ULID{}, "sword")
		require.Error(t, err)
		var ve *world.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "to_character_id", ve.Field)
	})
}

// ptrULID returns a pointer to the given ULID.
func ptrULID(id ulid.ULID) *ulid.ULID {
	return &id
}

func TestExaminePayload_Validate(t *testing.T) {
	charID := ulid.Make()
	targetID := ulid.Make()
	locID := ulid.Make()

	tests := []struct {
		name      string
		payload   world.ExaminePayload
		wantErr   bool
		wantField string
	}{
		{
			name: "valid examine location",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  world.TargetTypeLocation,
				TargetID:    targetID,
				TargetName:  "Town Square",
				LocationID:  locID,
			},
			wantErr: false,
		},
		{
			name: "valid examine object",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  world.TargetTypeObject,
				TargetID:    targetID,
				TargetName:  "Wooden Chest",
				LocationID:  locID,
			},
			wantErr: false,
		},
		{
			name: "valid examine character",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  world.TargetTypeCharacter,
				TargetID:    targetID,
				TargetName:  "Mysterious Stranger",
				LocationID:  locID,
			},
			wantErr: false,
		},
		{
			name: "zero character_id fails",
			payload: world.ExaminePayload{
				CharacterID: ulid.ULID{},
				TargetType:  world.TargetTypeObject,
				TargetID:    targetID,
				LocationID:  locID,
			},
			wantErr:   true,
			wantField: "character_id",
		},
		{
			name: "empty target_type fails",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  "",
				TargetID:    targetID,
				LocationID:  locID,
			},
			wantErr:   true,
			wantField: "target_type",
		},
		{
			name: "invalid target_type fails",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  world.TargetType("invalid"),
				TargetID:    targetID,
				LocationID:  locID,
			},
			wantErr:   true,
			wantField: "target_type",
		},
		{
			name: "zero target_id fails",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  world.TargetTypeObject,
				TargetID:    ulid.ULID{},
				LocationID:  locID,
			},
			wantErr:   true,
			wantField: "target_id",
		},
		{
			name: "zero location_id fails",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  world.TargetTypeObject,
				TargetID:    targetID,
				LocationID:  ulid.ULID{},
			},
			wantErr:   true,
			wantField: "location_id",
		},
		{
			name: "empty target_name allowed",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  world.TargetTypeObject,
				TargetID:    targetID,
				TargetName:  "",
				LocationID:  locID,
			},
			wantErr: false,
		},
		// Self-examination is explicitly supported (standard MUSH behavior).
		// Players commonly use "look me" to see their own description.
		{
			name: "self-examination allowed (character examines self)",
			payload: world.ExaminePayload{
				CharacterID: charID,
				TargetType:  world.TargetTypeCharacter,
				TargetID:    charID, // Same as CharacterID - examining self
				TargetName:  "Self",
				LocationID:  locID,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if tt.wantErr {
				require.Error(t, err)
				var validationErr *world.ValidationError
				require.ErrorAs(t, err, &validationErr)
				assert.Equal(t, tt.wantField, validationErr.Field)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
