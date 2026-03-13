// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
)

// --- Mock implementations ---

type mockAuthService struct {
	mock.Mock
}

func (m *mockAuthService) Login(ctx context.Context, username, password, userAgent, ipAddress string) (*auth.WebSession, string, error) {
	args := m.Called(ctx, username, password, userAgent, ipAddress)
	if args.Get(0) == nil {
		return nil, "", args.Error(2)
	}
	return args.Get(0).(*auth.WebSession), args.String(1), args.Error(2)
}

func (m *mockAuthService) Logout(ctx context.Context, sessionID ulid.ULID) error {
	args := m.Called(ctx, sessionID)
	return args.Error(0)
}

func (m *mockAuthService) SelectCharacter(ctx context.Context, sessionID, characterID ulid.ULID) error {
	args := m.Called(ctx, sessionID, characterID)
	return args.Error(0)
}

type mockRegistrationService struct {
	mock.Mock
}

func (m *mockRegistrationService) Register(ctx context.Context, username, password, email string) (*auth.Player, error) {
	args := m.Called(ctx, username, password, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.Player), args.Error(1)
}

func (m *mockRegistrationService) IsRegistrationEnabled() bool {
	args := m.Called()
	return args.Bool(0)
}

type mockCharacterLister struct {
	mock.Mock
}

func (m *mockCharacterLister) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*CharacterInfo, error) {
	args := m.Called(ctx, playerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*CharacterInfo), args.Error(1)
}

func (m *mockCharacterLister) GetByName(ctx context.Context, name string) (*CharacterInfo, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*CharacterInfo), args.Error(1)
}

// --- AuthHandler creation tests ---

func TestNewAuthHandler(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)

	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	assert.NotNil(t, handler)
	assert.Equal(t, authSvc, handler.authService)
	assert.Equal(t, regSvc, handler.regService)
	assert.Equal(t, charLister, handler.charLister)
}

