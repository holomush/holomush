// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/plugin/capability"
)

// Compile-time check: policy.Engine must satisfy types.AccessPolicyEngine.
var _ types.AccessPolicyEngine = (*policy.Engine)(nil)

// testContextKey is a type for context keys in tests.
type testContextKey string

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
	ac := policytest.NewGrantEngine()

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with character_id
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: all commands are returned as a Lua table
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result)

	resultTbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	commands := L.GetField(resultTbl, "commands")
	require.NotEqual(t, lua.LNil, commands, "commands field should exist")

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
	ac := policytest.NewGrantEngine()

	hf := New(nil, enforcer, WithEngine(ac)) // No WithCommandRegistry

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
			command.NewTestEntry(command.CommandEntryConfig{
				Name:         "say",
				Help:         "Say something to the room",
				Usage:        "say <message>",
				HelpText:     "# Say Command\n\nSay something that everyone in the room can hear.",
				Capabilities: []string{"communication.say"},
				Source:       "communication",
			}),
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
			command.NewTestEntry(command.CommandEntryConfig{Name: "say", Help: "Say something", Capabilities: []string{"comms.say"}, Source: "core"}),
			{Name: "look", Help: "Look around", Source: "core"}, // No capabilities required
			command.NewTestEntry(command.CommandEntryConfig{Name: "boot", Help: "Boot a player", Capabilities: []string{"admin.boot"}, Source: "admin"}),             // Admin only
			command.NewTestEntry(command.CommandEntryConfig{Name: "nuke", Help: "Dangerous", Capabilities: []string{"admin.nuke", "admin.danger"}, Source: "admin"}), // Multiple caps
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	// AccessControl that grants "comms.say" to our character but NOT admin caps
	charID := ulid.Make()
	ac := policytest.NewGrantEngine()
	ac.Grant(access.SubjectCharacter+charID.String(), "execute", "comms.say")

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with character_id
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: only commands the character can execute are returned
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result)

	resultTbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	commands := L.GetField(resultTbl, "commands")
	require.NotEqual(t, lua.LNil, commands, "commands field should exist")

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
			command.NewTestEntry(command.CommandEntryConfig{Name: "help", Help: "Get help", Capabilities: []string{}, Source: "core"}), // Empty slice
			{Name: "quit", Help: "Quit", Source: "core"}, // Nil slice (no capabilities)
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := policytest.NewGrantEngine() // No grants - character has zero capabilities

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: both commands are returned (no capabilities required)
	result := L.GetGlobal("result")
	resultTbl, ok := result.(*lua.LTable)
	require.True(t, ok)

	commands := L.GetField(resultTbl, "commands")
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
			command.NewTestEntry(command.CommandEntryConfig{Name: "nuke", Help: "Dangerous", Capabilities: []string{"admin.nuke", "admin.danger"}, Source: "admin"}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := policytest.NewGrantEngine()
	// Grant only ONE of the required capabilities
	ac.Grant(access.SubjectCharacter+charID.String(), "execute", "admin.nuke")
	// NOT granting admin.danger

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: nuke command should NOT be in results (needs BOTH caps)
	result := L.GetGlobal("result")
	resultTbl, ok := result.(*lua.LTable)
	require.True(t, ok)

	commands := L.GetField(resultTbl, "commands")
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
			command.NewTestEntry(command.CommandEntryConfig{Name: "nuke", Help: "Dangerous", Capabilities: []string{"admin.nuke", "admin.danger"}, Source: "admin"}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := policytest.NewGrantEngine()
	// Grant ALL required capabilities
	ac.Grant(access.SubjectCharacter+charID.String(), "execute", "admin.nuke")
	ac.Grant(access.SubjectCharacter+charID.String(), "execute", "admin.danger")

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: command IS included because character has both caps
	result := L.GetGlobal("result")
	resultTbl, ok := result.(*lua.LTable)
	require.True(t, ok)

	commands := L.GetField(resultTbl, "commands")
	tbl, ok := commands.(*lua.LTable)
	require.True(t, ok)

	count := 0
	tbl.ForEach(func(_, _ lua.LValue) {
		count++
	})
	assert.Equal(t, 1, count)
}

func TestListCommands_EngineError_HidesCapabilityCommands(t *testing.T) {
	// Given: commands with capabilities and an engine that always errors
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{Name: "boot", Help: "Boot a player", Capabilities: []string{"admin.boot"}, Source: "admin"}),
			{Name: "look", Help: "Look around", Source: "core"}, // No capabilities required
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	engineErr := errors.New("policy store unavailable")
	errorEngine := policytest.NewErrorEngine(engineErr)

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(errorEngine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: only commands without capabilities are returned (fail-closed)
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result)

	resultTbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	commands := L.GetField(resultTbl, "commands")
	require.NotEqual(t, lua.LNil, commands, "commands field should exist")

	tbl, ok := commands.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", commands)

	var names []string
	tbl.ForEach(func(_, v lua.LValue) {
		if cmdTbl, ok := v.(*lua.LTable); ok {
			names = append(names, L.GetField(cmdTbl, "name").String())
		}
	})

	assert.Contains(t, names, "look", "commands without capabilities should still appear")
	assert.NotContains(t, names, "boot", "commands with capabilities should be hidden when engine errors")
	assert.Len(t, names, 1)
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

	ac := policytest.NewGrantEngine()
	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

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

	ac := policytest.NewGrantEngine()
	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

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

func TestListCommands_NoEngineConfigured(t *testing.T) {
	// Given: no AccessPolicyEngine configured (nil)
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{Name: "say", Help: "Say something", Capabilities: []string{"comms.say"}}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	// NOT providing WithEngine
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
	assert.Contains(t, errVal.String(), "access engine not available")
}

// AccessRequest Verification Tests (PR #88 Priority 1)

func TestListCommands_VerifiesAccessRequest(t *testing.T) {
	// Given: a registry with a command requiring capabilities
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{
				Name:         "admin_cmd",
				Help:         "Admin command",
				Capabilities: []string{"admin.manage"},
				Source:       "admin",
			}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	subject := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	// Capture the AccessRequest using mock.MatchedBy
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(mockEngine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with character_id
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subject, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "execute", capturedRequest.Action, "action should be 'execute'")
	assert.Equal(t, "admin.manage", capturedRequest.Resource, "resource should be the capability")
}

func TestListCommands_EvaluateError_LogsErrorWithContext(t *testing.T) {
	// Given: a registry with a command requiring capabilities
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{
				Name:         "protected",
				Help:         "Protected command",
				Capabilities: []string{"admin.manage"},
				Source:       "core",
			}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	subject := access.CharacterSubject(charID.String())
	evalErr := errors.New("policy store unavailable")

	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	// Mock engine to return error for the capability evaluation
	mockEngine.EXPECT().Evaluate(mock.Anything, types.AccessRequest{
		Subject:  subject,
		Action:   "execute",
		Resource: "admin.manage",
	}).Return(types.Decision{}, evalErr)

	// Capture log output
	var logBuf bytes.Buffer
	oldLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
	slog.SetDefault(testLogger)
	defer slog.SetDefault(oldLogger)

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(mockEngine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Verify log output contains error and context
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "access evaluation failed", "log should mention access evaluation failure")
	assert.Contains(t, logOutput, subject, "log should contain subject")
	assert.Contains(t, logOutput, "execute", "log should contain action")
	assert.Contains(t, logOutput, "admin.manage", "log should contain resource (capability)")
	assert.Contains(t, logOutput, "policy store unavailable", "log should contain error message")
}

func TestListCommands_ExplicitDeny_FiltersCommands(t *testing.T) {
	// Given: a registry with commands requiring capabilities
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{Name: "say", Help: "Say something", Capabilities: []string{"comms.say"}, Source: "core"}),
			{Name: "look", Help: "Look around", Source: "core"}, // No capabilities required
			command.NewTestEntry(command.CommandEntryConfig{Name: "admin", Help: "Admin command", Capabilities: []string{"admin.manage"}, Source: "admin"}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	// DenyAllEngine returns EffectDeny with err == nil (explicit policy denial)
	charID := ulid.Make()
	denyEngine := policytest.DenyAllEngine()

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(denyEngine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with explicit deny engine
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: only commands with no capability requirements are returned
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result)

	resultTbl, ok := result.(*lua.LTable)
	require.True(t, ok)

	commands := L.GetField(resultTbl, "commands")
	require.NotEqual(t, lua.LNil, commands, "commands field should exist")

	tbl, ok := commands.(*lua.LTable)
	require.True(t, ok)

	// Collect returned command names
	var names []string
	tbl.ForEach(func(_, v lua.LValue) {
		if cmdTbl, ok := v.(*lua.LTable); ok {
			names = append(names, L.GetField(cmdTbl, "name").String())
		}
	})

	// Should include: look (no caps required)
	// Should NOT include: say (EffectDeny for comms.say), admin (EffectDeny for admin.manage)
	assert.Contains(t, names, "look", "commands without capabilities should be included")
	assert.NotContains(t, names, "say", "explicit deny should filter out command requiring comms.say")
	assert.NotContains(t, names, "admin", "explicit deny should filter out command requiring admin.manage")
	assert.Len(t, names, 1, "only commands without capability requirements should be included")
}

func TestListCommands_ThreadsLuaContext(t *testing.T) {
	// Given: a registry with a command requiring capabilities
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{
				Name:         "protected",
				Help:         "Protected command",
				Capabilities: []string{"admin.manage"},
				Source:       "core",
			}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	// Capture the context passed to Evaluate
	var capturedCtx context.Context
	mockEngine.EXPECT().Evaluate(mock.MatchedBy(func(ctx context.Context) bool {
		capturedCtx = ctx
		return true
	}), mock.Anything).Return(types.NewDecision(types.EffectDeny, "test", ""), nil)

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(mockEngine))

	L := lua.NewState()
	defer L.Close()

	// Set a context on the Lua state with a test value
	const testKey testContextKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")
	L.SetContext(parentCtx)

	hf.Register(L, "test-plugin")

	// When: list_commands is called
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: the context from L.Context() should be threaded to the engine
	require.NotNil(t, capturedCtx, "context should be passed to engine")
	assert.Equal(t, "test-value", capturedCtx.Value(testKey), "context should preserve values from L.Context()")
}

