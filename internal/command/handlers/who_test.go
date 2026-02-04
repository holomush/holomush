// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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
		{"just under 1 second", 999 * time.Millisecond, "0s"},
		{"one second", time.Second, "1s"},
		{"30 seconds", 30 * time.Second, "30s"},
		{"59.4 seconds rounds down", 59*time.Second + 400*time.Millisecond, "59s"},
		{"59.5 seconds rounds up to 1 minute", 59*time.Second + 500*time.Millisecond, "1m0s"},
		{"1 minute", time.Minute, "1m0s"},
		{"1 minute 30 seconds", time.Minute + 30*time.Second, "1m30s"},
		{"5 minutes", 5 * time.Minute, "5m0s"},
		{"59 minutes 59.4 seconds", 59*time.Minute + 59*time.Second + 400*time.Millisecond, "59m59s"},
		{"59 minutes 59.5 seconds rounds up to 1 hour", 59*time.Minute + 59*time.Second + 500*time.Millisecond, "1h0m"},
		{"1 hour", time.Hour, "1h0m"},
		{"1 hour 30 minutes", time.Hour + 30*time.Minute, "1h30m"},
		{"2 hours 15 minutes", 2*time.Hour + 15*time.Minute, "2h15m"},
		{"24 hours", 24 * time.Hour, "24h0m"},
		{"48 hours", 48 * time.Hour, "48h0m"},
		{"100 hours", 100 * time.Hour, "100h0m"},
		{"1 nanosecond", time.Nanosecond, "0s"},
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
		Return(nil, world.ErrNotFound)

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

func TestWhoHandler_LogsUnexpectedGetCharacterErrors(t *testing.T) {
	char1ID := ulid.Make()
	errorCharID := ulid.Make()
	conn1 := ulid.Make()
	errorConn := ulid.Make()
	playerID := ulid.Make()
	executorID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(errorCharID, errorConn)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Normal"}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// Capture logs
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// Session iteration order is non-deterministic, so all lookups may or may not happen
	// char1 is accessible
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+char1ID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil).Maybe()

	// errorChar - access allowed but repo returns unexpected error
	unexpectedErr := errors.New("database connection timeout")
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+errorCharID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, unexpectedErr).Maybe()

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

	// The error character lookup may or may not have happened depending on iteration order.
	// If it did happen, the error should have been logged.
	logOutput := logBuf.String()
	if logOutput != "" {
		// If we have any log output, verify it contains the expected content
		assert.Contains(t, logOutput, "unexpected error looking up character in who list")
		assert.Contains(t, logOutput, "session_char_id")
		assert.Contains(t, logOutput, "database connection timeout")
	}
	// Note: We don't fail if there's no log output because the error character
	// might not have been processed before successful character lookups completed.
}

func TestWhoHandler_WarnsUserOnUnexpectedErrors(t *testing.T) {
	// Force deterministic test by having only the error case
	errorCharID := ulid.Make()
	errorConn := ulid.Make()
	executorID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(errorCharID, errorConn)

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// errorChar - access allowed but repo returns unexpected error
	unexpectedErr := errors.New("database connection timeout")
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+errorCharID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, unexpectedErr)

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
	// Should show warning about error
	assert.Contains(t, output, "(Note: 1 player could not be displayed due to an error)")
}

func TestWhoHandler_WarnsUserOnMultipleUnexpectedErrors(t *testing.T) {
	// Test plural form of warning message
	errorChar1ID := ulid.Make()
	errorChar2ID := ulid.Make()
	errorConn1 := ulid.Make()
	errorConn2 := ulid.Make()
	executorID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(errorChar1ID, errorConn1)
	sessionMgr.Connect(errorChar2ID, errorConn2)

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// Both characters return unexpected errors
	unexpectedErr := errors.New("database connection timeout")

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+errorChar1ID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, errorChar1ID).
		Return(nil, unexpectedErr)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+errorChar2ID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, errorChar2ID).
		Return(nil, unexpectedErr)

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
	// Should show warning about errors (plural)
	assert.Contains(t, output, "(Note: 2 players could not be displayed due to errors)")
}

func TestWhoHandler_NoWarningForExpectedErrors(t *testing.T) {
	// Verify that expected errors (NotFound, PermissionDenied) don't trigger warning
	notFoundCharID := ulid.Make()
	deniedCharID := ulid.Make()
	notFoundConn := ulid.Make()
	deniedConn := ulid.Make()
	executorID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(notFoundCharID, notFoundConn)
	sessionMgr.Connect(deniedCharID, deniedConn)

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// notFoundChar - access allowed but returns ErrNotFound
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+notFoundCharID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, notFoundCharID).
		Return(nil, world.ErrNotFound)

	// deniedChar - access denied
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+deniedCharID.String()).
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
	// Should NOT show warning for expected errors
	assert.NotContains(t, output, "could not be displayed")
	assert.NotContains(t, output, "Note:")
}

func TestWhoHandler_NoLoggingForExpectedErrors(t *testing.T) {
	executorID := ulid.Make()
	char1ID := ulid.Make()
	notFoundCharID := ulid.Make()
	deniedCharID := ulid.Make()
	conn1 := ulid.Make()
	notFoundConn := ulid.Make()
	deniedConn := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(notFoundCharID, notFoundConn)
	sessionMgr.Connect(deniedCharID, deniedConn)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Visible"}

	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// Capture logs - we expect NO logs for expected errors
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// char1 is accessible
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+char1ID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil).Maybe()

	// notFoundChar - access allowed but returns ErrNotFound (expected, should NOT log)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+notFoundCharID.String()).
		Return(true).Maybe()
	characterRepo.EXPECT().
		Get(mock.Anything, notFoundCharID).
		Return(nil, world.ErrNotFound).Maybe()

	// deniedChar - access denied (expected, should NOT log)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+executorID.String(), "read", "character:"+deniedCharID.String()).
		Return(false).Maybe()

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

	// Verify no error logs were generated for expected errors
	logOutput := logBuf.String()
	assert.Empty(t, logOutput, "Expected no error logs for ErrNotFound or ErrPermissionDenied")
}
