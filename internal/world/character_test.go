// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
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
