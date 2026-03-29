// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corealiases

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// --- Mock ServiceProxy ---

type mockProxy struct {
	plugins.ServiceProxy // embed to satisfy interface; panics on unimplemented calls

	playerAliases map[string][]plugins.AliasEntry // keyed by playerID
	systemAliases []plugins.AliasEntry
	shadowCmd     string // if non-empty, CheckAliasShadow returns (true, shadowCmd)

	// Track calls for verification.
	setPlayerAliasCalls    []setPlayerAliasCall
	deletePlayerAliasCalls []deletePlayerAliasCall
	setSystemAliasCalls    []setSystemAliasCall
	deleteSystemAliasCalls []string

	// Error injection.
	setPlayerAliasErr  error
	setSystemAliasErr  error
	checkShadowErr     error
	listPlayerErr      error
	listSystemErr      error
	deletePlayerErr    error
	deleteSystemErr    error
}

type setPlayerAliasCall struct {
	PlayerID, Alias, Command string
}

type deletePlayerAliasCall struct {
	PlayerID, Alias string
}

type setSystemAliasCall struct {
	Alias, Command, CreatedBy string
}

func newMockProxy() *mockProxy {
	return &mockProxy{
		playerAliases: make(map[string][]plugins.AliasEntry),
	}
}

func (m *mockProxy) CheckAliasShadow(_ context.Context, alias string) (bool, string, error) {
	if m.checkShadowErr != nil {
		return false, "", m.checkShadowErr
	}
	if m.shadowCmd != "" {
		return true, m.shadowCmd, nil
	}
	return false, "", nil
}

func (m *mockProxy) ListPlayerAliases(_ context.Context, playerID string) ([]plugins.AliasEntry, error) {
	if m.listPlayerErr != nil {
		return nil, m.listPlayerErr
	}
	return m.playerAliases[playerID], nil
}

func (m *mockProxy) ListSystemAliases(_ context.Context) ([]plugins.AliasEntry, error) {
	if m.listSystemErr != nil {
		return nil, m.listSystemErr
	}
	return m.systemAliases, nil
}

func (m *mockProxy) SetPlayerAlias(_ context.Context, playerID, alias, command string) error {
	m.setPlayerAliasCalls = append(m.setPlayerAliasCalls, setPlayerAliasCall{playerID, alias, command})
	return m.setPlayerAliasErr
}

func (m *mockProxy) DeletePlayerAlias(_ context.Context, playerID, alias string) error {
	m.deletePlayerAliasCalls = append(m.deletePlayerAliasCalls, deletePlayerAliasCall{playerID, alias})
	return m.deletePlayerErr
}

func (m *mockProxy) SetSystemAlias(_ context.Context, alias, command, createdBy string) error {
	m.setSystemAliasCalls = append(m.setSystemAliasCalls, setSystemAliasCall{alias, command, createdBy})
	return m.setSystemAliasErr
}

func (m *mockProxy) DeleteSystemAlias(_ context.Context, alias string) error {
	m.deleteSystemAliasCalls = append(m.deleteSystemAliasCalls, alias)
	return m.deleteSystemErr
}

// --- Handler dispatch test ---

