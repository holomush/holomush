// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

func TestNewAuthService_NilDependencies(t *testing.T) {
	tests := []struct {
		name        string
		players     auth.PlayerRepository
		sessions    auth.WebSessionRepository
		hasher      auth.PasswordHasher
		expectError string
	}{
		{
			name:        "nil players repository",
			players:     nil,
			sessions:    mocks.NewMockWebSessionRepository(t),
			hasher:      mocks.NewMockPasswordHasher(t),
			expectError: "players repository is required",
		},
		{
			name:        "nil sessions repository",
			players:     mocks.NewMockPlayerRepository(t),
			sessions:    nil,
			hasher:      mocks.NewMockPasswordHasher(t),
			expectError: "sessions repository is required",
		},
		{
			name:        "nil password hasher",
			players:     mocks.NewMockPlayerRepository(t),
			sessions:    mocks.NewMockWebSessionRepository(t),
			hasher:      nil,
			expectError: "password hasher is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewAuthService(tt.players, tt.sessions, tt.hasher)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestNewAuthServiceWithLogger_NilLogger(t *testing.T) {
	players := mocks.NewMockPlayerRepository(t)
	sessions := mocks.NewMockWebSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)

	svc, err := auth.NewAuthServiceWithLogger(players, sessions, hasher, nil)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "logger")
}

