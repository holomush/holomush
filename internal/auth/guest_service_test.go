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

// passthroughTransactor returns a mock transactor that calls fn(ctx) directly,
// simulating a committed transaction. Use for success-path tests.
func passthroughTransactor(t *testing.T) *mocks.MockGuestTransactor {
	t.Helper()
	tr := mocks.NewMockGuestTransactor(t)
	tr.EXPECT().InTransaction(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	return tr
}

func TestNewGuestServiceNilDeps(t *testing.T) {
	validNamer := mocks.NewMockGuestNamer(t)
	validPlayers := mocks.NewMockPlayerRepository(t)
	validChars := mocks.NewMockGuestCharacterRepository(t)
	validSessions := mocks.NewMockPlayerSessionRepository(t)
	validTransactor := mocks.NewMockGuestTransactor(t)
	validBindings := mocks.NewMockGuestBindingCreator(t)

	tests := []struct {
		name       string
		namer      auth.GuestNamer
		players    auth.PlayerRepository
		chars      auth.GuestCharacterRepository
		sessions   auth.PlayerSessionRepository
		transactor auth.GuestTransactor
		bindings   auth.GuestBindingCreator
		wantErr    string
	}{
		{
			name:       "nil namer",
			namer:      nil,
			players:    validPlayers,
			chars:      validChars,
			sessions:   validSessions,
			transactor: validTransactor,
			bindings:   validBindings,
			wantErr:    "guest namer is required",
		},
		{
			name:       "nil players",
			namer:      validNamer,
			players:    nil,
			chars:      validChars,
			sessions:   validSessions,
			transactor: validTransactor,
			bindings:   validBindings,
			wantErr:    "players repository is required",
		},
		{
			name:       "nil chars",
			namer:      validNamer,
			players:    validPlayers,
			chars:      nil,
			sessions:   validSessions,
			transactor: validTransactor,
			bindings:   validBindings,
			wantErr:    "character repository is required",
		},
		{
			name:       "nil sessions",
			namer:      validNamer,
			players:    validPlayers,
			chars:      validChars,
			sessions:   nil,
			transactor: validTransactor,
			bindings:   validBindings,
			wantErr:    "player sessions repository is required",
		},
		{
			name:       "nil transactor",
			namer:      validNamer,
			players:    validPlayers,
			chars:      validChars,
			sessions:   validSessions,
			transactor: nil,
			bindings:   validBindings,
			wantErr:    "transactor is required",
		},
		{
			name:       "nil bindings",
			namer:      validNamer,
			players:    validPlayers,
			chars:      validChars,
			sessions:   validSessions,
			transactor: validTransactor,
			bindings:   nil,
			wantErr:    "binding creator is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewGuestService(tt.namer, tt.players, tt.chars, tt.sessions, tt.transactor, tt.bindings)
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
	transactor := passthroughTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

	charName := "Sapphire Diamond" // underscore→space conversion

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)

	chars.EXPECT().ExistsByName(ctx, charName).Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	chars.EXPECT().Create(mock.Anything, mock.AnythingOfType("*world.Character")).Return(nil).Once()
	bindings.EXPECT().Create(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), "initial_bind_guest").Return("bind-id-1", nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
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
	transactor := passthroughTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

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
	chars.EXPECT().Create(mock.Anything, mock.AnythingOfType("*world.Character")).Return(nil).Once()
	bindings.EXPECT().Create(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), "initial_bind_guest").Return("bind-id-2", nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, freeName, result.Player.Username)
	assert.Equal(t, freeCharName, result.Character.Name)
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
	transactor := passthroughTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Coral Breeze").Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	chars.EXPECT().Create(mock.Anything, mock.AnythingOfType("*world.Character")).Return(nil).Once()
	bindings.EXPECT().Create(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), "initial_bind_guest").Return("bind-id-3", nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(errors.New("db timeout")).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
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
	transactor := passthroughTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

	amberStartLoc := ulid.MustNew(ulid.Now(), nil)
	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(amberStartLoc)
	chars.EXPECT().ExistsByName(ctx, "Amber Storm").Return(false, nil).Once()
	// player.Create fails inside the transaction; rollback handles cleanup (no explicit Delete needed).
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(dbErr).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestGuestServiceReturnsErrorWhenCharCreateFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Topaz_Wind"
	startLoc := ulid.MustNew(ulid.Now(), nil)

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	transactor := passthroughTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Topaz Wind").Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	// chars.Create fails inside the transaction; rollback handles cleanup (no explicit players.Delete needed).
	chars.EXPECT().Create(mock.Anything, mock.AnythingOfType("*world.Character")).Return(errors.New("db error")).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")
}

func TestGuestServiceReturnsErrorWhenSessionCreateFails(t *testing.T) {
	ctx := context.Background()
	guestName := "Marble_Creek"
	startLoc := ulid.MustNew(ulid.Now(), nil)

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	transactor := passthroughTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Marble Creek").Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	chars.EXPECT().Create(mock.Anything, mock.AnythingOfType("*world.Character")).Return(nil).Once()
	bindings.EXPECT().Create(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), "initial_bind_guest").Return("bind-id-4", nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(errors.New("session db error")).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()
	// best-effort player cleanup after session create failure
	players.EXPECT().Delete(ctx, mock.AnythingOfType("ulid.ULID")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")
}

func TestGuestServiceReturnsErrorWhenNameExhausted(t *testing.T) {
	ctx := context.Background()

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	transactor := mocks.NewMockGuestTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

	// All 10 generated names already exist in the database.
	for range 10 {
		name := "Taken_Name"
		namer.EXPECT().GenerateName().Return(name, nil).Once()
		chars.EXPECT().ExistsByName(ctx, "Taken Name").Return(true, nil).Once()
		namer.EXPECT().ReleaseGuest(name).Once()
	}

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
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
	transactor := mocks.NewMockGuestTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	chars.EXPECT().ExistsByName(ctx, "Crystal Fog").Return(false, errors.New("db error")).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
	errutil.AssertErrorCode(t, err, "GUEST_CREATE_FAILED")
}

// Asserts guest creation mints a binding with reason "initial_bind_guest" in the
// same transaction, and that the returned binding ID is non-empty so a subsequent
// bindings.Current call resolves it (i.e., no orphan character row without a binding).
func TestCreateGuestMintsBinding(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Onyx_River"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)
	transactor := passthroughTransactor(t)
	bindings := mocks.NewMockGuestBindingCreator(t)

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Onyx River").Return(false, nil).Once()
	players.EXPECT().Create(mock.Anything, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()
	chars.EXPECT().Create(mock.Anything, mock.AnythingOfType("*world.Character")).Return(nil).Once()

	var (
		capturedPlayerID string
		capturedCharID   string
		capturedReason   string
	)
	bindings.EXPECT().
		Create(mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), "initial_bind_guest").
		RunAndReturn(func(_ context.Context, playerID, characterID, reason string) (string, error) {
			capturedPlayerID = playerID
			capturedCharID = characterID
			capturedReason = reason
			return "bind-guest-mint-1", nil
		}).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions, transactor, bindings)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Create was called with reason "initial_bind_guest" for the guest character.
	assert.Equal(t, "initial_bind_guest", capturedReason)
	assert.Equal(t, result.Character.ID.String(), capturedCharID)
	assert.Equal(t, result.Player.ID.String(), capturedPlayerID)
}
