// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	samberOops "github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
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

func TestAuthenticatePlayerReturnsTokenAndCharactersOnValidCredentials(t *testing.T) {
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

	sessionStore := sessiontest.NewStore(t)

	server := &CoreServer{
		presence: newTestPresenceEmitter(newTestEventStore()),

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

// replaces: TestAuthenticatePlayer_InvalidCredentials,
//
//	TestAuthenticatePlayer_ServiceNotConfigured,
//	TestAuthenticatePlayer_SessionRepoNotConfigured,
//	TestAuthenticatePlayer_SessionRepoCreateFails
func TestAuthenticatePlayer_ErrorPaths(t *testing.T) {
	tests := []struct {
		name            string
		authSvcNil      bool
		setupAuthSvc    func(*mockAuthServiceForHandlers)
		sessionRepoNil  bool
		wantMsgContains string
		wantNotEmptyMsg bool
	}{
		{
			name: "invalid credentials returns success=false",
			setupAuthSvc: func(svc *mockAuthServiceForHandlers) {
				svc.authenticatePlayerFunc = func(_ context.Context, _, _, _, _ string) (string, *auth.Player, error) {
					return "", nil, auth.ErrNotFound
				}
			},
			sessionRepoNil:  false,
			wantNotEmptyMsg: true,
		},
		{
			name:            "auth service not configured returns success=false with not configured message",
			authSvcNil:      true,
			sessionRepoNil:  true,
			wantMsgContains: "not configured",
		},
		{
			name:            "session repo not configured returns success=false with not configured message",
			sessionRepoNil:  true,
			wantMsgContains: "not configured",
		},
		{
			name: "auth service error returns success=false",
			setupAuthSvc: func(svc *mockAuthServiceForHandlers) {
				svc.authenticatePlayerFunc = func(_ context.Context, _, _, _, _ string) (string, *auth.Player, error) {
					return "", nil, errors.New("connection refused")
				}
			},
			sessionRepoNil:  false,
			wantNotEmptyMsg: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			server := &CoreServer{
				presence:     newTestPresenceEmitter(newTestEventStore()),
				sessionStore: sessiontest.NewStore(t),
			}

			if !tt.authSvcNil {
				authSvc := newMockAuthService(t)
				if tt.setupAuthSvc != nil {
					tt.setupAuthSvc(authSvc)
				}
				server.authService = authSvc
			}

			if !tt.sessionRepoNil {
				server.playerSessionRepo = authmocks.NewMockPlayerSessionRepository(t)
			}

			resp, err := server.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
				Username: "alice",
				Password: "password123",
			})
			require.NoError(t, err)
			assert.False(t, resp.Success)

			if tt.wantMsgContains != "" {
				assert.Contains(t, resp.ErrorMessage, tt.wantMsgContains)
			}
			if tt.wantNotEmptyMsg {
				assert.NotEmpty(t, resp.ErrorMessage)
			}
		})
	}
}

// --- SelectCharacter ---

// TestSelectCharacter covers the core SelectCharacter RPC scenarios: fresh
// session creation, error paths (invalid session/character), reattach
// (detached session found), and the LocationArrivedAt invariants.
//
// replaces: TestSelectCharacter_Success, TestSelectCharacter_InvalidSession,
//
//	TestSelectCharacter_InvalidCharacter, TestSelectCharacter_Reattach,
//	TestSelectCharacter_FreshSession_SetsLocationArrivedAt,
//	TestSelectCharacter_Reattach_PreservesLocationArrivedAt
func TestSelectCharacter(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, ctx context.Context)
	}{
		{
			name: "fresh session created returns success with new session id and character name",
			run: func(t *testing.T, ctx context.Context) {
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
				sessionStore, pool := sessiontest.NewStoreWithPool(t)
				sessiontest.SeedPlayerSession(t, pool, ps)

				server := &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
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
			},
		},
		{
			name: "invalid session token returns success=false with invalid or expired message",
			run: func(t *testing.T, ctx context.Context) {
				sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
				tokenHash := auth.HashSessionToken("bad-token")
				sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
					Return(nil, auth.ErrNotFound)

				server := &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
				}
				resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
					PlayerSessionToken: "bad-token",
					CharacterId:        ulid.Make().String(),
				})
				require.NoError(t, err)
				assert.False(t, resp.Success)
				assert.Contains(t, resp.ErrorMessage, "invalid or expired")
			},
		},
		{
			name: "character not owned by player returns success=false with does not belong message",
			run: func(t *testing.T, ctx context.Context) {
				playerID := ulid.Make()
				charID := ulid.Make()

				ps := makePlayerSession(playerID)
				sessionRepo := setupSessionRepo(t, ps)
				charRepo := authmocks.NewMockCharacterRepository(t)
				charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
					Return([]*world.Character{
						{ID: ulid.Make(), PlayerID: playerID, Name: "Other"},
					}, nil)

				server := &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
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
			},
		},
		{
			name: "detached session found returns success with existing session id and reattached=true",
			run: func(t *testing.T, ctx context.Context) {
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

				sessionStore := sessiontest.NewStore(t)
				require.NoError(t, sessionStore.Set(ctx, existingSessionID, &session.Info{
					ID:            existingSessionID,
					CharacterID:   charID,
					CharacterName: "Alice",
					LocationID:    locID,
					Status:        session.StatusDetached,
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}))

				server := &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
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
			},
		},
		{
			name: "fresh session sets location arrived at to a non-zero time not before test start",
			run: func(t *testing.T, ctx context.Context) {
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

				sessionStore, pool := sessiontest.NewStoreWithPool(t)
				sessiontest.SeedPlayerSession(t, pool, ps)

				before := time.Now()
				server := &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
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
				require.True(t, resp.Success)
				assert.False(t, resp.Reattached)

				stored, err := sessionStore.Get(ctx, sessionID.String())
				require.NoError(t, err)
				assert.False(t, stored.LocationArrivedAt.IsZero(),
					"LocationArrivedAt must be set on fresh session create")
				assert.False(t, stored.LocationArrivedAt.Before(before),
					"LocationArrivedAt must not be before the test started")
			},
		},
		{
			// Session-row-as-continuity rule (spec §5 row 2 + INV-PRIVACY-3, amended
			// 2026-05-18): reattach within TTL is the same session continuing — its
			// LocationArrivedAt MUST NOT be advanced. The original floor is preserved
			// so the player's own pre-disconnect scrollback survives page reload,
			// WiFi blip, and tmux-style telnet reattach.
			name: "reattach preserves original location arrived at and flips status back to active",
			run: func(t *testing.T, ctx context.Context) {
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

				originalArrival := time.Now().Add(-2 * time.Hour)
				sessionStore := sessiontest.NewStore(t)
				require.NoError(t, sessionStore.Set(ctx, existingSessionID, &session.Info{
					ID:                existingSessionID,
					CharacterID:       charID,
					CharacterName:     "Alice",
					LocationID:        locID,
					Status:            session.StatusDetached,
					LocationArrivedAt: originalArrival,
					CreatedAt:         originalArrival,
					UpdatedAt:         originalArrival,
				}))

				server := &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
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
				require.True(t, resp.Success)
				assert.True(t, resp.Reattached)

				stored, err := sessionStore.Get(ctx, existingSessionID)
				require.NoError(t, err)
				assert.True(t, stored.LocationArrivedAt.Equal(originalArrival),
					"LocationArrivedAt MUST be unchanged on reattach (spec §5 row 2, INV-PRIVACY-3); got %v, want %v",
					stored.LocationArrivedAt, originalArrival)
				assert.Equal(t, session.StatusActive, stored.Status,
					"reattach MUST flip status back to Active")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, context.Background())
		})
	}
}