func TestNewAuthHandler_NilDependencies(t *testing.T) {
	tests := []struct {
		name        string
		authService AuthService
		regService  RegistrationService
		charLister  CharacterLister
		expectError string
	}{
		{
			name:        "nil auth service",
			authService: nil,
			regService:  new(mockRegistrationService),
			charLister:  new(mockCharacterLister),
			expectError: "auth service is required",
		},
		{
			name:        "nil registration service",
			authService: new(mockAuthService),
			regService:  nil,
			charLister:  new(mockCharacterLister),
			expectError: "registration service is required",
		},
		{
			name:        "nil character lister",
			authService: new(mockAuthService),
			regService:  new(mockRegistrationService),
			charLister:  nil,
			expectError: "character lister is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewAuthHandler(tt.authService, tt.regService, tt.charLister)
			require.Error(t, err)
			assert.Nil(t, handler)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

// --- Connect command tests ---

func TestAuthHandler_HandleConnect_Success(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	playerID := ulid.Make()
	session := &auth.WebSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	token := "test-token-123"

	authSvc.On("Login", ctx, "testuser", "password", "telnet", "127.0.0.1").
		Return(session, token, nil)
	charLister.On("ListByPlayer", ctx, playerID).
		Return([]*CharacterInfo{
			{ID: ulid.Make(), Name: "Hero"},
			{ID: ulid.Make(), Name: "Wizard"},
		}, nil)

	result := handler.HandleConnect(ctx, "testuser", "password", "127.0.0.1")

	assert.True(t, result.Success)
	assert.Equal(t, playerID, result.PlayerID)
	assert.Equal(t, session.ID, result.SessionID)
	assert.Contains(t, result.Message, "Welcome")
	assert.Len(t, result.Characters, 2)

	authSvc.AssertExpectations(t)
	charLister.AssertExpectations(t)
}

func TestAuthHandler_HandleConnect_InvalidCredentials(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	authErr := oops.Code("AUTH_INVALID_CREDENTIALS").Errorf("invalid username or password")

	authSvc.On("Login", ctx, "baduser", "badpass", "telnet", "127.0.0.1").
		Return(nil, "", authErr)

	result := handler.HandleConnect(ctx, "baduser", "badpass", "127.0.0.1")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Invalid username or password")

	authSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleConnect_AccountLocked(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	authErr := oops.Code("AUTH_ACCOUNT_LOCKED").Errorf("account is temporarily locked")

	authSvc.On("Login", ctx, "lockeduser", "password", "telnet", "127.0.0.1").
		Return(nil, "", authErr)

	result := handler.HandleConnect(ctx, "lockeduser", "password", "127.0.0.1")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "locked")

	authSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleConnect_NoCharacters(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	playerID := ulid.Make()
	session := &auth.WebSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	token := "test-token-123"

	authSvc.On("Login", ctx, "newuser", "password", "telnet", "127.0.0.1").
		Return(session, token, nil)
	charLister.On("ListByPlayer", ctx, playerID).
		Return([]*CharacterInfo{}, nil)

	result := handler.HandleConnect(ctx, "newuser", "password", "127.0.0.1")

	assert.True(t, result.Success)
	assert.Len(t, result.Characters, 0)
	assert.Contains(t, result.Message, "no characters")

	authSvc.AssertExpectations(t)
	charLister.AssertExpectations(t)
}

// --- Create command tests ---

func TestAuthHandler_HandleCreate_Success(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	playerID := ulid.Make()
	player := &auth.Player{
		ID:       playerID,
		Username: "newplayer",
	}

	regSvc.On("IsRegistrationEnabled").Return(true)
	regSvc.On("Register", ctx, "newplayer", "securepass", "").Return(player, nil)

	result := handler.HandleCreate(ctx, "newplayer", "securepass")

	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "created")
	assert.Contains(t, result.Message, "connect")
	// Verify username is NOT echoed back (security: prevents enumeration)
	assert.NotContains(t, result.Message, "newplayer")

	regSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleCreate_RegistrationDisabled(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()

	regSvc.On("IsRegistrationEnabled").Return(false)

	result := handler.HandleCreate(ctx, "newplayer", "securepass")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "disabled")

	regSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleCreate_UsernameTaken(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	regErr := oops.Code("AUTH_USERNAME_TAKEN").Errorf("username already exists")

	regSvc.On("IsRegistrationEnabled").Return(true)
	regSvc.On("Register", ctx, "existinguser", "password", "").Return(nil, regErr)

	result := handler.HandleCreate(ctx, "existinguser", "password")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "already exists")

	regSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleCreate_InvalidUsername(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	regErr := oops.Code("AUTH_INVALID_USERNAME").Errorf("invalid username")

	regSvc.On("IsRegistrationEnabled").Return(true)
	regSvc.On("Register", ctx, "ab", "password", "").Return(nil, regErr)

	result := handler.HandleCreate(ctx, "ab", "password")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "invalid")

	regSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleCreate_InvalidPassword(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	regErr := oops.Code("AUTH_INVALID_PASSWORD").Errorf("password too weak")

	regSvc.On("IsRegistrationEnabled").Return(true)
	regSvc.On("Register", ctx, "newuser", "123", "").Return(nil, regErr)

	result := handler.HandleCreate(ctx, "newuser", "123")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Password")
	assert.Contains(t, result.Message, "invalid")

	regSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleConnect_CharacterListFailure(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	playerID := ulid.Make()
	session := &auth.WebSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	token := "test-token-123"
	listErr := oops.Code("DB_ERROR").Errorf("database error")

	authSvc.On("Login", ctx, "testuser", "password", "telnet", "127.0.0.1").
		Return(session, token, nil)
	charLister.On("ListByPlayer", ctx, playerID).
		Return(nil, listErr)

	result := handler.HandleConnect(ctx, "testuser", "password", "127.0.0.1")

	// Login should still succeed even if character list fails
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "Could not retrieve")
	assert.Nil(t, result.Characters)

	authSvc.AssertExpectations(t)
	charLister.AssertExpectations(t)
}

func TestAuthHandler_HandleConnect_GenericError(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	// Error without oops code (generic error)
	authErr := oops.Errorf("some database error")

	authSvc.On("Login", ctx, "testuser", "password", "telnet", "127.0.0.1").
		Return(nil, "", authErr)

	result := handler.HandleConnect(ctx, "testuser", "password", "127.0.0.1")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Login failed")

	authSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleCreate_GenericError(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	// Error without specific code
	regErr := oops.Errorf("some unexpected error")

	regSvc.On("IsRegistrationEnabled").Return(true)
	regSvc.On("Register", ctx, "newuser", "password", "").Return(nil, regErr)

	result := handler.HandleCreate(ctx, "newuser", "password")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Registration failed")

	regSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleEmbody_OwnershipVerificationFails(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sessionID := ulid.Make()
	playerID := ulid.Make()
	charID := ulid.Make()
	charInfo := &CharacterInfo{ID: charID, Name: "Hero"}
	listErr := oops.Code("DB_ERROR").Errorf("database error")

	charLister.On("GetByName", ctx, "Hero").Return(charInfo, nil)
	charLister.On("ListByPlayer", ctx, playerID).Return(nil, listErr)

	result := handler.HandleEmbody(ctx, sessionID, playerID, "Hero")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Could not verify")

	charLister.AssertExpectations(t)
}

func TestAuthHandler_HandleEmbody_SelectCharacterFails(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sessionID := ulid.Make()
	playerID := ulid.Make()
	charID := ulid.Make()
	charInfo := &CharacterInfo{ID: charID, Name: "Hero"}
	selectErr := oops.Code("SESSION_NOT_FOUND").Errorf("session not found")

	charLister.On("GetByName", ctx, "Hero").Return(charInfo, nil)
	charLister.On("ListByPlayer", ctx, playerID).Return([]*CharacterInfo{charInfo}, nil)
	authSvc.On("SelectCharacter", ctx, sessionID, charID).Return(selectErr)

	result := handler.HandleEmbody(ctx, sessionID, playerID, "Hero")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Failed to select")

	charLister.AssertExpectations(t)
	authSvc.AssertExpectations(t)
}

// --- Embody command tests ---

func TestAuthHandler_HandleEmbody_Success(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sessionID := ulid.Make()
	playerID := ulid.Make()
	charID := ulid.Make()
	charInfo := &CharacterInfo{ID: charID, Name: "Hero"}

	charLister.On("GetByName", ctx, "Hero").Return(charInfo, nil)
	// Ownership verification
	charLister.On("ListByPlayer", ctx, playerID).Return([]*CharacterInfo{charInfo}, nil)
	authSvc.On("SelectCharacter", ctx, sessionID, charID).Return(nil)

	result := handler.HandleEmbody(ctx, sessionID, playerID, "Hero")

	assert.True(t, result.Success)
	assert.Equal(t, charID, result.CharacterID)
	assert.Equal(t, "Hero", result.CharacterName)
	assert.Contains(t, result.Message, "Hero")

	charLister.AssertExpectations(t)
	authSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleEmbody_CharacterNotFound(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sessionID := ulid.Make()
	playerID := ulid.Make()
	charErr := oops.Code("CHARACTER_NOT_FOUND").Errorf("character not found")

	charLister.On("GetByName", ctx, "Unknown").Return(nil, charErr)

	result := handler.HandleEmbody(ctx, sessionID, playerID, "Unknown")

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "not found")

	charLister.AssertExpectations(t)
}

func TestAuthHandler_HandleEmbody_CharacterNotOwned(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sessionID := ulid.Make()
	playerID := ulid.Make()
	otherPlayerID := ulid.Make()
	charID := ulid.Make()

	// Character exists but belongs to a different player
	charInfo := &CharacterInfo{ID: charID, Name: "OtherHero"}
	charLister.On("GetByName", ctx, "OtherHero").Return(charInfo, nil)

	// We need a way to verify ownership - let's check with player's character list
	charLister.On("ListByPlayer", ctx, playerID).Return([]*CharacterInfo{}, nil)

	result := handler.HandleEmbody(ctx, sessionID, playerID, "OtherHero")

	// This should still work - the ownership check happens at select time
	// For now we trust the character lookup
	_ = otherPlayerID // unused but conceptually important
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "do not own")

	charLister.AssertExpectations(t)
}

func TestAuthHandler_HandleEmbody_CaseInsensitive(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sessionID := ulid.Make()
	playerID := ulid.Make()
	charID := ulid.Make()
	charInfo := &CharacterInfo{ID: charID, Name: "Hero"}

	// GetByName should be case-insensitive
	charLister.On("GetByName", ctx, "hero").Return(charInfo, nil)
	// Ownership verification
	charLister.On("ListByPlayer", ctx, playerID).Return([]*CharacterInfo{charInfo}, nil)
	authSvc.On("SelectCharacter", ctx, sessionID, charID).Return(nil)

	result := handler.HandleEmbody(ctx, sessionID, playerID, "hero")

	assert.True(t, result.Success)
	assert.Equal(t, "Hero", result.CharacterName) // Returns canonical name

	charLister.AssertExpectations(t)
	authSvc.AssertExpectations(t)
}

// --- Quit command tests ---

func TestAuthHandler_HandleQuit_WithSession(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sessionID := ulid.Make()

	authSvc.On("Logout", ctx, sessionID).Return(nil)

	result := handler.HandleQuit(ctx, &sessionID)

	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "Goodbye")

	authSvc.AssertExpectations(t)
}

func TestAuthHandler_HandleQuit_WithoutSession(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()

	result := handler.HandleQuit(ctx, nil)

	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "Goodbye")

	// Logout should not be called if no session
	authSvc.AssertNotCalled(t, "Logout", mock.Anything, mock.Anything)
}

func TestAuthHandler_HandleQuit_LogoutError(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sessionID := ulid.Make()
	logoutErr := oops.Code("SESSION_NOT_FOUND").Errorf("session not found")

	authSvc.On("Logout", ctx, sessionID).Return(logoutErr)

	result := handler.HandleQuit(ctx, &sessionID)

	// Even if logout fails, we still say goodbye
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "Goodbye")

	authSvc.AssertExpectations(t)
}

// --- Security tests ---

func TestAuthHandler_PasswordsNeverLeakedInErrorMessages(t *testing.T) {
	authSvc := new(mockAuthService)
	regSvc := new(mockRegistrationService)
	charLister := new(mockCharacterLister)
	handler, err := NewAuthHandler(authSvc, regSvc, charLister)
	require.NoError(t, err)

	ctx := context.Background()
	sensitivePassword := "SuperSecret123!"

	// Test connect with invalid credentials
	authErr := oops.Code("AUTH_INVALID_CREDENTIALS").Errorf("invalid username or password")
	authSvc.On("Login", ctx, "testuser", sensitivePassword, "telnet", "127.0.0.1").
		Return(nil, "", authErr)

	connectResult := handler.HandleConnect(ctx, "testuser", sensitivePassword, "127.0.0.1")
	assert.NotContains(t, connectResult.Message, sensitivePassword, "password leaked in connect error message")

	// Test create with invalid password
	regErr := oops.Code("AUTH_INVALID_PASSWORD").Errorf("password too weak")
	regSvc.On("IsRegistrationEnabled").Return(true)
	regSvc.On("Register", ctx, "newuser", sensitivePassword, "").Return(nil, regErr)

	createResult := handler.HandleCreate(ctx, "newuser", sensitivePassword)
	assert.NotContains(t, createResult.Message, sensitivePassword, "password leaked in create error message")

	authSvc.AssertExpectations(t)
	regSvc.AssertExpectations(t)
}
