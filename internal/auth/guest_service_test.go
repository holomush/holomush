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
)

func TestNewGuestService_NilDeps(t *testing.T) {
	validNamer := mocks.NewMockGuestNamer(t)
	validPlayers := mocks.NewMockPlayerRepository(t)
	validChars := mocks.NewMockGuestCharacterRepository(t)
	validSessions := mocks.NewMockPlayerSessionRepository(t)

	tests := []struct {
		name     string
		namer    auth.GuestNamer
		players  auth.PlayerRepository
		chars    auth.GuestCharacterRepository
		sessions auth.PlayerSessionRepository
		wantErr  string
	}{
		{
			name:     "nil namer",
			namer:    nil,
			players:  validPlayers,
			chars:    validChars,
			sessions: validSessions,
			wantErr:  "guest namer is required",
		},
		{
			name:     "nil players",
			namer:    validNamer,
			players:  nil,
			chars:    validChars,
			sessions: validSessions,
			wantErr:  "players repository is required",
		},
		{
			name:     "nil chars",
			namer:    validNamer,
			players:  validPlayers,
			chars:    nil,
			sessions: validSessions,
			wantErr:  "character repository is required",
		},
		{
			name:     "nil sessions",
			namer:    validNamer,
			players:  validPlayers,
			chars:    validChars,
			sessions: nil,
			wantErr:  "player sessions repository is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := auth.NewGuestService(tt.namer, tt.players, tt.chars, tt.sessions)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestGuestService_CreateGuest_Success(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Sapphire_Diamond"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)

	charName := "Sapphire Diamond" // underscore→space conversion

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)

	chars.EXPECT().ExistsByName(ctx, charName).Return(false, nil).Once()
	players.EXPECT().Create(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	chars.EXPECT().Create(ctx, mock.AnythingOfType("*world.Character")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions)
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

func TestGuestService_CreateGuest_NameCollision(t *testing.T) {
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	takenName := "Ruby_Flame"
	freeName := "Jade_River"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)

	takenCharName := "Ruby Flame" // underscore→space form
	freeCharName := "Jade River"  // underscore→space form

	// First name is taken in DB; second name is free.
	namer.EXPECT().GenerateName().Return(takenName, nil).Once()
	chars.EXPECT().ExistsByName(ctx, takenCharName).Return(true, nil).Once()
	namer.EXPECT().ReleaseGuest(takenName).Once()

	namer.EXPECT().GenerateName().Return(freeName, nil).Once()
	chars.EXPECT().ExistsByName(ctx, freeCharName).Return(false, nil).Once()

	namer.EXPECT().StartLocation().Return(startLoc)
	players.EXPECT().Create(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	chars.EXPECT().Create(ctx, mock.AnythingOfType("*world.Character")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, freeName, result.Player.Username)
	assert.Equal(t, freeCharName, result.Character.Name)
}

func TestGuestService_CreateGuest_UpdateDefaultCharacterFailure(t *testing.T) {
	// Update failure is best-effort — CreateGuest must still succeed.
	ctx := context.Background()
	startLoc := ulid.MustNew(ulid.Now(), nil)
	guestName := "Coral_Breeze"

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	namer.EXPECT().StartLocation().Return(startLoc)
	chars.EXPECT().ExistsByName(ctx, "Coral Breeze").Return(false, nil).Once()
	players.EXPECT().Create(ctx, mock.AnythingOfType("*auth.Player")).Return(nil).Once()
	chars.EXPECT().Create(ctx, mock.AnythingOfType("*world.Character")).Return(nil).Once()
	players.EXPECT().Update(ctx, mock.AnythingOfType("*auth.Player")).Return(errors.New("db timeout")).Once()
	sessions.EXPECT().Create(ctx, mock.AnythingOfType("*auth.PlayerSession")).Return(nil).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestGuestService_CreateGuest_PlayerCreateError(t *testing.T) {
	ctx := context.Background()
	guestName := "Amber_Storm"
	dbErr := errors.New("db error")

	namer := mocks.NewMockGuestNamer(t)
	players := mocks.NewMockPlayerRepository(t)
	chars := mocks.NewMockGuestCharacterRepository(t)
	sessions := mocks.NewMockPlayerSessionRepository(t)

	namer.EXPECT().GenerateName().Return(guestName, nil).Once()
	chars.EXPECT().ExistsByName(ctx, "Amber Storm").Return(false, nil).Once()
	players.EXPECT().Create(ctx, mock.AnythingOfType("*auth.Player")).Return(dbErr).Once()
	namer.EXPECT().ReleaseGuest(guestName).Once()

	svc, err := auth.NewGuestService(namer, players, chars, sessions)
	require.NoError(t, err)

	result, err := svc.CreateGuest(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
}
