// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

func TestCharacter_Validate(t *testing.T) {
	locID := ulid.Make()
	playerID := ulid.Make()
	charID := ulid.Make()

	t.Run("valid character", func(t *testing.T) {
		char := &world.Character{
			ID:         charID,
			PlayerID:   playerID,
			Name:       "TestChar",
			LocationID: &locID,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("empty name fails", func(t *testing.T) {
		char := &world.Character{
			ID:         charID,
			PlayerID:   playerID,
			Name:       "",
			LocationID: &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("nil location allowed", func(t *testing.T) {
		char := &world.Character{
			ID:         charID,
			PlayerID:   playerID,
			Name:       "TestChar",
			LocationID: nil,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("name at exactly max length passes", func(t *testing.T) {
		exactName := make([]byte, world.MaxNameLength)
		for i := range exactName {
			exactName[i] = 'a'
		}
		char := &world.Character{
			ID:         charID,
			PlayerID:   playerID,
			Name:       string(exactName),
			LocationID: &locID,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("name exceeds max length", func(t *testing.T) {
		longName := make([]byte, world.MaxNameLength+1)
		for i := range longName {
			longName[i] = 'a'
		}
		char := &world.Character{
			ID:         charID,
			PlayerID:   playerID,
			Name:       string(longName),
			LocationID: &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("name with control characters fails", func(t *testing.T) {
		char := &world.Character{
			ID:         charID,
			PlayerID:   playerID,
			Name:       "Test\x00Char",
			LocationID: &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("valid description", func(t *testing.T) {
		char := &world.Character{
			ID:          charID,
			PlayerID:    playerID,
			Name:        "TestChar",
			Description: "A brave adventurer.",
			LocationID:  &locID,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("empty description allowed", func(t *testing.T) {
		char := &world.Character{
			ID:          charID,
			PlayerID:    playerID,
			Name:        "TestChar",
			Description: "",
			LocationID:  &locID,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("description at exactly max length passes", func(t *testing.T) {
		exactDesc := make([]byte, world.MaxDescriptionLength)
		for i := range exactDesc {
			exactDesc[i] = 'a'
		}
		char := &world.Character{
			ID:          charID,
			PlayerID:    playerID,
			Name:        "TestChar",
			Description: string(exactDesc),
			LocationID:  &locID,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("description exceeds max length", func(t *testing.T) {
		longDesc := make([]byte, world.MaxDescriptionLength+1)
		for i := range longDesc {
			longDesc[i] = 'a'
		}
		char := &world.Character{
			ID:          charID,
			PlayerID:    playerID,
			Name:        "TestChar",
			Description: string(longDesc),
			LocationID:  &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "description")
	})

	t.Run("description with control characters fails", func(t *testing.T) {
		char := &world.Character{
			ID:          charID,
			PlayerID:    playerID,
			Name:        "TestChar",
			Description: "Has\x00null",
			LocationID:  &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "description")
	})

	t.Run("zero player_id fails", func(t *testing.T) {
		char := &world.Character{
			ID:         charID,
			Name:       "TestChar",
			LocationID: &locID,
			// PlayerID is zero value (not set)
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "player_id")
	})

	t.Run("valid player_id passes", func(t *testing.T) {
		char := &world.Character{
			ID:         charID,
			PlayerID:   playerID,
			Name:       "TestChar",
			LocationID: &locID,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("zero id fails", func(t *testing.T) {
		char := &world.Character{
			// ID is zero value (not set)
			PlayerID:   playerID,
			Name:       "TestChar",
			LocationID: &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id")
	})

	t.Run("valid id passes", func(t *testing.T) {
		char := &world.Character{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			Name:       "TestChar",
			LocationID: &locID,
		}
		require.NoError(t, char.Validate())
	})
}

func TestNewCharacter(t *testing.T) {
	playerID := ulid.Make()

	t.Run("valid construction succeeds", func(t *testing.T) {
		char, err := world.NewCharacter(playerID, "Hero")
		require.NoError(t, err)
		assert.NotNil(t, char)
		assert.False(t, char.ID.IsZero(), "ID should be generated")
		assert.Equal(t, playerID, char.PlayerID)
		assert.Equal(t, "Hero", char.Name)
		assert.False(t, char.CreatedAt.IsZero(), "CreatedAt should be set")
	})

	t.Run("empty name fails with validation error", func(t *testing.T) {
		char, err := world.NewCharacter(playerID, "")
		assert.Nil(t, char)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("zero PlayerID fails with validation error", func(t *testing.T) {
		var zeroID ulid.ULID
		char, err := world.NewCharacter(zeroID, "Hero")
		assert.Nil(t, char)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "player_id")
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		char1, err1 := world.NewCharacter(playerID, "Hero1")
		require.NoError(t, err1)
		char2, err2 := world.NewCharacter(playerID, "Hero2")
		require.NoError(t, err2)
		assert.NotEqual(t, char1.ID, char2.ID, "IDs should be unique")
	})
}

func TestCharacter_SetLocationID(t *testing.T) {
	tests := []struct {
		name       string
		locationID *ulid.ULID
		wantErr    bool
		errField   string
	}{
		{
			name:       "nil location succeeds",
			locationID: nil,
			wantErr:    false,
		},
		{
			name:       "valid non-nil ULID succeeds",
			locationID: func() *ulid.ULID { id := ulid.Make(); return &id }(),
			wantErr:    false,
		},
		{
			name:       "zero ULID fails",
			locationID: func() *ulid.ULID { var id ulid.ULID; return &id }(),
			wantErr:    true,
			errField:   "location_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			char, err := world.NewCharacter(ulid.Make(), "TestChar")
			require.NoError(t, err)

			err = char.SetLocationID(tt.locationID)

			if tt.wantErr {
				require.Error(t, err)
				var validationErr *world.ValidationError
				require.ErrorAs(t, err, &validationErr)
				assert.Equal(t, tt.errField, validationErr.Field)
			} else {
				require.NoError(t, err)
				if tt.locationID == nil {
					assert.Nil(t, char.LocationID)
				} else {
					require.NotNil(t, char.LocationID)
					assert.Equal(t, *tt.locationID, *char.LocationID)
				}
			}
		})
	}
}

func TestCharacter_SetLocationID_UpdatesField(t *testing.T) {
	char, err := world.NewCharacter(ulid.Make(), "TestChar")
	require.NoError(t, err)
	assert.Nil(t, char.LocationID, "LocationID should be nil initially")

	// Set to a valid location
	locID := ulid.Make()
	err = char.SetLocationID(&locID)
	require.NoError(t, err)
	require.NotNil(t, char.LocationID)
	assert.Equal(t, locID, *char.LocationID)

	// Change to a different location
	newLocID := ulid.Make()
	err = char.SetLocationID(&newLocID)
	require.NoError(t, err)
	require.NotNil(t, char.LocationID)
	assert.Equal(t, newLocID, *char.LocationID)

	// Set back to nil
	err = char.SetLocationID(nil)
	require.NoError(t, err)
	assert.Nil(t, char.LocationID)
}

func TestCharacter_SetName(t *testing.T) {
	t.Run("valid name updates field", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "OriginalName")
		require.NoError(t, err)

		err = char.SetName("NewName")
		require.NoError(t, err)
		assert.Equal(t, "NewName", char.Name)
	})

	t.Run("empty name returns error", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "OriginalName")
		require.NoError(t, err)

		err = char.SetName("")
		require.Error(t, err)
		var validationErr *world.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "name", validationErr.Field)
		// Name should be unchanged
		assert.Equal(t, "OriginalName", char.Name)
	})

	t.Run("name exceeding max length returns error", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "OriginalName")
		require.NoError(t, err)

		longName := strings.Repeat("x", world.MaxNameLength+1)
		err = char.SetName(longName)
		require.Error(t, err)
		var validationErr *world.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "name", validationErr.Field)
		// Name should be unchanged
		assert.Equal(t, "OriginalName", char.Name)
	})

	t.Run("name with control characters returns error", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "OriginalName")
		require.NoError(t, err)

		err = char.SetName("Name\x00WithNull")
		require.Error(t, err)
		// Name should be unchanged
		assert.Equal(t, "OriginalName", char.Name)
	})
}

func TestCharacter_SetDescription(t *testing.T) {
	t.Run("valid description updates field", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "TestChar")
		require.NoError(t, err)

		err = char.SetDescription("A brave adventurer.")
		require.NoError(t, err)
		assert.Equal(t, "A brave adventurer.", char.Description)
	})

	t.Run("empty description is valid", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "TestChar")
		require.NoError(t, err)
		char.Description = "Initial description"

		err = char.SetDescription("")
		require.NoError(t, err)
		assert.Equal(t, "", char.Description)
	})

	t.Run("description exceeding max length returns error", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "TestChar")
		require.NoError(t, err)
		char.Description = "Original"

		longDesc := strings.Repeat("x", world.MaxDescriptionLength+1)
		err = char.SetDescription(longDesc)
		require.Error(t, err)
		var validationErr *world.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "description", validationErr.Field)
		// Description should be unchanged
		assert.Equal(t, "Original", char.Description)
	})

	t.Run("description with control characters returns error", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "TestChar")
		require.NoError(t, err)
		char.Description = "Original"

		err = char.SetDescription("Desc\x00WithNull")
		require.Error(t, err)
		// Description should be unchanged
		assert.Equal(t, "Original", char.Description)
	})

	t.Run("description with newlines and tabs is valid", func(t *testing.T) {
		char, err := world.NewCharacter(ulid.Make(), "TestChar")
		require.NoError(t, err)

		descWithWhitespace := "Line one.\nLine two.\tTabbed."
		err = char.SetDescription(descWithWhitespace)
		require.NoError(t, err)
		assert.Equal(t, descWithWhitespace, char.Description)
	})
}

func TestNewCharacterWithID(t *testing.T) {
	playerID := ulid.Make()
	charID := ulid.Make()

	t.Run("valid construction succeeds", func(t *testing.T) {
		char, err := world.NewCharacterWithID(charID, playerID, "Hero")
		require.NoError(t, err)
		assert.NotNil(t, char)
		assert.Equal(t, charID, char.ID, "ID should match provided ID")
		assert.Equal(t, playerID, char.PlayerID)
		assert.Equal(t, "Hero", char.Name)
		assert.False(t, char.CreatedAt.IsZero(), "CreatedAt should be set")
	})

	t.Run("empty name fails with validation error", func(t *testing.T) {
		char, err := world.NewCharacterWithID(charID, playerID, "")
		assert.Nil(t, char)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("zero PlayerID fails with validation error", func(t *testing.T) {
		var zeroID ulid.ULID
		char, err := world.NewCharacterWithID(charID, zeroID, "Hero")
		assert.Nil(t, char)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "player_id")
	})

	t.Run("zero ID fails with validation error", func(t *testing.T) {
		var zeroID ulid.ULID
		char, err := world.NewCharacterWithID(zeroID, playerID, "Hero")
		assert.Nil(t, char)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id")
	})

	t.Run("uses provided ID exactly", func(t *testing.T) {
		specificID := ulid.Make()
		char, err := world.NewCharacterWithID(specificID, playerID, "Hero")
		require.NoError(t, err)
		assert.Equal(t, specificID, char.ID)
	})
}
