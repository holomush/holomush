// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// setupRoutingFixture creates a plugins directory with two Lua plugins:
//   - "say-plugin": a command plugin
//   - "echo-bot": an event plugin
func setupRoutingFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a Lua command plugin directory
	sayDir := filepath.Join(pluginsDir, "say-plugin")
	mkdirAll(t, sayDir)
	writeFile(t, filepath.Join(sayDir, "plugin.yaml"), []byte(`name: say-plugin
version: 1.0.0
type: lua
emits:
  - location
commands:
  - name: say
    help: Say something
lua-plugin:
  entry: main.lua
`))
	writeFile(t, filepath.Join(sayDir, "main.lua"), []byte("function on_command(c) end"))

	// Create a Lua event plugin directory
	luaDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte(`name: echo-bot
version: 1.0.0
type: lua
emits:
  - location
events:
  - say
lua-plugin:
  entry: main.lua
`))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte("function on_event(e) end"))

	return pluginsDir
}

type testEventEmitterHost struct {
	emitter     plugins.PluginIntentEmitter
	loadFn      func(context.Context, *plugins.Manifest, string) error
	loadedNames []string
}

func (h *testEventEmitterHost) SetEventEmitter(emitter plugins.PluginIntentEmitter) {
	h.emitter = emitter
}

func (h *testEventEmitterHost) Load(ctx context.Context, manifest *plugins.Manifest, dir string) error {
	h.loadedNames = append(h.loadedNames, manifest.Name)
	if h.loadFn != nil {
		return h.loadFn(ctx, manifest, dir)
	}
	return nil
}

func (h *testEventEmitterHost) Unload(context.Context, string) error { return nil }

