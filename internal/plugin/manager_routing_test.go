// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func setupRoutingFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a core plugin directory
	coreDir := filepath.Join(pluginsDir, "core-say")
	mkdirAll(t, coreDir)
	writeFile(t, filepath.Join(coreDir, "plugin.yaml"), []byte(`name: core-say
version: 1.0.0
type: core
commands:
  - name: say
    help: Say something
`))

	// Create a Lua plugin directory
	luaDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte(`name: echo-bot
version: 1.0.0
type: lua
events:
  - say
lua-plugin:
  entry: main.lua
`))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte("function on_event(e) end"))

	return pluginsDir
}

func TestManagerRegisterHost(t *testing.T) {
	mgr := plugins.NewManager(t.TempDir())

	mockHost := mocks.NewMockHost(t)
	mgr.RegisterHost(plugins.TypeCore, mockHost)

	// Registering another host for the same type replaces it
	mockHost2 := mocks.NewMockHost(t)
	mgr.RegisterHost(plugins.TypeCore, mockHost2)

	// No panic, no error -- just replacement
}

func TestManagerRegisterHostPanicsOnNil(t *testing.T) {
	mgr := plugins.NewManager(t.TempDir())

	assert.Panics(t, func() {
		mgr.RegisterHost(plugins.TypeCore, nil)
	})
}

func TestManagerDeliverCommandRoutesToCorrectHost(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockCore := mocks.NewMockHost(t)
	mockLua := mocks.NewMockHost(t)

	// Both hosts accept Load calls
	mockCore.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Expect Close on both hosts
	mockCore.EXPECT().Close(mock.Anything).Return(nil)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	expectedResp := &pluginsdk.CommandResponse{Output: "hello world"}
	mockCore.EXPECT().DeliverCommand(mock.Anything, "core-say", mock.Anything).Return(expectedResp, nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	mgr.RegisterHost(plugins.TypeCore, mockCore)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.Len(t, mgr.ListPlugins(), 2)

	resp, err := mgr.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{
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

	mockCore := mocks.NewMockHost(t)
	mockLua := mocks.NewMockHost(t)

	mockCore.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockCore.EXPECT().Close(mock.Anything).Return(nil)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	expectedEmits := []pluginsdk.EmitEvent{{Stream: "loc:1", Type: "say", Payload: `{}`}}
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(expectedEmits, nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	mgr.RegisterHost(plugins.TypeCore, mockCore)
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

func TestManagerDeliverCommandConcurrentSafety(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockCore := mocks.NewMockHost(t)
	mockLua := mocks.NewMockHost(t)

	mockCore.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockCore.EXPECT().Close(mock.Anything).Return(nil)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	const goroutines = 10

	resp := &pluginsdk.CommandResponse{Output: "ok"}
	mockCore.EXPECT().DeliverCommand(mock.Anything, "core-say", mock.Anything).Return(resp, nil).Times(goroutines)
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(nil, nil).Times(goroutines)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	mgr.RegisterHost(plugins.TypeCore, mockCore)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for range goroutines {
		go func() {
			defer wg.Done()
			_, err := mgr.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{})
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

	// Create a core plugin but don't register a core host
	coreDir := filepath.Join(pluginsDir, "core-say")
	mkdirAll(t, coreDir)
	writeFile(t, filepath.Join(coreDir, "plugin.yaml"), []byte(`name: core-say
version: 1.0.0
type: core
commands:
  - name: say
    help: Say something
`))

	mgr := plugins.NewManager(pluginsDir)
	require.NoError(t, mgr.LoadAll(context.Background()))

	// Plugin should be skipped since no core host is registered
	assert.Empty(t, mgr.ListPlugins())
}

func TestManagerPluginHostMappingTrackedCorrectly(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockCore := mocks.NewMockHost(t)
	mockLua := mocks.NewMockHost(t)

	mockCore.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mockCore.EXPECT().Close(mock.Anything).Return(nil)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	// Commands route to the right host
	coreResp := &pluginsdk.CommandResponse{Output: "from core"}
	mockCore.EXPECT().DeliverCommand(mock.Anything, "core-say", mock.Anything).Return(coreResp, nil)

	luaEmits := []pluginsdk.EmitEvent{{Stream: "s", Type: "say"}}
	mockLua.EXPECT().DeliverEvent(mock.Anything, "echo-bot", mock.Anything).Return(luaEmits, nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	mgr.RegisterHost(plugins.TypeCore, mockCore)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })

	require.NoError(t, mgr.LoadAll(context.Background()))

	// DeliverCommand to core plugin goes to mockCore
	resp, err := mgr.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{})
	require.NoError(t, err)
	assert.Equal(t, "from core", resp.Output)

	// DeliverEvent to lua plugin goes to mockLua
	emits, err := mgr.DeliverEvent(context.Background(), "echo-bot", pluginsdk.Event{})
	require.NoError(t, err)
	assert.Len(t, emits, 1)

	// Cross-check: delivering to wrong plugin type should still route correctly
	// (command to lua plugin should go to lua host)
}

func TestManagerCloseClearsPluginHostMapping(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	coreDir := filepath.Join(pluginsDir, "core-say")
	mkdirAll(t, coreDir)
	writeFile(t, filepath.Join(coreDir, "plugin.yaml"), []byte(`name: core-say
version: 1.0.0
type: core
commands:
  - name: say
    help: Say something
`))

	mockCore := mocks.NewMockHost(t)
	mockCore.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockCore.EXPECT().Close(mock.Anything).Return(nil)

	mgr := plugins.NewManager(pluginsDir)
	mgr.RegisterHost(plugins.TypeCore, mockCore)

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.Len(t, mgr.ListPlugins(), 1)

	require.NoError(t, mgr.Close(context.Background()))

	// After close, routing should fail
	_, err := mgr.DeliverCommand(context.Background(), "core-say", pluginsdk.CommandRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestManagerCloseClosesAllHosts(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)

	mockCore := mocks.NewMockHost(t)
	mockLua := mocks.NewMockHost(t)

	mockCore.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Both hosts should be closed
	mockCore.EXPECT().Close(mock.Anything).Return(nil)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua))
	mgr.RegisterHost(plugins.TypeCore, mockCore)

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.NoError(t, mgr.Close(context.Background()))

	// Mock expectations verify both Close() calls happened
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

func (p *testPolicyInstaller) RemovePluginPolicies(ctx context.Context, name string) error {
	return p.removeFn(ctx, name)
}

func (p *testPolicyInstaller) ReplacePluginPolicies(ctx context.Context, name string, policies []plugins.ManifestPolicy) error {
	return p.installFn(ctx, name, policies)
}
