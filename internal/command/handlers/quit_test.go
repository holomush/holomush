// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
)

func TestQuitHandlerWritesGoodbyeMessage(t *testing.T) {
	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   ulid.Make(),
		CharacterName: "Player",
		PlayerID:      ulid.Make(),
		Output:        &buf,
	})

	_ = QuitHandler(context.Background(), exec)

	assert.Contains(t, buf.String(), "Goodbye")
}

func TestQuitHandlerReturnsErrSessionEnded(t *testing.T) {
	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   ulid.Make(),
		CharacterName: "Player",
		PlayerID:      ulid.Make(),
		Output:        &buf,
	})

	err := QuitHandler(context.Background(), exec)

	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrSessionEnded), "error should wrap ErrSessionEnded")
}