func TestHandler_HandleCommand_Dispatch(t *testing.T) {
	h := &Handler{}
	proxy := newMockProxy()
	ctx := context.Background()

	tests := []struct {
		name    string
		command string
		args    string
		wantErr bool
	}{
		{"alias with valid args", "alias", "l=look", false},
		{"unalias with valid args", "unalias", "l", false},
		{"aliases", "aliases", "", false},
		{"sysalias with valid args", "sysalias", "l=look", false},
		{"sysunsalias with valid args", "sysunsalias", "l", false},
		{"sysaliases", "sysaliases", "", false},
		{"unknown command", "badcmd", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.HandleCommand(ctx, pluginsdk.CommandRequest{
				Command:     tt.command,
				Args:        tt.args,
				CharacterID: "01EXAMPLE00000000000000001",
			}, proxy)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Player alias tests ---

func TestAliasAdd_Success(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "l=look",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Alias 'l' added: look")
	require.Len(t, proxy.setPlayerAliasCalls, 1)
	assert.Equal(t, "player-1", proxy.setPlayerAliasCalls[0].PlayerID)
	assert.Equal(t, "l", proxy.setPlayerAliasCalls[0].Alias)
	assert.Equal(t, "look", proxy.setPlayerAliasCalls[0].Command)
}

func TestAliasAdd_WarnsOnCommandShadow(t *testing.T) {
	proxy := newMockProxy()
	proxy.shadowCmd = "look"
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "look=l",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Warning: 'look' is an existing command")
	assert.Contains(t, resp.Output, "Alias 'look' added: l")
}

func TestAliasAdd_WarnsOnSystemAliasShadow(t *testing.T) {
	proxy := newMockProxy()
	proxy.systemAliases = []plugins.AliasEntry{{Alias: "l", Command: "look"}}
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "l=examine",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Warning: 'l' is a system alias for 'look'")
}

func TestAliasAdd_WarnsOnReplace(t *testing.T) {
	proxy := newMockProxy()
	proxy.playerAliases["player-1"] = []plugins.AliasEntry{{Alias: "l", Command: "look"}}
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "l=examine",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Warning: Replacing existing alias 'l' (was: 'look')")
}

func TestAliasAdd_InvalidFormat(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "no-equals-sign",
		CharacterID: "player-1",
	}

	_, err := handleAliasAdd(context.Background(), cmd, proxy)
	assert.Error(t, err)
}

func TestAliasAdd_EmptyAlias(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "=look",
		CharacterID: "player-1",
	}

	_, err := handleAliasAdd(context.Background(), cmd, proxy)
	assert.Error(t, err)
}

func TestAliasAdd_EmptyCommand(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "l=",
		CharacterID: "player-1",
	}

	_, err := handleAliasAdd(context.Background(), cmd, proxy)
	assert.Error(t, err)
}

func TestAliasAdd_InvalidName(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "bad name=look",
		CharacterID: "player-1",
	}

	_, err := handleAliasAdd(context.Background(), cmd, proxy)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid alias name")
}

func TestAliasAdd_ProxyError(t *testing.T) {
	proxy := newMockProxy()
	proxy.setPlayerAliasErr = fmt.Errorf("circular reference")
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "l=look",
		CharacterID: "player-1",
	}

	_, err := handleAliasAdd(context.Background(), cmd, proxy)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference")
}

func TestAliasRemove_Success(t *testing.T) {
	proxy := newMockProxy()
	proxy.playerAliases["player-1"] = []plugins.AliasEntry{{Alias: "l", Command: "look"}}
	cmd := pluginsdk.CommandRequest{
		Command:     "unalias",
		Args:        "l",
		CharacterID: "player-1",
	}

	resp, err := handleAliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Alias 'l' removed.")
	require.Len(t, proxy.deletePlayerAliasCalls, 1)
	assert.Equal(t, "l", proxy.deletePlayerAliasCalls[0].Alias)
}

func TestAliasRemove_NotFound(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "unalias",
		Args:        "nonexistent",
		CharacterID: "player-1",
	}

	resp, err := handleAliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No alias 'nonexistent' found.")
	assert.Empty(t, proxy.deletePlayerAliasCalls)
}

func TestAliasRemove_EmptyArgs(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "unalias",
		Args:        "",
		CharacterID: "player-1",
	}

	_, err := handleAliasRemove(context.Background(), cmd, proxy)
	assert.Error(t, err)
}

func TestAliasList_Success(t *testing.T) {
	proxy := newMockProxy()
	proxy.playerAliases["player-1"] = []plugins.AliasEntry{
		{Alias: "n", Command: "north"},
		{Alias: "l", Command: "look"},
	}
	cmd := pluginsdk.CommandRequest{
		Command:     "aliases",
		Args:        "",
		CharacterID: "player-1",
	}

	resp, err := handleAliasList(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Your aliases:")
	assert.Contains(t, resp.Output, "l = look")
	assert.Contains(t, resp.Output, "n = north")
}

func TestAliasList_Empty(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "aliases",
		Args:        "",
		CharacterID: "player-1",
	}

	resp, err := handleAliasList(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "You have no aliases defined.")
}

// --- System alias tests ---

