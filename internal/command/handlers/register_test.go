// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
)

func TestRegisterAll_RegistersCoreCommands(t *testing.T) {
	reg := command.NewRegistry()

	RegisterAll(reg)

	// Verify all core commands are registered
	expectedCommands := []string{
		"look", "move", "quit", "who",
		"boot", "shutdown", "wall",
		"create", "set",
	}

	for _, name := range expectedCommands {
		cmd, ok := reg.Get(name)
		assert.True(t, ok, "command %s should be registered", name)
		assert.Equal(t, name, cmd.Name)
		assert.Equal(t, "core", cmd.Source)
		assert.NotEmpty(t, cmd.Help, "command %s should have help text", name)
		assert.NotEmpty(t, cmd.Usage, "command %s should have usage", name)
		assert.NotEmpty(t, cmd.HelpText, "command %s should have detailed help", name)
	}
}

func TestRegisterAll_CommandsHaveHandlers(t *testing.T) {
	reg := command.NewRegistry()

	RegisterAll(reg)

	commands := reg.All()
	for _, cmd := range commands {
		require.NotNil(t, cmd.Handler, "command %s should have a handler", cmd.Name)
	}
}

func TestRegisterAll_AdminCommandsHaveCapabilities(t *testing.T) {
	reg := command.NewRegistry()

	RegisterAll(reg)

	adminCommands := []struct {
		name       string
		capability string
	}{
		{"boot", "admin.boot"},
		{"shutdown", "admin.shutdown"},
		{"wall", "admin.wall"},
	}

	for _, tc := range adminCommands {
		cmd, ok := reg.Get(tc.name)
		require.True(t, ok, "command %s should be registered", tc.name)
		assert.Contains(t, cmd.Capabilities, tc.capability,
			"command %s should require capability %s", tc.name, tc.capability)
	}
}

func TestRegisterAll_ObjectCommandsHaveCapabilities(t *testing.T) {
	reg := command.NewRegistry()

	RegisterAll(reg)

	objectCommands := []struct {
		name       string
		capability string
	}{
		{"create", "objects.create"},
		{"set", "objects.set"},
	}

	for _, tc := range objectCommands {
		cmd, ok := reg.Get(tc.name)
		require.True(t, ok, "command %s should be registered", tc.name)
		assert.Contains(t, cmd.Capabilities, tc.capability,
			"command %s should require capability %s", tc.name, tc.capability)
	}
}