func TestAuthService_Login(t *testing.T) {
	ctx := context.Background()

	t.Run("successful login creates session", func(t *testing.T) {
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
		hasher.On("NeedsUpgrade", player.PasswordHash).Return(false)
		playerRepo.On("Update", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)
		sessionRepo.On("Create", ctx, mock.AnythingOfType("*auth.WebSession")).Return(nil)

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.NoError(t, err)
		assert.NotNil(t, session)
		assert.NotEmpty(t, token)
		assert.Equal(t, playerID, session.PlayerID)
		assert.Len(t, token, 64) // 32 bytes hex-encoded
	})

	t.Run("login fails for non-existent user with constant time", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerRepo.On("GetByUsername", ctx, "unknown").Return(nil, auth.ErrNotFound)
		// Verify is still called with dummy hash to prevent timing attacks
		hasher.On("Verify", "password123", mock.AnythingOfType("string")).Return(false, nil)

		session, token, err := svc.Login(ctx, "unknown", "password123", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, session)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("login fails for wrong password", func(t *testing.T) {
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

		session, token, err := svc.Login(ctx, "testuser", "wrongpassword", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, session)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("login fails for locked out user after password verification", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerID := ulid.Make()
		lockedUntil := time.Now().Add(15 * time.Minute)
		player := &auth.Player{
			ID:             playerID,
			Username:       "testuser",
			PasswordHash:   "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
			FailedAttempts: 7,
			LockedUntil:    &lockedUntil,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		// Password is verified first to prevent timing attacks (lockout check comes after)
		hasher.On("Verify", "password123", player.PasswordHash).Return(true, nil)

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, session)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "AUTH_ACCOUNT_LOCKED")
	})

	t.Run("login increments failure count on wrong password", func(t *testing.T) {
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
			FailedAttempts: 2,
			LockedUntil:    nil,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		hasher.On("Verify", "wrongpassword", player.PasswordHash).Return(false, nil)
		playerRepo.On("Update", ctx, mock.MatchedBy(func(p *auth.Player) bool {
			return p.FailedAttempts == 3
		})).Return(nil)

		_, _, loginErr := svc.Login(ctx, "testuser", "wrongpassword", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, loginErr)
	})

	t.Run("login resets failure count on success", func(t *testing.T) {
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
			FailedAttempts: 3,
			LockedUntil:    nil,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		hasher.On("Verify", "password123", player.PasswordHash).Return(true, nil)
		hasher.On("NeedsUpgrade", player.PasswordHash).Return(false)
		playerRepo.On("Update", ctx, mock.MatchedBy(func(p *auth.Player) bool {
			return p.FailedAttempts == 0 && p.LockedUntil == nil
		})).Return(nil)
		sessionRepo.On("Create", ctx, mock.AnythingOfType("*auth.WebSession")).Return(nil)

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.NoError(t, err)
		assert.NotNil(t, session)
		assert.NotEmpty(t, token)
	})

	t.Run("login triggers lockout at threshold", func(t *testing.T) {
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
			FailedAttempts: 6, // One more failure triggers lockout at 7
			LockedUntil:    nil,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		hasher.On("Verify", "wrongpassword", player.PasswordHash).Return(false, nil)
		playerRepo.On("Update", ctx, mock.MatchedBy(func(p *auth.Player) bool {
			return p.FailedAttempts == 7 && p.LockedUntil != nil
		})).Return(nil)

		_, _, loginErr := svc.Login(ctx, "testuser", "wrongpassword", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, loginErr)
		errutil.AssertErrorCode(t, loginErr, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("upgrades password hash when needed", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerID := ulid.Make()
		oldHash := "$bcrypt$2a$10$oldHash"
		newHash := "$argon2id$v=19$m=65536,t=1,p=4$newHash"
		player := &auth.Player{
			ID:             playerID,
			Username:       "testuser",
			PasswordHash:   oldHash,
			FailedAttempts: 0,
			LockedUntil:    nil,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		hasher.On("Verify", "password123", oldHash).Return(true, nil)
		hasher.On("NeedsUpgrade", oldHash).Return(true)
		hasher.On("Hash", "password123").Return(newHash, nil)
		playerRepo.On("Update", ctx, mock.MatchedBy(func(p *auth.Player) bool {
			return p.PasswordHash == newHash && p.FailedAttempts == 0
		})).Return(nil)
		sessionRepo.On("Create", ctx, mock.AnythingOfType("*auth.WebSession")).Return(nil)

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.NoError(t, err)
		assert.NotNil(t, session)
		assert.NotEmpty(t, token)
	})

	t.Run("login succeeds even if password upgrade fails", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerID := ulid.Make()
		oldHash := "$bcrypt$2a$10$oldHash"
		player := &auth.Player{
			ID:             playerID,
			Username:       "testuser",
			PasswordHash:   oldHash,
			FailedAttempts: 0,
			LockedUntil:    nil,
		}

		playerRepo.On("GetByUsername", ctx, "testuser").Return(player, nil)
		hasher.On("Verify", "password123", oldHash).Return(true, nil)
		hasher.On("NeedsUpgrade", oldHash).Return(true)
		hasher.On("Hash", "password123").Return("", errors.New("hash failure"))
		// Hash should NOT be changed on upgrade failure
		playerRepo.On("Update", ctx, mock.MatchedBy(func(p *auth.Player) bool {
			return p.PasswordHash == oldHash && p.FailedAttempts == 0
		})).Return(nil)
		sessionRepo.On("Create", ctx, mock.AnythingOfType("*auth.WebSession")).Return(nil)

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.NoError(t, err)
		assert.NotNil(t, session)
		assert.NotEmpty(t, token)
	})

	t.Run("propagates player repository errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		playerRepo.On("GetByUsername", ctx, "testuser").Return(nil, errors.New("database error"))

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, session)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "AUTH_LOGIN_FAILED")
	})

	t.Run("propagates hasher verify errors", func(t *testing.T) {
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
		hasher.On("Verify", "password123", player.PasswordHash).Return(false, errors.New("hasher error"))

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, session)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "AUTH_LOGIN_FAILED")
	})

	t.Run("propagates session create errors", func(t *testing.T) {
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
		hasher.On("NeedsUpgrade", player.PasswordHash).Return(false)
		playerRepo.On("Update", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)
		sessionRepo.On("Create", ctx, mock.AnythingOfType("*auth.WebSession")).Return(errors.New("session error"))

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, session)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "AUTH_SESSION_CREATE_FAILED")
	})
}

