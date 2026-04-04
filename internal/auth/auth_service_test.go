// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestNewAuthService_NilDependencies(t *testing.T) {
	tests := []struct {
		name           string
		players        auth.PlayerRepository
		playerSessions auth.PlayerSessionRepository
		hasher         auth.PasswordHasher
		expectError    string
	}{
		{
			name:           "nil players repository",
			players:        nil,
			playerSessions: mocks.NewMockPlayerSessionRepository(t),
			hasher:         mocks.NewMockPasswordHasher(t),
			expectError:    "players repository is required",
		},
		{
			name:           "nil player sessions repository",
			players:        mocks.NewMockPlayerRepository(t),
			playerSessions: nil,
			hasher:         mocks.NewMockPasswordHasher(t),
			expectError:    "player sessions repository is required",
		},
		{
			name:           "nil password hasher",
			players:        mocks.NewMockPlayerRepository(t),
			playerSessions: mocks.NewMockPlayerSessionRepository(t),
			hasher:         nil,
			expectError:    "password hasher is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewAuthService(tt.players, tt.playerSessions, tt.hasher)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestNewAuthServiceWithLoggerRejectsNilLogger(t *testing.T) {
	players := mocks.NewMockPlayerRepository(t)
	playerSessions := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)

	svc, err := auth.NewAuthServiceWithLogger(players, playerSessions, hasher, nil)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "logger")
}

func TestAuthService_Logout(t *testing.T) {
	ctx := context.Background()

	t.Run("successful logout deletes session and returns player ID", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		playerSessionRepo := mocks.NewMockPlayerSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, playerSessionRepo, hasher)
		require.NoError(t, err)

		playerID := ulid.Make()
		sessionID := ulid.Make()
		tokenHash := "somehash"
		session := &auth.PlayerSession{
			ID:       sessionID,
			PlayerID: playerID,
		}

		playerSessionRepo.On("GetByTokenHash", ctx, tokenHash).Return(session, nil)
		playerSessionRepo.On("Delete", ctx, sessionID).Return(nil)

		returnedPlayerID, logoutErr := svc.Logout(ctx, tokenHash)
		require.NoError(t, logoutErr)
		assert.Equal(t, playerID, returnedPlayerID)
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		playerSessionRepo := mocks.NewMockPlayerSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, playerSessionRepo, hasher)
		require.NoError(t, err)

		tokenHash := "nonexistenthash"
		playerSessionRepo.On("GetByTokenHash", ctx, tokenHash).Return(nil, auth.ErrNotFound)

		_, logoutErr := svc.Logout(ctx, tokenHash)
		require.Error(t, logoutErr)
		errutil.AssertErrorCode(t, logoutErr, "SESSION_NOT_FOUND")
	})

	t.Run("propagates get repository errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		playerSessionRepo := mocks.NewMockPlayerSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, playerSessionRepo, hasher)
		require.NoError(t, err)

		tokenHash := "somehash"
		playerSessionRepo.On("GetByTokenHash", ctx, tokenHash).Return(nil, errors.New("database error"))

		_, logoutErr := svc.Logout(ctx, tokenHash)
		require.Error(t, logoutErr)
		errutil.AssertErrorCode(t, logoutErr, "AUTH_LOGOUT_FAILED")
	})

	t.Run("propagates delete repository errors", func(t *testing.T) {
		playerRepo := mocks.NewMockPlayerRepository(t)
		playerSessionRepo := mocks.NewMockPlayerSessionRepository(t)
		hasher := mocks.NewMockPasswordHasher(t)
		svc, err := auth.NewAuthService(playerRepo, playerSessionRepo, hasher)
		require.NoError(t, err)

		playerID := ulid.Make()
		sessionID := ulid.Make()
		tokenHash := "somehash"
		session := &auth.PlayerSession{
			ID:       sessionID,
			PlayerID: playerID,
		}

		playerSessionRepo.On("GetByTokenHash", ctx, tokenHash).Return(session, nil)
		playerSessionRepo.On("Delete", ctx, sessionID).Return(errors.New("database error"))

		_, logoutErr := svc.Logout(ctx, tokenHash)
		require.Error(t, logoutErr)
		errutil.AssertErrorCode(t, logoutErr, "AUTH_LOGOUT_FAILED")
	})
}
