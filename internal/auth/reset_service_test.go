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
	"github.com/holomush/holomush/pkg/errutil"
)

// mockPlayerRepository is a mock for auth.PlayerRepository.
type mockPlayerRepository struct {
	mock.Mock
}

func (m *mockPlayerRepository) Create(ctx context.Context, player *auth.Player) error {
	args := m.Called(ctx, player)
	return args.Error(0)
}

func (m *mockPlayerRepository) GetByID(ctx context.Context, id ulid.ULID) (*auth.Player, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.Player), args.Error(1)
}

func (m *mockPlayerRepository) GetByUsername(ctx context.Context, username string) (*auth.Player, error) {
	args := m.Called(ctx, username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.Player), args.Error(1)
}

func (m *mockPlayerRepository) GetByEmail(ctx context.Context, email string) (*auth.Player, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.Player), args.Error(1)
}

func (m *mockPlayerRepository) Update(ctx context.Context, player *auth.Player) error {
	args := m.Called(ctx, player)
	return args.Error(0)
}

func (m *mockPlayerRepository) UpdatePassword(ctx context.Context, id ulid.ULID, passwordHash string) error {
	args := m.Called(ctx, id, passwordHash)
	return args.Error(0)
}

func (m *mockPlayerRepository) Delete(ctx context.Context, id ulid.ULID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// mockPasswordResetRepository is a mock for auth.PasswordResetRepository.
type mockPasswordResetRepository struct {
	mock.Mock
}

func (m *mockPasswordResetRepository) Create(ctx context.Context, reset *auth.PasswordReset) error {
	args := m.Called(ctx, reset)
	return args.Error(0)
}

func (m *mockPasswordResetRepository) GetByPlayer(ctx context.Context, playerID ulid.ULID) (*auth.PasswordReset, error) {
	args := m.Called(ctx, playerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.PasswordReset), args.Error(1)
}

func (m *mockPasswordResetRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*auth.PasswordReset, error) {
	args := m.Called(ctx, tokenHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.PasswordReset), args.Error(1)
}

func (m *mockPasswordResetRepository) Delete(ctx context.Context, id ulid.ULID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockPasswordResetRepository) DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error {
	args := m.Called(ctx, playerID)
	return args.Error(0)
}

func (m *mockPasswordResetRepository) DeleteExpired(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

// mockPasswordHasher is a mock for auth.PasswordHasher.
type mockPasswordHasher struct {
	mock.Mock
}

func (m *mockPasswordHasher) Hash(password string) (string, error) {
	args := m.Called(password)
	return args.String(0), args.Error(1)
}

func (m *mockPasswordHasher) Verify(password, hash string) (bool, error) {
	args := m.Called(password, hash)
	return args.Bool(0), args.Error(1)
}

func (m *mockPasswordHasher) NeedsUpgrade(hash string) bool {
	args := m.Called(hash)
	return args.Bool(0)
}

func TestPasswordResetService_RequestReset(t *testing.T) {
	ctx := context.Background()

	t.Run("generates token for existing player", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

		email := "test@example.com"
		playerID := ulid.Make()
		player := &auth.Player{ID: playerID, Email: &email}

		playerRepo.On("GetByEmail", ctx, email).Return(player, nil)
		resetRepo.On("Create", ctx, mock.AnythingOfType("*auth.PasswordReset")).Return(nil)

		token, err := svc.RequestReset(ctx, email)
		require.NoError(t, err)
		assert.NotEmpty(t, token)
		assert.Len(t, token, 64) // 32 bytes = 64 hex chars

		playerRepo.AssertExpectations(t)
		resetRepo.AssertExpectations(t)
	})

	t.Run("returns success for non-existent player to prevent enumeration", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

		email := "nonexistent@example.com"
		playerRepo.On("GetByEmail", ctx, email).Return(nil, auth.ErrNotFound)

		token, err := svc.RequestReset(ctx, email)
		require.NoError(t, err)
		assert.Empty(t, token) // No token returned for non-existent player

		playerRepo.AssertExpectations(t)
		// resetRepo.Create should NOT be called
		resetRepo.AssertNotCalled(t, "Create")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

		email := "test@example.com"
		playerRepo.On("GetByEmail", ctx, email).Return(nil, assert.AnError)

		token, err := svc.RequestReset(ctx, email)
		require.Error(t, err)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "RESET_REQUEST_FAILED")
	})

	t.Run("propagates reset repo create errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

		token := "nonexistent0123456789abcdef0123456789abcdef0123456789abcdef01"

		resetRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, auth.ErrNotFound)

		resultPlayerID, err := svc.ValidateToken(ctx, token)
		require.Error(t, err)
		assert.Equal(t, ulid.ULID{}, resultPlayerID)
		errutil.AssertErrorCode(t, err, "RESET_TOKEN_INVALID")
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

		resultPlayerID, err := svc.ValidateToken(ctx, "")
		require.Error(t, err)
		assert.Equal(t, ulid.ULID{}, resultPlayerID)
		errutil.AssertErrorCode(t, err, "RESET_TOKEN_EMPTY")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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

		playerRepo.AssertExpectations(t)
		resetRepo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})

	t.Run("returns error for invalid token", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

		token := "invalidtoken0123456789abcdef0123456789abcdef0123456789abcdef"
		newPassword := "newSecurePassword123"

		resetRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, auth.ErrNotFound)

		err := svc.ResetPassword(ctx, token, newPassword)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "RESET_TOKEN_INVALID")
	})

	t.Run("returns error for expired token", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

		// Empty password should fail before token validation
		err := svc.ResetPassword(ctx, "sometoken", "")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "RESET_PASSWORD_EMPTY")

		// Verify no repository calls were made (password checked first)
		resetRepo.AssertNotCalled(t, "GetByTokenHash")
		hasher.AssertNotCalled(t, "Hash")
	})

	t.Run("propagates hasher errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		resetRepo := new(mockPasswordResetRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewPasswordResetService(playerRepo, resetRepo, hasher)

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

		playerRepo.AssertExpectations(t)
		resetRepo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})
}