// TestSelectCharacterReattachesOnSecondCallWithSameToken asserts that calling
// SelectCharacter twice with the same token causes the second call to reattach
// to the session created by the first call.
func TestSelectCharacterReattachesOnSecondCallWithSameToken(t *testing.T) {
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

	sessionStore, pool := sessiontest.NewStoreWithPool(t)
	sessiontest.SeedPlayerSession(t, pool, ps)
	callCount := 0
	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
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

func TestCreatePlayerReturnsSessionTokenForNewAccount(t *testing.T) {
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
		presence: newTestPresenceEmitter(newTestEventStore()),

		sessionStore:      sessiontest.NewStore(t),
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

// replaces: TestCreatePlayer_ServiceNotConfigured,
//
//	TestCreatePlayer_UsernameTaken,
//	TestCreatePlayerReturnsGenericMessageForUnknownError,
//	TestCreatePlayerReturnsSanitizedMessageForUsernameTaken
func TestCreatePlayer_ErrorPaths(t *testing.T) {
	tests := []struct {
		name               string
		setupServer        func(t *testing.T) *CoreServer
		wantMsgContains    string
		wantNotEmptyMsg    bool
		wantMsgEquals      string
		wantMsgNotContains []string
	}{
		{
			name: "service not configured returns success=false with not configured message",
			setupServer: func(t *testing.T) *CoreServer {
				return &CoreServer{
					presence:     newTestPresenceEmitter(newTestEventStore()),
					sessionStore: sessiontest.NewStore(t),
				}
			},
			wantMsgContains: "not configured",
		},
		{
			name: "username taken returns success=false with non-empty error message",
			setupServer: func(t *testing.T) *CoreServer {
				authSvc := newMockAuthService(t)
				authSvc.createPlayerFunc = func(_ context.Context, _, _, _ string) (*auth.Player, *auth.PlayerSession, string, error) {
					return nil, nil, "", auth.ErrNotFound // simulates username taken via oops
				}
				return &CoreServer{
					presence:     newTestPresenceEmitter(newTestEventStore()),
					sessionStore: sessiontest.NewStore(t),
					authService:  authSvc,
				}
			},
			wantNotEmptyMsg: true,
		},
		{
			name: "unknown error returns generic message without leaking internal details",
			setupServer: func(t *testing.T) *CoreServer {
				authSvc := newMockAuthService(t)
				authSvc.createPlayerFunc = func(_ context.Context, _, _, _ string) (*auth.Player, *auth.PlayerSession, string, error) {
					// Plain error — no oops code. Client MUST NOT see the raw message.
					return nil, nil, "", errors.New("pq: relation \"players_private_v3\" does not exist")
				}
				return &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					authService:       authSvc,
					playerSessionRepo: authmocks.NewMockPlayerSessionRepository(t),
				}
			},
			wantMsgEquals:      msgGenericRequestFailed,
			wantMsgNotContains: []string{"players_private_v3", "pq:"},
		},
		{
			name: "oops username taken code returns sanitized message without leaking schema details",
			setupServer: func(t *testing.T) *CoreServer {
				authSvc := newMockAuthService(t)
				authSvc.createPlayerFunc = func(_ context.Context, _, _, _ string) (*auth.Player, *auth.PlayerSession, string, error) {
					return nil, nil, "", oopsCoded("REGISTER_USERNAME_TAKEN",
						"username \"alice\" is already taken in schema auth_v3",
						"operation", "check username availability")
				}
				return &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					authService:       authSvc,
					playerSessionRepo: authmocks.NewMockPlayerSessionRepository(t),
				}
			},
			wantMsgEquals:      msgRegisterUsernameTaken,
			wantMsgNotContains: []string{"schema", "operation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			server := tt.setupServer(t)

			resp, err := server.CreatePlayer(ctx, &corev1.CreatePlayerRequest{
				Username: "alice",
				Password: "strongpass1",
			})
			require.NoError(t, err)
			assert.False(t, resp.Success)

			if tt.wantMsgContains != "" {
				assert.Contains(t, resp.ErrorMessage, tt.wantMsgContains)
			}
			if tt.wantNotEmptyMsg {
				assert.NotEmpty(t, resp.ErrorMessage)
			}
			if tt.wantMsgEquals != "" {
				assert.Equal(t, tt.wantMsgEquals, resp.ErrorMessage)
			}
			for _, notContains := range tt.wantMsgNotContains {
				assert.NotContains(t, resp.ErrorMessage, notContains)
			}
		})
	}
}

// --- CreateCharacter ---

func TestCreateCharacterReturnsCharacterIDAndNameOnSuccess(t *testing.T) {
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
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
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

// replaces: TestCreateCharacter_InvalidSession,
//
//	TestCreateCharacter_NotConfigured,
//	TestCreateCharacterReturnsSanitizedMessageForNameTaken
func TestCreateCharacter_ErrorPaths(t *testing.T) {
	tests := []struct {
		name               string
		token              string
		setupServer        func(t *testing.T) *CoreServer
		wantMsgContains    string
		wantMsgEquals      string
		wantMsgNotContains []string
	}{
		{
			name:  "invalid session token returns success=false with invalid or expired message",
			token: "bad-token",
			setupServer: func(t *testing.T) *CoreServer {
				sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
				tokenHash := auth.HashSessionToken("bad-token")
				sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
					Return(nil, auth.ErrNotFound)
				return &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					playerSessionRepo: sessionRepo,
					characterService:  newMockCharacterService(t),
				}
			},
			wantMsgContains: "invalid or expired",
		},
		{
			name:  "player session repo not configured returns success=false with invalid or expired message",
			token: "some-token",
			setupServer: func(_ *testing.T) *CoreServer {
				// playerSessionRepo is nil — resolvePlayerSession returns error
				return &CoreServer{
					presence: newTestPresenceEmitter(newTestEventStore()),
				}
			},
			wantMsgContains: "invalid or expired",
		},
		{
			name:  "oops character name taken code returns sanitized message without leaking shard details",
			token: validToken,
			setupServer: func(t *testing.T) *CoreServer {
				playerID := ulid.Make()
				ps := makePlayerSession(playerID)
				sessionRepo := setupSessionRepo(t, ps)
				charSvc := newMockCharacterService(t)
				charSvc.createFunc = func(_ context.Context, _ ulid.ULID, _ string) (*world.Character, error) {
					return nil, oopsCoded("CHARACTER_NAME_TAKEN",
						"character \"Hero\" is already taken in shard char_shard_3",
						"shard", "char_shard_3")
				}
				return &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
					characterService:  charSvc,
				}
			},
			wantMsgEquals:      msgCharacterNameTaken,
			wantMsgNotContains: []string{"char_shard_3", "shard"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer(t)

			resp, err := server.CreateCharacter(context.Background(), &corev1.CreateCharacterRequest{
				PlayerSessionToken: tt.token,
				CharacterName:      "Hero",
			})
			require.NoError(t, err)
			assert.False(t, resp.Success)

			if tt.wantMsgContains != "" {
				assert.Contains(t, resp.ErrorMessage, tt.wantMsgContains)
			}
			if tt.wantMsgEquals != "" {
				assert.Equal(t, tt.wantMsgEquals, resp.ErrorMessage)
			}
			for _, notContains := range tt.wantMsgNotContains {
				assert.NotContains(t, resp.ErrorMessage, notContains)
			}
		})
	}
}

