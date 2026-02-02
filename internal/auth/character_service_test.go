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
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

// mockCharacterRepository is a mock for auth.CharacterRepository.
type mockCharacterRepository struct {
	mock.Mock
}

func (m *mockCharacterRepository) Create(ctx context.Context, char *world.Character) error {
	args := m.Called(ctx, char)
	return args.Error(0)
}

func (m *mockCharacterRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	args := m.Called(ctx, name)
	return args.Bool(0), args.Error(1)
}

func (m *mockCharacterRepository) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	args := m.Called(ctx, playerID)
	return args.Int(0), args.Error(1)
}

// mockLocationRepository is a mock for auth.LocationRepository.
type mockLocationRepository struct {
	mock.Mock
}

func (m *mockLocationRepository) GetStartingLocation(ctx context.Context) (*world.Location, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*world.Location), args.Error(1)
}

func TestCharacterService_Create(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	t.Run("creates character with valid name", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

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

		charRepo.AssertExpectations(t)
		locRepo.AssertExpectations(t)
	})

	t.Run("normalizes character name", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

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
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

		char, err := svc.Create(ctx, playerID, "123")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_INVALID_NAME")
	})

	t.Run("rejects empty name", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

		char, err := svc.Create(ctx, playerID, "")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_INVALID_NAME")
	})

	t.Run("rejects duplicate name case insensitive", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

		// "Alaric" already exists
		charRepo.On("ExistsByName", ctx, "Alaric").Return(true, nil)

		char, err := svc.Create(ctx, playerID, "ALARIC") // different case
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_NAME_TAKEN")
	})

	t.Run("rejects when player at character limit", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(auth.DefaultMaxCharacters, nil)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_LIMIT_REACHED")
	})

	t.Run("returns error when starting location unavailable", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(0, nil)
		locRepo.On("GetStartingLocation", ctx).Return(nil, world.ErrNotFound)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_NO_STARTING_LOCATION")
	})

	t.Run("propagates repository errors on ExistsByName", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, assert.AnError)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_CREATE_FAILED")
	})

	t.Run("propagates repository errors on CountByPlayer", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(0, assert.AnError)

		char, err := svc.Create(ctx, playerID, "alaric")
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_CREATE_FAILED")
	})

	t.Run("propagates repository errors on Create", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

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
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

		// Custom limit of 3
		charRepo.On("ExistsByName", ctx, "Alaric").Return(false, nil)
		charRepo.On("CountByPlayer", ctx, playerID).Return(3, nil)

		char, err := svc.CreateWithMaxCharacters(ctx, playerID, "alaric", 3)
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_LIMIT_REACHED")
	})

	t.Run("allows creation when under custom limit", func(t *testing.T) {
		charRepo := new(mockCharacterRepository)
		locRepo := new(mockLocationRepository)
		svc := auth.NewCharacterService(charRepo, locRepo)

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
