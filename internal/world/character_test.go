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

	t.Run("valid character", func(t *testing.T) {
		char := &world.Character{
			Name:       "TestChar",
			LocationID: &locID,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("empty name fails", func(t *testing.T) {
		char := &world.Character{
			Name:       "",
			LocationID: &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("nil location allowed", func(t *testing.T) {
		char := &world.Character{
			Name:       "TestChar",
			LocationID: nil,
		}
		require.NoError(t, char.Validate())
	})

	t.Run("name exceeds max length", func(t *testing.T) {
		longName := make([]byte, world.MaxNameLength+1)
		for i := range longName {
			longName[i] = 'a'
		}
		char := &world.Character{
			Name:       string(longName),
			LocationID: &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("name with control characters fails", func(t *testing.T) {
		char := &world.Character{
			Name:       "Test\x00Char",
			LocationID: &locID,
		}
		err := char.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})
}