// Verifies: INV-CRYPTO-120
// Asserts registered gRPC CreateCharacter routes through CharacterService.CreateBound
// with reason "initial_bind" — so the binding is minted in the SAME transaction as
// the character + genesis envelope inside the genesis service (05-15). The handler
// no longer owns a separate binding INSERT.
func TestCreateCharacterMintsBindingViaGenesis(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charSvc := newMockCharacterService(t)
	charSvc.createFunc = func(_ context.Context, pid ulid.ULID, name string) (*world.Character, error) {
		require.Equal(t, playerID, pid)
		return &world.Character{ID: charID, PlayerID: pid, Name: name}, nil
	}

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		playerSessionRepo: sessionRepo,
		characterService:  charSvc,
	}

	resp, err := server.CreateCharacter(ctx, &corev1.CreateCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterName:      "Binding Hero",
	})
	require.NoError(t, err)
	require.True(t, resp.Success)

	// CreateBound was called with reason "initial_bind" — the genesis service mints
	// the binding atomically with the character + envelope.
	assert.Equal(t, "initial_bind", charSvc.lastBindReason)
	assert.Equal(t, charID.String(), resp.CharacterId)
}

// --- ListCharacters ---

// replaces: TestListCharacters_InvalidSession_ReturnsError,
//
//	TestListCharacters_NotConfigured_ReturnsError
func TestListCharacters_ErrorPaths(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *CoreServer
		token       string
	}{
		{
			name: "invalid session token returns error",
			setupServer: func(t *testing.T) *CoreServer {
				t.Helper()
				sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
				sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken("bad-token")).
					Return(nil, auth.ErrNotFound)
				return &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					playerSessionRepo: sessionRepo,
				}
			},
			token: "bad-token",
		},
		{
			name: "session repo not configured returns error",
			setupServer: func(t *testing.T) *CoreServer {
				t.Helper()
				return &CoreServer{
					presence: newTestPresenceEmitter(newTestEventStore()),
					// playerSessionRepo is nil
				}
			},
			token: "some-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer(t)
			_, err := server.ListCharacters(context.Background(), &corev1.ListCharactersRequest{
				PlayerSessionToken: tt.token,
			})
			assert.Error(t, err)
		})
	}
}

func TestListCharactersReturnsCharactersWithoutLocationWhenNoWorldQuerier(t *testing.T) {
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
		presence: newTestPresenceEmitter(newTestEventStore()),

		sessionStore:      sessiontest.NewStore(t),
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

// replaces: TestListCharacters_ResolvesLocationName,
//
//	TestListCharacters_LocationLookupFailure_OmitsLocation
func TestListCharacters_LocationDerivation(t *testing.T) {
	tests := []struct {
		name              string
		charName          string
		setupWorldQuerier func(locID ulid.ULID) WorldQuerier
		wantLastLocation  string
	}{
		{
			name:     "resolves location name when world querier succeeds",
			charName: "Bob",
			setupWorldQuerier: func(locID ulid.ULID) WorldQuerier {
				return &mockWorldQuerier{
					location: &world.Location{ID: locID, Name: "The Nexus"},
				}
			},
			wantLastLocation: "The Nexus",
		},
		{
			name:     "location lookup failure omits last location field",
			charName: "Carol",
			setupWorldQuerier: func(_ ulid.ULID) WorldQuerier {
				return &mockWorldQuerier{
					locErr: errors.New("db connection failed"),
				}
			},
			wantLastLocation: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			playerID := ulid.Make()
			charID := ulid.Make()
			locID := ulid.Make()

			ps := makePlayerSession(playerID)
			sessionRepo := setupSessionRepo(t, ps)

			charRepo := authmocks.NewMockCharacterRepository(t)
			charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
				Return([]*world.Character{
					{ID: charID, PlayerID: playerID, Name: tt.charName, LocationID: &locID},
				}, nil)

			server := &CoreServer{
				presence:          newTestPresenceEmitter(newTestEventStore()),
				sessionStore:      sessiontest.NewStore(t),
				playerSessionRepo: sessionRepo,
				charRepo:          charRepo,
				worldQuerier:      tt.setupWorldQuerier(locID),
			}

			resp, err := server.ListCharacters(ctx, &corev1.ListCharactersRequest{
				PlayerSessionToken: validToken,
			})
			require.NoError(t, err)
			require.Len(t, resp.Characters, 1)

			if tt.wantLastLocation != "" {
				assert.Equal(t, tt.wantLastLocation, resp.Characters[0].LastLocation)
			} else {
				assert.Empty(t, resp.Characters[0].LastLocation)
			}
		})
	}
}

// --- RequestPasswordReset ---

func TestRequestPasswordReset_AlwaysSuccess(t *testing.T) {
	ctx := context.Background()

	resetSvc := newMockResetService(t)
	resetSvc.requestResetFunc = func(_ context.Context, _ string) (string, error) {
		return "reset-token-123", nil
	}

	server := &CoreServer{
		presence: newTestPresenceEmitter(newTestEventStore()),

		sessionStore: sessiontest.NewStore(t),
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
		presence: newTestPresenceEmitter(newTestEventStore()),

		sessionStore: sessiontest.NewStore(t),
	}

	resp, err := server.RequestPasswordReset(ctx, &corev1.RequestPasswordResetRequest{
		Email: "alice@example.com",
	})
	require.NoError(t, err)

	// Always returns success to prevent enumeration, even if not configured.
	assert.True(t, resp.Success)
}

// --- ConfirmPasswordReset ---

func TestConfirmPasswordResetReturnsSuccessForValidToken(t *testing.T) {
	ctx := context.Background()

	resetSvc := newMockResetService(t)
	resetSvc.resetPasswordFunc = func(_ context.Context, token, newPassword string) error {
		require.Equal(t, "reset-token-123", token)
		require.Equal(t, "newstrongpass", newPassword)
		return nil
	}

	server := &CoreServer{
		presence: newTestPresenceEmitter(newTestEventStore()),

		sessionStore: sessiontest.NewStore(t),
		resetService: resetSvc,
	}

	resp, err := server.ConfirmPasswordReset(ctx, &corev1.ConfirmPasswordResetRequest{
		Token:       "reset-token-123",
		NewPassword: "newstrongpass",
	})
	require.NoError(t, err)

	assert.True(t, resp.Success)
}