func TestListCommands_FallsBackToBackgroundContext(t *testing.T) {
	// Given: a registry with a command requiring capabilities
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{
				Name:         "protected",
				Help:         "Protected command",
				Capabilities: []string{"admin.manage"},
				Source:       "core",
			}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	// Capture the context passed to Evaluate
	var capturedCtx context.Context
	mockEngine.EXPECT().Evaluate(mock.MatchedBy(func(ctx context.Context) bool {
		capturedCtx = ctx
		return true
	}), mock.Anything).Return(types.NewDecision(types.EffectDeny, "test", ""), nil)

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(mockEngine))

	L := lua.NewState()
	defer L.Close()

	// Do NOT set context on L - should fall back to context.Background()

	hf.Register(L, "test-plugin")

	// When: list_commands is called without context set on L
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: context should not be nil (should have fallen back to context.Background())
	require.NotNil(t, capturedCtx, "context should not be nil even when L.Context() returns nil")
}

// F4: Incomplete metadata tests

func TestListCommands_IncompleteField_FalseWhenNoErrors(t *testing.T) {
	// Given: a registry with commands and an engine that succeeds
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{Name: "say", Help: "Say something", Capabilities: []string{"comms.say"}, Source: "core"}),
			{Name: "look", Help: "Look around", Source: "core"}, // No capabilities required
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := policytest.NewGrantEngine()
	ac.Grant(access.SubjectCharacter+charID.String(), "execute", "comms.say")

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with no engine errors
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: result.incomplete should be false
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result)

	tbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	incomplete := L.GetField(tbl, "incomplete")
	assert.Equal(t, lua.LFalse, incomplete, "incomplete should be false when no engine errors occur")

	// Verify commands array exists
	commands := L.GetField(tbl, "commands")
	require.NotEqual(t, lua.LNil, commands, "commands field should exist")
	cmdsTbl, ok := commands.(*lua.LTable)
	require.True(t, ok, "commands should be a table")

	count := 0
	cmdsTbl.ForEach(func(_, _ lua.LValue) {
		count++
	})
	assert.Equal(t, 2, count, "both commands should be included")
}

