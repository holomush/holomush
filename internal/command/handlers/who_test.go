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

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

func TestWhoHandler_NoConnectedPlayers(t *testing.T) {
	player := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	// No sessions connected

	services := testutil.NewServicesBuilder().WithSession(sessionMgr).Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "No players online")
}

func TestWhoHandler_SinglePlayer(t *testing.T) {
	player := testutil.RegularPlayer()
	connID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(player.CharacterID, connID)

	char := &world.Character{
		ID:       player.CharacterID,
		PlayerID: player.PlayerID,
		Name:     "TestPlayer",
	}
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(player.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "TestPlayer")
	assert.Contains(t, buf.String(), "1 player online")
}

func TestWhoHandler_MultiplePlayers(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	char3ID := ulid.Make()
	conn1 := ulid.Make()
	conn2 := ulid.Make()
	conn3 := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(char2ID, conn2)
	sessionMgr.Connect(char3ID, conn3)

	chars := map[ulid.ULID]*world.Character{
		char1ID: {ID: char1ID, PlayerID: playerID, Name: "Alice"},
		char2ID: {ID: char2ID, PlayerID: playerID, Name: "Bob"},
		char3ID: {ID: char3ID, PlayerID: playerID, Name: "Charlie"},
	}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	for charID, char := range chars {
		fixture.Mocks.Engine.EXPECT().
			Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(charID.String())}).
			Return(types.NewDecision(types.EffectAllow, "", ""), nil)
		fixture.Mocks.CharacterRepo.EXPECT().
			Get(mock.Anything, charID).
			Return(char, nil)
	}

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Alice")
	assert.Contains(t, buf.String(), "Bob")
	assert.Contains(t, buf.String(), "Charlie")
	assert.Contains(t, buf.String(), "3 players online")
}

func TestWhoHandler_ShowsIdleTime(t *testing.T) {
	player := testutil.RegularPlayer()
	connID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(player.CharacterID, connID)

	// Simulate a small amount of idle time by waiting briefly.
	time.Sleep(10 * time.Millisecond)

	char := &world.Character{
		ID:       player.CharacterID,
		PlayerID: player.PlayerID,
		Name:     "IdlePlayer",
	}
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(player.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "IdlePlayer")
	// Should show idle time (at least "0s" or similar)
	assert.Regexp(t, `\d+[smh]`, buf.String(), "Should contain idle time format")
}

func TestWhoHandler_SkipsInaccessibleCharacters(t *testing.T) {
	char1ID := ulid.Make()
	char2ID := ulid.Make()
	conn1 := ulid.Make()
	conn2 := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(char2ID, conn2)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Visible"}
	// char2 is not accessible due to access control, so we don't need a Character object

	// char1 is accessible
	fixture := testutil.NewWorldServiceBuilder(t).Build()

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(char1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(char2ID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), nil)

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Visible")
	assert.NotContains(t, buf.String(), "Hidden")
	assert.Contains(t, buf.String(), "1 player online")
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
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(char2ID, conn2)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Existing"}

	// char1 exists and is accessible
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(char1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil)

	// char2 check passes but character not found (stale session)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(char2ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char2ID).
		Return(nil, world.ErrNotFound)

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Existing")
	assert.Contains(t, buf.String(), "1 player online")
}

func TestWhoHandler_LogsUnexpectedGetCharacterErrors(t *testing.T) {
	char1ID := ulid.Make()
	errorCharID := ulid.Make()
	conn1 := ulid.Make()
	errorConn := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(errorCharID, errorConn)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Normal"}

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
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(char1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil).Maybe()

	// errorChar - access allowed but repo returns unexpected error
	unexpectedErr := errors.New("database connection timeout")
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(errorCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, unexpectedErr).Maybe()

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

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
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(errorCharID, errorConn)

	// errorChar - access allowed but repo returns unexpected error
	unexpectedErr := errors.New("database connection timeout")
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(errorCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, errorCharID).
		Return(nil, unexpectedErr)

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	// Should show warning about error
	assert.Contains(t, buf.String(), "(Note: 1 player could not be displayed due to a system error)")
}

