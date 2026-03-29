// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corebuilding_test

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
	corebuilding "github.com/holomush/holomush/plugins/core-building"
)

// --- Dig tests ---

func TestDig(t *testing.T) {
	handler := &corebuilding.Handler{}

	t.Run("empty args shows usage", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "dig", Args: "", CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Usage: dig")
	})

	t.Run("invalid args shows usage", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "dig", Args: "north", CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Usage: dig")
	})

	t.Run("basic dig without return", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("CreateLocation", mock.Anything, "char-1", "Town Square", "", "persistent").
			Return(&plugins.LocationResult{ID: "loc-new", Name: "Town Square"}, nil)
		proxy.On("CreateExit", mock.Anything, "char-1", "loc-1", "loc-new", "north", mock.Anything).
			Return(nil)

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "dig", Args: `north to "Town Square"`, CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, `Created "Town Square" with exit "north"`)
	})

	t.Run("dig with return exit", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("CreateLocation", mock.Anything, "char-1", "Market", "", "persistent").
			Return(&plugins.LocationResult{ID: "loc-new", Name: "Market"}, nil)
		proxy.On("CreateExit", mock.Anything, "char-1", "loc-1", "loc-new", "north", mock.Anything).
			Return(nil)

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "dig", Args: `north to "Market" return south`, CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, `and return exit "south"`)
	})

	t.Run("create location fails", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("CreateLocation", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil, errors.New("db error"))
		proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return()

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "dig", Args: `north to "Bad Place"`, CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Unable to create location")
	})

	t.Run("create exit fails", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("CreateLocation", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&plugins.LocationResult{ID: "loc-new", Name: "Good Place"}, nil)
		proxy.On("CreateExit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(errors.New("exit error"))
		proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return()

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "dig", Args: `north to "Good Place"`, CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Location created but exit failed")
	})
}

func TestDig_ExitOpts(t *testing.T) {
	handler := &corebuilding.Handler{}

	t.Run("bidirectional flag set for return exit", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("CreateLocation", mock.Anything, "char-1", "Market", "", "persistent").
			Return(&plugins.LocationResult{ID: "loc-new", Name: "Market"}, nil)

		var capturedOpts plugins.CreateExitOpts
		proxy.On("CreateExit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { capturedOpts = args.Get(5).(plugins.CreateExitOpts) }).
			Return(nil)

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "dig", Args: `north to "Market" return south`, CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Created")
		assert.True(t, capturedOpts.Bidirectional)
		assert.Equal(t, "south", capturedOpts.ReturnName)
	})

	t.Run("no bidirectional flag without return", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("CreateLocation", mock.Anything, "char-1", "Town Square", "", "persistent").
			Return(&plugins.LocationResult{ID: "loc-new", Name: "Town Square"}, nil)

		var capturedOpts plugins.CreateExitOpts
		proxy.On("CreateExit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { capturedOpts = args.Get(5).(plugins.CreateExitOpts) }).
			Return(nil)

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "dig", Args: `north to "Town Square"`, CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Created")
		assert.False(t, capturedOpts.Bidirectional)
		assert.Empty(t, capturedOpts.ReturnName)
	})
}

// --- Link tests ---

func TestLink(t *testing.T) {
	handler := &corebuilding.Handler{}

	t.Run("empty args shows usage", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "link", Args: "", CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Usage: link")
	})

	t.Run("invalid args shows usage", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "link", Args: "east", CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Usage: link")
	})

	t.Run("link by ID", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("QueryLocation", mock.Anything, "char-1", "01ABC123").
			Return(&plugins.LocationResult{ID: "01ABC123", Name: "Garden"}, nil)
		proxy.On("CreateExit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil)

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "link", Args: "east to #01ABC123", CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, `Linked "east" to "Garden"`)
	})

	t.Run("link by name", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("FindLocation", mock.Anything, "char-1", "Garden").
			Return(&plugins.LocationResult{ID: "loc-garden", Name: "Garden"}, nil)
		proxy.On("CreateExit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil)

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "link", Args: `east to "Garden"`, CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, `Linked "east" to "Garden"`)
	})

	t.Run("link by name without quotes", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("FindLocation", mock.Anything, "char-1", "Garden").
			Return(&plugins.LocationResult{ID: "loc-found", Name: "Garden"}, nil)
		proxy.On("CreateExit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil)

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "link", Args: "east to Garden", CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, `Linked "east" to "Garden"`)
	})

	t.Run("target not found by ID", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("QueryLocation", mock.Anything, "char-1", "nonexistent").
			Return(nil, errors.New("not found"))
		proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "link", Args: "east to #nonexistent", CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "unable to find location")
	})

	t.Run("target not found by name", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("FindLocation", mock.Anything, "char-1", "Nowhere").
			Return(nil, errors.New("not found"))
		proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "link", Args: `east to "Nowhere"`, CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "unable to find location")
	})

	t.Run("create exit fails", func(t *testing.T) {
		proxy := pluginmocks.NewMockServiceProxy(t)
		proxy.On("FindLocation", mock.Anything, "char-1", "Garden").
			Return(&plugins.LocationResult{ID: "loc-found", Name: "Garden"}, nil)
		proxy.On("CreateExit", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(errors.New("exit error"))
		proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return()

		resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
			Command: "link", Args: "east to Garden", CharacterID: "char-1", LocationID: "loc-1",
		}, proxy)
		require.NoError(t, err)
		assert.Contains(t, resp.Output, "Unable to create exit")
	})
}

func TestHandler_UnknownCommand(t *testing.T) {
	handler := &corebuilding.Handler{}
	proxy := pluginmocks.NewMockServiceProxy(t)
	resp, err := handler.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "destroy",
		Args:    "all",
	}, proxy)
	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Unknown building command")
}