func TestListCommands_IncompleteField_TrueWhenEngineErrors(t *testing.T) {
	// Given: a registry with commands and an engine that always errors
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{Name: "boot", Help: "Boot a player", Capabilities: []string{"admin.boot"}, Source: "admin"}),
			{Name: "look", Help: "Look around", Source: "core"}, // No capabilities required
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	engineErr := errors.New("policy store unavailable")
	errorEngine := policytest.NewErrorEngine(engineErr)

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(errorEngine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with engine that errors
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: result.incomplete should be true
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result)

	tbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	incomplete := L.GetField(tbl, "incomplete")
	assert.Equal(t, lua.LTrue, incomplete, "incomplete should be true when engine errors occur")

	// Verify commands array exists with only non-capability commands
	commands := L.GetField(tbl, "commands")
	require.NotEqual(t, lua.LNil, commands, "commands field should exist")
	cmdsTbl, ok := commands.(*lua.LTable)
	require.True(t, ok, "commands should be a table")

	var names []string
	cmdsTbl.ForEach(func(_, v lua.LValue) {
		if cmdTbl, ok := v.(*lua.LTable); ok {
			names = append(names, L.GetField(cmdTbl, "name").String())
		}
	})

	assert.Contains(t, names, "look", "commands without capabilities should still appear")
	assert.NotContains(t, names, "boot", "commands with capabilities should be hidden when engine errors")
	assert.Len(t, names, 1)
}

