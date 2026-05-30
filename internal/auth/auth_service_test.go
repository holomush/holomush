// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/core/coretest"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
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

	// Repository receives configured cap and atomically trims. Return a trimmed
	// ID to simulate the player having been over the cap prior to this call.
	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
		Return([]ulid.ULID{ulid.Make()}, nil).Once()

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

	// Below the cap: CreateWithCap returns empty slice (nothing to trim).
	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
		Return([]ulid.ULID(nil), nil).Once()

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
		Return([]ulid.ULID(nil), nil)

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
		Return([]ulid.ULID(nil), errors.New("db down")).Once()

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

// TestWithGameSessionFanoutIgnoresNilEngine verifies that when WithGameSessionFanout
// is called with a nil engine, the option is silently ignored and the service
// behaves as if fanout is not configured.
func TestWithGameSessionFanoutIgnoresNilEngine(t *testing.T) {
	playerRepo := mocks.NewMockPlayerRepository(t)
	playerSessionRepo := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	gameStore := sessionmocks.NewMockStore(t)

	// nil engine — fanout must be silently ignored.
	svc, err := auth.NewAuthService(
		playerRepo, playerSessionRepo, hasher,
		auth.WithGameSessionFanout(nil, gameStore),
	)
	require.NoError(t, err)
	require.NotNil(t, svc)
	// Service is functional; no game session calls should happen on login.
	_ = gameStore // the mock is strict — unused if not called; that is the assertion
}

// TestWithGameSessionFanoutIgnoresNilGameStore verifies that when WithGameSessionFanout
// is called with a nil game sessions store, the option is silently ignored.
func TestWithGameSessionFanoutIgnoresNilGameStore(t *testing.T) {
	playerRepo := mocks.NewMockPlayerRepository(t)
	playerSessionRepo := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)
	engine := core.NewEngine(coretest.NewMemoryEventStore())

	// nil gameSessions — fanout must be silently ignored.
	svc, err := auth.NewAuthService(
		playerRepo, playerSessionRepo, hasher,
		auth.WithGameSessionFanout(engine, nil),
	)
	require.NoError(t, err)
	require.NotNil(t, svc)
}

// TestConfigureGameSessionFanoutIgnoresNilArgs verifies that ConfigureGameSessionFanout
// is a no-op when either argument is nil, leaving fanout unconfigured.
func TestConfigureGameSessionFanoutIgnoresNilArgs(t *testing.T) {
	svc, _, _, _ := newTestAuthServiceWithCap(t, 0)

	// Both nil — no-op.
	svc.ConfigureGameSessionFanout(nil, nil)

	// nil engine — no-op.
	gameStore := sessionmocks.NewMockStore(t)
	svc.ConfigureGameSessionFanout(nil, gameStore)

	// nil gameSessions — no-op.
	engine := core.NewEngine(coretest.NewMemoryEventStore())
	svc.ConfigureGameSessionFanout(engine, nil)

	// All three calls must be no-ops: the service should work normally.
	// No panic and no unexpected calls on the mock.
	_ = gameStore
}