func (h *testEventEmitterHost) DeliverEvent(context.Context, string, pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

func (h *testEventEmitterHost) DeliverCommand(context.Context, string, pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return nil, nil
}

func (h *testEventEmitterHost) Plugins() []string { return append([]string(nil), h.loadedNames...) }

func (h *testEventEmitterHost) Close(context.Context) error { return nil }

func TestManagerRegisterHost(t *testing.T) {
	mgr := plugins.NewManager(t.TempDir())

	mockHost := mocks.NewMockHost(t)
	mgr.RegisterHost(plugins.TypeBinary, mockHost)

	// Registering another host for the same type replaces it
	mockHost2 := mocks.NewMockHost(t)
	mgr.RegisterHost(plugins.TypeBinary, mockHost2)

	// No panic, no error -- just replacement
}

func TestManagerRegisterHostPanicsOnNil(t *testing.T) {
	mgr := plugins.NewManager(t.TempDir())

	assert.Panics(t, func() {
		mgr.RegisterHost(plugins.TypeBinary, nil)
	})
}

func TestManagerRegisterHostBackfillsConfiguredEventEmitter(t *testing.T) {
	mgr := plugins.NewManager(t.TempDir())
	store := core.NewMemoryEventStore()

	mgr.ConfigureEventEmitter(store)

	host := &testEventEmitterHost{}
	mgr.RegisterHost(plugins.TypeBinary, host)

	require.NotNil(t, host.emitter)
}

func TestManagerLoadAllExposesInflightManifestToInitTimeEmitter(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	binDir := filepath.Join(pluginsDir, "scene-binary")
	mkdirAll(t, binDir)
	writeFile(t, filepath.Join(binDir, "plugin.yaml"), []byte(`name: scene-binary
version: 1.0.0
type: binary
emits:
  - scene
binary-plugin:
  executable: scene-binary
`))

	store := core.NewMemoryEventStore()
	host := &testEventEmitterHost{}
	host.loadFn = func(ctx context.Context, manifest *plugins.Manifest, _ string) error {
		if host.emitter == nil {
			return errors.New("event emitter was not injected")
		}
		emitCtx := core.WithActor(ctx, core.Actor{Kind: core.ActorPlugin, ID: manifest.Name})
		return host.emitter.Emit(emitCtx, manifest.Name, pluginsdk.EmitIntent{
			Stream:  "scene:test",
			Type:    pluginsdk.EventTypeSystem,
			Payload: `{"phase":"init"}`,
		})
	}

	mgr := plugins.NewManager(pluginsDir)
	mgr.RegisterHost(plugins.TypeBinary, host)
	mgr.ConfigureEventEmitter(store)

	require.NoError(t, mgr.LoadAll(context.Background()))

	events, err := store.Replay(context.Background(), "scene:test", ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "scene:test", events[0].Stream)
	assert.Equal(t, "scene-binary", events[0].Actor.ID)
	assert.Equal(t, core.ActorPlugin, events[0].Actor.Kind)
}

func TestManagerDeliverCommandRoutesToCorrectHost(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	// Both plugins load via Lua host
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	expectedResp := &pluginsdk.CommandResponse{Output: "hello world"}
	mockLua.EXPECT().DeliverCommand(mock.Anything, "say-plugin", mock.Anything).Return(expectedResp, nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.Len(t, mgr.ListPlugins(), 2)

	resp, err := mgr.DeliverCommand(context.Background(), "say-plugin", pluginsdk.CommandRequest{
		Command: "say",
		Args:    "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello world", resp.Output)
}

func TestManagerDeliverCommandUnknownPlugin(t *testing.T) {
	mgr := plugins.NewManager(t.TempDir())

	_, err := mgr.DeliverCommand(context.Background(), "nonexistent", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestManagerDeliverEventRoutesToCorrectHost(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	expectedEmits := []pluginsdk.EmitEvent{{Stream: "loc:1", Type: "say", Payload: `{}`}}
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(expectedEmits, nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))

	emits, err := mgr.DeliverEvent(context.Background(), "echo-bot", pluginsdk.Event{
		Stream: "loc:1",
		Type:   pluginsdk.EventTypeSay,
	})
	require.NoError(t, err)
	require.Len(t, emits, 1)
	assert.Equal(t, "loc:1", emits[0].Stream)
}

func TestManagerDeliverEventUnknownPlugin(t *testing.T) {
	mgr := plugins.NewManager(t.TempDir())

	_, err := mgr.DeliverEvent(context.Background(), "nonexistent", pluginsdk.Event{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestManagerEmitPluginEventUsesConfiguredSharedEmitter(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)
	store := core.NewMemoryEventStore()
	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	mgr.ConfigureEventEmitter(store)

	ctx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorCharacter,
		ID:   "01CHARACTERTEST",
	})

	err := mgr.EmitPluginEvent(ctx, "say-plugin", pluginsdk.EmitEvent{
		Stream:  "location:123",
		Type:    pluginsdk.EventTypeSay,
		Payload: `{"text":"hello"}`,
	})
	require.NoError(t, err)

	events, replayErr := store.Replay(context.Background(), "location:123", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	assert.Equal(t, core.ActorCharacter, events[0].Actor.Kind)
	assert.Equal(t, "01CHARACTERTEST", events[0].Actor.ID)
	assert.Equal(t, "location:123", events[0].Stream)
}

func TestManagerDeliverCommandConcurrentSafety(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	const goroutines = 10

	resp := &pluginsdk.CommandResponse{Output: "ok"}
	mockLua.EXPECT().DeliverCommand(mock.Anything, "say-plugin", mock.Anything).Return(resp, nil).Times(goroutines)
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(nil, nil).Times(goroutines)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for range goroutines {
		go func() {
			defer wg.Done()
			_, err := mgr.DeliverCommand(context.Background(), "say-plugin", pluginsdk.CommandRequest{})
			assert.NoError(t, err)
		}()
		go func() {
			defer wg.Done()
			_, err := mgr.DeliverEvent(context.Background(), "echo-bot", pluginsdk.Event{})
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
}

func TestManagerLoadAllSkipsPluginsWithoutHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a binary plugin but don't register a binary host
	binDir := filepath.Join(pluginsDir, "my-binary")
	mkdirAll(t, binDir)
	writeFile(t, filepath.Join(binDir, "plugin.yaml"), []byte(`name: my-binary
version: 1.0.0
type: binary
binary-plugin:
  executable: my-binary
`))

	mgr := plugins.NewManager(pluginsDir)
	require.NoError(t, mgr.LoadAll(context.Background()))

	// Plugin should be skipped since no binary host is registered
	assert.Empty(t, mgr.ListPlugins())
}

func TestManagerPluginHostMappingTrackedCorrectly(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	// Commands route to the right plugin
	sayResp := &pluginsdk.CommandResponse{Output: "from say-plugin"}
	mockLua.EXPECT().DeliverCommand(mock.Anything, "say-plugin", mock.Anything).Return(sayResp, nil)

	luaEmits := []pluginsdk.EmitEvent{{Stream: "s", Type: "say"}}
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(luaEmits, nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))

	// DeliverCommand to say-plugin
	resp, err := mgr.DeliverCommand(context.Background(), "say-plugin", pluginsdk.CommandRequest{})
	require.NoError(t, err)
	assert.Equal(t, "from say-plugin", resp.Output)

	// DeliverEvent to echo-bot
	emits, err := mgr.DeliverEvent(context.Background(), "echo-bot", pluginsdk.Event{})
	require.NoError(t, err)
	assert.Len(t, emits, 1)
}

func TestManagerCloseClearsPluginHostMapping(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	sayDir := filepath.Join(pluginsDir, "say-plugin")
	mkdirAll(t, sayDir)
	writeFile(t, filepath.Join(sayDir, "plugin.yaml"), []byte(`name: say-plugin
version: 1.0.0
type: lua
commands:
  - name: say
    help: Say something
lua-plugin:
  entry: main.lua
`))
	writeFile(t, filepath.Join(sayDir, "main.lua"), []byte(""))

	mockLua := mocks.NewMockHost(t)
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.Len(t, mgr.ListPlugins(), 1)

	require.NoError(t, mgr.Close(context.Background()))

	// After close, routing should fail
	_, err := mgr.DeliverCommand(context.Background(), "say-plugin", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestManagerCloseClosesAllHosts(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	// Host should be closed
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.NoError(t, mgr.Close(context.Background()))

	// Mock expectations verify Close() was called
}

// Verify the PluginPolicyInstaller test still works with new routing
func TestManagerLoadAllWithPoliciesMultiHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	luaDir := filepath.Join(pluginsDir, "policy-plugin")
	mkdirAll(t, luaDir)

	policyYAML := `name: policy-plugin
version: 1.0.0
type: lua
policies:
  - name: test-policy
    dsl: "allow admin all"
lua-plugin:
  entry: main.lua
`
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte(policyYAML))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte(""))

	mockHost := mocks.NewMockHost(t)
	mockHost.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockHost.EXPECT().Close(mock.Anything).Return(nil)

	policyInstalled := false
	installer := &testPolicyInstaller{
		installFn: func(_ context.Context, name string, _ []plugins.ManifestPolicy) error {
			assert.Equal(t, "policy-plugin", name)
			policyInstalled = true
			return nil
		},
		removeFn: func(context.Context, string) error { return nil },
	}

	mgr := plugins.NewManager(pluginsDir,
		plugins.WithLuaHost(mockHost),
		plugins.WithPolicyInstaller(installer),
	)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	assert.True(t, policyInstalled)
	assert.Len(t, mgr.ListPlugins(), 1)
}

// testPolicyInstaller implements PluginPolicyInstaller for tests.
type testPolicyInstaller struct {
	installFn func(context.Context, string, []plugins.ManifestPolicy) error
	removeFn  func(context.Context, string) error
}

func (p *testPolicyInstaller) InstallPluginPolicies(ctx context.Context, name string, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, name, policies)
}

func (p *testPolicyInstaller) InstallPluginPoliciesWithManifest(ctx context.Context, manifest *plugins.Manifest, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, manifest.Name, policies)
}

func (p *testPolicyInstaller) RemovePluginPolicies(ctx context.Context, name string) error {
	return p.removeFn(ctx, name)
}

func (p *testPolicyInstaller) ReplacePluginPolicies(ctx context.Context, name string, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, name, policies)
}

func (p *testPolicyInstaller) ReplacePluginPoliciesWithManifest(ctx context.Context, manifest *plugins.Manifest, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, manifest.Name, policies)
}
