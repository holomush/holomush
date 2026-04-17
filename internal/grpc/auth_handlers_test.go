// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	samberOops "github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// makePlayerSession builds an unexpired PlayerSession for use in tests.
func makePlayerSession(playerID ulid.ULID) *auth.PlayerSession {
	return &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: auth.HashSessionToken(validToken),
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// validToken is the standard test token used by setupSessionRepo.
const validToken = "valid-token"

// setupSessionRepo creates a MockPlayerSessionRepository that expects
// GetByTokenHash for validToken and returns the given session.
// It also sets up a best-effort RefreshTTL expectation.
func setupSessionRepo(t *testing.T, ps *auth.PlayerSession) *authmocks.MockPlayerSessionRepository {
	t.Helper()
	repo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken(validToken)
	repo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).Return(ps, nil)
	repo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil)
	return repo
}

// --- AuthenticatePlayer ---

func TestAuthenticatePlayer_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()

	authSvc := newMockAuthService(t)
	authSvc.authenticatePlayerFunc = func(_ context.Context, username, password, _, _ string) (string, *auth.Player, error) {
		require.Equal(t, "alice", username)
		require.Equal(t, "password123", password)
		return "raw-token", &auth.Player{
			ID:                 playerID,
			Username:           "alice",
			DefaultCharacterID: &charID,
		}, nil
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Alice", LocationID: &locID},
		}, nil)

	sessionStore := session.NewMemStore()

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore:      sessionStore,
		authService:       authSvc,
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
	}

	resp, err := server.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
		Username: "alice",
		Password: "password123",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, "raw-token", resp.PlayerSessionToken)
	assert.Len(t, resp.Characters, 1)
	assert.Equal(t, charID.String(), resp.Characters[0].CharacterId)
	assert.Equal(t, "Alice", resp.Characters[0].CharacterName)
	assert.Equal(t, charID.String(), resp.DefaultCharacterId)
}

func TestAuthenticatePlayer_InvalidCredentials(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)
	authSvc.authenticatePlayerFunc = func(_ context.Context, _, _, _, _ string) (string, *auth.Player, error) {
		return "", nil, auth.ErrNotFound
	}

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore:      session.NewMemStore(),
		authService:       authSvc,
		playerSessionRepo: authmocks.NewMockPlayerSessionRepository(t),
	}

	resp, err := server.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
		Username: "baduser",
		Password: "badpass",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.ErrorMessage)
}

func TestAuthenticatePlayer_ServiceNotConfigured(t *testing.T) {
	ctx := context.Background()

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
		// authService is nil
	}

	resp, err := server.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
		Username: "alice",
		Password: "password123",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "not configured")
}

func TestAuthenticatePlayer_SessionRepoNotConfigured(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
		authService:  authSvc,
		// playerSessionRepo is nil
	}

	resp, err := server.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
		Username: "alice",
		Password: "password123",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "not configured")
}

// --- SelectCharacter ---

func TestSelectCharacter_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()
	sessionID := core.NewULID()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Alice", LocationID: &locID},
		}, nil)

	sessionStore := session.NewMemStore()

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      sessionStore,
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		newSessionID:      func() ulid.ULID { return sessionID },
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, sessionID.String(), resp.SessionId)
	assert.Equal(t, "Alice", resp.CharacterName)
	assert.False(t, resp.Reattached)
}

func TestSelectCharacter_InvalidSession(t *testing.T) {
	ctx := context.Background()

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken("bad-token")
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
		Return(nil, auth.ErrNotFound)

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: "bad-token",
		CharacterId:        ulid.Make().String(),
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "invalid or expired")
}

func TestSelectCharacter_InvalidCharacter(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			// Character with a different ID — the requested one won't match.
			{ID: ulid.Make(), PlayerID: playerID, Name: "Other"},
		}, nil)

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "does not belong")
}

func TestSelectCharacter_Reattach(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()
	existingSessionID := core.NewULID().String()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Alice", LocationID: &locID},
		}, nil)

	sessionStore := session.NewMemStore()
	// Pre-populate a detached session for this character.
	require.NoError(t, sessionStore.Set(ctx, existingSessionID, &session.Info{
		ID:            existingSessionID,
		CharacterID:   charID,
		CharacterName: "Alice",
		LocationID:    locID,
		Status:        session.StatusDetached,
		EventCursors:  map[string]ulid.ULID{},
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}))

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      sessionStore,
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		newSessionID:      func() ulid.ULID { return core.NewULID() },
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, existingSessionID, resp.SessionId)
	assert.True(t, resp.Reattached)
}

