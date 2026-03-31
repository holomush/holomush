// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corealiases

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginmocks "github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// newAliasProxy creates a MockServiceProxy with default stubs for alias commands.
// Methods that aren't expected to be called in a test don't need setup — the
// mock will simply report them as unexpected if they fire.
func newAliasProxy(t *testing.T) *pluginmocks.MockServiceProxy {
	t.Helper()
	proxy := pluginmocks.NewMockServiceProxy(t)
	// Default: no command shadow.
	proxy.On("CheckAliasShadow", mock.Anything, mock.Anything).Return(false, "", nil).Maybe()
	// Default: empty player and system aliases.
	proxy.On("ListPlayerAliases", mock.Anything, mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	// Default: log calls are allowed.
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	return proxy
}

// --- Handler dispatch test ---

func TestHandler_HandleCommand_Dispatch(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	tests := []struct {
		name       string
		command    string
		args       string
		setup      func(*pluginmocks.MockServiceProxy)
		wantErr    bool
		wantStatus pluginsdk.CommandStatus
	}{
		{
			"alias with valid args", "alias", "l=look",
			func(p *pluginmocks.MockServiceProxy) {
				p.On("SetPlayerAlias", mock.Anything, mock.Anything, "l", "look").Return(nil)
			},
			false, pluginsdk.CommandOK,
		},
		{"unalias with valid args", "unalias", "l", nil, false, pluginsdk.CommandError},
		{"aliases", "aliases", "", nil, false, pluginsdk.CommandOK},
		{
			"sysalias with valid args", "sysalias", "l=look",
			func(p *pluginmocks.MockServiceProxy) {
				p.On("SetSystemAlias", mock.Anything, "l", "look", mock.Anything).Return(nil)
			},
			false, pluginsdk.CommandOK,
		},
		{"sysunsalias with valid args", "sysunsalias", "l", nil, false, pluginsdk.CommandError},
		{"sysaliases", "sysaliases", "", nil, false, pluginsdk.CommandOK},
		{"unknown command", "badcmd", "", nil, false, pluginsdk.CommandFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := newAliasProxy(t)
			if tt.setup != nil {
				tt.setup(proxy)
			}

			resp, err := h.HandleCommand(ctx, pluginsdk.CommandRequest{
				Command:     tt.command,
				Args:        tt.args,
				CharacterID: "01EXAMPLE00000000000000001",
			}, proxy)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantStatus, resp.Status)
			}
		})
	}
}

// --- Player alias tests ---

func TestAliasAdd_Success(t *testing.T) {
	proxy := newAliasProxy(t)
	proxy.On("SetPlayerAlias", mock.Anything, "player-1", "l", "look").Return(nil)

	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "l=look",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Alias 'l' added: look")
	proxy.AssertCalled(t, "SetPlayerAlias", mock.Anything, "player-1", "l", "look")
}

func TestAliasAdd_WarnsOnCommandShadow(t *testing.T) {
	proxy := newAliasProxy(t)
	// Override default: shadow the "look" command.
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, "look").Return(true, "look", nil)
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("ListPlayerAliases", mock.Anything, mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("SetPlayerAlias", mock.Anything, "player-1", "look", "l").Return(nil)
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

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
	proxy := newAliasProxy(t)
	// Override default: system aliases include "l".
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, mock.Anything).Return(false, "", nil).Maybe()
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry{{Alias: "l", Command: "look"}}, nil)
	proxy.On("ListPlayerAliases", mock.Anything, mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("SetPlayerAlias", mock.Anything, "player-1", "l", "examine").Return(nil)
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

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
	proxy := newAliasProxy(t)
	// Override default: player already has alias "l".
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, mock.Anything).Return(false, "", nil).Maybe()
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("ListPlayerAliases", mock.Anything, "player-1").
		Return([]plugins.AliasEntry{{Alias: "l", Command: "look"}}, nil)
	proxy.On("SetPlayerAlias", mock.Anything, "player-1", "l", "examine").Return(nil)
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

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
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "no-equals-sign",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
}

func TestAliasAdd_EmptyAlias(t *testing.T) {
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "=look",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
}

func TestAliasAdd_EmptyCommand(t *testing.T) {
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "l=",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
}

func TestAliasAdd_InvalidName(t *testing.T) {
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "bad name=look",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "invalid alias name")
}

func TestAliasAdd_ProxyError(t *testing.T) {
	proxy := newAliasProxy(t)
	proxy.On("SetPlayerAlias", mock.Anything, "player-1", "l", "look").
		Return(fmt.Errorf("circular reference"))

	cmd := pluginsdk.CommandRequest{
		Command:     "alias",
		Args:        "l=look",
		CharacterID: "player-1",
	}

	resp, err := handleAliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandFailure, resp.Status)
	assert.Contains(t, resp.Output, "Unable to create alias")
}