// replaces: TestConfirmPasswordReset_InvalidToken,
//
//	TestConfirmPasswordResetReturnsSanitizedMessageForInvalidToken
func TestConfirmPasswordReset_InvalidTokenPaths(t *testing.T) {
	tests := []struct {
		name               string
		resetPasswordFunc  func(context.Context, string, string) error
		wantNotEmptyMsg    bool
		wantMsgEquals      string
		wantMsgNotContains []string
	}{
		{
			name: "invalid token returns success=false with non-empty error message",
			resetPasswordFunc: func(_ context.Context, _, _ string) error {
				return auth.ErrNotFound
			},
			wantNotEmptyMsg: true,
		},
		{
			name: "sanitized message hides internal details for coded reset token error",
			resetPasswordFunc: func(_ context.Context, _, _ string) error {
				return oopsCoded("RESET_TOKEN_INVALID",
					"reset token not found in table password_resets on host db.internal.svc:5432",
					"host", "db.internal.svc:5432")
			},
			wantMsgEquals:      msgResetTokenInvalid,
			wantMsgNotContains: []string{"db.internal.svc", "password_resets"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			resetSvc := newMockResetService(t)
			resetSvc.resetPasswordFunc = tt.resetPasswordFunc

			server := &CoreServer{
				presence:     newTestPresenceEmitter(newTestEventStore()),
				sessionStore: sessiontest.NewStore(t),
				resetService: resetSvc,
			}

			resp, err := server.ConfirmPasswordReset(ctx, &corev1.ConfirmPasswordResetRequest{
				Token:       "bad-token",
				NewPassword: "newstrongpass",
			})
			require.NoError(t, err)
			assert.False(t, resp.Success)

			if tt.wantNotEmptyMsg {
				assert.NotEmpty(t, resp.ErrorMessage)
			}
			if tt.wantMsgEquals != "" {
				assert.Equal(t, tt.wantMsgEquals, resp.ErrorMessage)
			}
			for _, notContains := range tt.wantMsgNotContains {
				assert.NotContains(t, resp.ErrorMessage, notContains)
			}
		})
	}
}

// --- Logout ---

// TestLogoutEmitsSessionEndedForEachChildGameSession verifies that when a
// player logs out with 2 active game sessions, the Logout handler emits a
// session_ended event (cause=logout) on each character's stream before
// delegating to authService.Logout. This closes the "orphaned Subscribe on
// logout" gap described in the session lifecycle spec.
func TestLogoutEmitsSessionEndedForEachChildGameSession(t *testing.T) {
	ctx := context.Background()
	rawToken := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	tokenHash := auth.HashSessionToken(rawToken)

	playerSessionID := ulid.Make()
	playerID := ulid.Make()

	ps := &auth.PlayerSession{
		ID:        playerSessionID,
		PlayerID:  playerID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Two game sessions belonging to the same PlayerSession.
	char1ID := core.NewULID()
	sess1ID := core.NewULID()
	char2ID := core.NewULID()
	sess2ID := core.NewULID()
	locID := ulid.Make()

	store := newTestEventStore()
	sessStore, pool := sessiontest.NewStoreWithPool(t)
	sessiontest.SeedPlayerSession(t, pool, ps)
	require.NoError(t, sessStore.Set(ctx, sess1ID.String(), &session.Info{
		ID:              sess1ID.String(),
		CharacterID:     char1ID,
		CharacterName:   "CharOne",
		LocationID:      locID,
		PlayerID:        playerID,
		PlayerSessionID: playerSessionID,
		Status:          session.StatusActive,

		TTLSeconds: 1800,
	}))
	require.NoError(t, sessStore.Set(ctx, sess2ID.String(), &session.Info{
		ID:              sess2ID.String(),
		CharacterID:     char2ID,
		CharacterName:   "CharTwo",
		LocationID:      locID,
		PlayerID:        playerID,
		PlayerSessionID: playerSessionID,
		Status:          session.StatusActive,

		TTLSeconds: 1800,
	}))

	// playerSessionRepo: Logout handler looks up the session by token hash
	// before fanout (one call). authService here is the test mock, so it does
	// not call GetByTokenHash internally — only the handler-level lookup counts.
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).Return(ps, nil).Once()

	// Expected character -> session mapping. Both children MUST have their
	// session_ended events on the correct character's stream with the correct
	// session ID before authService.Logout is called. Asserting inside
	// logoutFunc ensures the fanout completed before the handler delegated.
	expectedSessionByChar := map[string]string{
		char1ID.String(): sess1ID.String(),
		char2ID.String(): sess2ID.String(),
	}

	authSvc := newMockAuthService(t)
	authSvc.logoutFunc = func(logoutCtx context.Context, th string) (ulid.ULID, error) {
		require.Equal(t, tokenHash, th)
		authSvc.logoutCalled = true

		// Fanout must have completed before authService.Logout is called.
		// Assert the exact char -> session pairing for every child.
		for charIDStr, wantSessID := range expectedSessionByChar {
			stream := "character." + charIDStr
			events, replayErr := store.Replay(logoutCtx, stream, ulid.ULID{}, 100)
			require.NoError(t, replayErr)

			var found *eventbus.Event
			for i := range events {
				if events[i].Type == eventbus.Type(eventvocab.EventTypeSessionEnded) {
					found = &events[i]
					break
				}
			}
			require.NotNilf(t, found,
				"expected session_ended on stream %s before authService.Logout", stream)

			var payload core.SessionEndedPayload
			require.NoError(t, json.Unmarshal(found.Payload, &payload))
			assert.Equal(t, core.SessionEndedCauseLogout, payload.Cause,
				"session_ended on stream %s should have cause=logout", stream)
			assert.Equal(t, charIDStr, payload.CharacterID,
				"session_ended on stream %s must carry its own character id", stream)
			assert.Equal(t, wantSessID, payload.SessionID,
				"session_ended on stream %s must reference that character's session, not another's", stream)
		}

		return playerID, nil
	}

	server := &CoreServer{
		presence:          newTestPresenceEmitter(store),
		sessionStore:      sessStore,
		publisher:         store,
		authService:       authSvc,
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.Logout(ctx, &corev1.LogoutRequest{
		PlayerSessionToken: rawToken,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)

	// authService.Logout must have been called exactly once.
	assert.True(t, authSvc.logoutCalled, "authService.Logout was not called")
}

func TestLogoutHashesTokenAndCallsAuthServiceLogout(t *testing.T) {
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
		presence: newTestPresenceEmitter(newTestEventStore()),

		sessionStore: sessiontest.NewStore(t),
		authService:  authSvc,
	}

	resp, err := server.Logout(ctx, &corev1.LogoutRequest{
		PlayerSessionToken: rawToken,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// replaces: TestLogout_SessionNotFound,
//
//	TestLogout_NotConfigured
func TestLogout_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		setupServer func(t *testing.T) *CoreServer
		token       string
	}{
		{
			name: "session not found returns error",
			setupServer: func(t *testing.T) *CoreServer {
				t.Helper()
				authSvc := newMockAuthService(t)
				authSvc.logoutFunc = func(_ context.Context, _ string) (ulid.ULID, error) {
					return ulid.ULID{}, auth.ErrNotFound
				}
				return &CoreServer{
					presence:     newTestPresenceEmitter(newTestEventStore()),
					sessionStore: sessiontest.NewStore(t),
					authService:  authSvc,
				}
			},
			token: "some-token",
		},
		{
			name: "not configured returns error",
			setupServer: func(t *testing.T) *CoreServer {
				t.Helper()
				return &CoreServer{
					presence:     newTestPresenceEmitter(newTestEventStore()),
					sessionStore: sessiontest.NewStore(t),
				}
			},
			token: "some-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer(t)
			_, err := server.Logout(ctx, &corev1.LogoutRequest{PlayerSessionToken: tt.token})
			assert.Error(t, err)
		})
	}
}

// --- resolvePlayerSession ---

func TestResolvePlayerSession_RepoNotConfigured(t *testing.T) {
	server := &CoreServer{
		presence:     newTestPresenceEmitter(newTestEventStore()),
		sessionStore: sessiontest.NewStore(t),
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
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
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
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
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
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
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
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
				}
				return server, &corev1.CheckPlayerSessionRequest{PlayerSessionToken: "bad-token"}
			},
			expectErr: true,
		},
		{
			name: "session repo not configured",
			setup: func(t *testing.T) (*CoreServer, *corev1.CheckPlayerSessionRequest) {
				server := &CoreServer{
					presence:     newTestPresenceEmitter(newTestEventStore()),
					sessionStore: sessiontest.NewStore(t),
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
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
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

func TestCheckPlayerSessionPopulatesPlayerIDIsGuestAndCharactersOnSuccess(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).
		Return(&auth.Player{ID: playerID, Username: "Jasper Iodine", IsGuest: true}, nil)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{{ID: charID, PlayerID: playerID, Name: "Jasper Iodine"}}, nil)

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		playerSessionRepo: sessionRepo,
		playerRepo:        playerRepo,
		charRepo:          charRepo,
	}

	resp, err := server.CheckPlayerSession(ctx, &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: validToken,
	})

	require.NoError(t, err)
	assert.Equal(t, "Jasper Iodine", resp.GetPlayerName())
	assert.Equal(t, playerID.String(), resp.GetPlayerId())
	assert.True(t, resp.GetIsGuest())
	require.Len(t, resp.GetCharacters(), 1)
	assert.Equal(t, "Jasper Iodine", resp.GetCharacters()[0].GetCharacterName())
	assert.Equal(t, charID.String(), resp.GetCharacters()[0].GetCharacterId())
}