func TestSelectCharacter_SameTokenTwice(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()
	sessionID1 := core.NewULID()
	sessionID2 := core.NewULID()

	ps := makePlayerSession(playerID)
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken(validToken)
	// Two calls with the same token both succeed.
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).Return(ps, nil).Times(2)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil).Times(2)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Alice", LocationID: &locID},
		}, nil).Times(2)

	sessionStore := session.NewMemStore()
	callCount := 0
	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      sessionStore,
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		newSessionID: func() ulid.ULID {
			callCount++
			if callCount == 1 {
				return sessionID1
			}
			return sessionID2
		},
	}

	// First call creates a new session.
	resp1, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.NoError(t, err)
	assert.True(t, resp1.Success)
	assert.Equal(t, sessionID1.String(), resp1.SessionId)

	// Second call reattaches to the session created by the first call.
	resp2, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.NoError(t, err)
	assert.True(t, resp2.Success)
	assert.True(t, resp2.Reattached)
}

// --- CreatePlayer ---

func TestCreatePlayer_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	sessionID := ulid.Make()

	authSvc := newMockAuthService(t)
	authSvc.createPlayerFunc = func(_ context.Context, username, password, email string) (*auth.Player, *auth.PlayerSession, string, error) {
		require.Equal(t, "newuser", username)
		require.Equal(t, "strongpass1", password)
		require.Equal(t, "new@example.com", email)
		return &auth.Player{ID: playerID, Username: "newuser"},
			&auth.PlayerSession{ID: sessionID, PlayerID: playerID},
			"raw-session-token",
			nil
	}

	playerSessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	playerSessionRepo.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.PlayerSession")).Return(nil)

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore:      session.NewMemStore(),
		authService:       authSvc,
		playerSessionRepo: playerSessionRepo,
	}

	resp, err := server.CreatePlayer(ctx, &corev1.CreatePlayerRequest{
		Username: "newuser",
		Password: "strongpass1",
		Email:    "new@example.com",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, "raw-session-token", resp.PlayerSessionToken)
	assert.Empty(t, resp.Characters)
}

func TestCreatePlayer_UsernameTaken(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)
	authSvc.createPlayerFunc = func(_ context.Context, _, _, _ string) (*auth.Player, *auth.PlayerSession, string, error) {
		return nil, nil, "", auth.ErrNotFound // simulates username taken via oops
	}

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
		authService:  authSvc,
	}

	resp, err := server.CreatePlayer(ctx, &corev1.CreatePlayerRequest{
		Username: "taken",
		Password: "strongpass1",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.ErrorMessage)
}

func TestCreatePlayer_ServiceNotConfigured(t *testing.T) {
	ctx := context.Background()

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
	}

	resp, err := server.CreatePlayer(ctx, &corev1.CreatePlayerRequest{
		Username: "alice",
		Password: "password123",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "not configured")
}

// --- CreateCharacter ---

func TestCreateCharacter_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charSvc := newMockCharacterService(t)
	charSvc.createFunc = func(_ context.Context, pid ulid.ULID, name string) (*world.Character, error) {
		require.Equal(t, playerID, pid)
		require.Equal(t, "New Hero", name)
		return &world.Character{ID: charID, PlayerID: pid, Name: "New Hero"}, nil
	}

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
		characterService:  charSvc,
	}

	resp, err := server.CreateCharacter(ctx, &corev1.CreateCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterName:      "New Hero",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, charID.String(), resp.CharacterId)
	assert.Equal(t, "New Hero", resp.CharacterName)
}

func TestCreateCharacter_NotConfigured(t *testing.T) {
	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),
		// playerSessionRepo is nil — resolvePlayerSession returns error
	}
	resp, err := server.CreateCharacter(context.Background(), &corev1.CreateCharacterRequest{
		PlayerSessionToken: "some-token",
		CharacterName:      "Hero",
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "invalid or expired")
}

func TestCreateCharacter_InvalidSession(t *testing.T) {
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken("bad-token")
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
		Return(nil, auth.ErrNotFound)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		playerSessionRepo: sessionRepo,
		characterService:  newMockCharacterService(t),
	}
	resp, err := server.CreateCharacter(context.Background(), &corev1.CreateCharacterRequest{
		PlayerSessionToken: "bad-token",
		CharacterName:      "Hero",
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "invalid or expired")
}

// --- ListCharacters ---

func TestListCharacters_InvalidSession_ReturnsError(t *testing.T) {
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken("bad-token")
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
		Return(nil, auth.ErrNotFound)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		playerSessionRepo: sessionRepo,
	}
	_, err := server.ListCharacters(context.Background(), &corev1.ListCharactersRequest{
		PlayerSessionToken: "bad-token",
	})
	assert.Error(t, err, "ListCharacters should return error for invalid session")
}

func TestListCharacters_NotConfigured_ReturnsError(t *testing.T) {
	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),
		// playerSessionRepo is nil
	}
	_, err := server.ListCharacters(context.Background(), &corev1.ListCharactersRequest{
		PlayerSessionToken: "some-token",
	})
	assert.Error(t, err, "ListCharacters should return error when session repo not configured")
}

