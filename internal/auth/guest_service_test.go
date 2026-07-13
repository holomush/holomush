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
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

// recordingGuestGenesis is a hand-rolled auth.CharacterGenesis fake for guest
// tests: it records the character and bind reason and returns the configured
// error (simulating the character + binding + envelope atomic unit).
type recordingGuestGenesis struct {
	err            error
	calls          int
	lastChar       *world.Character
	lastBindReason string
}

func (g *recordingGuestGenesis) Create(_ context.Context, char *world.Character, bindReason string) error {
	g.calls++
	g.lastChar = char
	g.lastBindReason = bindReason
	return g.err
}

// recordingGuestCleaner is a hand-rolled auth.GuestCleaner fake standing in for
// the tombstone-emitting CharacterReapingService in guest-service unit tests. It
// records the cleanup call so a test can assert failed-guest cleanup routes
// through the reaping service (not a raw player-cascade delete).
type recordingGuestCleaner struct {
	err    error
	calls  int
	lastID ulid.ULID
}

func (c *recordingGuestCleaner) DeleteGuestPlayer(_ context.Context, playerID ulid.ULID) error {
	c.calls++
	c.lastID = playerID
	return c.err
}

func TestNewGuestServiceNilDeps(t *testing.T) {
	validNamer := mocks.NewMockGuestNamer(t)
	validPlayers := mocks.NewMockPlayerRepository(t)
	validChars := mocks.NewMockGuestCharacterRepository(t)
	validSessions := mocks.NewMockPlayerSessionRepository(t)
	validGenesis := &recordingGuestGenesis{}
	validCleaner := &recordingGuestCleaner{}

	tests := []struct {
		name     string
		namer    auth.GuestNamer
		players  auth.PlayerRepository
		chars    auth.GuestCharacterRepository
		sessions auth.PlayerSessionRepository
		genesis  auth.CharacterGenesis
		cleaner  auth.GuestCleaner
		wantErr  string
	}{
		{"nil namer", nil, validPlayers, validChars, validSessions, validGenesis, validCleaner, "guest namer is required"},
		{"nil players", validNamer, nil, validChars, validSessions, validGenesis, validCleaner, "players repository is required"},
		{"nil chars", validNamer, validPlayers, nil, validSessions, validGenesis, validCleaner, "character repository is required"},
		{"nil sessions", validNamer, validPlayers, validChars, nil, validGenesis, validCleaner, "player sessions repository is required"},
		{"nil genesis", validNamer, validPlayers, validChars, validSessions, nil, validCleaner, "character genesis service is required"},
		{"nil cleaner", validNamer, validPlayers, validChars, validSessions, validGenesis, nil, "guest cleaner is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewGuestService(tt.namer, tt.players, tt.chars, tt.sessions, tt.genesis, tt.cleaner)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestGuestServiceCreatesGuestSuccessfully(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Sapphire_Diamond"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}

	charName := "Sapphire Diamond" // underscore→space conversion
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)

	chars.EXPECT().ExistsByName(ctx, charName).Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, guestName, result.Player.Username)
	assert.True(t, result.Player.IsGuest)
	assert.Equal(t, charName, result.Character.Name)
	assert.NotNil(t, result.Character.LocationID)
	assert.Equal(t, startLoc, *result.Character.LocationID)
	assert.NotEmpty(t, result.RawToken)
	assert.NotNil(t, result.PlayerSession)
	assert.Equal(t, result.Player.ID, result.PlayerSession.PlayerID)

	// Character routed through the genesis service with the guest binding reason.
	assert.Equal(t, 1, genesis.calls)
	assert.Equal(t, "initial_bind_guest", genesis.lastBindReason)
	assert.Equal(t, result.Character, genesis.lastChar)
}