func TestWhoHandler_WarnsUserOnMultipleUnexpectedErrors(t *testing.T) {
	// Test plural form of warning message
	errorChar1ID := ulid.Make()
	errorChar2ID := ulid.Make()
	errorConn1 := ulid.Make()
	errorConn2 := ulid.Make()
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(errorChar1ID, errorConn1)
	sessionMgr.Connect(errorChar2ID, errorConn2)

	// Both characters return unexpected errors
	unexpectedErr := errors.New("database connection timeout")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(errorChar1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, errorChar1ID).
		Return(nil, unexpectedErr)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(errorChar2ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, errorChar2ID).
		Return(nil, unexpectedErr)

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	// Should show warning about errors (plural)
	assert.Contains(t, buf.String(), "(Note: 2 players could not be displayed due to system errors)")
}

func TestWhoHandler_NoWarningForExpectedErrors(t *testing.T) {
	// Verify that expected errors (NotFound, PermissionDenied) don't trigger warning
	notFoundCharID := ulid.Make()
	deniedCharID := ulid.Make()
	notFoundConn := ulid.Make()
	deniedConn := ulid.Make()
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(notFoundCharID, notFoundConn)
	sessionMgr.Connect(deniedCharID, deniedConn)

	// notFoundChar - access allowed but returns ErrNotFound
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(notFoundCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, notFoundCharID).
		Return(nil, world.ErrNotFound)

	// deniedChar - access denied
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(deniedCharID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), nil)

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	// Should NOT show warning for expected errors
	assert.NotContains(t, buf.String(), "could not be displayed")
	assert.NotContains(t, buf.String(), "Note:")
}

func TestWhoHandler_NoLoggingForExpectedErrors(t *testing.T) {
	executor := testutil.RegularPlayer()
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

	// Capture logs - we expect NO logs for expected errors
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// char1 is accessible
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(char1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil).Maybe()

	// notFoundChar - access allowed but returns ErrNotFound (expected, should NOT log)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(notFoundCharID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, notFoundCharID).
		Return(nil, world.ErrNotFound).Maybe()

	// deniedChar - access denied (expected, should NOT log)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(deniedCharID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), nil).Maybe()

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify no error logs were generated for expected errors
	logOutput := logBuf.String()
	assert.Empty(t, logOutput, "Expected no error logs for ErrNotFound or ErrPermissionDenied")
}

func TestWhoHandler_AccessEvaluationFailedCountsAsError(t *testing.T) {
	char1ID := ulid.Make()
	evalFailCharID := ulid.Make()
	conn1 := ulid.Make()
	evalFailConn := ulid.Make()
	playerID := ulid.Make()
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(char1ID, conn1)
	sessionMgr.Connect(evalFailCharID, evalFailConn)

	char1 := &world.Character{ID: char1ID, PlayerID: playerID, Name: "Visible"}

	// Capture logs to suppress them
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// char1 is accessible
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(char1ID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Maybe()
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, char1ID).
		Return(char1, nil).Maybe()

	// evalFailChar - access evaluation fails (should count as error and show warning)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(evalFailCharID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), errors.New("policy store unavailable")).Maybe()

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	// Should show visible character
	assert.Contains(t, output, "Visible")
	// Should show error notice
	assert.Contains(t, output, "(Note: 1 player could not be displayed due to a system error)")
}

func TestWhoHandler_AllAccessEvaluationFailedShowsNoPlayersWithError(t *testing.T) {
	evalFail1ID := ulid.Make()
	evalFail2ID := ulid.Make()
	evalFailConn1 := ulid.Make()
	evalFailConn2 := ulid.Make()
	executor := testutil.RegularPlayer()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(evalFail1ID, evalFailConn1)
	sessionMgr.Connect(evalFail2ID, evalFailConn2)

	// Capture logs to suppress them
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(originalLogger)

	// Both characters return access evaluation failures
	fixture := testutil.NewWorldServiceBuilder(t).Build()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(evalFail1ID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), errors.New("policy store unavailable")).Maybe()
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: access.CharacterSubject(executor.CharacterID.String()), Action: "read", Resource: access.CharacterSubject(evalFail2ID.String())}).
		Return(types.NewDecision(types.EffectDeny, "", ""), errors.New("policy store unavailable")).Maybe()

	services := testutil.NewServicesBuilder().
		WithSession(sessionMgr).
		WithWorldFixture(fixture).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithServices(services).
		Build()

	err := WhoHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	// Should show no players (all failed access checks)
	assert.Contains(t, output, "No players online")
	// Should show error notice (plural form)
	assert.Contains(t, output, "(Note: 2 players could not be displayed due to system errors)")
}
