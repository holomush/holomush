// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// mockPluginDeliverer implements command.PluginCommandDeliverer for testing.
type mockPluginDeliverer struct {
	called     bool
	pluginName string
	cmd        pluginsdk.CommandRequest
	response   *pluginsdk.CommandResponse
	err        error
}

func (m *mockPluginDeliverer) DeliverCommand(_ context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	m.called = true
	m.pluginName = pluginName
	m.cmd = cmd
	return m.response, m.err
}

func TestDispatchPluginBackedCommand(t *testing.T) {
	// Set up a plugin-backed command in the registry
	registry := command.NewRegistry()
	entry := command.NewTestEntry(command.CommandEntryConfig{
		Name:       "greet",
		PluginName: "core-greeting",
		Source:     "core-greeting",
	})
	err := registry.Register(entry)
	require.NoError(t, err)

	// Set up a mock deliverer
	deliverer := &mockPluginDeliverer{
		response: &pluginsdk.CommandResponse{
			Output: "Hello, World!",
		},
	}

	// Create a permit-all engine (AllowAllEngine handles both Layer 1 and Layer 2)
	engine := policytest.AllowAllEngine()

	dispatcher, err := command.NewDispatcher(registry, engine,
		command.WithPluginDeliverer(deliverer),
	)
	require.NoError(t, err)

	// Create execution context
	var buf bytes.Buffer
	charID := ulid.Make()
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   charID,
		CharacterName: "TestPlayer",
		LocationID:    ulid.Make(),
		// SessionID left zero to skip activity update (requires real session store)
		Output:   &buf,
		Services: command.NewTestServices(command.ServicesConfig{}),
	})

	// Dispatch
	err = dispatcher.Dispatch(context.Background(), "greet hello", exec)
	require.NoError(t, err)

	// Verify the deliverer was called with correct args
	assert.True(t, deliverer.called)
	assert.Equal(t, "core-greeting", deliverer.pluginName)
	assert.Equal(t, "greet", deliverer.cmd.Command)
	assert.Equal(t, "hello", deliverer.cmd.Args)
	assert.Equal(t, charID.String(), deliverer.cmd.CharacterID)
	assert.Equal(t, "TestPlayer", deliverer.cmd.CharacterName)
	assert.Equal(t, "greet", deliverer.cmd.InvokedAs)

	// Verify the output was written
	assert.Equal(t, "Hello, World!", buf.String())
}

func TestDispatchPluginBackedCommandNoDeliverer(t *testing.T) {
	registry := command.NewRegistry()
	entry := command.NewTestEntry(command.CommandEntryConfig{
		Name:       "greet",
		PluginName: "core-greeting",
		Source:     "core-greeting",
	})
	err := registry.Register(entry)
	require.NoError(t, err)

	// No plugin deliverer configured — use AllowAllEngine so auth passes (tests plugin path, not auth)
	engine := policytest.AllowAllEngine()
	dispatcher, err := command.NewDispatcher(registry, engine)
	require.NoError(t, err)

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{}),
	})

	err = dispatcher.Dispatch(context.Background(), "greet hello", exec)
	assert.Error(t, err)
	errutil.AssertErrorCode(t, err, "NO_PLUGIN_DELIVERER")
}

func TestNewCommandEntryPluginName(t *testing.T) {
	// Plugin-backed entry: PluginName set, no handler
	entry, err := command.NewCommandEntry(command.CommandEntryConfig{
		Name:       "say",
		PluginName: "core-communication",
		Source:     "core-communication",
	})
	require.NoError(t, err)
	assert.Equal(t, "core-communication", entry.PluginName())
	assert.Nil(t, entry.Handler())

	// Error: both handler and plugin name
	_, err = command.NewCommandEntry(command.CommandEntryConfig{
		Name:       "say",
		Handler:    func(context.Context, *command.CommandExecution) error { return nil },
		PluginName: "core-communication",
	})
	assert.Error(t, err)
	errutil.AssertErrorCode(t, err, "AMBIGUOUS_HANDLER")

	// Error: neither handler nor plugin name
	_, err = command.NewCommandEntry(command.CommandEntryConfig{
		Name: "say",
	})
	assert.Error(t, err)
}