func TestListCharacters_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Alice", LocationID: &locID},
		}, nil)

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
	}

	resp, err := server.ListCharacters(ctx, &corev1.ListCharactersRequest{
		PlayerSessionToken: validToken,
	})
	require.NoError(t, err)

	require.Len(t, resp.Characters, 1)
	assert.Equal(t, charID.String(), resp.Characters[0].CharacterId)
	assert.Equal(t, "Alice", resp.Characters[0].CharacterName)
	assert.Empty(t, resp.Characters[0].LastLocation, "no worldQuerier = no location name, never expose raw IDs")
}

func TestListCharacters_ResolvesLocationName(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Bob", LocationID: &locID},
		}, nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		worldQuerier: &mockWorldQuerier{
			location: &world.Location{ID: locID, Name: "The Nexus"},
		},
	}

	resp, err := server.ListCharacters(ctx, &corev1.ListCharactersRequest{
		PlayerSessionToken: validToken,
	})
	require.NoError(t, err)

	require.Len(t, resp.Characters, 1)
	assert.Equal(t, "The Nexus", resp.Characters[0].LastLocation, "should resolve location ID to name")
}

func TestListCharacters_LocationLookupFailure_OmitsLocation(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Carol", LocationID: &locID},
		}, nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		worldQuerier: &mockWorldQuerier{
			locErr: errors.New("db connection failed"),
		},
	}

	resp, err := server.ListCharacters(ctx, &corev1.ListCharactersRequest{
		PlayerSessionToken: validToken,
	})
	require.NoError(t, err)

	require.Len(t, resp.Characters, 1)
	assert.Empty(t, resp.Characters[0].LastLocation, "should not expose ULID when lookup fails")
}

// --- RequestPasswordReset ---

func TestRequestPasswordReset_AlwaysSuccess(t *testing.T) {
	ctx := context.Background()

	resetSvc := newMockResetService(t)
	resetSvc.requestResetFunc = func(_ context.Context, _ string) (string, error) {
		return "reset-token-123", nil
	}

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
		resetService: resetSvc,
	}

	resp, err := server.RequestPasswordReset(ctx, &corev1.RequestPasswordResetRequest{
		Email: "alice@example.com",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
}

func TestRequestPasswordReset_NotConfigured(t *testing.T) {
	ctx := context.Background()

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
	}

	resp, err := server.RequestPasswordReset(ctx, &corev1.RequestPasswordResetRequest{
		Email: "alice@example.com",
	})
	require.NoError(t, err)

	// Always returns success to prevent enumeration, even if not configured.
	assert.True(t, resp.Success)
}

// --- ConfirmPasswordReset ---

func TestConfirmPasswordReset_Success(t *testing.T) {
	ctx := context.Background()

	resetSvc := newMockResetService(t)
	resetSvc.resetPasswordFunc = func(_ context.Context, token, newPassword string) error {
		require.Equal(t, "reset-token-123", token)
		require.Equal(t, "newstrongpass", newPassword)
		return nil
	}

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
		resetService: resetSvc,
	}

	resp, err := server.ConfirmPasswordReset(ctx, &corev1.ConfirmPasswordResetRequest{
		Token:       "reset-token-123",
		NewPassword: "newstrongpass",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
}

func TestConfirmPasswordReset_InvalidToken(t *testing.T) {
	ctx := context.Background()

	resetSvc := newMockResetService(t)
	resetSvc.resetPasswordFunc = func(_ context.Context, _, _ string) error {
		return auth.ErrNotFound
	}

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
		resetService: resetSvc,
	}

	resp, err := server.ConfirmPasswordReset(ctx, &corev1.ConfirmPasswordResetRequest{
		Token:       "bad-token",
		NewPassword: "newstrongpass",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.NotEmpty(t, resp.ErrorMessage)
}

// --- Logout ---

func TestLogout_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	rawToken := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	expectedHash := auth.HashSessionToken(rawToken)

	authSvc := newMockAuthService(t)
	authSvc.logoutFunc = func(_ context.Context, tokenHash string) (ulid.ULID, error) {
		require.Equal(t, expectedHash, tokenHash)
		return playerID, nil
	}

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
		authService:  authSvc,
	}

	resp, err := server.Logout(ctx, &corev1.LogoutRequest{
		PlayerSessionToken: rawToken,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestLogout_SessionNotFound(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)
	authSvc.logoutFunc = func(_ context.Context, _ string) (ulid.ULID, error) {
		return ulid.ULID{}, auth.ErrNotFound
	}

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
		authService:  authSvc,
	}

	_, err := server.Logout(ctx, &corev1.LogoutRequest{
		PlayerSessionToken: "some-token",
	})
	assert.Error(t, err)
}

func TestLogout_NotConfigured(t *testing.T) {
	ctx := context.Background()

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: session.NewMemStore(),
	}

	_, err := server.Logout(ctx, &corev1.LogoutRequest{
		PlayerSessionToken: "some-token",
	})
	assert.Error(t, err)
}

