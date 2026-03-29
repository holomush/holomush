// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corehelp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// --- test stub proxy ---

type stubProxy struct {
	commands    []plugins.CommandInfo
	commandsErr error
	helpInfo    *plugins.CommandHelpInfo
	helpErr     error
}

func (s *stubProxy) ListCommands(_ context.Context, _ string) ([]plugins.CommandInfo, error) {
	return s.commands, s.commandsErr
}

func (s *stubProxy) GetCommandHelp(_ context.Context, _, _ string) (*plugins.CommandHelpInfo, error) {
	return s.helpInfo, s.helpErr
}

// Unused ServiceProxy methods — stubs required by the interface.

func (s *stubProxy) QueryLocation(context.Context, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (s *stubProxy) QueryCharacter(context.Context, string, string) (*plugins.CharacterResult, error) {
	return nil, nil
}

func (s *stubProxy) QueryLocationCharacters(context.Context, string, string) ([]plugins.CharacterResult, error) {
	return nil, nil
}

func (s *stubProxy) QueryObject(context.Context, string, string) (*plugins.ObjectResult, error) {
	return nil, nil
}

func (s *stubProxy) FindLocation(context.Context, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (s *stubProxy) GetCharactersByLocation(context.Context, string, string) ([]plugins.CharacterResult, error) {
	return nil, nil
}

func (s *stubProxy) GetObjectsByLocation(context.Context, string, string) ([]plugins.ObjectResult, error) {
	return nil, nil
}

func (s *stubProxy) CreateLocation(context.Context, string, string, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (s *stubProxy) CreateExit(context.Context, string, string, string, string, plugins.CreateExitOpts) error {
	return nil
}

func (s *stubProxy) CreateObject(context.Context, string, string, string) (*plugins.ObjectResult, error) {
	return nil, nil
}

func (s *stubProxy) UpdateLocation(context.Context, string, string, string, string) error {
	return nil
}

func (s *stubProxy) UpdateCharacterDescription(context.Context, string, string, string) error {
	return nil
}

func (s *stubProxy) SetProperty(context.Context, string, string, string, string, string) error {
	return nil
}

func (s *stubProxy) GetProperty(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (s *stubProxy) FindPropertyByPrefix(context.Context, string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}

func (s *stubProxy) ListPropertiesByParent(context.Context, string, string, string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}

func (s *stubProxy) KVGet(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}

func (s *stubProxy) KVSet(context.Context, string, string, string) error { return nil }
func (s *stubProxy) KVDelete(context.Context, string, string) error      { return nil }

func (s *stubProxy) FindSessionByName(context.Context, string) (*plugins.SessionResult, error) {
	return nil, nil
}

func (s *stubProxy) SetLastWhispered(context.Context, string, string) error { return nil }
func (s *stubProxy) DisconnectSession(context.Context, string, string) error { return nil }

func (s *stubProxy) ListActiveSessions(context.Context) ([]plugins.SessionResult, error) {
	return nil, nil
}

func (s *stubProxy) BroadcastSystemMessage(context.Context, string) error { return nil }
func (s *stubProxy) UpdateActivity(context.Context, string) error         { return nil }
func (s *stubProxy) SetPlayerAlias(context.Context, string, string, string) error { return nil }
func (s *stubProxy) DeletePlayerAlias(context.Context, string, string) error      { return nil }

func (s *stubProxy) ListPlayerAliases(context.Context, string) ([]plugins.AliasEntry, error) {
	return nil, nil
}

func (s *stubProxy) SetSystemAlias(context.Context, string, string, string) error { return nil }
func (s *stubProxy) DeleteSystemAlias(context.Context, string) error              { return nil }

func (s *stubProxy) ListSystemAliases(context.Context) ([]plugins.AliasEntry, error) {
	return nil, nil
}

func (s *stubProxy) CheckAliasShadow(context.Context, string) (bool, string, error) {
	return false, "", nil
}

func (s *stubProxy) EmitEvent(context.Context, string, string, []byte) error { return nil }
func (s *stubProxy) GetStartingLocationID(context.Context) (string, error)   { return "", nil }
func (s *stubProxy) Log(context.Context, string, string)                     {}

// --- tests ---

func baseCmd() pluginsdk.CommandRequest {
	return pluginsdk.CommandRequest{
		Command:     "help",
		CharacterID: "char-01",
		SessionID:   "sess-01",
		LocationID:  "loc-01",
	}
}

func TestListCommands_GroupedBySource(t *testing.T) {
	proxy := &stubProxy{
		commands: []plugins.CommandInfo{
			{Name: "say", Help: "Say something", Source: "core"},
			{Name: "look", Help: "Look around", Source: "core"},
			{Name: "dig", Help: "Create a location", Source: "building"},
		},
	}

	h := &Handler{}
	cmd := baseCmd()

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)

	assert.Contains(t, resp.Output, "Available Commands")
	assert.Contains(t, resp.Output, "Building")
	assert.Contains(t, resp.Output, "Core")
	assert.Contains(t, resp.Output, "say")
	assert.Contains(t, resp.Output, "dig")
	assert.Contains(t, resp.Output, "Type 'help <command>' for detailed help.")
}

func TestListCommands_Empty(t *testing.T) {
	proxy := &stubProxy{commands: []plugins.CommandInfo{}}

	h := &Handler{}
	cmd := baseCmd()

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, "No commands available.", resp.Output)
}

func TestListCommands_Error(t *testing.T) {
	proxy := &stubProxy{commandsErr: errors.New("db down")}

	h := &Handler{}
	cmd := baseCmd()

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Error listing commands")
	assert.Contains(t, resp.Output, "db down")
}

func TestListCommands_EmptySourceDefaultsToOther(t *testing.T) {
	proxy := &stubProxy{
		commands: []plugins.CommandInfo{
			{Name: "mystery", Help: "Unknown origin", Source: ""},
		},
	}

	h := &Handler{}
	cmd := baseCmd()

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Other")
}

func TestShowCommandHelp_Success(t *testing.T) {
	proxy := &stubProxy{
		helpInfo: &plugins.CommandHelpInfo{
			Name:     "say",
			Help:     "Say something to the location",
			Usage:    "say <message>",
			HelpText: "Sends a message to everyone in the location.",
			Source:   "core",
		},
	}

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "say"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "say")
	assert.Contains(t, resp.Output, "Say something to the location")
	assert.Contains(t, resp.Output, "Usage:")
	assert.Contains(t, resp.Output, "say <message>")
	assert.Contains(t, resp.Output, "Sends a message")
	assert.Contains(t, resp.Output, "Source: core")
}

func TestShowCommandHelp_NotFound(t *testing.T) {
	proxy := &stubProxy{}

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "bogus"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Unknown command: bogus")
	assert.Contains(t, resp.Output, "Type 'help' to see available commands.")
}

func TestShowCommandHelp_OtherError(t *testing.T) {
	proxy := &stubProxy{
		helpErr: errors.New("internal failure"),
	}

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "say"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Error getting help")
	assert.Contains(t, resp.Output, "internal failure")
}

func TestSearchCommands_Matches(t *testing.T) {
	proxy := &stubProxy{
		commands: []plugins.CommandInfo{
			{Name: "say", Help: "Say something", Source: "core"},
			{Name: "pose", Help: "Pose an action", Source: "core"},
			{Name: "dig", Help: "Create a location", Source: "building"},
		},
	}

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "search say"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Search Results")
	assert.Contains(t, resp.Output, "say")
	assert.NotContains(t, resp.Output, "dig")
	assert.Contains(t, resp.Output, "Found 1 command(s).")
}

func TestSearchCommands_NoMatches(t *testing.T) {
	proxy := &stubProxy{
		commands: []plugins.CommandInfo{
			{Name: "say", Help: "Say something", Source: "core"},
		},
	}

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "search zzzzz"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No commands found matching 'zzzzz'.")
}

func TestSearchCommands_Error(t *testing.T) {
	proxy := &stubProxy{commandsErr: errors.New("timeout")}

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "search test"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Error searching commands")
	assert.Contains(t, resp.Output, "timeout")
}

func TestSearchCommands_CaseInsensitive(t *testing.T) {
	proxy := &stubProxy{
		commands: []plugins.CommandInfo{
			{Name: "Say", Help: "Say something", Source: "core"},
		},
	}

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "search SAY"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Found 1 command(s).")
}

func TestSearchCommands_MatchesDescription(t *testing.T) {
	proxy := &stubProxy{
		commands: []plugins.CommandInfo{
			{Name: "dig", Help: "Create a new location", Source: "building"},
		},
	}

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "search location"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "dig")
	assert.Contains(t, resp.Output, "Found 1 command(s).")
}

func TestParseSearchTerm(t *testing.T) {
	tests := []struct {
		name     string
		args     string
		wantTerm string
		wantOK   bool
	}{
		{"valid search", "search build", "build", true},
		{"case insensitive prefix", "Search build", "build", true},
		{"no search prefix", "say", "", false},
		{"search with no term", "search ", "", false},
		{"search with extra spaces", "search   build ", "build", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term, ok := parseSearchTerm(tt.args)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantTerm, term)
			}
		})
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"core", "Core"},
		{"building", "Building"},
		{"", ""},
		{"A", "A"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, capitalize(tt.input))
		})
	}
}
