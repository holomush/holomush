// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
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

func TestNewPasswordResetService_NilDependencies(t *testing.T) {
	tests := []struct {
		name        string
		playerRepo  auth.PlayerRepository
		resetRepo   auth.PasswordResetRepository
		hasher      auth.PasswordHasher
		expectError string
	}{
		{
			name:        "nil player repository",
			playerRepo:  nil,
			resetRepo:   mocks.NewMockPasswordResetRepository(t),
			hasher:      mocks.NewMockPasswordHasher(t),
			expectError: "player repository is required",
		},
		{
			name:        "nil reset repository",
			playerRepo:  mocks.NewMockPlayerRepository(t),
			resetRepo:   nil,
			hasher:      mocks.NewMockPasswordHasher(t),
			expectError: "reset repository is required",
		},
		{
			name:        "nil password hasher",
			playerRepo:  mocks.NewMockPlayerRepository(t),
			resetRepo:   mocks.NewMockPasswordResetRepository(t),
			hasher:      nil,
			expectError: "password hasher is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewPasswordResetService(tt.playerRepo, tt.resetRepo, tt.hasher)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestPasswordResetService_RequestReset(t *testing.T) {
	ctx := context.Background()

	t.Run("generates token for existing player", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		email := "test@example.com"
		playerID := ulid.Make()
		player := &auth.Player{ID: playerID, Email: &email}

		playerRepo.On("GetByEmail", ctx, email).Return(player, nil)
		resetRepo.On("Create", ctx, mock.AnythingOfType("*auth.PasswordReset")).Return(nil)

		token, err := svc.RequestReset(ctx, email)
		require.NoError(t, err)
		assert.NotEmpty(t, token)
		assert.Len(t, token, 64) // 32 bytes = 64 hex chars
	})

	t.Run("returns success for non-existent player to prevent enumeration", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		email := "nonexistent@example.com"
		playerRepo.On("GetByEmail", ctx, email).Return(nil, auth.ErrNotFound)

		token, err := svc.RequestReset(ctx, email)
		require.NoError(t, err)
		assert.Empty(t, token) // No token returned for non-existent player

		// resetRepo.Create should NOT be called
		resetRepo.AssertNotCalled(t, "Create")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		email := "test@example.com"
		playerRepo.On("GetByEmail", ctx, email).Return(nil, assert.AnError)

		token, err := svc.RequestReset(ctx, email)
		require.Error(t, err)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "RESET_REQUEST_FAILED")
	})

	t.Run("propagates reset repo create errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		email := "test@example.com"
		playerID := ulid.Make()
		player := &auth.Player{ID: playerID, Email: &email}

		playerRepo.On("GetByEmail", ctx, email).Return(player, nil)
		resetRepo.On("Create", ctx, mock.AnythingOfType("*auth.PasswordReset")).Return(assert.AnError)

		token, err := svc.RequestReset(ctx, email)
		require.Error(t, err)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "RESET_REQUEST_FAILED")
	})
}

func TestPasswordResetService_ValidateToken(t *testing.T) {
	ctx := context.Background()

	t.Run("returns player ID for valid token", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Generate a real token
		token, tokenHash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: tokenHash,
			ExpiresAt: time.Now().Add(time.Hour),
		}

		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(reset, nil)

		resultPlayerID, err := svc.ValidateToken(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, playerID, resultPlayerID)
	})

	t.Run("returns error for expired token", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Generate a real token
		token, tokenHash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: tokenHash,
			ExpiresAt: time.Now().Add(-time.Hour), // Expired
		}

		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(reset, nil)

		resultPlayerID, err := svc.ValidateToken(ctx, token)
		require.Error(t, err)
		assert.Equal(t, ulid.ULID{}, resultPlayerID)
		errutil.AssertErrorCode(t, err, "RESET_TOKEN_EXPIRED")
	})

	t.Run("returns error for non-existent token", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		token := "nonexistent0123456789abcdef0123456789abcdef0123456789abcdef01"

		resetRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, auth.ErrNotFound)

		resultPlayerID, err := svc.ValidateToken(ctx, token)
		require.Error(t, err)
		assert.Equal(t, ulid.ULID{}, resultPlayerID)
		errutil.AssertErrorCode(t, err, "RESET_TOKEN_INVALID")
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		resultPlayerID, err := svc.ValidateToken(ctx, "")
		require.Error(t, err)
		assert.Equal(t, ulid.ULID{}, resultPlayerID)
		errutil.AssertErrorCode(t, err, "RESET_TOKEN_EMPTY")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		token := "sometoken0123456789abcdef0123456789abcdef0123456789abcdef01"

		resetRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, assert.AnError)

		resultPlayerID, err := svc.ValidateToken(ctx, token)
		require.Error(t, err)
		assert.Equal(t, ulid.ULID{}, resultPlayerID)
		errutil.AssertErrorCode(t, err, "RESET_VALIDATE_FAILED")
	})
}