func TestAuthService_Logout(t *testing.T) {
	ctx := context.Background()

	t.Run("successful logout deletes session", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		sessionID := ulid.Make()
		sessionRepo.On("Delete", ctx, sessionID).Return(nil)

		logoutErr := svc.Logout(ctx, sessionID)
		require.NoError(t, logoutErr)
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		sessionID := ulid.Make()
		sessionRepo.On("Delete", ctx, sessionID).Return(auth.ErrNotFound)

		logoutErr := svc.Logout(ctx, sessionID)
		require.Error(t, logoutErr)
		errutil.AssertErrorCode(t, logoutErr, "SESSION_NOT_FOUND")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		sessionID := ulid.Make()
		sessionRepo.On("Delete", ctx, sessionID).Return(errors.New("database error"))

		logoutErr := svc.Logout(ctx, sessionID)
		require.Error(t, logoutErr)
		errutil.AssertErrorCode(t, logoutErr, "AUTH_LOGOUT_FAILED")
	})
}

func TestAuthService_ValidateSession(t *testing.T) {
	ctx := context.Background()

	t.Run("validates active session", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		token, tokenHash, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		sessionID := ulid.Make()
		session := &auth.WebSession{
			ID:         sessionID,
			PlayerID:   playerID,
			TokenHash:  tokenHash,
			ExpiresAt:  time.Now().Add(24 * time.Hour),
			CreatedAt:  time.Now(),
			LastSeenAt: time.Now().Add(-time.Hour),
		}

		sessionRepo.On("GetByTokenHash", ctx, tokenHash).Return(session, nil)
		sessionRepo.On("UpdateLastSeen", ctx, sessionID, mock.AnythingOfType("time.Time")).Return(nil)

		result, err := svc.ValidateSession(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, session.ID, result.ID)
		assert.Equal(t, playerID, result.PlayerID)
	})

	t.Run("returns error for expired session", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		token, tokenHash, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		sessionID := ulid.Make()
		session := &auth.WebSession{
			ID:         sessionID,
			PlayerID:   playerID,
			TokenHash:  tokenHash,
			ExpiresAt:  time.Now().Add(-time.Hour), // Expired
			CreatedAt:  time.Now().Add(-25 * time.Hour),
			LastSeenAt: time.Now().Add(-2 * time.Hour),
		}

		sessionRepo.On("GetByTokenHash", ctx, tokenHash).Return(session, nil)

		result, err := svc.ValidateSession(ctx, token)
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "SESSION_EXPIRED")
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		token := "nonexistent0123456789abcdef0123456789abcdef0123456789abcdef01"

		sessionRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, auth.ErrNotFound)

		result, err := svc.ValidateSession(ctx, token)
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID")
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		result, err := svc.ValidateSession(ctx, "")
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "SESSION_TOKEN_EMPTY")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		token := "sometoken0123456789abcdef0123456789abcdef0123456789abcdef0123"

		sessionRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, errors.New("database error"))

		result, err := svc.ValidateSession(ctx, token)
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "SESSION_VALIDATE_FAILED")
	})

	t.Run("continues if last seen update fails", func(t *testing.T) {
		// Last seen update failure should not prevent validation from succeeding
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		token, tokenHash, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		playerID := ulid.Make()
		sessionID := ulid.Make()
		session := &auth.WebSession{
			ID:         sessionID,
			PlayerID:   playerID,
			TokenHash:  tokenHash,
			ExpiresAt:  time.Now().Add(24 * time.Hour),
			CreatedAt:  time.Now(),
			LastSeenAt: time.Now().Add(-time.Hour),
		}

		sessionRepo.On("GetByTokenHash", ctx, tokenHash).Return(session, nil)
		sessionRepo.On("UpdateLastSeen", ctx, sessionID, mock.AnythingOfType("time.Time")).Return(errors.New("update failed"))

		// Validation should still succeed
		result, err := svc.ValidateSession(ctx, token)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestAuthService_SelectCharacter(t *testing.T) {
	ctx := context.Background()

	t.Run("updates session character", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		sessionID := ulid.Make()
		characterID := ulid.Make()

		sessionRepo.On("UpdateCharacter", ctx, sessionID, characterID).Return(nil)

		selectErr := svc.SelectCharacter(ctx, sessionID, characterID)
		require.NoError(t, selectErr)
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		sessionID := ulid.Make()
		characterID := ulid.Make()

		sessionRepo.On("UpdateCharacter", ctx, sessionID, characterID).Return(auth.ErrNotFound)

		selectErr := svc.SelectCharacter(ctx, sessionID, characterID)
		require.Error(t, selectErr)
		errutil.AssertErrorCode(t, selectErr, "SESSION_NOT_FOUND")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		sessionID := ulid.Make()
		characterID := ulid.Make()

		sessionRepo.On("UpdateCharacter", ctx, sessionID, characterID).Return(errors.New("database error"))

		selectErr := svc.SelectCharacter(ctx, sessionID, characterID)
		require.Error(t, selectErr)
		errutil.AssertErrorCode(t, selectErr, "SESSION_SELECT_CHAR_FAILED")
	})
}

// TestService_SessionStateTransitions tests the state machine for session state changes:
// anonymous -> authenticated (login) -> character-selected (SelectCharacter)
func TestService_SessionStateTransitions(t *testing.T) {
	ctx := context.Background()

	t.Run("SelectCharacter succeeds on session with no character", func(t *testing.T) {
		// This tests the transition: authenticated -> character-selected
		// When a session has no character yet (CharacterID is nil), SelectCharacter should succeed
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		sessionID := ulid.Make()
		characterID := ulid.Make()

		// The repository's UpdateCharacter handles setting the character on a session
		// regardless of whether it previously had one
		sessionRepo.On("UpdateCharacter", ctx, sessionID, characterID).Return(nil)

		selectErr := svc.SelectCharacter(ctx, sessionID, characterID)
		require.NoError(t, selectErr)
		sessionRepo.AssertExpectations(t)
	})

	t.Run("SelectCharacter updates existing character selection", func(t *testing.T) {
		// This tests replacing an existing character with a different one
		// A player may switch between multiple characters they own
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		sessionID := ulid.Make()
		newCharacterID := ulid.Make()

		// UpdateCharacter replaces any existing character selection
		sessionRepo.On("UpdateCharacter", ctx, sessionID, newCharacterID).Return(nil)

		selectErr := svc.SelectCharacter(ctx, sessionID, newCharacterID)
		require.NoError(t, selectErr)
		sessionRepo.AssertExpectations(t)
	})

	t.Run("full state transition flow: login then select character", func(t *testing.T) {
		// This tests the complete flow:
		// 1. Login creates session WITHOUT character (CharacterID is nil)
		// 2. SelectCharacter adds character to the session
		playerRepo := mocks.NewMockPlayerRepository(t)
		sessionRepo := mocks.NewMockWebSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, sessionRepo, hasher)
		require.NoError(t, err)

		// Step 1: Login
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
		hasher.On("NeedsUpgrade", player.PasswordHash).Return(false)
		playerRepo.On("Update", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)

		var createdSession *auth.WebSession
		sessionRepo.On("Create", ctx, mock.AnythingOfType("*auth.WebSession")).Run(func(args mock.Arguments) {
			createdSession = args.Get(1).(*auth.WebSession)
		}).Return(nil)

		session, token, loginErr := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.NoError(t, loginErr)
		assert.NotNil(t, session)
		assert.NotEmpty(t, token)

		// Verify session was created without a character (nil CharacterID)
		assert.Nil(t, createdSession.CharacterID, "session should be created without character")
		assert.Equal(t, playerID, createdSession.PlayerID, "session should have correct player ID")

		// Step 2: Select character
		characterID := ulid.Make()
		sessionRepo.On("UpdateCharacter", ctx, session.ID, characterID).Return(nil)

		selectErr := svc.SelectCharacter(ctx, session.ID, characterID)
		require.NoError(t, selectErr)

		sessionRepo.AssertExpectations(t)
		playerRepo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})
}
