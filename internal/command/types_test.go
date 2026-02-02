// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandEntry_HasRequiredFields(t *testing.T) {
	entry := &CommandEntry{
		Name:         "say",
		Capabilities: []string{"rp:speak"},
		Help:         "Say something to the room",
		Usage:        "say <message>",
		HelpText:     "Speaks a message to everyone in the current location.",
		Source:       "core",
	}

	assert.Equal(t, "say", entry.Name)
	assert.Equal(t, []string{"rp:speak"}, entry.Capabilities)
	assert.Equal(t, "Say something to the room", entry.Help)
	assert.Equal(t, "say <message>", entry.Usage)
	assert.Equal(t, "Speaks a message to everyone in the current location.", entry.HelpText)
	assert.Equal(t, "core", entry.Source)
	assert.Nil(t, entry.Handler, "Handler should be nil when not set")
}

func TestCommandExecution_HasRequiredFields(t *testing.T) {
	exec := &CommandExecution{}

	// Verify all ULID fields are zero when not set
	assert.True(t, exec.CharacterID.IsZero(), "CharacterID should be zero when not set")
	assert.True(t, exec.LocationID.IsZero(), "LocationID should be zero when not set")
	assert.True(t, exec.PlayerID.IsZero(), "PlayerID should be zero when not set")
	assert.True(t, exec.SessionID.IsZero(), "SessionID should be zero when not set")

	// Verify string fields
	assert.Empty(t, exec.CharacterName, "CharacterName should be empty when not set")
	assert.Empty(t, exec.Args, "Args should be empty when not set")

	// Verify pointer fields
	assert.Nil(t, exec.Output, "Output should be nil when not set")
	assert.Nil(t, exec.Services, "Services should be nil when not set")
}

func TestServices_HasAllDependencies(t *testing.T) {
	svc := &Services{}

	assert.Nil(t, svc.World, "World service should be nil when not set")
	assert.Nil(t, svc.Session, "Session service should be nil when not set")
	assert.Nil(t, svc.Access, "Access service should be nil when not set")
	assert.Nil(t, svc.Events, "Events service should be nil when not set")
}

func TestCommandHandler_Signature(t *testing.T) {
	// Verify CommandHandler can be assigned a function with the correct signature
	var handler CommandHandler = func(_ context.Context, _ *CommandExecution) error {
		return nil
	}
	assert.NotNil(t, handler, "Handler should be assignable")
}
