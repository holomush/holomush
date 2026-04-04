// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginmocks "github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// --- Test doubles ---

type echoCommandHandler struct{}

func (h *echoCommandHandler) HandleCommand(_ context.Context, cmd pluginsdk.CommandRequest, _ plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	return &pluginsdk.CommandResponse{
		Output: "echo: " + cmd.Args,
	}, nil
}

type echoEventHandler struct{}

func (h *echoEventHandler) HandleEvent(_ context.Context, event pluginsdk.Event, _ plugins.ServiceProxy) ([]pluginsdk.EmitEvent, error) {
	return []pluginsdk.EmitEvent{
		{Stream: event.Stream, Type: event.Type, Payload: event.Payload},
	}, nil
}

type failingCommandHandler struct{}

func (h *failingCommandHandler) HandleCommand(_ context.Context, _ pluginsdk.CommandRequest, _ plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	return nil, errors.New("handler error")
}

// --- Helpers ---

func coreManifest(name string) *plugins.Manifest {
	return &plugins.Manifest{
		Name:    name,
		Version: "1.0.0",
		Type:    plugins.TypeCore,
	}
}

// --- Tests ---

func TestLocalPluginHostDeliverCommand(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	resp, err := host.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{
		Command: "say",
		Args:    "hello world",
	})
	require.NoError(t, err)
	assert.Equal(t, "echo: hello world", resp.Output)
}

func TestLocalPluginHostDeliverEvent(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-notify", nil, &echoEventHandler{})
	require.NoError(t, host.Load(context.Background(), coreManifest("core-notify"), ""))

	emits, err := host.DeliverEvent(context.Background(), "core-notify", pluginsdk.Event{
		Stream:  "location:abc",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	require.Len(t, emits, 1)
	assert.Equal(t, "location:abc", emits[0].Stream)
	assert.Equal(t, pluginsdk.EventTypeSay, emits[0].Type)
}

func TestLocalPluginHostDeliverCommandNoHandler(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-notify", nil, &echoEventHandler{})
	require.NoError(t, host.Load(context.Background(), coreManifest("core-notify"), ""))

	_, err := host.DeliverCommand(context.Background(), "core-notify", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrNoCommandHandler)
}

func TestLocalPluginHostDeliverEventNoHandler(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	_, err := host.DeliverEvent(context.Background(), "core-say", pluginsdk.Event{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrNoEventHandler)
}

func TestLocalPluginHostLoadNonCoreManifest(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))

	m := &plugins.Manifest{
		Name:    "lua-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{
			Entry: "main.lua",
		},
	}
	err := host.Load(context.Background(), m, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only accepts core plugins")
}

func TestLocalPluginHostLoadNoRegisteredHandler(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))

	err := host.Load(context.Background(), coreManifest("unknown-plugin"), "")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHandlerNotRegistered)
}

func TestLocalPluginHostLoadNilManifest(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	err := host.Load(context.Background(), nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest cannot be nil")
}

func TestLocalPluginHostLoadDuplicate(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	err := host.Load(context.Background(), coreManifest("core-say"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already loaded")
}

func TestLocalPluginHostUnload(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	require.NoError(t, host.Unload(context.Background(), "core-say"))
	assert.Empty(t, host.Plugins())

	// DeliverCommand after unload should fail.
	_, err := host.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{})
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
	host.RegisterHandler("core-a", &echoCommandHandler{}, nil)
	host.RegisterHandler("core-b", nil, &echoEventHandler{})
	require.NoError(t, host.Load(context.Background(), coreManifest("core-a"), ""))
	require.NoError(t, host.Load(context.Background(), coreManifest("core-b"), ""))

	names := host.Plugins()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "core-a")
	assert.Contains(t, names, "core-b")
}

func TestLocalPluginHostClose(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	require.NoError(t, host.Close(context.Background()))

	// All operations should fail after close.
	_, err := host.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHostClosed)

	_, err = host.DeliverEvent(context.Background(), "core-say", pluginsdk.Event{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHostClosed)

	err = host.Load(context.Background(), coreManifest("core-other"), "")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHostClosed)

	err = host.Unload(context.Background(), "core-say")
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

func TestLocalPluginHostHandlerErrorPropagated(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-fail", &failingCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-fail"), ""))

	_, err := host.DeliverCommand(context.Background(), "core-fail", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler error")
}

func TestLocalPluginHostConcurrentAccess(t *testing.T) {
	host := plugins.NewLocalPluginHost(pluginmocks.NewMockServiceProxy(t))
	host.RegisterHandler("core-say", &echoCommandHandler{}, &echoEventHandler{})
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	var wg sync.WaitGroup
	const goroutines = 50
	errCh := make(chan error, goroutines*2)

	wg.Add(goroutines * 3)

	for range goroutines {
		go func() {
			defer wg.Done()
			_, err := host.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{Args: "test"})
			errCh <- err
		}()
		go func() {
			defer wg.Done()
			_, err := host.DeliverEvent(context.Background(), "core-say", pluginsdk.Event{Stream: "loc:1"})
			errCh <- err
		}()
		go func() {
			defer wg.Done()
			_ = host.Plugins()
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		assert.NoError(t, err)
	}
}
