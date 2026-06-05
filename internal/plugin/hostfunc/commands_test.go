// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
)

// These tests cover ONLY the list_commands / get_command_help host-function
// SHIMS — the thin Go↔Lua bridge over commandquery.Querier (design spec INV-COMMAND-1).
// The ABAC filter itself (deny/error/infra-failure/circuit-breaker/AND-logic,
// no-capability visibility, capability-gated denial) is exercised exhaustively
// in internal/command/commandquery/query_test.go and is NOT re-tested here.
//
// Shim responsibilities verified below:
//   (a) the Lua-table shape {commands:[{name,help,usage,source}], incomplete}
//       is built correctly from a Querier result;
//   (b) the incomplete→warning-string mapping;
//   (c) nil querier → "command registry not available";
//   (d) get_command_help Detail→Lua table mapping + typed-error
//       (NOT_FOUND/PERMISSION_DENIED/UNAVAILABLE) → legacy-string translation;
//   (e) the NOT-FOUND-wins-over-bad-character_id precedence.

// mockCommandRegistry implements commandquery.Registry for shim tests.
type mockCommandRegistry struct {
	commands []command.CommandEntry
}

func (m *mockCommandRegistry) All() []command.CommandEntry { return m.commands }

func (m *mockCommandRegistry) Get(name string) (command.CommandEntry, bool) {
	for _, cmd := range m.commands {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return command.CommandEntry{}, false
}

// newAllowQuerier builds a Querier over the given commands with an allow-all
// engine (the filter behavior is tested in commandquery; here we only need a
// querier that yields a populated, complete result).
func newAllowQuerier(commands []command.CommandEntry) *commandquery.Querier {
	registry := &mockCommandRegistry{commands: commands}
	return commandquery.New(registry, policytest.AllowAllEngine(), nil)
}

func TestListCommandsShimBuildsLuaTableFromQuerierResult(t *testing.T) {
	// (a) The shim maps a Querier result onto the {commands:[...], incomplete}
	// Lua table with name/help/usage/source per command.
	q := newAllowQuerier([]command.CommandEntry{
		{Name: "say", Help: "Say something", Usage: "say <message>", Source: "communication"},
		{Name: "look", Help: "Look around", Usage: "look [target]", Source: "core"},
	})
	charID := ulid.Make()

	hf := New(nil, WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.list_commands("` + charID.String() + `")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	resultTbl, ok := result.(*lua.LTable)
	require.True(t, ok, "expected table, got %T", result)

	// (b) incomplete=false → second return is nil.
	assert.Equal(t, lua.LFalse, L.GetField(resultTbl, "incomplete"))
	assert.Equal(t, lua.LNil, L.GetGlobal("err"), "err must be nil when result is complete")

	commands, ok := L.GetField(resultTbl, "commands").(*lua.LTable)
	require.True(t, ok)

	var sayFound bool
	count := 0
	commands.ForEach(func(_, v lua.LValue) {
		count++
		if cmdTbl, ok := v.(*lua.LTable); ok && L.GetField(cmdTbl, "name").String() == "say" {
			sayFound = true
			assert.Equal(t, "Say something", L.GetField(cmdTbl, "help").String())
			assert.Equal(t, "say <message>", L.GetField(cmdTbl, "usage").String())
			assert.Equal(t, "communication", L.GetField(cmdTbl, "source").String())
		}
	})
	assert.Equal(t, 2, count)
	assert.True(t, sayFound, "expected 'say' command in shim-built table")
}

func TestListCommandsShimMapsIncompleteToWarningString(t *testing.T) {
	// (b) When the Querier reports Incomplete (engine error on a gated command),
	// the shim sets incomplete=true and returns the warning string. The filtering
	// logic that produces Incomplete is tested in commandquery; here we only assert
	// the shim translates it.
	registry := &mockCommandRegistry{commands: []command.CommandEntry{
		command.NewTestEntry(command.CommandEntryConfig{
			Name: "boot", Help: "Boot a player",
			Capabilities: []command.Capability{{Action: "admin", Resource: "server", Scope: command.ScopeGlobal}},
			Source:       "admin",
		}),
		{Name: "look", Help: "Look around", Source: "core"},
	}}
	q := commandquery.New(registry, policytest.NewErrorEngine(errors.New("policy store unavailable")), nil)
	charID := ulid.Make()

	hf := New(nil, WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.list_commands("` + charID.String() + `")`)
	require.NoError(t, err)

	resultTbl, ok := L.GetGlobal("result").(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, lua.LTrue, L.GetField(resultTbl, "incomplete"))

	errVal := L.GetGlobal("err")
	assert.NotEqual(t, lua.LNil, errVal)
	assert.Contains(t, errVal.String(), "system error",
		"incomplete result must carry the warning string")
}

func TestListCommandsShimNilQuerierReturnsRegistryUnavailable(t *testing.T) {
	// (c) nil querier → "command registry not available".
	charID := ulid.Make()
	hf := New(nil) // no WithCommandQuerier
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`commands, err = holomush.list_commands("` + charID.String() + `")`)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.NotEqual(t, lua.LNil, errVal)
	assert.Contains(t, errVal.String(), "command registry not available")
}

func TestListCommandsShimEmptyCharacterIDRaises(t *testing.T) {
	hf := New(nil, WithCommandQuerier(newAllowQuerier(nil)))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`commands, err = holomush.list_commands("")`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "character ID cannot be empty")
}

func TestListCommandsShimInvalidCharacterIDRaises(t *testing.T) {
	hf := New(nil, WithCommandQuerier(newAllowQuerier(nil)))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`commands, err = holomush.list_commands("not-a-ulid")`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character ID")
}

func TestGetCommandHelpShimMapsDetailToLuaTable(t *testing.T) {
	// (d) Detail→Lua table mapping, including the structured capabilities array.
	registry := &mockCommandRegistry{commands: []command.CommandEntry{
		command.NewTestEntry(command.CommandEntryConfig{
			Name:         "say",
			Help:         "Say something to the room",
			Usage:        "say <message>",
			HelpText:     "# Say Command\n\nSay something that everyone in the room can hear.",
			Capabilities: []command.Capability{{Action: "emit", Resource: "stream", Scope: command.ScopeLocal}},
			Source:       "communication",
		}),
	}}
	charID := ulid.Make()
	ac := policytest.NewGrantEngine()
	ac.GrantCommandExecution(access.SubjectCharacter+charID.String(), "say")
	ac.Grant(access.SubjectCharacter+charID.String(), "emit", "stream")
	q := commandquery.New(registry, ac, nil)

	hf := New(nil, WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`info, err = holomush.get_command_help("say", "` + charID.String() + `")`)
	require.NoError(t, err)

	tbl, ok := L.GetGlobal("info").(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "say", L.GetField(tbl, "name").String())
	assert.Equal(t, "Say something to the room", L.GetField(tbl, "help").String())
	assert.Equal(t, "say <message>", L.GetField(tbl, "usage").String())
	assert.Equal(t, "# Say Command\n\nSay something that everyone in the room can hear.", L.GetField(tbl, "help_text").String())
	assert.Equal(t, "communication", L.GetField(tbl, "source").String())

	caps, ok := L.GetField(tbl, "capabilities").(*lua.LTable)
	require.True(t, ok)
	capEntry, ok := L.GetTable(caps, lua.LNumber(1)).(*lua.LTable)
	require.True(t, ok, "capability entry should be a table")
	assert.Equal(t, "emit", L.GetField(capEntry, "action").String())
	assert.Equal(t, "stream", L.GetField(capEntry, "resource").String())
	assert.Equal(t, "local", L.GetField(capEntry, "scope").String())

	assert.Equal(t, lua.LNil, L.GetGlobal("err"))
}

func TestGetCommandHelpShimTranslatesNotFound(t *testing.T) {
	// (d) NOT_FOUND → "command not found: <name>".
	registry := &mockCommandRegistry{commands: []command.CommandEntry{}}
	q := commandquery.New(registry, policytest.AllowAllEngine(), nil)
	charID := ulid.Make()

	hf := New(nil, WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`info, err = holomush.get_command_help("nonexistent", "` + charID.String() + `")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("info"))
	assert.Contains(t, L.GetGlobal("err").String(), "command not found")
}

func TestGetCommandHelpShimTranslatesPermissionDenied(t *testing.T) {
	// (d) PERMISSION_DENIED → "access denied".
	registry := &mockCommandRegistry{commands: []command.CommandEntry{
		command.NewTestEntry(command.CommandEntryConfig{
			Name:         "secret-cmd",
			Help:         "Secret command",
			Capabilities: []command.Capability{{Action: "admin", Resource: "server", Scope: command.ScopeGlobal}},
			Source:       "admin",
		}),
	}}
	q := commandquery.New(registry, policytest.NewGrantEngine(), nil) // no grants → deny
	charID := ulid.Make()

	hf := New(nil, WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`info, err = holomush.get_command_help("secret-cmd", "` + charID.String() + `")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("info"))
	assert.Contains(t, L.GetGlobal("err").String(), "access denied")
}

func TestGetCommandHelpShimTranslatesUnavailableToCheckFailed(t *testing.T) {
	// (d) UNAVAILABLE (engine error) → "access check failed".
	registry := &mockCommandRegistry{commands: []command.CommandEntry{
		command.NewTestEntry(command.CommandEntryConfig{
			Name:         "secret-cmd",
			Help:         "Secret command",
			Capabilities: []command.Capability{{Action: "admin", Resource: "server", Scope: command.ScopeGlobal}},
			Source:       "admin",
		}),
	}}
	q := commandquery.New(registry, policytest.NewErrorEngine(assert.AnError), nil)
	charID := ulid.Make()

	hf := New(nil, WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`info, err = holomush.get_command_help("secret-cmd", "` + charID.String() + `")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("info"))
	assert.Contains(t, L.GetGlobal("err").String(), "access check failed")
}

func TestGetCommandHelpShimNilQuerierReturnsRegistryUnavailable(t *testing.T) {
	// (c) nil querier → "command registry not available".
	charID := ulid.Make()
	hf := New(nil) // no WithCommandQuerier
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`info, err = holomush.get_command_help("say", "` + charID.String() + `")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("info"))
	assert.Contains(t, L.GetGlobal("err").String(), "command registry not available")
}

func TestGetCommandHelpShimEmptyCommandNameRaises(t *testing.T) {
	charID := ulid.Make()
	hf := New(nil, WithCommandQuerier(newAllowQuerier(nil)))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.get_command_help("", "` + charID.String() + `")`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command name cannot be empty")
}

func TestGetCommandHelpShimNotFoundWinsOverInvalidCharacterID(t *testing.T) {
	// (e) Precedence: a non-existent command must resolve to "command not found"
	// even when the character_id is malformed — the lookup (NOT_FOUND) happens
	// inside Querier.Help before the subject is consulted, so the shim must NOT
	// pre-parse/validate character_id ahead of the lookup.
	registry := &mockCommandRegistry{commands: []command.CommandEntry{
		{Name: "look", Help: "Look around", Source: "core"},
	}}
	q := commandquery.New(registry, policytest.AllowAllEngine(), nil)

	hf := New(nil, WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`info, err = holomush.get_command_help("nonexistent", "not-a-ulid")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("info"))
	assert.Contains(t, L.GetGlobal("err").String(), "command not found",
		"non-existent command must win over invalid character_id")
}

func TestGetCommandHelpShimNoCapabilitiesSkipsAccessCheck(t *testing.T) {
	// (d) A command with no capabilities returns help without an access check —
	// the querier resolves Detail directly; the shim maps it through.
	registry := &mockCommandRegistry{commands: []command.CommandEntry{
		{Name: "look", Help: "Look around", Usage: "look [target]", Source: "core"},
	}}
	q := commandquery.New(registry, policytest.DenyAllEngine(), nil) // deny-all, but no caps → no check
	charID := ulid.Make()

	hf := New(nil, WithCommandQuerier(q))
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")

	err := L.DoString(`info, err = holomush.get_command_help("look", "` + charID.String() + `")`)
	require.NoError(t, err)

	tbl, ok := L.GetGlobal("info").(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "look", L.GetField(tbl, "name").String())
	assert.Equal(t, lua.LNil, L.GetGlobal("err"))
}
