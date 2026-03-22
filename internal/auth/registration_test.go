// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name        string
		password    string
		expectError bool
		errorCode   string
	}{
		{
			name:        "valid 8 character password",
			password:    "abcdefgh",
			expectError: false,
		},
		{
			name:        "valid long password",
			password:    "this_is_a_very_long_password_that_should_be_valid",
			expectError: false,
		},
		{
			name:        "too short password (7 chars)",
			password:    "abcdefg",
			expectError: true,
			errorCode:   "AUTH_INVALID_PASSWORD",
		},
		{
			name:        "empty password",
			password:    "",
			expectError: true,
			errorCode:   "AUTH_INVALID_PASSWORD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidatePassword(tt.password)
			if tt.expectError {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.errorCode)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestService_ValidateCredentials(t *testing.T) {
	ctx := context.Background()

	t.Run("valid credentials return player", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerID := ulid.Make()
		player := &auth.Player{
			ID:             playerID,
			Username:       "testuser",
			PasswordHash:   "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
			FailedAttempts: 0,
			LockedUntil:    nil,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		hasher.On("Verify", "password123", player.PasswordHash).Return(true, nil)
		playerRepo.On("Update", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)

		result, err := svc.ValidateCredentials(ctx, "testuser", "password123")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, playerID, result.ID)
	})

	t.Run("invalid password returns error", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerID := ulid.Make()
		player := &auth.Player{
			ID:             playerID,
			Username:       "testuser",
			PasswordHash:   "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
			FailedAttempts: 0,
			LockedUntil:    nil,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		hasher.On("Verify", "wrongpassword", player.PasswordHash).Return(false, nil)
		playerRepo.On("Update", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)

		result, err := svc.ValidateCredentials(ctx, "testuser", "wrongpassword")
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("non-existent user uses constant time and returns error", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerRepo.On("GetByUsername", ctx, "unknown").Return(nil, auth.ErrNotFound)
		// Verify still called with dummy hash for constant-time behavior
		hasher.On("Verify", "password123", mock.AnythingOfType("string")).Return(false, nil)

		result, err := svc.ValidateCredentials(ctx, "unknown", "password123")
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("locked account returns error after password check", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerID := ulid.Make()
		lockedAt := time.Now().Add(15 * time.Minute)
		player := &auth.Player{
			ID:             playerID,
			Username:       "testuser",
			PasswordHash:   "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
			FailedAttempts: 7,
			LockedUntil:    &lockedAt,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		// Password is verified first (constant-time), lockout check comes after
		hasher.On("Verify", "password123", player.PasswordHash).Return(true, nil)

		result, err := svc.ValidateCredentials(ctx, "testuser", "password123")
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "AUTH_ACCOUNT_LOCKED")
	})
}

func TestService_CreatePlayer(t *testing.T) {
	ctx := context.Background()

	t.Run("success with email", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerRepo.On("GetByUsername", ctx, "newuser").Return(nil, auth.ErrNotFound)
		hasher.On("Hash", "password123").Return("$argon2id$v=19$m=65536,t=1,p=4$salt$hash", nil)
		playerRepo.On("Create", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)

		player, token, err := svc.CreatePlayer(ctx, "newuser", "password123", "user@example.com")
		require.NoError(t, err)
		assert.NotNil(t, player)
		assert.NotNil(t, token)
		assert.Equal(t, "newuser", player.Username)
		assert.NotNil(t, player.Email)
		assert.Equal(t, "user@example.com", *player.Email)
		assert.NotEmpty(t, token.Token)
		assert.Equal(t, player.ID, token.PlayerID)
	})

	t.Run("success without email", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerRepo.On("GetByUsername", ctx, "newuser").Return(nil, auth.ErrNotFound)
		hasher.On("Hash", "password123").Return("$argon2id$v=19$m=65536,t=1,p=4$salt$hash", nil)
		playerRepo.On("Create", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)

		player, token, err := svc.CreatePlayer(ctx, "newuser", "password123", "")
		require.NoError(t, err)
		assert.NotNil(t, player)
		assert.NotNil(t, token)
		assert.Nil(t, player.Email)
	})

	t.Run("username taken returns error", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		existing := &auth.Player{ID: ulid.Make(), Username: "existing"}
		playerRepo.On("GetByUsername", ctx, "existing").Return(existing, nil)

		player, token, err := svc.CreatePlayer(ctx, "existing", "password123", "")
		require.Error(t, err)
		assert.Nil(t, player)
		assert.Nil(t, token)
		errutil.AssertErrorCode(t, err, "REGISTER_USERNAME_TAKEN")
	})

	t.Run("invalid username returns error", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		// Username starting with digit is invalid
		player, token, err := svc.CreatePlayer(ctx, "1invalid", "password123", "")
		require.Error(t, err)
		assert.Nil(t, player)
		assert.Nil(t, token)
		errutil.AssertErrorCode(t, err, "REGISTER_INVALID_USERNAME")
	})

	t.Run("invalid password returns error", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		// Password too short (< 8 chars)
		player, token, err := svc.CreatePlayer(ctx, "validuser", "short", "")
		require.Error(t, err)
		assert.Nil(t, player)
		assert.Nil(t, token)
		errutil.AssertErrorCode(t, err, "REGISTER_INVALID_PASSWORD")
	})

	t.Run("repository error on username check propagates", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerRepo.On("GetByUsername", ctx, "validuser").Return(nil, errors.New("database error"))

		player, token, err := svc.CreatePlayer(ctx, "validuser", "password123", "")
		require.Error(t, err)
		assert.Nil(t, player)
		assert.Nil(t, token)
		errutil.AssertErrorCode(t, err, "REGISTER_FAILED")
	})
}
