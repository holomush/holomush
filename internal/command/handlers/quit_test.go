// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
)

func TestQuitHandler_OutputsGoodbyeMessage(t *testing.T) {
	characterID := ulid.Make()
	connID := ulid.Make()

	sessionManager := core.NewSessionManager()
	sessionManager.Connect(characterID, connID)

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{Session: sessionManager}),
	}

	err := QuitHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Goodbye")
}

func TestQuitHandler_EndsSession(t *testing.T) {
	characterID := ulid.Make()
	connID := ulid.Make()

	sessionManager := core.NewSessionManager()
	sessionManager.Connect(characterID, connID)
	require.NotNil(t, sessionManager.GetSession(characterID), "Session should exist before quit")

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{Session: sessionManager}),
	}

	err := QuitHandler(context.Background(), exec)
	require.NoError(t, err)

	// Session should be removed after quit
	assert.Nil(t, sessionManager.GetSession(characterID), "Session should not exist after quit")
}

func TestQuitHandler_ReturnsErrorOnSessionEndFailure(t *testing.T) {
	characterID := ulid.Make()

	// Don't create a session - EndSession will fail
	sessionManager := core.NewSessionManager()

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{Session: sessionManager}),
	}

	err := QuitHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify it returns a WorldError with player message
	msg := command.PlayerMessage(err)
	assert.NotEmpty(t, msg)
}

func TestQuitHandler_OutputsGoodbyeBeforeError(t *testing.T) {
	characterID := ulid.Make()

	// Don't create a session - EndSession will fail
	sessionManager := core.NewSessionManager()

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{Session: sessionManager}),
	}

	// Even though there's an error, goodbye should still be output
	_ = QuitHandler(context.Background(), exec)

	output := buf.String()
	assert.Contains(t, output, "Goodbye")
}
