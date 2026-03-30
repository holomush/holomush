// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corehelp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginmocks "github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return([]plugins.CommandInfo{
		{Name: "say", Help: "Say something", Source: "core"},
		{Name: "look", Help: "Look around", Source: "core"},
		{Name: "dig", Help: "Create a location", Source: "building"},
	}, nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return([]plugins.CommandInfo{}, nil)

	h := &Handler{}
	cmd := baseCmd()

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Equal(t, "No commands available.", resp.Output)
}

func TestListCommands_Error(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return(nil, errors.New("db down"))
	proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

	h := &Handler{}
	cmd := baseCmd()

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "temporarily unavailable")
}

func TestListCommands_EmptySourceDefaultsToOther(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return([]plugins.CommandInfo{
		{Name: "mystery", Help: "Unknown origin", Source: ""},
	}, nil)

	h := &Handler{}
	cmd := baseCmd()

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Other")
}

func TestShowCommandHelp_Success(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("GetCommandHelp", mock.Anything, "say", "char-01").Return(&plugins.CommandHelpInfo{
		Name:     "say",
		Help:     "Say something to the location",
		Usage:    "say <message>",
		HelpText: "Sends a message to everyone in the location.",
		Source:   "core",
	}, nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("GetCommandHelp", mock.Anything, "bogus", "char-01").
		Return(nil, nil)

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "bogus"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Unknown command: bogus")
	assert.Contains(t, resp.Output, "Type 'help' to see available commands.")
}

func TestShowCommandHelp_OtherError(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("GetCommandHelp", mock.Anything, "say", "char-01").
		Return(nil, errors.New("internal failure"))
	proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "say"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "temporarily unavailable")
}

func TestSearchCommands_Matches(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return([]plugins.CommandInfo{
		{Name: "say", Help: "Say something", Source: "core"},
		{Name: "pose", Help: "Pose an action", Source: "core"},
		{Name: "dig", Help: "Create a location", Source: "building"},
	}, nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return([]plugins.CommandInfo{
		{Name: "say", Help: "Say something", Source: "core"},
	}, nil)

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "search zzzzz"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No commands found matching 'zzzzz'.")
}

func TestSearchCommands_Error(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return(nil, errors.New("timeout"))
	proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "search test"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "temporarily unavailable")
}

func TestSearchCommands_CaseInsensitive(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return([]plugins.CommandInfo{
		{Name: "Say", Help: "Say something", Source: "core"},
	}, nil)

	h := &Handler{}
	cmd := baseCmd()
	cmd.Args = "search SAY"

	resp, err := h.HandleCommand(context.Background(), cmd, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Found 1 command(s).")
}

func TestSearchCommands_MatchesDescription(t *testing.T) {
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListCommands", mock.Anything, "char-01").Return([]plugins.CommandInfo{
		{Name: "dig", Help: "Create a new location", Source: "building"},
	}, nil)

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