// --- resolvePlayerSession ---

func TestResolvePlayerSession_RepoNotConfigured(t *testing.T) {
	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore()),
		sessionStore: session.NewMemStore(),
		// playerSessionRepo is nil
	}

	ps, err := server.resolvePlayerSession(context.Background(), "some-token")
	assert.Nil(t, ps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestResolvePlayerSession_TokenNotFound(t *testing.T) {
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken("unknown-token")
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
		Return(nil, auth.ErrNotFound)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	ps, err := server.resolvePlayerSession(context.Background(), "unknown-token")
	assert.Nil(t, ps)
	require.Error(t, err)
}

func TestResolvePlayerSession_RefreshTTLError_StillReturnsSession(t *testing.T) {
	playerID := ulid.Make()
	ps := makePlayerSession(playerID)

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	tokenHash := auth.HashSessionToken(validToken)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).Return(ps, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).
		Return(errors.New("redis timeout"))

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	got, err := server.resolvePlayerSession(context.Background(), validToken)
	require.NoError(t, err)
	assert.Equal(t, ps.ID, got.ID)
	assert.Equal(t, playerID, got.PlayerID)
}

// --- CheckPlayerSession tests ---

func TestCheckPlayerSession(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T) (*CoreServer, *corev1.CheckPlayerSessionRequest)
		expectErr  bool
		errContain string
		expectName string
	}{
		{
			name: "valid session returns player name",
			setup: func(t *testing.T) (*CoreServer, *corev1.CheckPlayerSessionRequest) {
				playerID := ulid.Make()
				ps := makePlayerSession(playerID)
				sessionRepo := setupSessionRepo(t, ps)
				playerRepo := authmocks.NewMockPlayerRepository(t)
				playerRepo.EXPECT().GetByID(mock.Anything, playerID).
					Return(&auth.Player{ID: playerID, Username: "alice"}, nil)
				server := &CoreServer{
					engine:            core.NewEngine(core.NewMemoryEventStore()),
					sessionStore:      session.NewMemStore(),
					playerSessionRepo: sessionRepo,
					playerRepo:        playerRepo,
				}
				return server, &corev1.CheckPlayerSessionRequest{PlayerSessionToken: validToken}
			},
			expectName: "alice",
		},
		{
			name: "invalid token returns error",
			setup: func(t *testing.T) (*CoreServer, *corev1.CheckPlayerSessionRequest) {
				sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
				tokenHash := auth.HashSessionToken("bad-token")
				sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
					Return(nil, auth.ErrNotFound)
				server := &CoreServer{
					engine:            core.NewEngine(core.NewMemoryEventStore()),
					sessionStore:      session.NewMemStore(),
					playerSessionRepo: sessionRepo,
				}
				return server, &corev1.CheckPlayerSessionRequest{PlayerSessionToken: "bad-token"}
			},
			expectErr: true,
		},
		{
			name: "session repo not configured",
			setup: func(_ *testing.T) (*CoreServer, *corev1.CheckPlayerSessionRequest) {
				server := &CoreServer{
					engine:       core.NewEngine(core.NewMemoryEventStore()),
					sessionStore: session.NewMemStore(),
				}
				return server, &corev1.CheckPlayerSessionRequest{PlayerSessionToken: validToken}
			},
			expectErr:  true,
			errContain: "not configured",
		},
		{
			name: "player repo not configured",
			setup: func(t *testing.T) (*CoreServer, *corev1.CheckPlayerSessionRequest) {
				playerID := ulid.Make()
				ps := makePlayerSession(playerID)
				sessionRepo := setupSessionRepo(t, ps)
				server := &CoreServer{
					engine:            core.NewEngine(core.NewMemoryEventStore()),
					sessionStore:      session.NewMemStore(),
					playerSessionRepo: sessionRepo,
				}
				return server, &corev1.CheckPlayerSessionRequest{PlayerSessionToken: validToken}
			},
			expectErr:  true,
			errContain: "not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, req := tt.setup(t)
			resp, err := server.CheckPlayerSession(context.Background(), req)
			if tt.expectErr {
				assert.Nil(t, resp)
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectName, resp.GetPlayerName())
			}
		})
	}
}

// --- AuthenticatePlayer additional paths ---

