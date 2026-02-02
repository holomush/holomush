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
	"github.com/holomush/holomush/pkg/errutil"
)

// mockWebSessionRepository is a mock for auth.WebSessionRepository.
type mockWebSessionRepository struct {
	mock.Mock
}

func (m *mockWebSessionRepository) Create(ctx context.Context, session *auth.WebSession) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *mockWebSessionRepository) GetByID(ctx context.Context, id ulid.ULID) (*auth.WebSession, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.WebSession), args.Error(1)
}

func (m *mockWebSessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*auth.WebSession, error) {
	args := m.Called(ctx, tokenHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.WebSession), args.Error(1)
}

func (m *mockWebSessionRepository) GetByPlayer(ctx context.Context, playerID ulid.ULID) ([]*auth.WebSession, error) {
	args := m.Called(ctx, playerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*auth.WebSession), args.Error(1)
}

func (m *mockWebSessionRepository) UpdateLastSeen(ctx context.Context, id ulid.ULID, lastSeen time.Time) error {
	args := m.Called(ctx, id, lastSeen)
	return args.Error(0)
}

func (m *mockWebSessionRepository) UpdateCharacter(ctx context.Context, id, characterID ulid.ULID) error {
	args := m.Called(ctx, id, characterID)
	return args.Error(0)
}

func (m *mockWebSessionRepository) Delete(ctx context.Context, id ulid.ULID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockWebSessionRepository) DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error {
	args := m.Called(ctx, playerID)
	return args.Error(0)
}

func (m *mockWebSessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func TestAuthService_Login(t *testing.T) {
	ctx := context.Background()

	t.Run("successful login creates session", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		playerRepo.AssertExpectations(t)
		sessionRepo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})

	t.Run("login fails for non-existent user with constant time", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		playerRepo.On("GetByUsername", ctx, "unknown").Return(nil, auth.ErrNotFound)
		// Verify is still called with dummy hash to prevent timing attacks
		hasher.On("Verify", "password123", mock.AnythingOfType("string")).Return(false, nil)

		session, token, err := svc.Login(ctx, "unknown", "password123", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, session)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_CREDENTIALS")

		playerRepo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})

	t.Run("login fails for wrong password", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		playerRepo.AssertExpectations(t)
	})

	t.Run("login fails for locked out user after password verification", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		playerRepo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})

	t.Run("login increments failure count on wrong password", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		_, _, err := svc.Login(ctx, "testuser", "wrongpassword", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)

		playerRepo.AssertExpectations(t)
	})

	t.Run("login resets failure count on success", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		playerRepo.AssertExpectations(t)
	})

	t.Run("login triggers lockout at threshold", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		_, _, err := svc.Login(ctx, "testuser", "wrongpassword", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_CREDENTIALS")

		playerRepo.AssertExpectations(t)
	})

	t.Run("upgrades password hash when needed", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		playerRepo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})

	t.Run("login succeeds even if password upgrade fails", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		playerRepo.AssertExpectations(t)
		hasher.AssertExpectations(t)
	})

	t.Run("propagates player repository errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		playerRepo.On("GetByUsername", ctx, "testuser").Return(nil, errors.New("database error"))

		session, token, err := svc.Login(ctx, "testuser", "password123", "Mozilla/5.0", "192.168.1.1")
		require.Error(t, err)
		assert.Nil(t, session)
		assert.Empty(t, token)
		errutil.AssertErrorCode(t, err, "AUTH_LOGIN_FAILED")
	})

	t.Run("propagates hasher verify errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		sessionID := ulid.Make()
		sessionRepo.On("Delete", ctx, sessionID).Return(nil)

		err := svc.Logout(ctx, sessionID)
		require.NoError(t, err)

		sessionRepo.AssertExpectations(t)
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		sessionID := ulid.Make()
		sessionRepo.On("Delete", ctx, sessionID).Return(auth.ErrNotFound)

		err := svc.Logout(ctx, sessionID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		sessionID := ulid.Make()
		sessionRepo.On("Delete", ctx, sessionID).Return(errors.New("database error"))

		err := svc.Logout(ctx, sessionID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_LOGOUT_FAILED")
	})
}

func TestAuthService_ValidateSession(t *testing.T) {
	ctx := context.Background()

	t.Run("validates active session", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		sessionRepo.AssertExpectations(t)
	})

	t.Run("returns error for expired session", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		token := "nonexistent0123456789abcdef0123456789abcdef0123456789abcdef01"

		sessionRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, auth.ErrNotFound)

		result, err := svc.ValidateSession(ctx, token)
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID")
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		result, err := svc.ValidateSession(ctx, "")
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "SESSION_TOKEN_EMPTY")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		token := "sometoken0123456789abcdef0123456789abcdef0123456789abcdef0123"

		sessionRepo.On("GetByTokenHash", ctx, mock.AnythingOfType("string")).Return(nil, errors.New("database error"))

		result, err := svc.ValidateSession(ctx, token)
		require.Error(t, err)
		assert.Nil(t, result)
		errutil.AssertErrorCode(t, err, "SESSION_VALIDATE_FAILED")
	})

	t.Run("continues if last seen update fails", func(t *testing.T) {
		// Last seen update failure should not prevent validation from succeeding
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

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

		sessionRepo.AssertExpectations(t)
	})
}

func TestAuthService_SelectCharacter(t *testing.T) {
	ctx := context.Background()

	t.Run("updates session character", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		sessionID := ulid.Make()
		characterID := ulid.Make()

		sessionRepo.On("UpdateCharacter", ctx, sessionID, characterID).Return(nil)

		err := svc.SelectCharacter(ctx, sessionID, characterID)
		require.NoError(t, err)

		sessionRepo.AssertExpectations(t)
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		sessionID := ulid.Make()
		characterID := ulid.Make()

		sessionRepo.On("UpdateCharacter", ctx, sessionID, characterID).Return(auth.ErrNotFound)

		err := svc.SelectCharacter(ctx, sessionID, characterID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		playerRepo := new(mockPlayerRepository)
		sessionRepo := new(mockWebSessionRepository)
		hasher := new(mockPasswordHasher)
		svc := auth.NewAuthService(playerRepo, sessionRepo, hasher)

		sessionID := ulid.Make()
		characterID := ulid.Make()

		sessionRepo.On("UpdateCharacter", ctx, sessionID, characterID).Return(errors.New("database error"))

		err := svc.SelectCharacter(ctx, sessionID, characterID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_SELECT_CHAR_FAILED")
	})
}