func TestListCommands_IncompleteField_TrueWhenPartialErrors(t *testing.T) {
	// Given: a registry with multiple commands and an engine that errors for some
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{Name: "say", Help: "Say something", Capabilities: []string{"comms.say"}, Source: "core"}),
			{Name: "look", Help: "Look around", Source: "core"}, // No capabilities required
			command.NewTestEntry(command.CommandEntryConfig{Name: "boot", Help: "Boot a player", Capabilities: []string{"admin.boot"}, Source: "admin"}),
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	subject := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)

	// comms.say succeeds
	mockEngine.EXPECT().Evaluate(mock.Anything, types.AccessRequest{
		Subject:  subject,
		Action:   "execute",
		Resource: "comms.say",
	}).Return(types.NewDecision(types.EffectAllow, "test", ""), nil).Maybe()

	// admin.boot errors
	mockEngine.EXPECT().Evaluate(mock.Anything, types.AccessRequest{
		Subject:  subject,
		Action:   "execute",
		Resource: "admin.boot",
	}).Return(types.Decision{}, errors.New("policy store unavailable")).Maybe()

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(mockEngine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with partial engine errors
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: result.incomplete should be true
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result)

	tbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	incomplete := L.GetField(tbl, "incomplete")
	assert.Equal(t, lua.LTrue, incomplete, "incomplete should be true when any engine errors occur")

	// Verify commands array exists
	commands := L.GetField(tbl, "commands")
	require.NotEqual(t, lua.LNil, commands, "commands field should exist")
	cmdsTbl, ok := commands.(*lua.LTable)
	require.True(t, ok, "commands should be a table")

	var names []string
	cmdsTbl.ForEach(func(_, v lua.LValue) {
		if cmdTbl, ok := v.(*lua.LTable); ok {
			names = append(names, L.GetField(cmdTbl, "name").String())
		}
	})

	// Should include: say (granted), look (no caps required)
	// Should NOT include: boot (errored)
	assert.Contains(t, names, "say")
	assert.Contains(t, names, "look")
	assert.NotContains(t, names, "boot")
}

func TestListCommands_ReturnsErrorWhenEngineErrors(t *testing.T) {
	// Given: a registry with commands and an engine that always errors
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{Name: "boot", Help: "Boot a player", Capabilities: []string{"admin.boot"}, Source: "admin"}),
			{Name: "look", Help: "Look around", Source: "core"}, // No capabilities required
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	engineErr := errors.New("policy store unavailable")
	errorEngine := policytest.NewErrorEngine(engineErr)

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(errorEngine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with engine that errors
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: the second return value should be an error string (not lua.LNil)
	errVal := L.GetGlobal("err")
	assert.NotEqual(t, lua.LNil, errVal, "error return value should not be nil when engine errors")
	assert.Contains(t, errVal.String(), "access engine errors", "error message should indicate access engine errors")

	// AND the result table should still be returned with incomplete: true
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result, "result table should still be returned")

	tbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	incomplete := L.GetField(tbl, "incomplete")
	assert.Equal(t, lua.LTrue, incomplete, "incomplete should be true when engine errors occur")

	// Verify commands array exists with only non-capability commands
	commands := L.GetField(tbl, "commands")
	require.NotEqual(t, lua.LNil, commands, "commands field should exist")
	cmdsTbl, ok := commands.(*lua.LTable)
	require.True(t, ok, "commands should be a table")

	var names []string
	cmdsTbl.ForEach(func(_, v lua.LValue) {
		if cmdTbl, ok := v.(*lua.LTable); ok {
			names = append(names, L.GetField(cmdTbl, "name").String())
		}
	})

	assert.Contains(t, names, "look", "commands without capabilities should still appear")
	assert.NotContains(t, names, "boot", "commands with capabilities should be hidden when engine errors")
	assert.Len(t, names, 1)
}

func TestListCommands_NoErrorWhenEngineSucceeds(t *testing.T) {
	// Given: a registry with commands and an engine that succeeds
	registry := &mockCommandRegistry{
		commands: []command.CommandEntry{
			command.NewTestEntry(command.CommandEntryConfig{Name: "say", Help: "Say something", Capabilities: []string{"comms.say"}, Source: "core"}),
			{Name: "look", Help: "Look around", Source: "core"}, // No capabilities required
		},
	}

	enforcer := capability.NewEnforcer()
	require.NoError(t, enforcer.SetGrants("test-plugin", []string{"command.list"}))

	charID := ulid.Make()
	ac := policytest.NewGrantEngine()
	ac.Grant(access.SubjectCharacter+charID.String(), "execute", "comms.say")

	hf := New(nil, enforcer, WithCommandRegistry(registry), WithEngine(ac))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	// When: list_commands is called with no engine errors
	err := L.DoString(`
		result, err = holomush.list_commands("` + charID.String() + `")
	`)
	require.NoError(t, err)

	// Then: the second return value should be lua.LNil (no error)
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LNil, errVal, "error return value should be nil when no engine errors occur")

	// AND result.incomplete should be false
	result := L.GetGlobal("result")
	require.NotEqual(t, lua.LNil, result)

	tbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	incomplete := L.GetField(tbl, "incomplete")
	assert.Equal(t, lua.LFalse, incomplete, "incomplete should be false when no engine errors occur")
}
