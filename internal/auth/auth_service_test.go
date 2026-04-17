// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

// newTestAuthServiceWithCap builds an auth.Service configured with the given
// per-player session cap (<= 0 disables). Returns the service plus the
// underlying mocks so tests can script expectations.
func newTestAuthServiceWithCap(t *testing.T, maxSessions int) (*auth.Service, *mocks.MockPlayerRepository, *mocks.MockPlayerSessionRepository, *mocks.MockPasswordHasher) {
	t.Helper()
	playerRepo := mocks.NewMockPlayerRepository(t)
	playerSessionRepo := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	svc, err := auth.NewAuthService(playerRepo, playerSessionRepo, hasher)
	require.NoError(t, err)
	svc.SetMaxSessionsPerPlayer(maxSessions)
	return svc, playerRepo, playerSessionRepo, hasher
}

// testPlayerWithCredentials returns a fake player and scripts the mocks to
// accept the given username/password pair against the stored hash.
func testPlayerWithCredentials(t *testing.T, playerRepo *mocks.MockPlayerRepository, hasher *mocks.MockPasswordHasher, username string) *auth.Player {
	t.Helper()
	player := &auth.Player{
		ID:             ulid.Make(),
		Username:       username,
		PasswordHash:   "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
		FailedAttempts: 0,
		LockedUntil:    nil,
	}
	// Every AuthenticatePlayer call triggers GetByUsername -> Verify -> Update.
	playerRepo.On("GetByUsername", mock.Anything, username).Return(player, nil)
	hasher.On("Verify", "password", player.PasswordHash).Return(true, nil)
	playerRepo.On("Update", mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil)
	return player
}

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

func TestAuthenticatePlayerCallsCreateWithCapWhenCapExceeded(t *testing.T) {
	ctx := context.Background()
	const capN = 3

	svc, playerRepo, sessionRepo, hasher := newTestAuthServiceWithCap(t, capN)
	player := testPlayerWithCredentials(t, playerRepo, hasher, "alice")

	// Repository receives configured cap and atomically trims. Return trimmed > 0
	// to simulate the player having been over the cap prior to this call.
	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
		Return(1, nil).Once()

	tok, gotPlayer, err := svc.AuthenticatePlayer(ctx, "alice", "password", "ua", "ip")
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
	require.NotNil(t, gotPlayer)
	assert.Equal(t, player.ID, gotPlayer.ID)
}

func TestAuthenticatePlayerDoesNotTrimWhenBelowCap(t *testing.T) {
	ctx := context.Background()
	const capN = 5

	svc, playerRepo, sessionRepo, hasher := newTestAuthServiceWithCap(t, capN)
	player := testPlayerWithCredentials(t, playerRepo, hasher, "bob")

	// Below the cap: CreateWithCap returns trimmed=0 (nothing to trim).
	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
		Return(0, nil).Once()

	tok, gotPlayer, err := svc.AuthenticatePlayer(ctx, "bob", "password", "ua", "ip")
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
	require.NotNil(t, gotPlayer)
	assert.Equal(t, player.ID, gotPlayer.ID)
}

func TestAuthenticatePlayerPassesDisabledCapToRepository(t *testing.T) {
	ctx := context.Background()
	const capDisabled = 0 // <= 0 disables enforcement

	svc, playerRepo, sessionRepo, hasher := newTestAuthServiceWithCap(t, capDisabled)
	testPlayerWithCredentials(t, playerRepo, hasher, "carol")

	// When cap is disabled, the service still routes through CreateWithCap
	// (single code path) and the repository's cap <= 0 branch skips trimming.
	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capDisabled).
		Return(0, nil)

	// Authenticate many times; none should trigger trimming.
	for i := 0; i < 10; i++ {
		tok, gotPlayer, err := svc.AuthenticatePlayer(ctx, "carol", "password", "ua", "ip")
		require.NoError(t, err)
		assert.NotEmpty(t, tok)
		require.NotNil(t, gotPlayer)
		assert.Equal(t, "carol", gotPlayer.Username)
	}
}

func TestAuthenticatePlayerPropagatesCreateWithCapError(t *testing.T) {
	ctx := context.Background()
	const capN = 3

	svc, playerRepo, sessionRepo, hasher := newTestAuthServiceWithCap(t, capN)
	testPlayerWithCredentials(t, playerRepo, hasher, "alice")

	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
		Return(0, errors.New("db down")).Once()

	tok, gotPlayer, err := svc.AuthenticatePlayer(ctx, "alice", "password", "ua", "ip")
	require.Error(t, err)
	assert.Empty(t, tok)
	assert.Nil(t, gotPlayer)
	errutil.AssertErrorCode(t, err, "AUTH_LOGIN_FAILED")
}

func TestAuthenticatePlayerReturnsErrorOnInvalidCredentials(t *testing.T) {
	ctx := context.Background()

	svc, playerRepo, sessionRepo, hasher := newTestAuthServiceWithCap(t, 0)

	playerRepo.On("GetByUsername", ctx, "ghost").Return(nil, auth.ErrNotFound)
	hasher.On("Verify", "password", mock.AnythingOfType("string")).Return(false, nil)

	tok, gotPlayer, err := svc.AuthenticatePlayer(ctx, "ghost", "password", "ua", "ip")
	require.Error(t, err)
	assert.Empty(t, tok)
	assert.Nil(t, gotPlayer)
	errutil.AssertErrorCode(t, err, "AUTH_INVALID_CREDENTIALS")

	// Session repo never consulted on invalid credentials.
	_ = sessionRepo // mockery strict mode will fail if it sees unexpected calls
}