func TestAliasRemove_Success(t *testing.T) {
	proxy := newAliasProxy(t)
	// Override: player has alias "l".
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, mock.Anything).Return(false, "", nil).Maybe()
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("ListPlayerAliases", mock.Anything, "player-1").
		Return([]plugins.AliasEntry{{Alias: "l", Command: "look"}}, nil)
	proxy.On("DeletePlayerAlias", mock.Anything, "player-1", "l").Return(nil)
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	cmd := pluginsdk.CommandRequest{
		Command:     "unalias",
		Args:        "l",
		CharacterID: "player-1",
	}

	resp, err := handleAliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Alias 'l' removed.")
	proxy.AssertCalled(t, "DeletePlayerAlias", mock.Anything, "player-1", "l")
}

func TestAliasRemove_NotFound(t *testing.T) {
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "unalias",
		Args:        "nonexistent",
		CharacterID: "player-1",
	}

	resp, err := handleAliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No alias 'nonexistent' found.")
	proxy.AssertNotCalled(t, "DeletePlayerAlias", mock.Anything, mock.Anything, mock.Anything)
}

func TestAliasRemove_EmptyArgs(t *testing.T) {
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "unalias",
		Args:        "",
		CharacterID: "player-1",
	}

	resp, err := handleAliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
}

func TestAliasList_Success(t *testing.T) {
	proxy := newAliasProxy(t)
	// Override: player has aliases.
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, mock.Anything).Return(false, "", nil).Maybe()
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("ListPlayerAliases", mock.Anything, "player-1").Return([]plugins.AliasEntry{
		{Alias: "n", Command: "north"},
		{Alias: "l", Command: "look"},
	}, nil)
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

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
	proxy := newAliasProxy(t)
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
	proxy := newAliasProxy(t)
	proxy.On("SetSystemAlias", mock.Anything, "l", "look", "admin-1").Return(nil)

	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "l=look",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "System alias 'l' added: look")
	proxy.AssertCalled(t, "SetSystemAlias", mock.Anything, "l", "look", "admin-1")
}

func TestSysaliasAdd_BlocksOnExistingSystemAlias(t *testing.T) {
	proxy := newAliasProxy(t)
	// Override: system alias "l" already exists.
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, mock.Anything).Return(false, "", nil).Maybe()
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry{{Alias: "l", Command: "look"}}, nil)
	proxy.On("ListPlayerAliases", mock.Anything, mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "l=examine",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "shadows existing system alias")
	proxy.AssertNotCalled(t, "SetSystemAlias", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestSysaliasAdd_WarnsOnCommandShadow(t *testing.T) {
	proxy := newAliasProxy(t)
	// Override: "look" shadows a command.
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, "look").Return(true, "look", nil)
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("ListPlayerAliases", mock.Anything, mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("SetSystemAlias", mock.Anything, "look", "examine", "admin-1").Return(nil)
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

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
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "no-equals",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
}

func TestSysaliasAdd_ProxyError(t *testing.T) {
	proxy := newAliasProxy(t)
	proxy.On("SetSystemAlias", mock.Anything, "l", "look", "admin-1").
		Return(fmt.Errorf("circular reference"))

	cmd := pluginsdk.CommandRequest{
		Command:     "sysalias",
		Args:        "l=look",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasAdd(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandFailure, resp.Status)
	assert.Contains(t, resp.Output, "Unable to create system alias")
}

func TestSysaliasRemove_Success(t *testing.T) {
	proxy := newAliasProxy(t)
	// Override: system alias "l" exists.
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, mock.Anything).Return(false, "", nil).Maybe()
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry{{Alias: "l", Command: "look"}}, nil)
	proxy.On("ListPlayerAliases", mock.Anything, mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("DeleteSystemAlias", mock.Anything, "l").Return(nil)
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	cmd := pluginsdk.CommandRequest{
		Command:     "sysunsalias",
		Args:        "l",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "System alias 'l' removed.")
	proxy.AssertCalled(t, "DeleteSystemAlias", mock.Anything, "l")
}

func TestSysaliasRemove_NotFound(t *testing.T) {
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "sysunsalias",
		Args:        "nonexistent",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No system alias 'nonexistent' found.")
	proxy.AssertNotCalled(t, "DeleteSystemAlias", mock.Anything, mock.Anything)
}

func TestSysaliasRemove_EmptyArgs(t *testing.T) {
	proxy := newAliasProxy(t)
	cmd := pluginsdk.CommandRequest{
		Command:     "sysunsalias",
		Args:        "",
		CharacterID: "admin-1",
	}

	resp, err := handleSysaliasRemove(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
}

func TestSysaliasList_Success(t *testing.T) {
	proxy := newAliasProxy(t)
	// Override: system aliases exist.
	proxy.ExpectedCalls = nil
	proxy.On("CheckAliasShadow", mock.Anything, mock.Anything).Return(false, "", nil).Maybe()
	proxy.On("ListSystemAliases", mock.Anything).Return([]plugins.AliasEntry{
		{Alias: "n", Command: "north"},
		{Alias: "l", Command: "look"},
	}, nil)
	proxy.On("ListPlayerAliases", mock.Anything, mock.Anything).Return([]plugins.AliasEntry(nil), nil).Maybe()
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	resp, err := handleSysaliasList(context.Background(), proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "System aliases:")
	assert.Contains(t, resp.Output, "l = look")
	assert.Contains(t, resp.Output, "n = north")
}

func TestSysaliasList_Empty(t *testing.T) {
	proxy := newAliasProxy(t)

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