// TestAuthenticatePlayerEmitsSessionEndedForEvictedSessionChildren verifies
// that when CreateWithCap trims a PlayerSession and WithGameSessionFanout is
// configured, the service emits session_ended (cause=evicted) on the child
// game session's character stream before the FK cascade deletes it.
func TestAuthenticatePlayerEmitsSessionEndedForEvictedSessionChildren(t *testing.T) {
	ctx := context.Background()
	const capN = 2

	// --- set up in-memory event store + engine ---
	eventStore := coretest.NewMemoryEventStore()
	engine := core.NewEngine(eventStore)

	// --- set up session mock ---
	gameStore := sessionmocks.NewMockStore(t)

	// --- set up auth service mocks ---
	playerRepo := mocks.NewMockPlayerRepository(t)
	sessionRepo := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)

	svc, err := auth.NewAuthService(
		playerRepo,
		sessionRepo,
		hasher,
		auth.WithGameSessionFanout(engine, gameStore),
	)
	require.NoError(t, err)
	svc.SetMaxSessionsPerPlayer(capN)

	// Seed player + credentials.
	player := &auth.Player{
		ID:             ulid.Make(),
		Username:       "dave",
		PasswordHash:   "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
		FailedAttempts: 0,
		LockedUntil:    nil,
	}
	playerRepo.On("GetByUsername", ctx, "dave").Return(player, nil)
	hasher.On("Verify", "password", player.PasswordHash).Return(true, nil)
	playerRepo.On("Update", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)

	// Two active PlayerSessions exist at the cap.
	evictedPSID := ulid.Make()
	keptPSID := ulid.Make()
	activePSs := []*auth.PlayerSession{
		{ID: evictedPSID, PlayerID: player.ID},
		{ID: keptPSID, PlayerID: player.ID},
	}
	sessionRepo.On("ListByPlayer", ctx, player.ID).Return(activePSs, nil).Once()

	// The child game session belonging to the evicted PS.
	charID := ulid.Make()
	childSessionID := "child-session-01"
	gameStore.On("ListByPlayerSession", ctx, mock.MatchedBy(func(ids []ulid.ULID) bool {
		return len(ids) == 2
	})).Return([]*session.Info{{
		ID:              childSessionID,
		CharacterID:     charID,
		CharacterName:   "DaveChar",
		PlayerSessionID: evictedPSID,
	}}, nil).Once()

	// CreateWithCap returns the evicted PS ID.
	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
		Return([]ulid.ULID{evictedPSID}, nil).Once()

	tok, gotPlayer, authErr := svc.AuthenticatePlayer(ctx, "dave", "password", "ua", "ip")
	require.NoError(t, authErr)
	assert.NotEmpty(t, tok)
	require.NotNil(t, gotPlayer)

	// Verify a session_ended event was emitted on the child's character stream.
	stream := "character." + charID.String()
	events, replayErr := eventStore.Replay(ctx, stream, ulid.ULID{}, 100)
	require.NoError(t, replayErr)
	require.Len(t, events, 1, "expected exactly one session_ended event on character stream")

	assert.Equal(t, core.EventTypeSessionEnded, events[0].Type)

	var payload core.SessionEndedPayload
	require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
	assert.Equal(t, core.SessionEndedCauseEvicted, payload.Cause)
	assert.Equal(t, childSessionID, payload.SessionID)
}

// TestAuthenticatePlayerSkipsChildSessionsNotInTrimmedSet verifies that when the
// candidate children snapshot includes sessions belonging to a PlayerSession that
// was NOT trimmed (e.g. concurrent login created it just before the snapshot),
// those children are skipped — session_ended is only emitted for children whose
// PlayerSessionID is in the trimmedIDs set.
func TestAuthenticatePlayerSkipsChildSessionsNotInTrimmedSet(t *testing.T) {
	ctx := context.Background()
	const capN = 2

	eventStore := coretest.NewMemoryEventStore()
	engine := core.NewEngine(eventStore)
	gameStore := sessionmocks.NewMockStore(t)

	playerRepo := mocks.NewMockPlayerRepository(t)
	sessionRepo := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)

	svc, err := auth.NewAuthService(
		playerRepo,
		sessionRepo,
		hasher,
		auth.WithGameSessionFanout(engine, gameStore),
	)
	require.NoError(t, err)
	svc.SetMaxSessionsPerPlayer(capN)

	player := &auth.Player{
		ID:             ulid.Make(),
		Username:       "eve",
		PasswordHash:   "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
		FailedAttempts: 0,
	}
	playerRepo.On("GetByUsername", ctx, "eve").Return(player, nil)
	hasher.On("Verify", "password", player.PasswordHash).Return(true, nil)
	playerRepo.On("Update", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)

	evictedPSID := ulid.Make()
	keptPSID := ulid.Make()
	activePSs := []*auth.PlayerSession{
		{ID: evictedPSID, PlayerID: player.ID},
		{ID: keptPSID, PlayerID: player.ID},
	}
	sessionRepo.On("ListByPlayer", ctx, player.ID).Return(activePSs, nil).Once()

	// candidateChildren contains two sessions — one from the evicted PS and
	// one from the kept PS (which was NOT trimmed).
	evictedCharID := ulid.Make()
	keptCharID := ulid.Make()
	gameStore.On("ListByPlayerSession", ctx, mock.MatchedBy(func(ids []ulid.ULID) bool {
		return len(ids) == 2
	})).Return([]*session.Info{
		{
			ID:              "evicted-child",
			CharacterID:     evictedCharID,
			CharacterName:   "EveEvicted",
			PlayerSessionID: evictedPSID,
		},
		{
			ID:              "kept-child",
			CharacterID:     keptCharID,
			CharacterName:   "EveKept",
			PlayerSessionID: keptPSID, // NOT in trimmedIDs — must be skipped
		},
	}, nil).Once()

	// Only evictedPSID is trimmed.
	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
		Return([]ulid.ULID{evictedPSID}, nil).Once()

	tok, gotPlayer, authErr := svc.AuthenticatePlayer(ctx, "eve", "password", "ua", "ip")
	require.NoError(t, authErr)
	assert.NotEmpty(t, tok)
	require.NotNil(t, gotPlayer)

	// Evicted child should have session_ended.
	evictedStream := "character." + evictedCharID.String()
	evictedEvents, replayErr := eventStore.Replay(ctx, evictedStream, ulid.ULID{}, 100)
	require.NoError(t, replayErr)
	require.Len(t, evictedEvents, 1, "evicted child must have a session_ended event")
	assert.Equal(t, core.EventTypeSessionEnded, evictedEvents[0].Type)

	// Kept child must NOT have session_ended.
	keptStream := "character." + keptCharID.String()
	keptEvents, keptErr := eventStore.Replay(ctx, keptStream, ulid.ULID{}, 100)
	require.NoError(t, keptErr)
	assert.Empty(t, keptEvents, "kept child must not have a session_ended event")
}