// TestCheckPlayerSession_ErrorTranslation consolidates the four error-path
// tests for CheckPlayerSession into a single table-driven test.
//
// replaces: TestCheckPlayerSessionAuthFailureTranslatesToCodesUnauthenticated,
//
//	TestCheckPlayerSessionInfraFailureNotTranslated,
//	TestCheckPlayerSessionWrapsPlayerLookupFailureAsPlayerLookupFailed,
//	TestCheckPlayerSessionWrapsCharacterLookupFailureAsCharacterLookupFailed
func TestCheckPlayerSession_ErrorTranslation(t *testing.T) {
	tests := []struct {
		name                   string
		token                  string
		setupServer            func(t *testing.T) *CoreServer
		wantStatusCode         codes.Code // assert gRPC status code == this when non-zero
		wantNotUnauthenticated bool       // assert status code (if present) != Unauthenticated
		wantOopsCode           string     // assert top-level oops code via ErrorAs when non-empty
	}{
		{
			name:  "known auth-failure oops code is translated to codes.Unauthenticated",
			token: "bad-token",
			setupServer: func(t *testing.T) *CoreServer {
				sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
				tokenHash := auth.HashSessionToken("bad-token")
				sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
					Return(nil, samberOops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("unknown token"))
				return &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
				}
			},
			wantStatusCode: codes.Unauthenticated,
		},
		{
			name: "infrastructure failure is not translated to Unauthenticated and carries NOT_CONFIGURED oops code",
			setupServer: func(t *testing.T) *CoreServer {
				// playerSessionRepo unset → resolvePlayerSession returns NOT_CONFIGURED.
				return &CoreServer{
					presence:     newTestPresenceEmitter(newTestEventStore()),
					sessionStore: sessiontest.NewStore(t),
				}
			},
			wantNotUnauthenticated: true,
			wantOopsCode:           "NOT_CONFIGURED",
		},
		{
			name: "player repo error is wrapped as PLAYER_LOOKUP_FAILED oops code",
			setupServer: func(t *testing.T) *CoreServer {
				playerID := ulid.Make()
				ps := makePlayerSession(playerID)
				sessionRepo := setupSessionRepo(t, ps)
				playerRepo := authmocks.NewMockPlayerRepository(t)
				playerRepo.EXPECT().GetByID(mock.Anything, playerID).
					Return(nil, errors.New("connection refused"))
				return &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
					playerRepo:        playerRepo,
				}
			},
			wantOopsCode: "PLAYER_LOOKUP_FAILED",
		},
		{
			name: "character repo error is wrapped as CHARACTER_LOOKUP_FAILED oops code",
			setupServer: func(t *testing.T) *CoreServer {
				playerID := ulid.Make()
				ps := makePlayerSession(playerID)
				sessionRepo := setupSessionRepo(t, ps)
				playerRepo := authmocks.NewMockPlayerRepository(t)
				playerRepo.EXPECT().GetByID(mock.Anything, playerID).
					Return(&auth.Player{ID: playerID, Username: "alice"}, nil)
				charRepo := authmocks.NewMockCharacterRepository(t)
				charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
					Return(nil, errors.New("char repo down"))
				return &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
					playerRepo:        playerRepo,
					charRepo:          charRepo,
				}
			},
			wantOopsCode: "CHARACTER_LOOKUP_FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			server := tt.setupServer(t)

			token := tt.token
			if token == "" {
				token = validToken
			}

			resp, err := server.CheckPlayerSession(ctx, &corev1.CheckPlayerSessionRequest{
				PlayerSessionToken: token,
			})

			assert.Nil(t, resp)
			require.Error(t, err)

			if tt.wantStatusCode != 0 {
				statusErr, ok := status.FromError(err)
				require.True(t, ok, "auth failure must be a gRPC status error")
				assert.Equal(t, tt.wantStatusCode, statusErr.Code())
			}

			if tt.wantNotUnauthenticated {
				statusErr, ok := status.FromError(err)
				if ok {
					assert.NotEqual(t, codes.Unauthenticated, statusErr.Code(),
						"infra failures must not be translated to Unauthenticated")
				}
			}

			if tt.wantOopsCode != "" {
				var oopsErr samberOops.OopsError
				require.ErrorAs(t, err, &oopsErr)
				assert.Equal(t, tt.wantOopsCode, oopsErr.Code())
			}
		})
	}
}

// TestIsPlayerSessionAuthError covers the predicate's branches that the
// multi-tab session isolation work (PR #271) added as part of the gateway's
// cookie-collision gate. It runs through each of the four branches:
//   - errors.Is matches auth.ErrNotFound (sentinel error → true)
//   - non-oops error → false
//   - oops error with a known auth-failure code → true
//   - oops error with an unrelated code → false
func TestIsPlayerSessionAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "returns true for auth.ErrNotFound sentinel",
			err:  auth.ErrNotFound,
			want: true,
		},
		{
			name: "returns false for plain stdlib error that is not an oops",
			err:  errors.New("transport flake"),
			want: false,
		},
		{
			name: "returns true for oops error coded PLAYER_SESSION_NOT_FOUND",
			err:  samberOops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("unknown token"),
			want: true,
		},
		{
			name: "returns true for oops error coded PLAYER_SESSION_EXPIRED",
			err:  samberOops.Code("PLAYER_SESSION_EXPIRED").Errorf("expired token"),
			want: true,
		},
		{
			name: "returns true for oops error coded NOT_CONFIGURED",
			err:  samberOops.Code("NOT_CONFIGURED").Errorf("session service not configured"),
			want: true,
		},
		{
			name: "returns false for oops error with unrelated code",
			err:  samberOops.Code("DATABASE_UNAVAILABLE").Errorf("pg down"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPlayerSessionAuthError(tt.err))
		})
	}
}

// --- Test helper mocks (lightweight, function-based) ---

