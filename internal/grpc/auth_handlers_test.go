// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
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

// --- AuthenticatePlayer ---

func TestAuthenticatePlayer_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()

	authSvc := newMockAuthService(t)
	authSvc.validateCredentialsFunc = func(_ context.Context, username, password string) (*auth.Player, error) {
		require.Equal(t, "alice", username)
		require.Equal(t, "password123", password)
		return &auth.Player{
			ID:                 playerID,
			Username:           "alice",
			DefaultCharacterID: &charID,
		}, nil
	}

	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.PlayerToken")).
		Return(nil)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Alice", LocationID: &locID},
		}, nil)

	sessionStore := session.NewMemStore()

	server := &CoreServer{
		engine:          core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:        core.NewSessionManager(),
		sessionStore:    sessionStore,
		authService:     authSvc,
		playerTokenRepo: tokenRepo,
		charRepo:        charRepo,
	}

	resp, err := server.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
		Username: "alice",
		Password: "password123",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.NotEmpty(t, resp.PlayerToken)
	assert.Len(t, resp.Characters, 1)
	assert.Equal(t, charID.String(), resp.Characters[0].CharacterId)
	assert.Equal(t, "Alice", resp.Characters[0].CharacterName)
	assert.Equal(t, charID.String(), resp.DefaultCharacterId)
}

func TestAuthenticatePlayer_InvalidCredentials(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)
	authSvc.validateCredentialsFunc = func(_ context.Context, _, _ string) (*auth.Player, error) {
		return nil, auth.ErrNotFound
	}

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
		sessionStore: session.NewMemStore(),
		authService:  authSvc,
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
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
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

// --- SelectCharacter ---

func TestSelectCharacter_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()
	sessionID := core.NewULID()

	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().GetByToken(mock.Anything, "valid-token").
		Return(&auth.PlayerToken{
			Token:     "valid-token",
			PlayerID:  playerID,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}, nil)
	tokenRepo.EXPECT().DeleteByToken(mock.Anything, "valid-token").Return(nil)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Alice", LocationID: &locID},
		}, nil)

	sessionStore := session.NewMemStore()
	sessions := core.NewSessionManager()

	server := &CoreServer{
		engine:          core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:        sessions,
		sessionStore:    sessionStore,
		playerTokenRepo: tokenRepo,
		charRepo:        charRepo,
		newSessionID:    func() ulid.ULID { return sessionID },
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerToken: "valid-token",
		CharacterId: charID.String(),
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, sessionID.String(), resp.SessionId)
	assert.Equal(t, "Alice", resp.CharacterName)
	assert.False(t, resp.Reattached)
}

func TestSelectCharacter_ExpiredToken(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().GetByToken(mock.Anything, "expired-token").
		Return(&auth.PlayerToken{
			Token:     "expired-token",
			PlayerID:  playerID,
			ExpiresAt: time.Now().Add(-1 * time.Minute),
		}, nil)

	server := &CoreServer{
		engine:          core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:        core.NewSessionManager(),
		sessionStore:    session.NewMemStore(),
		playerTokenRepo: tokenRepo,
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerToken: "expired-token",
		CharacterId: ulid.Make().String(),
	})
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "expired")
}

func TestSelectCharacter_InvalidCharacter(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()

	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().GetByToken(mock.Anything, "valid-token").
		Return(&auth.PlayerToken{
			Token:     "valid-token",
			PlayerID:  playerID,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}, nil)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			// Character with a different ID — the requested one won't match.
			{ID: ulid.Make(), PlayerID: playerID, Name: "Other"},
		}, nil)

	server := &CoreServer{
		engine:          core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:        core.NewSessionManager(),
		sessionStore:    session.NewMemStore(),
		playerTokenRepo: tokenRepo,
		charRepo:        charRepo,
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerToken: "valid-token",
		CharacterId: charID.String(),
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

	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().GetByToken(mock.Anything, "valid-token").
		Return(&auth.PlayerToken{
			Token:     "valid-token",
			PlayerID:  playerID,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}, nil)
	tokenRepo.EXPECT().DeleteByToken(mock.Anything, "valid-token").Return(nil)

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

	sessions := core.NewSessionManager()
	server := &CoreServer{
		engine:          core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:        sessions,
		sessionStore:    sessionStore,
		playerTokenRepo: tokenRepo,
		charRepo:        charRepo,
		newSessionID:    func() ulid.ULID { return core.NewULID() },
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerToken: "valid-token",
		CharacterId: charID.String(),
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, existingSessionID, resp.SessionId)
	assert.True(t, resp.Reattached)
}

