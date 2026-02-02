// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

func TestWhoHandler_NoConnectedPlayers(t *testing.T) {
	characterID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	// No sessions connected

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		Output:      &buf,
		Services:    &command.Services{Session: sessionMgr},
	}

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No players online")
}

func TestWhoHandler_SinglePlayer(t *testing.T) {
	characterID := ulid.Make()
	connID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(characterID, connID)

	char := &world.Character{
		ID:       characterID,
		PlayerID: playerID,
		Name:     "TestPlayer",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "character:"+characterID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, characterID).
		Return(char, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		Output:      &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
		},
	}

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "TestPlayer")
	assert.Contains(t, output, "1 player online")
}

func TestWhoHandler_MultiplePlayers(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	char3ID := ulid.Make()
	conn1 := ulid.Make()
	conn2 := ulid.Make()
	conn3 := ulid.Make()
	playerID := ulid.Make()
	executorID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(char2ID, conn2)
	sessionMgr.Connect(char3ID, conn3)

	chars := map[ulid.ULID]*world.Character{
		char1ID: {ID: char1ID, PlayerID: playerID, Name: "Alice"},
		char2ID: {ID: char2ID, PlayerID: playerID, Name: "Bob"},
		char3ID: {ID: char3ID, PlayerID: playerID, Name: "Charlie"},
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	for charID, char := range chars {
		accessControl.EXPECT().
			Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+charID.String()).
			Return(true)
		characterRepo.EXPECT().
			Get(mock.Anything, charID).
			Return(char, nil)
	}

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: executorID,
		Output:      &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
		},
	}

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Alice")
	assert.Contains(t, output, "Bob")
	assert.Contains(t, output, "Charlie")
	assert.Contains(t, output, "3 players online")
}

func TestWhoHandler_ShowsIdleTime(t *testing.T) {
	characterID := ulid.Make()
	connID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(characterID, connID)

	// Simulate 5 minutes of idle time by manipulating the session
	// We need to wait briefly to have a non-zero idle time
	time.Sleep(10 * time.Millisecond)

	char := &world.Character{
		ID:       characterID,
		PlayerID: playerID,
		Name:     "IdlePlayer",
	}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "character:"+characterID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, characterID).
		Return(char, nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		Output:      &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
		},
	}

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "IdlePlayer")
	// Should show idle time (at least "0s" or similar)
	assert.Regexp(t, `\d+[smh]`, output, "Should contain idle time format")
}

func TestWhoHandler_SkipsInaccessibleCharacters(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	conn1 := ulid.Make()
	conn2 := ulid.Make()
	playerID := ulid.Make()
	executorID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(char2ID, conn2)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Visible"}
	// char2 is not accessible due to access control, so we don't need a Character object

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// char1 is accessible
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+char1ID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil)

	// char2 is not accessible (access denied)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+char2ID.String()).
		Return(false)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: executorID,
		Output:      &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
		},
	}

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Visible")
	assert.NotContains(t, output, "Hidden")
	assert.Contains(t, output, "1 player online")
}

func TestFormatIdleTime(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"zero", 0, "0s"},
		{"sub-second", 500 * time.Millisecond, "0s"},
		{"one second", time.Second, "1s"},
		{"30 seconds", 30 * time.Second, "30s"},
		{"1 minute", time.Minute, "1m0s"},
		{"1 minute 30 seconds", time.Minute + 30*time.Second, "1m30s"},
		{"5 minutes", 5 * time.Minute, "5m0s"},
		{"1 hour", time.Hour, "1h0m"},
		{"1 hour 30 minutes", time.Hour + 30*time.Minute, "1h30m"},
		{"2 hours 15 minutes", 2*time.Hour + 15*time.Minute, "2h15m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatIdleTime(tt.duration)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestWhoHandler_SkipsCharacterNotFound(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	conn1 := ulid.Make()
	conn2 := ulid.Make()
	playerID := ulid.Make()
	executorID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(char2ID, conn2)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Existing"}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// char1 exists and is accessible
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+char1ID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil)

	// char2 check passes but character not found (stale session)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+char2ID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, char2ID).
		Return(nil, errors.New("not found"))

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: executorID,
		Output:      &buf,
		Services: &command.Services{
			Session: sessionMgr,
			World:   worldService,
		},
	}

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Existing")
	assert.Contains(t, output, "1 player online")
}