// mockAuthServiceForHandlers wraps auth.Service methods used by handlers.
type mockAuthServiceForHandlers struct {
	t                       *testing.T
	validateCredentialsFunc func(ctx context.Context, username, password string) (*auth.Player, error)
	authenticatePlayerFunc  func(ctx context.Context, username, password, userAgent, ipAddress string) (string, *auth.Player, error)
	createPlayerFunc        func(ctx context.Context, username, password, email string) (*auth.Player, *auth.PlayerSession, string, error)
	logoutFunc              func(ctx context.Context, tokenHash string) (ulid.ULID, error)
	logoutCalled            bool
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

// mockCharacterServiceForHandlers wraps auth.CharacterService methods used by
// handlers. CreateBound records the bind reason and delegates to createFunc.
type mockCharacterServiceForHandlers struct {
	t              *testing.T
	createFunc     func(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error)
	lastBindReason string
}

func newMockCharacterService(t *testing.T) *mockCharacterServiceForHandlers {
	return &mockCharacterServiceForHandlers{t: t}
}

func (m *mockCharacterServiceForHandlers) CreateBound(ctx context.Context, playerID ulid.ULID, name, bindReason string) (*world.Character, error) {
	m.lastBindReason = bindReason
	if m.createFunc != nil {
		return m.createFunc(ctx, playerID, name)
	}
	m.t.Fatal("unexpected call to CharacterService.CreateBound")
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
// CreatePlayer and CreateCharacter sanitized-message cases are covered by
// TestCreatePlayer_ErrorPaths and TestCreateCharacter_ErrorPaths above.
// ConfirmPasswordReset sanitized-message case is covered by
// TestConfirmPasswordReset_InvalidTokenPaths above.

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
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
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

// replaces: TestListPlayerSessionsReturnsEmptyForInvalidToken,
//
//	TestListPlayerSessionsReturnsEmptyForExpiredSession
func TestListPlayerSessions_ReturnsEmptyPaths(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		setupRepo func(t *testing.T) *authmocks.MockPlayerSessionRepository
	}{
		{
			name:  "invalid token returns empty session list",
			token: "tok-not-valid",
			setupRepo: func(t *testing.T) *authmocks.MockPlayerSessionRepository {
				t.Helper()
				sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
				sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken("tok-not-valid")).
					Return(nil, auth.ErrNotFound)
				return sessionRepo
			},
		},
		{
			name:  "expired session returns empty session list",
			token: validToken,
			setupRepo: func(t *testing.T) *authmocks.MockPlayerSessionRepository {
				t.Helper()
				playerID := ulid.Make()
				expiredPS := &auth.PlayerSession{
					ID:        ulid.Make(),
					PlayerID:  playerID,
					TokenHash: auth.HashSessionToken(validToken),
					ExpiresAt: time.Now().Add(-time.Hour),
					CreatedAt: time.Now().Add(-2 * time.Hour),
					UpdatedAt: time.Now().Add(-time.Hour),
				}
				sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
				sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken(validToken)).
					Return(expiredPS, nil)
				return sessionRepo
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			sessionRepo := tt.setupRepo(t)

			server := &CoreServer{
				presence:          newTestPresenceEmitter(newTestEventStore()),
				sessionStore:      sessiontest.NewStore(t),
				playerSessionRepo: sessionRepo,
			}

			resp, err := server.ListPlayerSessions(ctx, &corev1.ListPlayerSessionsRequest{
				PlayerSessionToken: tt.token,
			})
			require.NoError(t, err)
			assert.Empty(t, resp.Sessions)
		})
	}
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
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.RevokePlayerSession(ctx, &corev1.RevokePlayerSessionRequest{
		PlayerSessionToken: validToken,
		TargetSessionId:    targetPS.ID.String(),
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

// replaces: TestRevokePlayerSessionRejectsForeignSession,
//
//	TestRevokePlayerSessionRejectsInvalidToken,
//	TestRevokePlayerSessionRejectsInvalidTargetID
func TestRevokePlayerSession_RejectsPaths(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		setup func(t *testing.T) (*CoreServer, *corev1.RevokePlayerSessionRequest)
	}{
		{
			name: "rejects foreign session owned by different player",
			setup: func(t *testing.T) (*CoreServer, *corev1.RevokePlayerSessionRequest) {
				t.Helper()
				playerA, playerB := ulid.Make(), ulid.Make()
				callerPS := &auth.PlayerSession{
					ID:        ulid.Make(),
					PlayerID:  playerA,
					TokenHash: auth.HashSessionToken(validToken),
					ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
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
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
				}
				return server, &corev1.RevokePlayerSessionRequest{
					PlayerSessionToken: validToken,
					TargetSessionId:    bPS.ID.String(),
				}
			},
		},
		{
			name: "rejects invalid token when caller session not found",
			setup: func(t *testing.T) (*CoreServer, *corev1.RevokePlayerSessionRequest) {
				t.Helper()
				sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
				sessionRepo.EXPECT().GetByTokenHash(mock.Anything, auth.HashSessionToken("invalid")).
					Return(nil, auth.ErrNotFound)
				server := &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
				}
				return server, &corev1.RevokePlayerSessionRequest{
					PlayerSessionToken: "invalid",
					TargetSessionId:    ulid.Make().String(),
				}
			},
		},
		{
			name: "rejects invalid target ID that fails ULID parse",
			setup: func(t *testing.T) (*CoreServer, *corev1.RevokePlayerSessionRequest) {
				t.Helper()
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
				// No GetByID expectation — "not-a-ulid" fails parse first.
				server := &CoreServer{
					presence:          newTestPresenceEmitter(newTestEventStore()),
					sessionStore:      sessiontest.NewStore(t),
					playerSessionRepo: sessionRepo,
				}
				return server, &corev1.RevokePlayerSessionRequest{
					PlayerSessionToken: validToken,
					TargetSessionId:    "not-a-ulid",
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, req := tt.setup(t)
			resp, err := server.RevokePlayerSession(ctx, req)
			require.NoError(t, err)
			assert.False(t, resp.Success)
			assert.Contains(t, resp.ErrorMessage, "session not found")
		})
	}
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
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
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
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
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
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
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
			presence: newTestPresenceEmitter(newTestEventStore()), sessionStore: sessiontest.NewStore(t),
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
			presence: newTestPresenceEmitter(newTestEventStore()), sessionStore: sessiontest.NewStore(t),
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
			presence: newTestPresenceEmitter(newTestEventStore()), sessionStore: sessiontest.NewStore(t),
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
			presence: newTestPresenceEmitter(newTestEventStore()), sessionStore: sessiontest.NewStore(t),
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

// =============================================================================
// Logout fanout error-branch tests (session lifecycle events, 9es6)
// =============================================================================

// TestLogoutProceedsWithoutFanoutWhenGetByTokenHashFails verifies that when the
// player session lookup fails (GetByTokenHash returns an error), Logout logs a
// warning and proceeds to call authService.Logout without doing any fanout.
func TestLogoutProceedsWithoutFanoutWhenGetByTokenHashFails(t *testing.T) {
	ctx := context.Background()
	rawToken := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	tokenHash := auth.HashSessionToken(rawToken)
	playerID := ulid.Make()

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).
		Return(nil, errors.New("db unavailable")).Once()

	authSvc := newMockAuthService(t)
	authSvc.logoutFunc = func(_ context.Context, _ string) (ulid.ULID, error) {
		authSvc.logoutCalled = true
		return playerID, nil
	}

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		publisher:         newTestEventStore(),
		authService:       authSvc,
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.Logout(ctx, &corev1.LogoutRequest{PlayerSessionToken: rawToken})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, authSvc.logoutCalled, "authService.Logout must still be called after lookup failure")
}

