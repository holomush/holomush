// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/accesstest"
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
	// Given: a registry with commands (no capabilities required)
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "say", Help: "Say something", Usage: "say <message>", Source: "communication"},
			{Name: "look", Help: "Look around", Usage: "look [target]", Source: "core"},
			{Name: "quit", Help: "Disconnect", Usage: "quit", Source: "core"},
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	// Character can see all commands (no capabilities required on any command)
	charID := ulid.Make()
	ac := accesstest.NewMockAccessControl()

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithAccessControl(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with character_id
	err := L.DoString(`
		commands, err = holomush.list_commands("` + charID.String() + `")
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

	// Verify first command structure (order may vary, so check by name)
	var sayFound bool
	tbl.ForEach(func(_, v lua.LValue) {
		if cmdTbl, ok := v.(*lua.LTable); ok {
			if L.GetField(cmdTbl, "name").String() == "say" {
				sayFound = true
				assert.Equal(t, "Say something", L.GetField(cmdTbl, "help").String())
				assert.Equal(t, "say <message>", L.GetField(cmdTbl, "usage").String())
				assert.Equal(t, "communication", L.GetField(cmdTbl, "source").String())
			}
		}
	})
	assert.True(t, sayFound, "expected 'say' command in results")
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
	// Given: no command registry configured (but access control is available)
	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := accesstest.NewMockAccessControl()

	hf := New(nil, enforcer, WithAccessControl(ac)) // No WithCommandRegistry

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		commands, err = holomush.list_commands("` + charID.String() + `")
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

func TestListCommands_FiltersCommandsByCharacterCapabilities(t *testing.T) {
	// Given: a registry with commands having different capabilities
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "say", Help: "Say something", Capabilities: []string{"comms.say"}, Source: "core"},
			{Name: "look", Help: "Look around", Capabilities: nil, Source: "core"},                               // No capabilities required
			{Name: "boot", Help: "Boot a player", Capabilities: []string{"admin.boot"}, Source: "admin"},         // Admin only
			{Name: "nuke", Help: "Dangerous", Capabilities: []string{"admin.nuke", "admin.danger"}, Source: "admin"}, // Multiple caps
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	// AccessControl that grants "comms.say" to our character but NOT admin caps
	charID := ulid.Make()
	ac := accesstest.NewMockAccessControl()
	ac.Grant("char:"+charID.String(), "execute", "comms.say")

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithAccessControl(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with character_id
	err := L.DoString(`
		commands, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: only commands the character can execute are returned
	commands := L.GetGlobal("commands")
	require.NotEqual(t, lua.LNil, commands)

	tbl, ok := commands.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", commands)

	// Collect returned command names
	var names []string
	tbl.ForEach(func(_, v lua.LValue) {
		if cmdTbl, ok := v.(*lua.LTable); ok {
			names = append(names, L.GetField(cmdTbl, "name").String())
		}
	})

	// Should include: say (has comms.say), look (no caps required)
	// Should NOT include: boot (needs admin.boot), nuke (needs admin.nuke AND admin.danger)
	assert.Contains(t, names, "say")
	assert.Contains(t, names, "look")
	assert.NotContains(t, names, "boot")
	assert.NotContains(t, names, "nuke")
	assert.Len(t, names, 2)
}

func TestListCommands_EmptyCapabilitiesAlwaysIncluded(t *testing.T) {
	// Given: commands with empty capabilities slice (not nil)
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "help", Help: "Get help", Capabilities: []string{}, Source: "core"}, // Empty slice
			{Name: "quit", Help: "Quit", Capabilities: nil, Source: "core"},            // Nil slice
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := accesstest.NewMockAccessControl() // No grants - character has zero capabilities

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithAccessControl(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		commands, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: both commands are returned (no capabilities required)
	commands := L.GetGlobal("commands")
	tbl, ok := commands.(*lua.LTable)
	require.True(t, ok)

	count := 0
	tbl.ForEach(func(_, _ lua.LValue) {
		count++
	})
	assert.Equal(t, 2, count)
}

func TestListCommands_RequiresAllCapabilities_ANDLogic(t *testing.T) {
	// Given: a command requiring multiple capabilities (AND logic)
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "nuke", Help: "Dangerous", Capabilities: []string{"admin.nuke", "admin.danger"}, Source: "admin"},
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := accesstest.NewMockAccessControl()
	// Grant only ONE of the required capabilities
	ac.Grant("char:"+charID.String(), "execute", "admin.nuke")
	// NOT granting admin.danger

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithAccessControl(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		commands, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: nuke command should NOT be in results (needs BOTH caps)
	commands := L.GetGlobal("commands")
	tbl, ok := commands.(*lua.LTable)
	require.True(t, ok)

	count := 0
	tbl.ForEach(func(_, _ lua.LValue) {
		count++
	})
	assert.Equal(t, 0, count, "command requiring multiple capabilities should not appear when only one is granted")
}

func TestListCommands_WithAllCapabilitiesGranted(t *testing.T) {
	// Given: a command requiring multiple capabilities
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "nuke", Help: "Dangerous", Capabilities: []string{"admin.nuke", "admin.danger"}, Source: "admin"},
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := accesstest.NewMockAccessControl()
	// Grant ALL required capabilities
	ac.Grant("char:"+charID.String(), "execute", "admin.nuke")
	ac.Grant("char:"+charID.String(), "execute", "admin.danger")

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithAccessControl(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		commands, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: command IS included because character has both caps
	commands := L.GetGlobal("commands")
	tbl, ok := commands.(*lua.LTable)
	require.True(t, ok)

	count := 0
	tbl.ForEach(func(_, _ lua.LValue) {
		count++
	})
	assert.Equal(t, 1, count)
}

func TestListCommands_InvalidCharacterID(t *testing.T) {
	// Given: valid setup
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "say", Help: "Say something"},
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	ac := accesstest.NewMockAccessControl()
	hf := New(nil, enforcer, WithCommandRegistry(registry), WithAccessControl(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with invalid character ID
	err := L.DoString(`
		commands, err = holomush.list_commands("not-a-ulid")
	`)

	// Then: error is raised
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character ID")
}

func TestListCommands_EmptyCharacterID(t *testing.T) {
	// Given: valid setup
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "say", Help: "Say something"},
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	ac := accesstest.NewMockAccessControl()
	hf := New(nil, enforcer, WithCommandRegistry(registry), WithAccessControl(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with empty character ID
	err := L.DoString(`
		commands, err = holomush.list_commands("")
	`)

	// Then: error is raised
	require.Error(t, err)
	assert.Contains(t, err.Error(), "character ID cannot be empty")
}

func TestListCommands_NoAccessControlConfigured(t *testing.T) {
	// Given: no AccessControl configured (nil)
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			{Name: "say", Help: "Say something", Capabilities: []string{"comms.say"}},
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	// NOT providing WithAccessControl
	hf := New(nil, enforcer, WithCommandRegistry(registry))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	charID := ulid.Make()

	// When: list_commands is called with character_id
	err := L.DoString(`
		commands, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: error is returned (access control required for filtering)
	errVal := L.GetGlobal("err")
	assert.NotEqual(t, lua.LNil, errVal)
	assert.Contains(t, errVal.String(), "access control not available")
}