func TestSysaliasAdd_Success(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "l=look",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "System alias 'l' added: look")
	require.Len(t, proxy.setSystemAliasCalls, 1)
	assert.Equal(t, "l", proxy.setSystemAliasCalls[0].Alias)
	assert.Equal(t, "look", proxy.setSystemAliasCalls[0].Command)
	assert.Equal(t, "admin-1", proxy.setSystemAliasCalls[0].CreatedBy)
}

func TestSysaliasAdd_BlocksOnExistingSystemAlias(t *testing.T) {
	proxy := newMockProxy()
	proxy.systemAliases = []plugins.AliasEntry{{Alias: "l", Command: "look"}}
	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "l=examine",
		CharacterID: "admin-1",
	}

	_, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shadows existing system alias")
	assert.Empty(t, proxy.setSystemAliasCalls)
}

func TestSysaliasAdd_WarnsOnCommandShadow(t *testing.T) {
	proxy := newMockProxy()
	proxy.shadowCmd = "look"
	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "look=examine",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Warning: 'look' is an existing command")
	assert.Contains(t, resp.Output, "System alias 'look' added: examine")
}

func TestSysaliasAdd_InvalidFormat(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "no-equals",
		CharacterID: "admin-1",
	}

	_, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	assert.Error(t, err)
}

func TestSysaliasAdd_ProxyError(t *testing.T) {
	proxy := newMockProxy()
	proxy.setSystemAliasErr = fmt.Errorf("circular reference")
	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "l=look",
		CharacterID: "admin-1",
	}

	_, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	assert.Error(t, err)
}

func TestSysaliasRemove_Success(t *testing.T) {
	proxy := newMockProxy()
	proxy.systemAliases = []plugins.AliasEntry{{Alias: "l", Command: "look"}}
	cmd := pluginsdk.CommandRequest{
		Command:     "sysunsalias",
		Args:        "l",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "System alias 'l' removed.")
	require.Len(t, proxy.deleteSystemAliasCalls, 1)
	assert.Equal(t, "l", proxy.deleteSystemAliasCalls[0])
}

func TestSysaliasRemove_NotFound(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "sysunsalias",
		Args:        "nonexistent",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No system alias 'nonexistent' found.")
	assert.Empty(t, proxy.deleteSystemAliasCalls)
}

func TestSysaliasRemove_EmptyArgs(t *testing.T) {
	proxy := newMockProxy()
	cmd := pluginsdk.CommandRequest{
		Command:     "sysunsalias",
		Args:        "",
		CharacterID: "admin-1",
	}

	_, err := handleSysaliasRemove(context.Background(), cmd, proxy)
	assert.Error(t, err)
}

func TestSysaliasList_Success(t *testing.T) {
	proxy := newMockProxy()
	proxy.systemAliases = []plugins.AliasEntry{
		{Alias: "n", Command: "north"},
		{Alias: "l", Command: "look"},
	}

	resp, err := handleSysaliasList(context.Background(), proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "System aliases:")
	assert.Contains(t, resp.Output, "l = look")
	assert.Contains(t, resp.Output, "n = north")
}

func TestSysaliasList_Empty(t *testing.T) {
	proxy := newMockProxy()

	resp, err := handleSysaliasList(context.Background(), proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No system aliases defined.")
}

// --- Helper tests ---

func TestParseAliasDefinition(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantAlias string
		wantCmd   string
		wantErr   bool
	}{
		{"basic", "l=look", "l", "look", false},
		{"with spaces", "  l = look  ", "l", "look", false},
		{"command with args", "aa=attack all", "aa", "attack all", false},
		{"no equals", "nope", "", "", true},
		{"empty alias", "=look", "", "", true},
		{"empty command", "l=", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alias, cmd, err := parseAliasDefinition(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantAlias, alias)
				assert.Equal(t, tt.wantCmd, cmd)
			}
		})
	}
}

func TestValidateAliasName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple", "l", false},
		{"with digits", "go2", false},
		{"with hyphens", "my-alias", false},
		{"with underscores", "my_alias", false},
		{"with plus", "cmd+", false},
		{"with spaces", "bad name", true},
		{"with special chars", "bad@name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAliasName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