func TestAuthenticatePlayer_SessionRepoCreateFails(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)
	authSvc.authenticatePlayerFunc = func(_ context.Context, _, _, _, _ string) (string, *auth.Player, error) {
		return "", nil, errors.New("connection refused")
	}

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		authService:       authSvc,
		playerSessionRepo: authmocks.NewMockPlayerSessionRepository(t),
	}

	// The service wraps store errors in user-facing response messages — the
	// handler surfaces them as Success:false rather than returning an error.
	resp, err := server.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
		Username: "alice",
		Password: "password123",
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
}

// --- Test helper mocks (lightweight, function-based) ---

// mockAuthServiceForHandlers wraps auth.Service methods used by handlers.
type mockAuthServiceForHandlers struct {
	t                       *testing.T
	validateCredentialsFunc func(ctx context.Context, username, password string) (*auth.Player, error)
	authenticatePlayerFunc  func(ctx context.Context, username, password, userAgent, ipAddress string) (string, *auth.Player, error)
	createPlayerFunc        func(ctx context.Context, username, password, email string) (*auth.Player, *auth.PlayerSession, string, error)
	logoutFunc              func(ctx context.Context, tokenHash string) (ulid.ULID, error)
}

func newMockAuthService(t *testing.T) *mockAuthServiceForHandlers {
	return &mockAuthServiceForHandlers{t: t}
}

func (m *mockAuthServiceForHandlers) ValidateCredentials(ctx context.Context, username, password string) (*auth.Player, error) {
	if m.validateCredentialsFunc != nil {
		return m.validateCredentialsFunc(ctx, username, password)
	}
	m.t.Fatal("unexpected call to ValidateCredentials")
	return nil, nil
}

func (m *mockAuthServiceForHandlers) AuthenticatePlayer(ctx context.Context, username, password, userAgent, ipAddress string) (string, *auth.Player, error) {
	if m.authenticatePlayerFunc != nil {
		return m.authenticatePlayerFunc(ctx, username, password, userAgent, ipAddress)
	}
	m.t.Fatal("unexpected call to AuthenticatePlayer")
	return "", nil, nil
}

func (m *mockAuthServiceForHandlers) CreatePlayer(ctx context.Context, username, password, email string) (*auth.Player, *auth.PlayerSession, string, error) {
	if m.createPlayerFunc != nil {
		return m.createPlayerFunc(ctx, username, password, email)
	}
	m.t.Fatal("unexpected call to CreatePlayer")
	return nil, nil, "", nil
}

func (m *mockAuthServiceForHandlers) Logout(ctx context.Context, tokenHash string) (ulid.ULID, error) {
	if m.logoutFunc != nil {
		return m.logoutFunc(ctx, tokenHash)
	}
	m.t.Fatal("unexpected call to Logout")
	return ulid.ULID{}, nil
}

// mockCharacterServiceForHandlers wraps auth.CharacterService methods used by handlers.
type mockCharacterServiceForHandlers struct {
	t          *testing.T
	createFunc func(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error)
}

func newMockCharacterService(t *testing.T) *mockCharacterServiceForHandlers {
	return &mockCharacterServiceForHandlers{t: t}
}

func (m *mockCharacterServiceForHandlers) Create(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, playerID, name)
	}
	m.t.Fatal("unexpected call to CharacterService.Create")
	return nil, nil
}

// mockResetServiceForHandlers wraps auth.PasswordResetService methods used by handlers.
type mockResetServiceForHandlers struct {
	t                 *testing.T
	requestResetFunc  func(ctx context.Context, email string) (string, error)
	resetPasswordFunc func(ctx context.Context, token, newPassword string) error
}

func newMockResetService(t *testing.T) *mockResetServiceForHandlers {
	return &mockResetServiceForHandlers{t: t}
}

func (m *mockResetServiceForHandlers) RequestReset(ctx context.Context, email string) (string, error) {
	if m.requestResetFunc != nil {
		return m.requestResetFunc(ctx, email)
	}
	m.t.Fatal("unexpected call to RequestReset")
	return "", nil
}

func (m *mockResetServiceForHandlers) ResetPassword(ctx context.Context, token, newPassword string) error {
	if m.resetPasswordFunc != nil {
		return m.resetPasswordFunc(ctx, token, newPassword)
	}
	m.t.Fatal("unexpected call to ResetPassword")
	return nil
}

// --- Sanitized error-message tests (bd-nscu) ---
//
// These tests assert that the three handlers previously leaking raw
// err.Error() strings now return only a fixed, user-facing message derived
// from the oops error code. The raw error remains available server-side via
// slog but MUST NOT reach the client.

