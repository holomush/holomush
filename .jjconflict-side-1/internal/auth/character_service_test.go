// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestNewCharacterService_NilDependencies(t *testing.T) {
	tests := []struct {
		name        string
		charRepo    auth.CharacterRepository
		locRepo     auth.LocationRepository
		expectError string
	}{
		{
			name:        "nil character repository",
			charRepo:    nil,
			locRepo:     mocks.NewMockLocationRepository(t),
			expectError: "character repository is required",
		},
		{
			name:        "nil location repository",
			charRepo:    mocks.NewMockCharacterRepository(t),
			locRepo:     nil,
			expectError: "location repository is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewCharacterService(tt.charRepo, tt.locRepo)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestCharacterService_Create(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	t.Run("creates character with valid name", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		startingLoc := &world.Location{ID: ulid.Make()}
		locRepo.On("GetStartingLocation", ctx).Return(startingLoc, nil)
		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(0, nil)
		charRepo.On("Create", ctx, mock.AnythingOfType("*world.Character")).Return(nil)

		char, err := svc.Create(ctx, playerID, "alaric")
		require.NoError(t, err)
		require.NotNil(t, char)
		assert.Equal(t, "Alaric", char.Name) // normalized to Initial Caps
		assert.Equal(t, playerID, char.PlayerID)
		assert.Equal(t, &startingLoc.ID, char.LocationID)
	})

	t.Run("normalizes character name", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		startingLoc := &world.Location{ID: ulid.Make()}
		locRepo.On("GetStartingLocation", ctx).Return(startingLoc, nil)
		charRepo.On("ExistsByName", ctx, "John Smith").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(0, nil)
		charRepo.On("Create", ctx, mock.AnythingOfType("*world.Character")).Return(nil)

		char, err := svc.Create(ctx, playerID, "  jOHN   sMITH  ")
		require.NoError(t, err)
		assert.Equal(t, "John Smith", char.Name)
	})

	t.Run("rejects invalid character name", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		char, err := svc.Create(ctx, playerID, "123")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_INVALID_NAME")
	})

	t.Run("rejects empty name", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		char, err := svc.Create(ctx, playerID, "")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_INVALID_NAME")
	})

	t.Run("rejects duplicate name case insensitive", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		// "Alaric" already exists
		charRepo.On("ExistsByName", ctx, "Alaric").Return(true, nil)

		char, err := svc.Create(ctx, playerID, "ALARIC") // different case
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_NAME_TAKEN")
	})

	t.Run("rejects when player at character limit", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(auth.DefaultMaxCharacters, nil)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_LIMIT_REACHED")
	})

	t.Run("returns error when starting location unavailable", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(0, nil)
		locRepo.On("GetStartingLocation", ctx).Return(nil, world.ErrNotFound)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_NO_STARTING_LOCATION")
	})

	t.Run("propagates repository errors on ExistsByName", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, assert.AnError)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_CREATE_FAILED")
	})

	t.Run("propagates repository errors on CountByPlayer", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(0, assert.AnError)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_CREATE_FAILED")
	})

	t.Run("propagates repository errors on Create", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		startingLoc := &world.Location{ID: ulid.Make()}
		locRepo.On("GetStartingLocation", ctx).Return(startingLoc, nil)
		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(0, nil)
		charRepo.On("Create", ctx, mock.AnythingOfType("*world.Character")).Return(assert.AnError)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_CREATE_FAILED")
	})
}

func TestCharacterService_CreateWithMaxCharacters(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	t.Run("respects custom max characters limit", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		// Custom limit of 3
		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(3, nil)

		char, err := svc.CreateWithMaxCharacters(ctx, playerID, "alaric", 3)
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_LIMIT_REACHED")
	})

	t.Run("allows creation when under custom limit", func(t *testing.T) {
		charRepo := mocks.NewMockCharacterRepository(t)
		locRepo := mocks.NewMockLocationRepository(t)
		svc, err := auth.NewCharacterService(charRepo, locRepo)
		require.NoError(t, err)

		startingLoc := &world.Location{ID: ulid.Make()}
		locRepo.On("GetStartingLocation", ctx).Return(startingLoc, nil)
		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(9, nil)
		charRepo.On("Create", ctx, mock.AnythingOfType("*world.Character")).Return(nil)

		// Custom limit of 10
		char, err := svc.CreateWithMaxCharacters(ctx, playerID, "alaric", 10)
		require.NoError(t, err)
		require.NotNil(t, char)
		assert.Equal(t, "Alaric", char.Name)
	})
}
