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
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// --- Test doubles ---

type stubProxy struct{}

func (s *stubProxy) QueryLocation(_ context.Context, _, _ string) (*plugins.LocationResult, error) {
	return nil, nil
}
func (s *stubProxy) QueryCharacter(_ context.Context, _, _ string) (*plugins.CharacterResult, error) {
	return nil, nil
}
func (s *stubProxy) QueryLocationCharacters(_ context.Context, _, _ string) ([]plugins.CharacterResult, error) {
	return nil, nil
}
func (s *stubProxy) QueryObject(_ context.Context, _, _ string) (*plugins.ObjectResult, error) {
	return nil, nil
}
func (s *stubProxy) FindLocation(_ context.Context, _, _ string) (*plugins.LocationResult, error) {
	return nil, nil
}
func (s *stubProxy) GetCharactersByLocation(_ context.Context, _, _ string) ([]plugins.CharacterResult, error) {
	return nil, nil
}
func (s *stubProxy) GetObjectsByLocation(_ context.Context, _, _ string) ([]plugins.ObjectResult, error) {
	return nil, nil
}
func (s *stubProxy) CreateLocation(_ context.Context, _, _, _, _ string) (*plugins.LocationResult, error) {
	return nil, nil
}
func (s *stubProxy) CreateExit(_ context.Context, _, _, _, _ string, _ plugins.CreateExitOpts) error {
	return nil
}
func (s *stubProxy) CreateObject(_ context.Context, _, _, _ string) (*plugins.ObjectResult, error) {
	return nil, nil
}
func (s *stubProxy) UpdateLocation(_ context.Context, _, _, _, _ string) error { return nil }
func (s *stubProxy) UpdateCharacterDescription(_ context.Context, _, _, _ string) error {
	return nil
}
func (s *stubProxy) SetProperty(_ context.Context, _, _, _, _, _ string) error { return nil }
func (s *stubProxy) GetProperty(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}
func (s *stubProxy) FindPropertyByPrefix(_ context.Context, _ string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}
func (s *stubProxy) ListPropertiesByParent(_ context.Context, _, _, _ string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}
func (s *stubProxy) KVGet(_ context.Context, _, _ string) (string, bool, error) { return "", false, nil }
func (s *stubProxy) KVSet(_ context.Context, _, _, _ string) error              { return nil }
func (s *stubProxy) KVDelete(_ context.Context, _, _ string) error              { return nil }
func (s *stubProxy) FindSessionByName(_ context.Context, _ string) (*plugins.SessionResult, error) {
	return nil, nil
}
func (s *stubProxy) SetLastWhispered(_ context.Context, _, _ string) error { return nil }
func (s *stubProxy) DisconnectSession(_ context.Context, _, _ string) error { return nil }
func (s *stubProxy) ListActiveSessions(_ context.Context) ([]plugins.SessionResult, error) {
	return nil, nil
}
func (s *stubProxy) BroadcastSystemMessage(_ context.Context, _ string) error { return nil }
func (s *stubProxy) UpdateActivity(_ context.Context, _ string) error         { return nil }
func (s *stubProxy) SetPlayerAlias(_ context.Context, _, _, _ string) error   { return nil }
func (s *stubProxy) DeletePlayerAlias(_ context.Context, _, _ string) error   { return nil }
func (s *stubProxy) ListPlayerAliases(_ context.Context, _ string) ([]plugins.AliasEntry, error) {
	return nil, nil
}
func (s *stubProxy) SetSystemAlias(_ context.Context, _, _, _ string) error { return nil }
func (s *stubProxy) DeleteSystemAlias(_ context.Context, _ string) error    { return nil }
func (s *stubProxy) ListSystemAliases(_ context.Context) ([]plugins.AliasEntry, error) {
	return nil, nil
}
func (s *stubProxy) CheckAliasShadow(_ context.Context, _ string) (bool, string, error) {
	return false, "", nil
}
func (s *stubProxy) ListCommands(_ context.Context, _ string) ([]plugins.CommandInfo, error) {
	return nil, nil
}
func (s *stubProxy) GetCommandHelp(_ context.Context, _, _ string) (*plugins.CommandHelpInfo, error) {
	return nil, nil
}
func (s *stubProxy) EmitEvent(_ context.Context, _, _ string, _ []byte) error { return nil }
func (s *stubProxy) GetStartingLocationID(_ context.Context) (string, error)  { return "", nil }
func (s *stubProxy) Log(_ context.Context, _, _ string)                       {}

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