func TestCreatePlayerReturnsSanitizedMessageForUsernameTaken(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)
	authSvc.createPlayerFunc = func(_ context.Context, _, _, _ string) (*auth.Player, *auth.PlayerSession, string, error) {
		return nil, nil, "", oopsCoded("REGISTER_USERNAME_TAKEN",
			"username \"alice\" is already taken in schema auth_v3",
			"operation", "check username availability")
	}

	playerSessionRepo := authmocks.NewMockPlayerSessionRepository(t)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		authService:       authSvc,
		playerSessionRepo: playerSessionRepo,
	}

	resp, err := server.CreatePlayer(ctx, &corev1.CreatePlayerRequest{
		Username: "alice",
		Password: "strongpass1",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Equal(t, msgRegisterUsernameTaken, resp.ErrorMessage)
	assert.NotContains(t, resp.ErrorMessage, "schema")
	assert.NotContains(t, resp.ErrorMessage, "operation")
}

func TestCreatePlayerReturnsGenericMessageForUnknownError(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)
	authSvc.createPlayerFunc = func(_ context.Context, _, _, _ string) (*auth.Player, *auth.PlayerSession, string, error) {
		// Plain error — no oops code. Client MUST NOT see the raw message.
		return nil, nil, "", errors.New("pq: relation \"players_private_v3\" does not exist")
	}

	playerSessionRepo := authmocks.NewMockPlayerSessionRepository(t)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		authService:       authSvc,
		playerSessionRepo: playerSessionRepo,
	}

	resp, err := server.CreatePlayer(ctx, &corev1.CreatePlayerRequest{
		Username: "alice",
		Password: "strongpass1",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Equal(t, msgGenericRequestFailed, resp.ErrorMessage)
	assert.NotContains(t, resp.ErrorMessage, "players_private_v3")
	assert.NotContains(t, resp.ErrorMessage, "pq:")
}

func TestCreateCharacterReturnsSanitizedMessageForNameTaken(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charSvc := newMockCharacterService(t)
	charSvc.createFunc = func(_ context.Context, _ ulid.ULID, _ string) (*world.Character, error) {
		return nil, oopsCoded("CHARACTER_NAME_TAKEN",
			"character \"Hero\" is already taken in shard char_shard_3",
			"shard", "char_shard_3")
	}

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
		characterService:  charSvc,
	}

	resp, err := server.CreateCharacter(ctx, &corev1.CreateCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterName:      "Hero",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Equal(t, msgCharacterNameTaken, resp.ErrorMessage)
	assert.NotContains(t, resp.ErrorMessage, "char_shard_3")
	assert.NotContains(t, resp.ErrorMessage, "shard")
}

func TestConfirmPasswordResetReturnsSanitizedMessageForInvalidToken(t *testing.T) {
	ctx := context.Background()

	resetSvc := newMockResetService(t)
	resetSvc.resetPasswordFunc = func(_ context.Context, _, _ string) error {
		return oopsCoded("RESET_TOKEN_INVALID",
			"reset token not found in table password_resets on host db.internal.svc:5432",
			"host", "db.internal.svc:5432")
	}

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore()),
		sessionStore: session.NewMemStore(),
		resetService: resetSvc,
	}

	resp, err := server.ConfirmPasswordReset(ctx, &corev1.ConfirmPasswordResetRequest{
		Token:       "bad-token",
		NewPassword: "newstrongpass",
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Equal(t, msgResetTokenInvalid, resp.ErrorMessage)
	assert.NotContains(t, resp.ErrorMessage, "db.internal.svc")
	assert.NotContains(t, resp.ErrorMessage, "password_resets")
}

// --- ListPlayerSessions ---

func TestListPlayerSessionsReturnsCallersOwnSessionsWithIsCurrentFlag(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	// Two PlayerSessions for the same player - caller's current session is ps1.
	ps1 := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: auth.HashSessionToken(validToken),
		UserAgent: "agent-1",
		IPAddress: "10.0.0.1",
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now().Add(-30 * time.Minute),
	}
	ps2 := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: "other-hash",
		UserAgent: "agent-2",
		IPAddress: "10.0.0.2",
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-15 * time.Minute),
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(ps1, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps1.ID, auth.PlayerSessionTTL).Return(nil)
	sessionRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*auth.PlayerSession{ps1, ps2}, nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.ListPlayerSessions(ctx, &corev1.ListPlayerSessionsRequest{
		PlayerSessionToken: validToken,
	})
	require.NoError(t, err)

	require.Len(t, resp.Sessions, 2)
	var currents int
	var currentID string
	for _, s := range resp.Sessions {
		if s.IsCurrent {
			currents++
			currentID = s.Id
		}
	}
	assert.Equal(t, 1, currents, "exactly one session should be is_current")
	assert.Equal(t, ps1.ID.String(), currentID, "current session must match the caller's PlayerSession ID")
}

func TestListPlayerSessionsReturnsEmptyForInvalidToken(t *testing.T) {
	ctx := context.Background()

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken("tok-not-valid")).
		Return(nil, auth.ErrNotFound)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.ListPlayerSessions(ctx, &corev1.ListPlayerSessionsRequest{
		PlayerSessionToken: "tok-not-valid",
	})
	require.NoError(t, err)
	// Empty list on auth failure - enumeration-safe (no "token invalid" signal).
	assert.Empty(t, resp.Sessions)
}

