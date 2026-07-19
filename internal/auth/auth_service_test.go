// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
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
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakePresenceEmitter is a hand-rolled auth.PresenceEmitter recording every
// EmitLeave/EmitSessionEnded call, standing in for *presence.Emitter without
// pulling internal/presence into this test's import graph — internal/auth
// declares its own consumer-defined interface for exactly this reason
// (FINDING-1: internal/eventbus's transitive closure contains internal/auth,
// so internal/auth importing internal/presence, which imports
// internal/eventbus, would create a cycle).
type fakePresenceEmitter struct {
	mu                sync.Mutex
	leaveCalls        []leaveCall
	sessionEndedCalls []sessionEndedCall
	sessionEndedErr   error
}

type leaveCall struct {
	char   core.CharacterRef
	reason string
}

type sessionEndedCall struct {
	char      core.CharacterRef
	sessionID string
	cause     string
	reason    string
}

func (f *fakePresenceEmitter) EmitLeave(_ context.Context, char core.CharacterRef, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaveCalls = append(f.leaveCalls, leaveCall{char: char, reason: reason})
	return nil
}

func (f *fakePresenceEmitter) EmitSessionEnded(_ context.Context, char core.CharacterRef, sessionID, cause, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionEndedCalls = append(f.sessionEndedCalls, sessionEndedCall{char: char, sessionID: sessionID, cause: cause, reason: reason})
	return f.sessionEndedErr
}

// sessionEndedCallsFor returns the EmitSessionEnded calls recorded for the
// given character ID, in call order.
func (f *fakePresenceEmitter) sessionEndedCallsFor(charID ulid.ULID) []sessionEndedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sessionEndedCall
	for _, c := range f.sessionEndedCalls {
		if c.char.ID == charID {
			out = append(out, c)
		}
	}
	return out
}

// sessionEndedCallCount returns the total number of EmitSessionEnded calls
// recorded, regardless of character.
func (f *fakePresenceEmitter) sessionEndedCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessionEndedCalls)
}

var _ auth.PresenceEmitter = (*fakePresenceEmitter)(nil)

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
	pres := &fakePresenceEmitter{}

	// nil gameSessions — fanout must be silently ignored.
	svc, err := auth.NewAuthService(
		playerRepo, playerSessionRepo, hasher,
		auth.WithGameSessionFanout(pres, nil),
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

	// nil presence emitter — no-op.
	gameStore := sessionmocks.NewMockStore(t)
	svc.ConfigureGameSessionFanout(nil, gameStore)

	// nil gameSessions — no-op.
	pres := &fakePresenceEmitter{}
	svc.ConfigureGameSessionFanout(pres, nil)

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

	// --- set up presence emitter fake ---
	pres := &fakePresenceEmitter{}

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
		auth.WithGameSessionFanout(pres, gameStore),
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

	// Verify a session_ended emission was recorded for the child's character.
	calls := pres.sessionEndedCallsFor(charID)
	require.Len(t, calls, 1, "expected exactly one session_ended emission for this character")
	assert.Equal(t, core.SessionEndedCauseEvicted, calls[0].cause)
	assert.Equal(t, childSessionID, calls[0].sessionID)
}

// TestAuthenticatePlayerSkipsChildSessionsNotInTrimmedSet verifies that when the
// candidate children snapshot includes sessions belonging to a PlayerSession that
// was NOT trimmed (e.g. concurrent login created it just before the snapshot),
// those children are skipped — session_ended is only emitted for children whose
// PlayerSessionID is in the trimmedIDs set.
func TestAuthenticatePlayerSkipsChildSessionsNotInTrimmedSet(t *testing.T) {
	ctx := context.Background()
	const capN = 2

	pres := &fakePresenceEmitter{}
	gameStore := sessionmocks.NewMockStore(t)

	playerRepo := mocks.NewMockPlayerRepository(t)
	sessionRepo := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)

	svc, err := auth.NewAuthService(
		playerRepo,
		sessionRepo,
		hasher,
		auth.WithGameSessionFanout(pres, gameStore),
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
	evictedCalls := pres.sessionEndedCallsFor(evictedCharID)
	require.Len(t, evictedCalls, 1, "evicted child must have a session_ended emission")

	// Kept child must NOT have session_ended.
	keptCalls := pres.sessionEndedCallsFor(keptCharID)
	assert.Empty(t, keptCalls, "kept child must not have a session_ended emission")
}

// TestAuthenticatePlayerEvictionFanoutContinuesOnEndSessionError verifies that
// when EndSession fails for one evicted game session, the fanout loop continues
// to process subsequent sessions — the error is logged but not fatal.
func TestAuthenticatePlayerEvictionFanoutContinuesOnEndSessionError(t *testing.T) {
	ctx := context.Background()
	const capN = 1

	// Presence emitter that rejects every EmitSessionEnded call — simulates
	// EmitSessionEnded failure.
	pres := &fakePresenceEmitter{sessionEndedErr: errors.New("event store down")}
	gameStore := sessionmocks.NewMockStore(t)

	playerRepo := mocks.NewMockPlayerRepository(t)
	sessionRepo := mocks.NewMockPlayerSessionRepository(t)
	hasher := mocks.NewMockPasswordHasher(t)

	svc, err := auth.NewAuthService(
		playerRepo,
		sessionRepo,
		hasher,
		auth.WithGameSessionFanout(pres, gameStore),
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

	// Assert that EmitSessionEnded was attempted for BOTH evicted children —
	// the fanout loop must not stop after the first failure.
	assert.Equal(t, 2, pres.sessionEndedCallCount(),
		"fanout must attempt EmitSessionEnded for every evicted child, not stop after the first failure")
}
