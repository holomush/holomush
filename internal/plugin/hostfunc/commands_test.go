// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/plugin/capability"
)

// mockCommandRegistry implements CommandRegistry for testing.
type mockCommandRegistry struct {
	commands []command.CommandEntry
}

func (m *mockCommandRegistry) All() []command.CommandEntry {
	return m.commands
}

func (m *mockCommandRegistry) Get(name string) (command.CommandEntry, bool) {
	for _, cmd := range m.commands {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return command.CommandEntry{}, false
}

func TestListCommands_ReturnsAllCommands(t *testing.T) {
	// Given: a registry with commands
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "say", Help: "Say something", Usage: "say <message>", Source: "communication"},
			{Name: "look", Help: "Look around", Usage: "look [target]", Source: "core"},
			{Name: "quit", Help: "Disconnect", Usage: "quit", Source: "core"},
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	hf := New(nil, enforcer, WithCommandRegistry(registry))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		commands, err = holomush.list_commands()
	`)
	require.NoError(t, err)

	// Then: all commands are returned as a Lua table
	commands := L.GetGlobal("commands")
	require.NotEqual(t, lua.LNil, commands)

	tbl, ok := commands.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", commands)

	// Verify we got 3 commands
	count := 0
	tbl.ForEach(func(_, _ lua.LValue) {
		count++
	})
	assert.Equal(t, 3, count)

	// Verify first command structure
	first := L.GetTable(tbl, lua.LNumber(1))
	require.NotEqual(t, lua.LNil, first)

	firstTbl, ok := first.(*lua.LTable)
	require.True(t, ok)

	assert.Equal(t, "say", L.GetField(firstTbl, "name").String())
	assert.Equal(t, "Say something", L.GetField(firstTbl, "help").String())
	assert.Equal(t, "say <message>", L.GetField(firstTbl, "usage").String())
	assert.Equal(t, "communication", L.GetField(firstTbl, "source").String())
}

func TestListCommands_RequiresCapability(t *testing.T) {
	// Given: a registry with commands but NO capability granted
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "say", Help: "Say something"},
		},
	}

	enforcer := capability.NewEnforcer()
	// NOT granting command.list capability

	hf := New(nil, enforcer, WithCommandRegistry(registry))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called without capability
	err := L.DoString(`holomush.list_commands()`)

	// Then: capability error is raised
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capability denied")
}

func TestListCommands_NoRegistry(t *testing.T) {
	// Given: no command registry configured
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	hf := New(nil, enforcer) // No WithCommandRegistry

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		commands, err = holomush.list_commands()
	`)
	require.NoError(t, err)

	// Then: error is returned
	errVal := L.GetGlobal("err")
	assert.NotEqual(t, lua.LNil, errVal)
	assert.Contains(t, errVal.String(), "command registry not available")
}

func TestGetCommandHelp_ReturnsCommandDetails(t *testing.T) {
	// Given: a registry with a command that has detailed help
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{
				Name:         "say",
				Help:         "Say something to the room",
				Usage:        "say <message>",
				HelpText:     "# Say Command\n\nSay something that everyone in the room can hear.",
				Capabilities: []string{"communication.say"},
				Source:       "communication",
			},
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.help"}))

	hf := New(nil, enforcer, WithCommandRegistry(registry))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: get_command_help is called
	err := L.DoString(`
		info, err = holomush.get_command_help("say")
	`)
	require.NoError(t, err)

	// Then: detailed command info is returned
	info := L.GetGlobal("info")
	require.NotEqual(t, lua.LNil, info)

	tbl, ok := info.(*lua.LTable)
	require.True(t, ok)

	assert.Equal(t, "say", L.GetField(tbl, "name").String())
	assert.Equal(t, "Say something to the room", L.GetField(tbl, "help").String())
	assert.Equal(t, "say <message>", L.GetField(tbl, "usage").String())
	assert.Equal(t, "# Say Command\n\nSay something that everyone in the room can hear.", L.GetField(tbl, "help_text").String())
	assert.Equal(t, "communication", L.GetField(tbl, "source").String())

	// Check capabilities array
	caps := L.GetField(tbl, "capabilities")
	require.NotEqual(t, lua.LNil, caps)
	capsTbl, ok := caps.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "communication.say", L.GetTable(capsTbl, lua.LNumber(1)).String())
}

func TestGetCommandHelp_CommandNotFound(t *testing.T) {
	// Given: an empty registry
	registry := &mockCommandRegistry{commands: []command.CommandEntry{}}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.help"}))

	hf := New(nil, enforcer, WithCommandRegistry(registry))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: get_command_help is called for non-existent command
	err := L.DoString(`
		info, err = holomush.get_command_help("nonexistent")
	`)
	require.NoError(t, err)

	// Then: nil result with error message
	info := L.GetGlobal("info")
	assert.Equal(t, lua.LNil, info)

	errVal := L.GetGlobal("err")
	assert.NotEqual(t, lua.LNil, errVal)
	assert.Contains(t, errVal.String(), "command not found")
}

func TestGetCommandHelp_RequiresCapability(t *testing.T) {
	// Given: a registry with commands but NO capability granted
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "say", Help: "Say something"},
		},
	}

	enforcer := capability.NewEnforcer()
	// NOT granting command.help capability

	hf := New(nil, enforcer, WithCommandRegistry(registry))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: get_command_help is called without capability
	err := L.DoString(`holomush.get_command_help("say")`)

	// Then: capability error is raised
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capability denied")
}

func TestGetCommandHelp_NoRegistry(t *testing.T) {
	// Given: no command registry configured
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.help"}))

	hf := New(nil, enforcer) // No WithCommandRegistry

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: get_command_help is called
	err := L.DoString(`
		info, err = holomush.get_command_help("say")
	`)
	require.NoError(t, err)

	// Then: error is returned
	errVal := L.GetGlobal("err")
	assert.NotEqual(t, lua.LNil, errVal)
	assert.Contains(t, errVal.String(), "command registry not available")
}

func TestGetCommandHelp_EmptyCommandName(t *testing.T) {
	// Given: a valid setup
	registry := &mockCommandRegistry{commands: []command.CommandEntry{}}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.help"}))

	hf := New(nil, enforcer, WithCommandRegistry(registry))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: get_command_help is called with empty name
	err := L.DoString(`holomush.get_command_help("")`)

	// Then: error is raised
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command name cannot be empty")
}