// --- CreatePlayer ---

func TestCreatePlayer_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	authSvc := newMockAuthService(t)
	authSvc.createPlayerFunc = func(_ context.Context, username, password, email string) (*auth.Player, *auth.PlayerToken, error) {
		require.Equal(t, "newuser", username)
		require.Equal(t, "strongpass1", password)
		require.Equal(t, "new@example.com", email)
		return &auth.Player{ID: playerID, Username: "newuser"},
			&auth.PlayerToken{Token: "new-token", PlayerID: playerID, ExpiresAt: time.Now().Add(5 * time.Minute)},
			nil
	}

	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.PlayerToken")).Return(nil)

	server := &CoreServer{
		engine:          core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:        core.NewSessionManager(),
		sessionStore:    session.NewMemStore(),
		authService:     authSvc,
		playerTokenRepo: tokenRepo,
	}

	resp, err := server.CreatePlayer(ctx, &corev1.CreatePlayerRequest{
		Username: "newuser",
		Password: "strongpass1",
		Email:    "new@example.com",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, "new-token", resp.PlayerToken)
	assert.Empty(t, resp.Characters)
}

func TestCreatePlayer_UsernameTaken(t *testing.T) {
	ctx := context.Background()

	authSvc := newMockAuthService(t)
	authSvc.createPlayerFunc = func(_ context.Context, _, _, _ string) (*auth.Player, *auth.PlayerToken, error) {
		return nil, nil, auth.ErrNotFound // simulates username taken via oops
	}

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
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
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
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

	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().GetByToken(mock.Anything, "valid-token").
		Return(&auth.PlayerToken{
			Token:     "valid-token",
			PlayerID:  playerID,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}, nil)

	charSvc := newMockCharacterService(t)
	charSvc.createFunc = func(_ context.Context, pid ulid.ULID, name string) (*world.Character, error) {
		require.Equal(t, playerID, pid)
		require.Equal(t, "New Hero", name)
		return &world.Character{ID: charID, PlayerID: pid, Name: "New Hero"}, nil
	}

	server := &CoreServer{
		engine:           core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:         core.NewSessionManager(),
		sessionStore:     session.NewMemStore(),
		playerTokenRepo:  tokenRepo,
		characterService: charSvc,
	}

	resp, err := server.CreateCharacter(ctx, &corev1.CreateCharacterRequest{
		PlayerToken:   "valid-token",
		CharacterName: "New Hero",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, charID.String(), resp.CharacterId)
	assert.Equal(t, "New Hero", resp.CharacterName)
}

func TestCreateCharacter_NotConfigured(t *testing.T) {
	server := &CoreServer{
		engine:   core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions: core.NewSessionManager(),
	}
	resp, err := server.CreateCharacter(context.Background(), &corev1.CreateCharacterRequest{
		PlayerToken:   "some-token",
		CharacterName: "Hero",
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "not configured")
}

func TestCreateCharacter_ExpiredToken(t *testing.T) {
	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().GetByToken(mock.Anything, "expired-token").
		Return(&auth.PlayerToken{
			Token:     "expired-token",
			PlayerID:  ulid.Make(),
			ExpiresAt: time.Now().Add(-1 * time.Minute),
		}, nil)

	server := &CoreServer{
		engine:           core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:         core.NewSessionManager(),
		playerTokenRepo:  tokenRepo,
		characterService: nil, // token expires before reaching service
	}
	resp, err := server.CreateCharacter(context.Background(), &corev1.CreateCharacterRequest{
		PlayerToken:   "expired-token",
		CharacterName: "Hero",
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "expired")
}

// --- ListCharacters ---

func TestListCharacters_NotConfigured(t *testing.T) {
	server := &CoreServer{
		engine:   core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions: core.NewSessionManager(),
	}
	resp, err := server.ListCharacters(context.Background(), &corev1.ListCharactersRequest{
		PlayerToken: "some-token",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Characters)
}

func TestListCharacters_ExpiredToken(t *testing.T) {
	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().GetByToken(mock.Anything, "expired-token").
		Return(&auth.PlayerToken{
			Token:     "expired-token",
			PlayerID:  ulid.Make(),
			ExpiresAt: time.Now().Add(-1 * time.Minute),
		}, nil)

	server := &CoreServer{
		engine:          core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:        core.NewSessionManager(),
		playerTokenRepo: tokenRepo,
	}
	resp, err := server.ListCharacters(context.Background(), &corev1.ListCharactersRequest{
		PlayerToken: "expired-token",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Characters)
}

func TestListCharacters_Success(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()

	tokenRepo := authmocks.NewMockPlayerTokenRepository(t)
	tokenRepo.EXPECT().GetByToken(mock.Anything, "valid-token").
		Return(&auth.PlayerToken{
			Token:     "valid-token",
			PlayerID:  playerID,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}, nil)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{ID: charID, PlayerID: playerID, Name: "Alice", LocationID: &locID},
		}, nil)

	server := &CoreServer{
		engine:          core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:        core.NewSessionManager(),
		sessionStore:    session.NewMemStore(),
		playerTokenRepo: tokenRepo,
		charRepo:        charRepo,
	}

	resp, err := server.ListCharacters(ctx, &corev1.ListCharactersRequest{
		PlayerToken: "valid-token",
	})
	require.NoError(t, err)

	require.Len(t, resp.Characters, 1)
	assert.Equal(t, charID.String(), resp.Characters[0].CharacterId)
	assert.Equal(t, "Alice", resp.Characters[0].CharacterName)
}

// --- RequestPasswordReset ---

func TestRequestPasswordReset_AlwaysSuccess(t *testing.T) {
	ctx := context.Background()

	resetSvc := newMockResetService(t)
	resetSvc.requestResetFunc = func(_ context.Context, _ string) (string, error) {
		return "reset-token-123", nil
	}

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
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
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
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
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
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
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
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
	sessionID := ulid.Make()

	authSvc := newMockAuthService(t)
	authSvc.logoutFunc = func(_ context.Context, sid ulid.ULID) error {
		require.Equal(t, sessionID, sid)
		return nil
	}

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
		sessionStore: session.NewMemStore(),
		authService:  authSvc,
	}

	resp, err := server.Logout(ctx, &corev1.LogoutRequest{
		SessionId: sessionID.String(),
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestLogout_InvalidSessionID(t *testing.T) {
	ctx := context.Background()

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
		sessionStore: session.NewMemStore(),
		authService:  newMockAuthService(t),
	}

	_, err := server.Logout(ctx, &corev1.LogoutRequest{
		SessionId: "not-a-ulid",
	})
	// Invalid ULID returns a gRPC error.
	assert.Error(t, err)
}

func TestLogout_NotConfigured(t *testing.T) {
	ctx := context.Background()

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:     core.NewSessionManager(),
		sessionStore: session.NewMemStore(),
	}

	_, err := server.Logout(ctx, &corev1.LogoutRequest{
		SessionId: ulid.Make().String(),
	})
	assert.Error(t, err)
}

// --- Test helper mocks (lightweight, function-based) ---

// mockAuthServiceForHandlers wraps auth.Service methods used by handlers.
type mockAuthServiceForHandlers struct {
	t                       *testing.T
	validateCredentialsFunc func(ctx context.Context, username, password string) (*auth.Player, error)
	createPlayerFunc        func(ctx context.Context, username, password, email string) (*auth.Player, *auth.PlayerToken, error)
	logoutFunc              func(ctx context.Context, sessionID ulid.ULID) error
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

func (m *mockAuthServiceForHandlers) CreatePlayer(ctx context.Context, username, password, email string) (*auth.Player, *auth.PlayerToken, error) {
	if m.createPlayerFunc != nil {
		return m.createPlayerFunc(ctx, username, password, email)
	}
	m.t.Fatal("unexpected call to CreatePlayer")
	return nil, nil, nil
}

func (m *mockAuthServiceForHandlers) Logout(ctx context.Context, sessionID ulid.ULID) error {
	if m.logoutFunc != nil {
		return m.logoutFunc(ctx, sessionID)
	}
	m.t.Fatal("unexpected call to Logout")
	return nil
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