// TestLogoutProceedsWithoutFanoutWhenListByPlayerSessionFails verifies that when
// ListByPlayerSession returns an error, Logout logs a warning and still calls
// authService.Logout (no game sessions are individually closed, but the player
// session itself is deleted).
func TestLogoutProceedsWithoutFanoutWhenListByPlayerSessionFails(t *testing.T) {
	ctx := context.Background()
	rawToken := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	tokenHash := auth.HashSessionToken(rawToken)
	playerID := ulid.Make()
	playerSessionID := ulid.Make()

	ps := &auth.PlayerSession{
		ID:        playerSessionID,
		PlayerID:  playerID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).Return(ps, nil).Once()

	// Inject a mock session store that fails ListByPlayerSession.
	mockSessStore := sessionmocks.NewMockStore(t)
	mockSessStore.EXPECT().ListByPlayerSession(mock.Anything, []ulid.ULID{playerSessionID}).
		Return(nil, errors.New("session store unavailable")).Once()

	authSvc := newMockAuthService(t)
	authSvc.logoutFunc = func(_ context.Context, _ string) (ulid.ULID, error) {
		authSvc.logoutCalled = true
		return playerID, nil
	}

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      mockSessStore,
		publisher:         newTestEventStore(),
		authService:       authSvc,
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.Logout(ctx, &corev1.LogoutRequest{PlayerSessionToken: rawToken})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, authSvc.logoutCalled, "authService.Logout must still be called after list failure")
}

// TestLogoutFanoutContinuesAfterIndividualSessionErrors verifies that when
// HandleDisconnect, EndSession, or Delete fail for a child game session, the
// fanout loop continues (other sessions are still processed) and authService.Logout
// is still called.
func TestLogoutFanoutContinuesAfterIndividualSessionErrors(t *testing.T) {
	ctx := context.Background()
	rawToken := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
	tokenHash := auth.HashSessionToken(rawToken)
	playerID := ulid.Make()
	playerSessionID := ulid.Make()

	ps := &auth.PlayerSession{
		ID:        playerSessionID,
		PlayerID:  playerID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(auth.PlayerSessionTTL),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Two game sessions.
	char1ID := core.NewULID()
	sess1ID := core.NewULID().String()
	char2ID := core.NewULID()
	sess2ID := core.NewULID().String()
	locID := ulid.Make()

	// Make EndSession fail for first session by using a failing event store for it.
	// We'll use a Postgres-backed store pre-populated with both sessions; EndSession goes via
	// the engine. Use a mockEventStore that rejects Append for session_ended.
	// Track per-type append counts so we can assert the fanout attempted
	// EndSession for BOTH children (not just the first one before giving up).
	var appendMu sync.Mutex
	sessionEndedAppends := 0
	failingStore := &mockEventStore{
		publishFunc: func(_ context.Context, ev eventbus.Event) error {
			appendMu.Lock()
			if ev.Type == eventbus.Type(eventvocab.EventTypeSessionEnded) {
				sessionEndedAppends++
			}
			appendMu.Unlock()
			if ev.Type == eventbus.Type(eventvocab.EventTypeSessionEnded) {
				return errors.New("event store unavailable")
			}
			return nil
		},
	}

	sessStore, pool2 := sessiontest.NewStoreWithPool(t)
	sessiontest.SeedPlayerSession(t, pool2, ps)
	require.NoError(t, sessStore.Set(ctx, sess1ID, &session.Info{
		ID:              sess1ID,
		CharacterID:     char1ID,
		CharacterName:   "CharOne",
		LocationID:      locID,
		PlayerID:        playerID,
		PlayerSessionID: playerSessionID,
		Status:          session.StatusActive,

		TTLSeconds: 1800,
	}))
	require.NoError(t, sessStore.Set(ctx, sess2ID, &session.Info{
		ID:              sess2ID,
		CharacterID:     char2ID,
		CharacterName:   "CharTwo",
		LocationID:      locID,
		PlayerID:        playerID,
		PlayerSessionID: playerSessionID,
		Status:          session.StatusActive,

		TTLSeconds: 1800,
	}))

	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, tokenHash).Return(ps, nil).Once()

	authSvc := newMockAuthService(t)
	authSvc.logoutFunc = func(_ context.Context, _ string) (ulid.ULID, error) {
		authSvc.logoutCalled = true
		return playerID, nil
	}

	server := &CoreServer{
		presence:          newTestPresenceEmitter(failingStore),
		sessionStore:      sessStore,
		publisher:         failingStore,
		authService:       authSvc,
		playerSessionRepo: sessionRepo,
	}

	resp, err := server.Logout(ctx, &corev1.LogoutRequest{PlayerSessionToken: rawToken})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, authSvc.logoutCalled, "authService.Logout must still be called when individual session errors occur")

	// Both children must have had EndSession attempted — fanout must not stop
	// after the first failure.
	appendMu.Lock()
	defer appendMu.Unlock()
	assert.Equal(t, 2, sessionEndedAppends,
		"logout fanout must attempt EndSession for every child game session, not stop after the first failure")
}

// =============================================================================
// Guest session INV-PRIVACY-2 floor fields (iwzt.5)
// =============================================================================

// TestGuestSessionCarriesCharacterCreatedAt verifies that SelectCharacter for a
// guest player stamps a non-zero GuestCharacterCreatedAt on the created
// session.Info so that QueryStreamHistory has a temporal floor for INV-PRIVACY-2
// (per-session attach interval). IsGuest is intentionally NOT stamped — see
// holomush-hfvc for the disconnect-path redesign that will re-enable it.
func TestGuestSessionCarriesCharacterCreatedAt(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()
	charCreatedAt := time.Now().Add(-5 * time.Minute) // non-zero, fixed reference

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{
			{
				ID:         charID,
				PlayerID:   playerID,
				Name:       "Sapphire Diamond",
				LocationID: &locID,
				CreatedAt:  charCreatedAt,
			},
		}, nil)

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).
		Return(&auth.Player{
			ID:      playerID,
			IsGuest: true,
		}, nil)

	sessionStore, pool := sessiontest.NewStoreWithPool(t)
	sessiontest.SeedPlayerSession(t, pool, ps)
	sessionID := core.NewULID()

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessionStore,
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		playerRepo:        playerRepo,
		newSessionID:      func() ulid.ULID { return sessionID },
	}

	resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.NoError(t, err)
	require.True(t, resp.Success)

	info, err := sessionStore.Get(ctx, resp.SessionId)
	require.NoError(t, err)

	// Non-zero GuestCharacterCreatedAt is the INV-PRIVACY-2 guest-overlay signal
	// used by streamScopeFloor. We intentionally do NOT stamp IsGuest=true
	// here — that flag also drives the immediate-delete branch at
	// server.go::Disconnect:1260 which breaks page-reload reattach.
	// Redesign of that disconnect path is tracked separately.
	assert.False(t, info.GuestCharacterCreatedAt.IsZero(),
		"guest session must capture character.CreatedAt for INV-PRIVACY-2 floor")
	assert.WithinDuration(t, charCreatedAt, info.GuestCharacterCreatedAt, time.Second,
		"GuestCharacterCreatedAt must match character.CreatedAt")
}