// TestAuthenticatePlayerEvictionFanoutContinuesOnEndSessionError verifies that
// when EndSession fails for one evicted game session, the fanout loop continues
// to process subsequent sessions — the error is logged but not fatal.
func TestAuthenticatePlayerEvictionFanoutContinuesOnEndSessionError(t *testing.T) {
	ctx := context.Background()
	const capN = 1

	// Event store that rejects all Append calls — simulates EndSession failure.
	failingStore := &failingEventStore{}
	engine := core.NewEngine(failingStore)
	gameStore := sessionmocks.NewMockStore(t)

	playerRepo := mocks.NewMockPlayerRepository(t)
	sessionRepo := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)

	svc, err := auth.NewAuthService(
		playerRepo,
		sessionRepo,
		hasher,
		auth.WithGameSessionFanout(engine, gameStore),
	)
	require.NoError(t, err)
	svc.SetMaxSessionsPerPlayer(capN)

	player := &auth.Player{
		ID:             ulid.Make(),
		Username:       "frank",
		PasswordHash:   "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
		FailedAttempts: 0,
	}
	playerRepo.On("GetByUsername", ctx, "frank").Return(player, nil)
	hasher.On("Verify", "password", player.PasswordHash).Return(true, nil)
	playerRepo.On("Update", ctx, mock.AnythingOfType("*auth.Player")).Return(nil)

	evictedPSID := ulid.Make()
	activePSs := []*auth.PlayerSession{{ID: evictedPSID, PlayerID: player.ID}}
	sessionRepo.On("ListByPlayer", ctx, player.ID).Return(activePSs, nil).Once()

	child1CharID := ulid.Make()
	child2CharID := ulid.Make()
	gameStore.On("ListByPlayerSession", ctx, mock.MatchedBy(func(ids []ulid.ULID) bool {
		return len(ids) == 1
	})).Return([]*session.Info{
		{ID: "child-1", CharacterID: child1CharID, CharacterName: "C1", PlayerSessionID: evictedPSID},
		{ID: "child-2", CharacterID: child2CharID, CharacterName: "C2", PlayerSessionID: evictedPSID},
	}, nil).Once()

	sessionRepo.On("CreateWithCap", ctx, mock.AnythingOfType("*auth.PlayerSession"), capN).
		Return([]ulid.ULID{evictedPSID}, nil).Once()

	// AuthenticatePlayer must succeed even though EndSession fails for both children.
	tok, gotPlayer, authErr := svc.AuthenticatePlayer(ctx, "frank", "password", "ua", "ip")
	require.NoError(t, authErr)
	assert.NotEmpty(t, tok)
	require.NotNil(t, gotPlayer)

	// Assert that EndSession was attempted for BOTH evicted children — the
	// fanout loop must not stop after the first failure. One Append per
	// child's session_ended event means we expect exactly 2 calls.
	assert.Equal(t, 2, failingStore.SessionEndedAppendCount(),
		"fanout must attempt EndSession for every evicted child, not stop after the first failure")
}

// failingEventStore is a test double that rejects all Append calls. It counts
// Append calls per event type so tests can assert that the eviction fanout
// attempted EndSession for every child before giving up.
type failingEventStore struct {
	mu                  sync.Mutex
	sessionEndedAppends int
}

func (f *failingEventStore) Append(_ context.Context, ev core.Event) error {
	f.mu.Lock()
	if ev.Type == core.EventTypeSessionEnded {
		f.sessionEndedAppends++
	}
	f.mu.Unlock()
	return errors.New("event store down")
}

// SessionEndedAppendCount returns the number of Append attempts for
// session_ended events.
func (f *failingEventStore) SessionEndedAppendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessionEndedAppends
}

var _ core.EventAppender = (*failingEventStore)(nil)