func TestListPlayerSessionsReturnsEmptyForExpiredSession(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	expiredPS := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: auth.HashSessionToken(validToken),
		ExpiresAt: time.Now().Add(-time.Hour), // expired
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-time.Hour),
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).
		Return(expiredPS, nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.ListPlayerSessions(ctx, &corev1.ListPlayerSessionsRequest{
		PlayerSessionToken: validToken,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Sessions)
}

// --- RevokePlayerSession ---

func TestRevokePlayerSessionRevokesOwnOtherSession(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	// Caller's current session (tokenA).
	callerPS := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: auth.HashSessionToken(validToken),
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// Another session owned by the same player that we'll revoke.
	targetPS := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: "other-hash",
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(callerPS, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, callerPS.ID, auth.PlayerSessionTTL).Return(nil)
	sessionRepo.EXPECT().GetByID(mock.Anything, targetPS.ID).Return(targetPS, nil)
	sessionRepo.EXPECT().Delete(mock.Anything, targetPS.ID).Return(nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.RevokePlayerSession(ctx, &corev1.RevokePlayerSessionRequest{
		PlayerSessionToken: validToken,
		TargetSessionId:    targetPS.ID.String(),
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestRevokePlayerSessionRejectsForeignSession(t *testing.T) {
	ctx := context.Background()
	playerA := ulid.Make()
	playerB := ulid.Make()

	callerPS := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerA,
		TokenHash: auth.HashSessionToken(validToken),
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// Session owned by a different player - caller must not be able to revoke it.
	bPS := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerB,
		TokenHash: "bs-hash",
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(callerPS, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, callerPS.ID, auth.PlayerSessionTTL).Return(nil)
	sessionRepo.EXPECT().GetByID(mock.Anything, bPS.ID).Return(bPS, nil)
	// Delete MUST NOT be called for a foreign session - the mock would fail the test
	// if an unexpected call were made.

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.RevokePlayerSession(ctx, &corev1.RevokePlayerSessionRequest{
		PlayerSessionToken: validToken,
		TargetSessionId:    bPS.ID.String(),
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "session not found")
}

func TestRevokePlayerSessionRejectsInvalidToken(t *testing.T) {
	ctx := context.Background()

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken("invalid")).
		Return(nil, auth.ErrNotFound)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.RevokePlayerSession(ctx, &corev1.RevokePlayerSessionRequest{
		PlayerSessionToken: "invalid",
		TargetSessionId:    ulid.Make().String(),
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "session not found")
}

func TestRevokePlayerSessionRejectsInvalidTargetID(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	callerPS := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: auth.HashSessionToken(validToken),
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(callerPS, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, callerPS.ID, auth.PlayerSessionTTL).Return(nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.RevokePlayerSession(ctx, &corev1.RevokePlayerSessionRequest{
		PlayerSessionToken: validToken,
		TargetSessionId:    "not-a-ulid",
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "session not found")
}

// --- RevokeOtherPlayerSessions ---

func TestRevokeOtherPlayerSessionsKeepsCallerDeletesRest(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	// Caller's current session.
	callerPS := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: auth.HashSessionToken(validToken),
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	// Two other sessions for the same player - both must be revoked.
	other1 := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: "other-1",
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	other2 := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: "other-2",
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(callerPS, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, callerPS.ID, auth.PlayerSessionTTL).Return(nil)
	sessionRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*auth.PlayerSession{callerPS, other1, other2}, nil)
	// Caller's own session MUST NOT be deleted; only the other two.
	sessionRepo.EXPECT().Delete(mock.Anything, other1.ID).Return(nil)
	sessionRepo.EXPECT().Delete(mock.Anything, other2.ID).Return(nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.RevokeOtherPlayerSessions(ctx, &corev1.RevokeOtherPlayerSessionsRequest{
		PlayerSessionToken: validToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.EqualValues(t, 2, resp.RevokedCount)
}

func TestRevokeOtherPlayerSessionsRejectsInvalidToken(t *testing.T) {
	ctx := context.Background()

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken("invalid")).
		Return(nil, auth.ErrNotFound)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.RevokeOtherPlayerSessions(ctx, &corev1.RevokeOtherPlayerSessionsRequest{
		PlayerSessionToken: "invalid",
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
}

func TestRevokeOtherPlayerSessionsSucceedsWithNoOtherSessions(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	callerPS := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: auth.HashSessionToken(validToken),
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(callerPS, nil)
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, callerPS.ID, auth.PlayerSessionTTL).Return(nil)
	sessionRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*auth.PlayerSession{callerPS}, nil)

	server := &CoreServer{
		engine:            core.NewEngine(core.NewMemoryEventStore()),
		sessionStore:      session.NewMemStore(),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.RevokeOtherPlayerSessions(ctx, &corev1.RevokeOtherPlayerSessionsRequest{
		PlayerSessionToken: validToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.EqualValues(t, 0, resp.RevokedCount)
}

// --- TTL refresh on session-management RPCs ---

// TestSessionManagementRPCsRefreshCallerTTL verifies that the three
// session-management RPCs (ListPlayerSessions, RevokePlayerSession,
// RevokeOtherPlayerSessions) refresh the caller's session TTL so that an
// actively-managing user cannot have their session expire mid-use.
func TestSessionManagementRPCsRefreshCallerTTL(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	newCaller := func() *auth.PlayerSession {
		return &auth.PlayerSession{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: auth.HashSessionToken(validToken),
			ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	t.Run("ListPlayerSessions refreshes TTL", func(t *testing.T) {
		caller := newCaller()
		repo := authmocks.NewMockPlayerSessionRepository(t)
		repo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(caller, nil)
		repo.EXPECT().RefreshTTL(mock.Anything, caller.ID, auth.PlayerSessionTTL).Return(nil).Once()
		repo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*auth.PlayerSession{caller}, nil)

		server := &CoreServer{
			engine: core.NewEngine(core.NewMemoryEventStore()), sessionStore: session.NewMemStore(),
			playerSessionRepo: repo,
		}
		_, err := server.ListPlayerSessions(ctx, &corev1.ListPlayerSessionsRequest{PlayerSessionToken: validToken})
		require.NoError(t, err)
	})

	t.Run("RevokePlayerSession refreshes TTL", func(t *testing.T) {
		caller := newCaller()
		target := &auth.PlayerSession{ID: ulid.Make(), PlayerID: playerID, ExpiresAt: time.Now().Add(time.Hour)}
		repo := authmocks.NewMockPlayerSessionRepository(t)
		repo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(caller, nil)
		repo.EXPECT().RefreshTTL(mock.Anything, caller.ID, auth.PlayerSessionTTL).Return(nil).Once()
		repo.EXPECT().GetByID(mock.Anything, target.ID).Return(target, nil)
		repo.EXPECT().Delete(mock.Anything, target.ID).Return(nil)

		server := &CoreServer{
			engine: core.NewEngine(core.NewMemoryEventStore()), sessionStore: session.NewMemStore(),
			playerSessionRepo: repo,
		}
		_, err := server.RevokePlayerSession(ctx, &corev1.RevokePlayerSessionRequest{
			PlayerSessionToken: validToken, TargetSessionId: target.ID.String(),
		})
		require.NoError(t, err)
	})

	t.Run("RevokeOtherPlayerSessions refreshes TTL", func(t *testing.T) {
		caller := newCaller()
		repo := authmocks.NewMockPlayerSessionRepository(t)
		repo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(caller, nil)
		repo.EXPECT().RefreshTTL(mock.Anything, caller.ID, auth.PlayerSessionTTL).Return(nil).Once()
		repo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*auth.PlayerSession{caller}, nil)

		server := &CoreServer{
			engine: core.NewEngine(core.NewMemoryEventStore()), sessionStore: session.NewMemStore(),
			playerSessionRepo: repo,
		}
		_, err := server.RevokeOtherPlayerSessions(ctx, &corev1.RevokeOtherPlayerSessionsRequest{PlayerSessionToken: validToken})
		require.NoError(t, err)
	})

	t.Run("RefreshTTL error is swallowed and does not fail the RPC", func(t *testing.T) {
		// Best-effort: RefreshTTL failures must not break the RPC.
		caller := newCaller()
		repo := authmocks.NewMockPlayerSessionRepository(t)
		repo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).Return(caller, nil)
		repo.EXPECT().RefreshTTL(mock.Anything, caller.ID, auth.PlayerSessionTTL).Return(errors.New("db down"))
		repo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*auth.PlayerSession{caller}, nil)

		server := &CoreServer{
			engine: core.NewEngine(core.NewMemoryEventStore()), sessionStore: session.NewMemStore(),
			playerSessionRepo: repo,
		}
		resp, err := server.ListPlayerSessions(ctx, &corev1.ListPlayerSessionsRequest{PlayerSessionToken: validToken})
		require.NoError(t, err)
		require.Len(t, resp.Sessions, 1)
	})
}

// oopsCoded is a small helper that builds an oops error with arbitrary
// structured context alongside a raw message, so tests can assert nothing
// in the context leaks to the client.
func oopsCoded(code, msg string, kv ...string) error {
	b := samberOops.Code(code)
	for i := 0; i+1 < len(kv); i += 2 {
		b = b.With(kv[i], kv[i+1])
	}
	return b.Errorf("%s", msg)
}