func TestPasswordResetService_ResetPassword(t *testing.T) {
	ctx := context.Background()

	t.Run("resets password with valid token", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Generate a real token
		token, tokenHash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: tokenHash,
			ExpiresAt: time.Now().Add(time.Hour),
		}
		newPassword := "newSecurePassword123"
		hashedPassword := "$argon2id$v=19$m=65536,t=1,p=4$salt$hash" //nolint:gosec // Test data, not real credentials

		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(reset, nil)
		hasher.On("Hash", newPassword).Return(hashedPassword, nil)
		playerRepo.On("UpdatePassword", ctx, playerID, hashedPassword).Return(nil)
		resetRepo.On("DeleteByPlayer", ctx, playerID).Return(nil)

		err = svc.ResetPassword(ctx, token, newPassword)
		require.NoError(t, err)
	})

	t.Run("returns error for invalid token", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		token := "invalidtoken0123456789abcdef0123456789abcdef0123456789abcdef"
		newPassword := "newSecurePassword123"

		resetRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, auth.ErrNotFound)

		resetErr := svc.ResetPassword(ctx, token, newPassword)
		require.Error(t, resetErr)
		errutil.AssertErrorCode(t, resetErr, "RESET_TOKEN_INVALID")
	})

	t.Run("returns error for expired token", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Generate a real token
		token, tokenHash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: tokenHash,
			ExpiresAt: time.Now().Add(-time.Hour), // Expired
		}
		newPassword := "newSecurePassword123"

		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(reset, nil)

		err = svc.ResetPassword(ctx, token, newPassword)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "RESET_TOKEN_EXPIRED")
	})

	t.Run("returns error for empty password", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Empty password should fail before token validation
		resetErr := svc.ResetPassword(ctx, "sometoken", "")
		require.Error(t, resetErr)
		errutil.AssertErrorCode(t, resetErr, "RESET_PASSWORD_EMPTY")

		// Verify no repository calls were made (password checked first)
		resetRepo.AssertNotCalled(t, "GetByTokenHash")
		hasher.AssertNotCalled(t, "Hash")
	})

	t.Run("propagates hasher errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Generate a real token
		token, tokenHash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: tokenHash,
			ExpiresAt: time.Now().Add(time.Hour),
		}
		newPassword := "newSecurePassword123"

		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(reset, nil)
		hasher.On("Hash", newPassword).Return("", assert.AnError)

		err = svc.ResetPassword(ctx, token, newPassword)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "RESET_PASSWORD_FAILED")
	})

	t.Run("propagates player update errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Generate a real token
		token, tokenHash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: tokenHash,
			ExpiresAt: time.Now().Add(time.Hour),
		}
		newPassword := "newSecurePassword123"
		hashedPassword := "$argon2id$v=19$m=65536,t=1,p=4$salt$hash" //nolint:gosec // Test data, not real credentials

		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(reset, nil)
		hasher.On("Hash", newPassword).Return(hashedPassword, nil)
		playerRepo.On("UpdatePassword", ctx, playerID, hashedPassword).Return(assert.AnError)

		err = svc.ResetPassword(ctx, token, newPassword)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "RESET_PASSWORD_FAILED")
	})

	t.Run("propagates reset deletion errors but still succeeds", func(t *testing.T) {
		// Password was updated, so even if deletion fails, the reset is complete
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Generate a real token
		token, tokenHash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: tokenHash,
			ExpiresAt: time.Now().Add(time.Hour),
		}
		newPassword := "newSecurePassword123"
		hashedPassword := "$argon2id$v=19$m=65536,t=1,p=4$salt$hash" //nolint:gosec // Test data, not real credentials

		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(reset, nil)
		hasher.On("Hash", newPassword).Return(hashedPassword, nil)
		playerRepo.On("UpdatePassword", ctx, playerID, hashedPassword).Return(nil)
		resetRepo.On("DeleteByPlayer", ctx, playerID).Return(assert.AnError)

		// Password was updated successfully, so we return success even if token cleanup fails
		// This is a design choice - the main operation succeeded
		err = svc.ResetPassword(ctx, token, newPassword)
		require.NoError(t, err)
	})

	t.Run("token cannot be reused after successful reset", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		resetRepo := mocks.NewMockPasswordResetRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)
		require.NoError(t, err)

		// Generate a real token
		token, tokenHash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: tokenHash,
			ExpiresAt: time.Now().Add(time.Hour),
		}
		newPassword1 := "newSecurePassword123"
		newPassword2 := "anotherPassword456"
		hashedPassword := "$argon2id$v=19$m=65536,t=1,p=4$salt$hash" //nolint:gosec // Test data, not real credentials

		// First reset succeeds - token is found
		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(reset, nil).Once()
		hasher.On("Hash", newPassword1).Return(hashedPassword, nil)
		playerRepo.On("UpdatePassword", ctx, playerID, hashedPassword).Return(nil)
		resetRepo.On("DeleteByPlayer", ctx, playerID).Return(nil)

		err = svc.ResetPassword(ctx, token, newPassword1)
		require.NoError(t, err)

		// Second reset with same token fails - token was deleted
		resetRepo.On("GetByTokenHash", ctx, tokenHash).Return(nil, auth.ErrNotFound).Once()

		err = svc.ResetPassword(ctx, token, newPassword2)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "RESET_TOKEN_INVALID")
	})
}