func TestLocalPluginHost_DeliverCommand(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	resp, err := host.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{
		Command: "say",
		Args:    "hello world",
	})
	require.NoError(t, err)
	assert.Equal(t, "echo: hello world", resp.Output)
}

func TestLocalPluginHost_DeliverEvent(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
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

func TestLocalPluginHost_DeliverCommand_NoHandler(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	host.RegisterHandler("core-notify", nil, &echoEventHandler{})
	require.NoError(t, host.Load(context.Background(), coreManifest("core-notify"), ""))

	_, err := host.DeliverCommand(context.Background(), "core-notify", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrNoCommandHandler)
}

func TestLocalPluginHost_DeliverEvent_NoHandler(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	_, err := host.DeliverEvent(context.Background(), "core-say", pluginsdk.Event{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrNoEventHandler)
}

func TestLocalPluginHost_Load_NonCoreManifest(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})

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

func TestLocalPluginHost_Load_NoRegisteredHandler(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})

	err := host.Load(context.Background(), coreManifest("unknown-plugin"), "")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrHandlerNotRegistered)
}

func TestLocalPluginHost_Load_NilManifest(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	err := host.Load(context.Background(), nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest cannot be nil")
}

func TestLocalPluginHost_Load_Duplicate(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	err := host.Load(context.Background(), coreManifest("core-say"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already loaded")
}

func TestLocalPluginHost_Unload(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	host.RegisterHandler("core-say", &echoCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-say"), ""))

	require.NoError(t, host.Unload(context.Background(), "core-say"))
	assert.Empty(t, host.Plugins())

	// DeliverCommand after unload should fail.
	_, err := host.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrPluginNotLoaded)
}

func TestLocalPluginHost_Unload_NotLoaded(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	err := host.Unload(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrPluginNotLoaded)
}

func TestLocalPluginHost_Plugins(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	host.RegisterHandler("core-a", &echoCommandHandler{}, nil)
	host.RegisterHandler("core-b", nil, &echoEventHandler{})
	require.NoError(t, host.Load(context.Background(), coreManifest("core-a"), ""))
	require.NoError(t, host.Load(context.Background(), coreManifest("core-b"), ""))

	names := host.Plugins()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "core-a")
	assert.Contains(t, names, "core-b")
}

func TestLocalPluginHost_Close(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
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

func TestLocalPluginHost_Close_Idempotent(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	require.NoError(t, host.Close(context.Background()))
	require.NoError(t, host.Close(context.Background()))
}

func TestLocalPluginHost_DeliverCommand_NotLoaded(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	_, err := host.DeliverCommand(context.Background(), "missing", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrPluginNotLoaded)
}

func TestLocalPluginHost_DeliverEvent_NotLoaded(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	_, err := host.DeliverEvent(context.Background(), "missing", pluginsdk.Event{})
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrPluginNotLoaded)
}

func TestLocalPluginHost_HandlerError_Propagated(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
	host.RegisterHandler("core-fail", &failingCommandHandler{}, nil)
	require.NoError(t, host.Load(context.Background(), coreManifest("core-fail"), ""))

	_, err := host.DeliverCommand(context.Background(), "core-fail", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler error")
}

func TestLocalPluginHost_ConcurrentAccess(t *testing.T) {
	host := plugins.NewLocalPluginHost(&stubProxy{})
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