// TestSelectCharacterArriveEventEmission asserts the arrive-event suppression
// rule: only "comms_hub" suppresses the arrive event; empty ClientType (legacy)
// and "terminal" both emit it.
//
// replaces: TestSelectCharacterSkipsArriveForCommsHubFreshSession,
//
//	TestSelectCharacterStillEmitsArriveByDefault,
//	TestSelectCharacterStillEmitsArriveForTerminalClientType
func TestSelectCharacterArriveEventEmission(t *testing.T) {
	tests := []struct {
		name            string
		clientType      string
		wantArriveCount int
		wantArriveEmit  string // description for assertion message
	}{
		{
			name:            "comms_hub client type suppresses arrive event and still creates session",
			clientType:      "comms_hub",
			wantArriveCount: 0,
			wantArriveEmit:  "comms_hub must not emit arrive event",
		},
		{
			name:            "empty client type emits arrive event (legacy behavior)",
			clientType:      "",
			wantArriveCount: 1,
			wantArriveEmit:  "empty client_type must emit exactly one arrive event",
		},
		{
			name:            "terminal client type emits arrive event (only comms_hub suppresses)",
			clientType:      "terminal",
			wantArriveCount: 1,
			wantArriveEmit:  "terminal client_type must emit exactly one arrive event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			sessionStore, pool := sessiontest.NewStoreWithPool(t)
			sessiontest.SeedPlayerSession(t, pool, ps)

			eventStore := newTestEventStore()
			server := &CoreServer{
				presence:          newTestPresenceEmitter(eventStore),
				sessionStore:      sessionStore,
				playerSessionRepo: sessionRepo,
				charRepo:          charRepo,
				newSessionID:      func() ulid.ULID { return sessionID },
			}

			resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
				PlayerSessionToken: validToken,
				CharacterId:        charID.String(),
				ClientType:         tt.clientType,
			})
			require.NoError(t, err)
			require.True(t, resp.Success)
			assert.False(t, resp.Reattached)

			events, err := eventStore.Replay(ctx, "location."+locID.String(), ulid.ULID{}, 100)
			require.NoError(t, err)
			var arriveCount int
			for i := range events {
				if events[i].Type == eventbus.Type(eventvocab.EventTypeArrive) {
					arriveCount++
				}
			}
			assert.Equal(t, tt.wantArriveCount, arriveCount, tt.wantArriveEmit)
		})
	}
}

// TestSelectCharacterGridPresent asserts the GridPresent field rule
// (holomush-5rh.8.9): comms_hub sessions are grid-absent; terminal sessions
// are grid-present. The EXISTS predicate in ListActiveByLocation is the
// authoritative presence gate; GridPresent is a consistency field.
//
// replaces: TestSelectCharacterCommsHubFreshSessionHasGridPresentFalse,
//
//	TestSelectCharacterTerminalFreshSessionHasGridPresentTrue
func TestSelectCharacterGridPresent(t *testing.T) {
	tests := []struct {
		name            string
		clientType      string
		wantGridPresent bool
	}{
		{
			name:            "comms_hub fresh session has grid present false",
			clientType:      "comms_hub",
			wantGridPresent: false,
		},
		{
			name:            "terminal fresh session has grid present true",
			clientType:      "terminal",
			wantGridPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			sessionStore, pool := sessiontest.NewStoreWithPool(t)
			sessiontest.SeedPlayerSession(t, pool, ps)

			server := &CoreServer{
				presence:          newTestPresenceEmitter(newTestEventStore()),
				sessionStore:      sessionStore,
				playerSessionRepo: sessionRepo,
				charRepo:          charRepo,
				newSessionID:      func() ulid.ULID { return sessionID },
			}

			resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
				PlayerSessionToken: validToken,
				CharacterId:        charID.String(),
				ClientType:         tt.clientType,
			})
			require.NoError(t, err)
			require.True(t, resp.Success)

			info, err := sessionStore.Get(ctx, sessionID.String())
			require.NoError(t, err)
			assert.Equal(t, tt.wantGridPresent, info.GridPresent,
				"%s fresh session MUST have GridPresent=%v (holomush-5rh.8.9)",
				tt.clientType, tt.wantGridPresent)
		})
	}
}

// --- ListAllCharacters ---

// Verifies: INV-ACCESS-9
func TestListAllCharactersDeniedWhenPolicyDenies(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	// Ownership check: ListByPlayer returns the requesting char so ownership passes.
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{{ID: charID, PlayerID: playerID, Name: "Alice"}}, nil)
	// ListAll MUST NOT be called when ABAC denies — the gate must block before the read.

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		accessEngine:      policytest.DenyAllEngine(),
	}

	_, err := server.ListAllCharacters(ctx, &corev1.ListAllCharactersRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

// TestListAllCharactersDeniedWhenEngineUnconfigured exercises the fail-closed
// nil-engine guard: a CoreServer with no accessEngine must deny (not panic),
// and must not reach the directory read.
func TestListAllCharactersDeniedWhenEngineUnconfigured(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	// Ownership passes; the nil-engine guard must fire before ListAll is reached.
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{{ID: charID, PlayerID: playerID, Name: "Alice"}}, nil)

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		// accessEngine intentionally nil → fail-closed default-deny.
	}

	_, err := server.ListAllCharacters(ctx, &corev1.ListAllCharactersRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestListAllCharactersReturnsDirectoryForAnyAuthenticatedCaller(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	charID := ulid.Make()

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	alice := &world.Character{ID: ulid.Make(), Name: "Alice"}
	bob := &world.Character{ID: ulid.Make(), Name: "Bob"}

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{{ID: charID, PlayerID: playerID, Name: "Guest"}}, nil)
	charRepo.EXPECT().ListAll(mock.Anything).Return([]*world.Character{alice, bob}, nil).Once()

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		accessEngine:      policytest.AllowAllEngine(),
	}

	resp, err := server.ListAllCharacters(ctx, &corev1.ListAllCharactersRequest{
		PlayerSessionToken: validToken,
		CharacterId:        charID.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetCharacters(), 2)
	assert.Equal(t, alice.ID.String(), resp.GetCharacters()[0].CharacterId)
	assert.Equal(t, "Alice", resp.GetCharacters()[0].Name)
	assert.Equal(t, bob.ID.String(), resp.GetCharacters()[1].CharacterId)
	assert.Equal(t, "Bob", resp.GetCharacters()[1].Name)
}

func TestListAllCharactersRejectsMissingToken(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()

	// Session repo will find no session for an empty/unknown token.
	ps := makePlayerSession(playerID)
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	// For an empty token the hash won't match any session.
	sessionRepo.EXPECT().
		GetByTokenHash(mock.Anything, mock.Anything).
		Return(nil, samberOops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("not found"))

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		playerSessionRepo: sessionRepo,
		accessEngine:      policytest.AllowAllEngine(),
	}
	_ = ps // declared for clarity; session repo is pre-wired to reject

	_, err := server.ListAllCharacters(ctx, &corev1.ListAllCharactersRequest{
		PlayerSessionToken: "",
		CharacterId:        ulid.Make().String(),
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestListAllCharactersRejectsNonOwnedCharacter(t *testing.T) {
	ctx := context.Background()
	playerID := ulid.Make()
	ownedCharID := ulid.Make()
	foreignCharID := ulid.Make() // not in this player's roster

	ps := makePlayerSession(playerID)
	sessionRepo := setupSessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).
		Return([]*world.Character{{ID: ownedCharID, PlayerID: playerID, Name: "Alice"}}, nil)

	server := &CoreServer{
		presence:          newTestPresenceEmitter(newTestEventStore()),
		sessionStore:      sessiontest.NewStore(t),
		playerSessionRepo: sessionRepo,
		charRepo:          charRepo,
		accessEngine:      policytest.AllowAllEngine(),
	}

	_, err := server.ListAllCharacters(ctx, &corev1.ListAllCharactersRequest{
		PlayerSessionToken: validToken,
		CharacterId:        foreignCharID.String(),
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}