func TestGuestServiceRetriesOnNameCollision(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	takenName := "Ruby_Flame"
	freeName := "Jade_River"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}

	cleaner := &recordingGuestCleaner{}
	takenCharName := "Ruby Flame" // underscore→space form
	freeCharName := "Jade River"  // underscore→space form

	// First name is taken in DB; second name is free.
	namer.EXPECT().GenerateName().Return(takenName, nil).Once()
	chars.EXPECT().ExistsByName(ctx, takenCharName).Return(true, nil).Once()
	namer.EXPECT().ReleaseGuest(takenName).Once()

	namer.EXPECT().GenerateName().Return(freeName, nil).Once()
	chars.EXPECT().ExistsByName(ctx, freeCharName).Return(false, nil).Once()

	namer.EXPECT().StartLocation().Return(startLoc)
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, freeName, result.Player.Username)
	assert.Equal(t, freeCharName, result.Character.Name)
	assert.Equal(t, 1, genesis.calls)
}

func TestGuestServiceSucceedsWhenDefaultCharacterUpdateFails(t *testing.T) {
	// Update failure is best-effort — CreateGuest must still succeed.
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Coral_Breeze"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Coral Breeze").Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(errors.New("db timeout")).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestGuestServiceReturnsErrorWhenPlayerCreateFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Amber_Storm"
	dbErr := errors.New("db error")

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}

	cleaner := &recordingGuestCleaner{}
	amberStartLoc := ulid.MustNew(ulid.Now(), nil)
	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(amberStartLoc)
	chars.EXPECT().ExistsByName(ctx, "Amber Storm").Return(false, nil).Once()
	// player.Create (committed first, own pool) fails -> release name, no genesis.
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(dbErr).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, 0, genesis.calls)
}

func TestGuestServiceReturnsErrorWhenCharCreateFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Topaz_Wind"
	startLoc := ulid.MustNew(ulid.Now(), nil)

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{err: errors.New("db error")}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Topaz Wind").Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	// genesis fails after the player commit -> release name + orphan-player cleanup
	// through the tombstone-emitting reaping service (D-06), NOT a raw player delete.
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")

	// Failed-guest cleanup routed through the reaping service (tombstone-emitting),
	// not a raw player-cascade delete.
	assert.Equal(t, 1, cleaner.calls)
}

func TestGuestServiceReturnsErrorWhenSessionCreateFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Marble_Creek"
	startLoc := ulid.MustNew(ulid.Now(), nil)

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Marble Creek").Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(errors.New("session db error")).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")

	// best-effort cleanup after session-create failure routes through the reaping
	// service (character tombstoned before player delete), not a raw player delete.
	assert.Equal(t, 1, cleaner.calls)
}

func TestGuestServiceReturnsErrorWhenNameExhausted(t *testing.T) {
	ctx := context.Background()

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	// All 10 generated names already exist in the database.
	for range 10 {
		name := "Taken_Name"
		namer.EXPECT().GenerateName().Return(name, nil).Once()
		chars.EXPECT().ExistsByName(ctx, "Taken Name").Return(true, nil).Once()
		namer.EXPECT().ReleaseGuest(name).Once()
	}

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_NAME_EXHAUSTED")
}

func TestGuestServiceReturnsErrorWhenExistsByNameFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Crystal_Fog"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	chars.EXPECT().ExistsByName(ctx, "Crystal Fog").Return(false, errors.New("db error")).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")
}

// Verifies: INV-CRYPTO-120
// Asserts guest creation routes the character through the genesis service with
// reason "initial_bind_guest" — so the binding is minted in the SAME transaction
// as the character + genesis envelope (no orphan character row without a binding).
func TestCreateGuestMintsBinding(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Onyx_River"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	genesis := &recordingGuestGenesis{}
	cleaner := &recordingGuestCleaner{}

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Onyx River").Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, genesis, cleaner)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Character routed through genesis with reason "initial_bind_guest" — the
	// genesis service mints the binding atomically with the character + envelope.
	assert.Equal(t, 1, genesis.calls)
	assert.Equal(t, "initial_bind_guest", genesis.lastBindReason)
	assert.Equal(t, result.Character.ID, genesis.lastChar.ID)
	assert.Equal(t, result.Player.ID, genesis.lastChar.PlayerID)
}
