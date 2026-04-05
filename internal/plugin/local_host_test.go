// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginmocks "github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// luaManifest returns a minimal valid Lua plugin manifest for use in LocalPluginHost tests.
func luaManifest(name string) *plugins.Manifest {
	return &plugins.Manifest{
		Name:    name,
		Version: "1.0.0",
		Type:    plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{
			Entry: "main.lua",
		},
	}
}

// --- Tests ---

func TestLocalPluginHostDeliverCommandReturnsErrNoCommandHandler(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	require.NoError(t, host.Load(context.Background(), luaManifest("my-plugin"), ""))

	_, err := host.DeliverCommand(context.Background(), "my-plugin", pluginsdk.CommandRequest{
		Command: "say",
		Args:    "hello world",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrNoCommandHandler)
}

func TestLocalPluginHostDeliverEventReturnsErrNoEventHandler(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	require.NoError(t, host.Load(context.Background(), luaManifest("my-plugin"), ""))

	_, err := host.DeliverEvent(context.Background(), "my-plugin", pluginsdk.Event{
		Stream:  "location:abc",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrNoEventHandler)
}

func TestLocalPluginHostLoadNilManifest(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	err := host.Load(context.Background(), nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest cannot be nil")
}

func TestLocalPluginHostLoadDuplicate(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	require.NoError(t, host.Load(context.Background(), luaManifest("my-plugin"), ""))

	err := host.Load(context.Background(), luaManifest("my-plugin"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already loaded")
}

func TestLocalPluginHostUnload(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	require.NoError(t, host.Load(context.Background(), luaManifest("my-plugin"), ""))

	require.NoError(t, host.Unload(context.Background(), "my-plugin"))
	assert.Empty(t, host.Plugins())

	// DeliverCommand after unload should fail.
	_, err := host.DeliverCommand(context.Background(), "my-plugin", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrPluginNotLoaded)
}

func TestLocalPluginHostUnloadNotLoaded(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	err := host.Unload(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrPluginNotLoaded)
}

func TestLocalPluginHostPlugins(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	require.NoError(t, host.Load(context.Background(), luaManifest("plugin-a"), ""))
	require.NoError(t, host.Load(context.Background(), luaManifest("plugin-b"), ""))

	names := host.Plugins()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "plugin-a")
	assert.Contains(t, names, "plugin-b")
}

func TestLocalPluginHostClose(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	require.NoError(t, host.Load(context.Background(), luaManifest("my-plugin"), ""))

	require.NoError(t, host.Close(context.Background()))

	// All operations should fail after close.
	_, err := host.DeliverCommand(context.Background(), "my-plugin", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHostClosed)

	_, err = host.DeliverEvent(context.Background(), "my-plugin", pluginsdk.Event{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHostClosed)

	err = host.Load(context.Background(), luaManifest("other-plugin"), "")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHostClosed)

	err = host.Unload(context.Background(), "my-plugin")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHostClosed)

	assert.Nil(t, host.Plugins())
}

func TestLocalPluginHostCloseIdempotent(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	require.NoError(t, host.Close(context.Background()))
	require.NoError(t, host.Close(context.Background()))
}

func TestLocalPluginHostDeliverCommandNotLoaded(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	_, err := host.DeliverCommand(context.Background(), "missing", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrPluginNotLoaded)
}

func TestLocalPluginHostDeliverEventNotLoaded(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	_, err := host.DeliverEvent(context.Background(), "missing", pluginsdk.Event{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrPluginNotLoaded)
}

func TestLocalPluginHostConcurrentAccess(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	require.NoError(t, host.Load(context.Background(), luaManifest("my-plugin"), ""))

	var wg sync.WaitGroup
	const goroutines = 50

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			_ = host.Plugins()
		}()
	}

	wg.Wait()
}
