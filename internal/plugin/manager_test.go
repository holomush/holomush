// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Helper functions for creating test fixtures with secure permissions.
func mkdirAll(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o750))
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, content, 0o600))
}

func TestManagerDiscover(t *testing.T) {
	dir := t.TempDir()

	// Create plugin directories
	echoDir := filepath.Join(dir, "plugins", "echo-bot")
	mkdirAll(t, echoDir)

	manifest := `
name: echo-bot
version: 1.0.0
type: lua
events:
  - say
lua-plugin:
  entry: main.lua
`
	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte("function on_event(e) end"))

	mgr := plugins.NewManager(filepath.Join(dir, "plugins"))
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)

	require.Len(t, manifests, 1)
	assert.Equal(t, "echo-bot", manifests[0].Manifest.Name)
	assert.Equal(t, echoDir, manifests[0].Dir)
}

func TestManagerDiscoverSkipsInvalidPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create valid plugin
	validDir := filepath.Join(pluginsDir, "valid")
	mkdirAll(t, validDir)
	validManifest := `name: valid
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua`
	writeFile(t, filepath.Join(validDir, "plugin.yaml"), []byte(validManifest))
	writeFile(t, filepath.Join(validDir, "main.lua"), []byte(""))

	// Create invalid plugin (bad YAML)
	invalidDir := filepath.Join(pluginsDir, "invalid")
	mkdirAll(t, invalidDir)
	writeFile(t, filepath.Join(invalidDir, "plugin.yaml"), []byte("invalid: ["))

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	// Should succeed but only return valid plugin
	require.NoError(t, err)
	assert.Len(t, manifests, 1, "len(manifests) should be 1 (valid only)")
}

func TestManagerDiscoverEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, manifests, "len(manifests) should be 0 for empty directory")
}

func TestManagerDiscoverNonExistentDirectory(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "non-existent-plugins")

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err, "Discover() should handle non-existent dir gracefully")
	assert.Empty(t, manifests, "len(manifests) should be 0 for non-existent directory")
}

func TestManagerDiscoverSkipsFilesNotDirectories(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	// Create a file (not directory) in plugins dir - should be skipped
	writeFile(t, filepath.Join(pluginsDir, "not-a-plugins.txt"), []byte("hello"))

	// Create valid plugin
	validDir := filepath.Join(pluginsDir, "valid")
	mkdirAll(t, validDir)
	validManifest := `name: valid
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua`
	writeFile(t, filepath.Join(validDir, "plugin.yaml"), []byte(validManifest))
	writeFile(t, filepath.Join(validDir, "main.lua"), []byte(""))

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	assert.Len(t, manifests, 1, "len(manifests) should be 1 (files should be skipped)")
}

func TestManagerDiscoverSkipsDirWithoutManifest(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create directory without plugin.yaml
	noManifestDir := filepath.Join(pluginsDir, "no-manifest")
	mkdirAll(t, noManifestDir)
	// Only create a lua file, no plugin.yaml
	writeFile(t, filepath.Join(noManifestDir, "main.lua"), []byte(""))

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, manifests, "len(manifests) should be 0 (dir without manifest should be skipped)")
}

func TestManagerDiscoverMultiplePlugins(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	testPlugins := []struct {
		name    string
		version string
	}{
		{"alpha-plugin", "1.0.0"},
		{"beta-plugin", "2.0.0"},
		{"gamma-plugin", "3.0.0"},
	}

	for _, p := range testPlugins {
		pluginDir := filepath.Join(pluginsDir, p.name)
		mkdirAll(t, pluginDir)
		manifest := "name: " + p.name + "\nversion: " + p.version + "\ntype: lua\nlua-plugin:\n  entry: main.lua"
		writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest))
		writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte(""))
	}

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, manifests, 3)

	// Sort by name for deterministic comparison
	names := make([]string, 0, len(manifests))
	for _, m := range manifests {
		names = append(names, m.Manifest.Name)
	}
	sort.Strings(names)

	expected := []string{"alpha-plugin", "beta-plugin", "gamma-plugin"}
	assert.Equal(t, expected, names)
}

func TestManagerDiscoverBinaryPlugin(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create binary plugin
	binaryDir := filepath.Join(pluginsDir, "binary-plugin")
	mkdirAll(t, binaryDir)
	manifest := `name: binary-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: plugin-${os}-${arch}`
	writeFile(t, filepath.Join(binaryDir, "plugin.yaml"), []byte(manifest))

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.Equal(t, plugins.TypeBinary, manifests[0].Manifest.Type)
}

func TestManagerListPluginsNoPluginsLoaded(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugins.NewManager(pluginsDir)
	plugins := mgr.ListPlugins()
	assert.Empty(t, plugins, "ListPlugins() should return empty slice before any plugins loaded")
}

func TestManagerLoadAllLuaPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a valid Lua plugin
	echoDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, echoDir)
	manifest := `name: echo-bot
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua`
	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)

	plugins := mgr.ListPlugins()
	require.Len(t, plugins, 1, "ListPlugins() returned wrong number of plugins")
	assert.Equal(t, "echo-bot", plugins[0])
}

func TestManagerLoadAllSkipsInvalidManifests(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create valid plugin
	validDir := filepath.Join(pluginsDir, "valid")
	mkdirAll(t, validDir)
	writeFile(t, filepath.Join(validDir, "plugin.yaml"), []byte("name: valid\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(validDir, "main.lua"), []byte(""))

	// Create invalid plugin
	invalidDir := filepath.Join(pluginsDir, "invalid")
	mkdirAll(t, invalidDir)
	writeFile(t, filepath.Join(invalidDir, "plugin.yaml"), []byte("invalid yaml ["))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "LoadAll() should skip invalid plugins")

	plugins := mgr.ListPlugins()
	assert.Len(t, plugins, 1, "ListPlugins() should return 1 (invalid should be skipped)")
}

func TestManagerLoadAllSkipsLuaPluginsWithoutHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a Lua plugin
	luaDir := filepath.Join(pluginsDir, "lua-plugin")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte("name: lua-plugin\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte(""))

	// Create manager without LuaHost - Lua plugins should be skipped
	mgr := plugins.NewManager(pluginsDir)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "LoadAll() should skip Lua plugins without host")

	// No plugins should be loaded since there's no LuaHost
	plugins := mgr.ListPlugins()
	assert.Empty(t, plugins, "ListPlugins() should be empty (no LuaHost)")
}

func TestManagerLoadAllSkipsBinaryPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a binary plugin
	binaryDir := filepath.Join(pluginsDir, "binary-plugin")
	mkdirAll(t, binaryDir)
	writeFile(t, filepath.Join(binaryDir, "plugin.yaml"), []byte("name: binary-plugin\nversion: 1.0.0\ntype: binary\nbinary-plugin:\n  executable: plugin"))

	mgr := plugins.NewManager(pluginsDir)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "LoadAll() should skip binary plugins")

	// Binary plugins are not yet supported
	plugins := mgr.ListPlugins()
	assert.Empty(t, plugins, "ListPlugins() should be empty (binary not supported)")
}

func TestManagerLoadAllFailsOnLuaSyntaxError(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a Lua plugin with syntax error
	luaDir := filepath.Join(pluginsDir, "bad-lua")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte("name: bad-lua\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte("function broken"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
	err := mgr.LoadAll(context.Background())
	// LoadAll should succeed but log a warning and skip the bad plugin
	require.NoError(t, err, "LoadAll() should skip plugins with load errors")

	plugins := mgr.ListPlugins()
	assert.Empty(t, plugins, "ListPlugins() should be empty (bad Lua syntax)")
}

func TestManagerCloseWithoutLuaHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugins.NewManager(pluginsDir)

	// Close should succeed even without LuaHost
	assert.NoError(t, mgr.Close(context.Background()))
}

func TestManagerClose(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a plugin
	echoDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, echoDir)
	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte("name: echo-bot\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte(""))

	luaHost := pluginlua.NewHost()
	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
	require.NoError(t, mgr.LoadAll(context.Background()))

	// Verify plugin is loaded
	require.Len(t, mgr.ListPlugins(), 1, "expected 1 plugin to be loaded")

	// Close manager
	require.NoError(t, mgr.Close(context.Background()))

	// After close, ListPlugins should return empty
	assert.Empty(t, mgr.ListPlugins(), "ListPlugins() after Close() should be empty")
}

func TestManagerClosePropagatesHostError(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a plugin
	echoDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, echoDir)
	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte("name: echo-bot\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte(""))

	hostErr := errors.New("cleanup failed")
	mockHost := mocks.NewMockHost(t)

	// Manager calls Load on the host, then tracks plugins internally
	mockHost.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockHost.EXPECT().Close(mock.Anything).Return(hostErr)

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockHost))
	require.NoError(t, mgr.LoadAll(context.Background()))

	// Verify plugin is loaded (Manager tracks this internally, not via Host.Plugins())
	require.Len(t, mgr.ListPlugins(), 1, "expected 1 plugin to be loaded")

	// Close should return the error
	err := mgr.Close(context.Background())
	require.Error(t, err, "Close() should return error from host")
	assert.ErrorIs(t, err, hostErr)

	// Even on error, loaded map should be cleared
	assert.Empty(t, mgr.ListPlugins(), "ListPlugins() after failed Close() should be empty")
}

// Compile-time check: Manager implements attribute.PluginRegistry.
var _ attribute.PluginRegistry = (*plugins.Manager)(nil)

func TestManagerIsPluginLoaded(t *testing.T) {
	m := plugins.NewManager("/nonexistent")
	assert.False(t, m.IsPluginLoaded("echo-bot"), "no plugins loaded yet")
}

func TestManagerGetLoadedPluginReturnsFalseWhenNotLoaded(t *testing.T) {
	m := plugins.NewManager("/nonexistent")
	dp, ok := m.GetLoadedPlugin("nonexistent")
	assert.False(t, ok, "should return false for unloaded plugin")
	assert.Nil(t, dp, "should return nil for unloaded plugin")
}

func TestManagerGetLoadedPluginReturnsPluginAfterLoad(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	echoDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, echoDir)

	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte(`
name: echo-bot
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte("function on_event(e) end"))

	host := pluginlua.NewHost()
	m := plugins.NewManager(pluginsDir, plugins.WithLuaHost(host))
	require.NoError(t, m.LoadAll(context.Background()))

	dp, ok := m.GetLoadedPlugin("echo-bot")
	require.True(t, ok, "should find loaded plugin")
	assert.Equal(t, "echo-bot", dp.Manifest.Name)
	assert.Equal(t, "1.0.0", dp.Manifest.Version)

	require.NoError(t, m.Close(context.Background()))
}

func TestManagerWithServiceRegistryReturnsConfiguredRegistry(t *testing.T) {
	reg := plugins.NewServiceRegistry()
	m := plugins.NewManager("/nonexistent", plugins.WithServiceRegistry(reg))
	assert.Same(t, reg, m.Registry(), "Registry() should return the configured service registry")
}

func TestManagerRegistryReturnsNilWhenNotConfigured(t *testing.T) {
	m := plugins.NewManager("/nonexistent")
	assert.Nil(t, m.Registry(), "Registry() should return nil when no registry is configured")
}

func TestManagerLoadAllUsesDAGWhenRegistryConfigured(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Server pre-registers a service. A Lua consumer plugin requires it.
	// The registry exposes the service name so DAG resolution can satisfy the
	// Requires declaration without a plugin-to-plugin edge.
	consumerDir := filepath.Join(pluginsDir, "consumer")
	mkdirAll(t, consumerDir)
	writeFile(t, filepath.Join(consumerDir, "plugin.yaml"), []byte(`name: consumer
version: 1.0.0
type: lua
requires:
  - holomush.test.v1.ServerService
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(consumerDir, "main.lua"), []byte("function on_event(e) end"))

	// Register the service as a server-internal service in the registry.
	reg := plugins.NewServiceRegistry()
	require.NoError(t, reg.Register(plugins.RegisteredService{
		Name:       "holomush.test.v1.ServerService",
		PluginName: "",
		PluginType: "",
	}))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithServiceRegistry(reg))
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "LoadAll() with DAG resolution should succeed")

	loaded := mgr.ListPlugins()
	assert.Len(t, loaded, 1, "consumer plugin should be loaded")
	assert.Contains(t, loaded, "consumer")
}

// stubClientConn is a minimal grpc.ClientConnInterface for testing.
type stubClientConn struct {
	grpc.ClientConnInterface
}

// mockBinaryHost implements both Host and ServiceConnProvider for testing
// service registration in loadPlugin.
type mockBinaryHost struct {
	loadErr    error
	unloadErr  error
	closeErr   error
	conn       grpc.ClientConnInterface
	connErr    error
	pluginList []string
}

func (h *mockBinaryHost) Load(_ context.Context, _ *plugins.Manifest, _ string) error {
	return h.loadErr
}
func (h *mockBinaryHost) Unload(_ context.Context, _ string) error { return h.unloadErr }
func (h *mockBinaryHost) DeliverEvent(_ context.Context, _ string, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}
func (h *mockBinaryHost) DeliverCommand(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return nil, nil
}
func (h *mockBinaryHost) Plugins() []string                                    { return h.pluginList }
func (h *mockBinaryHost) Close(_ context.Context) error                        { return h.closeErr }
func (h *mockBinaryHost) PluginConn(_ string) (grpc.ClientConnInterface, error) { return h.conn, h.connErr }

func TestManagerRegistersProvidedServicesAfterBinaryPluginLoad(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	providerDir := filepath.Join(pluginsDir, "scene-provider")
	mkdirAll(t, providerDir)
	writeFile(t, filepath.Join(providerDir, "plugin.yaml"), []byte(`name: scene-provider
version: 1.0.0
type: binary
provides:
  - holomush.scene.v1.SceneService
binary-plugin:
  executable: scene-plugin`))

	fakeConn := &stubClientConn{}
	binaryHost := &mockBinaryHost{conn: fakeConn}

	reg := plugins.NewServiceRegistry()

	mgr := plugins.NewManager(pluginsDir, plugins.WithServiceRegistry(reg))
	mgr.RegisterHost(plugins.TypeBinary, binaryHost)

	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)

	loaded := mgr.ListPlugins()
	require.Contains(t, loaded, "scene-provider")

	svc, resolveErr := reg.Resolve("holomush.scene.v1.SceneService")
	require.NoError(t, resolveErr, "provided service should be registered after load")
	assert.Equal(t, "scene-provider", svc.PluginName)
	assert.Equal(t, plugins.TypeBinary, svc.PluginType)
	assert.Same(t, fakeConn, svc.Conn)
}

func TestManagerSkipsServiceRegistrationWhenNoRegistry(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	providerDir := filepath.Join(pluginsDir, "provider")
	mkdirAll(t, providerDir)
	writeFile(t, filepath.Join(providerDir, "plugin.yaml"), []byte(`name: provider
version: 1.0.0
type: binary
provides:
  - holomush.test.v1.TestService
binary-plugin:
  executable: test-plugin`))

	fakeConn := &stubClientConn{}
	binaryHost := &mockBinaryHost{conn: fakeConn}

	// No registry configured — service registration should be silently skipped.
	mgr := plugins.NewManager(pluginsDir)
	mgr.RegisterHost(plugins.TypeBinary, binaryHost)

	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)
	assert.Contains(t, mgr.ListPlugins(), "provider")
}

func TestManagerSkipsServiceRegistrationWhenHostLacksConnProvider(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	providerDir := filepath.Join(pluginsDir, "provider")
	mkdirAll(t, providerDir)
	writeFile(t, filepath.Join(providerDir, "plugin.yaml"), []byte(`name: provider
version: 1.0.0
type: binary
provides:
  - holomush.test.v1.TestService
binary-plugin:
  executable: test-plugin`))

	// Use a MockHost (which does NOT implement ServiceConnProvider) as the binary host.
	mockHost := mocks.NewMockHost(t)
	mockHost.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mockHost.EXPECT().Close(mock.Anything).Return(nil)

	reg := plugins.NewServiceRegistry()
	mgr := plugins.NewManager(pluginsDir, plugins.WithServiceRegistry(reg))
	mgr.RegisterHost(plugins.TypeBinary, mockHost)

	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)

	// Service should NOT be registered because MockHost doesn't implement ServiceConnProvider.
	_, resolveErr := reg.Resolve("holomush.test.v1.TestService")
	require.Error(t, resolveErr, "service should not be registered when host lacks ServiceConnProvider")

	require.NoError(t, mgr.Close(context.Background()))
}

func TestManagerRegistersMultipleProvidedServices(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	providerDir := filepath.Join(pluginsDir, "multi-provider")
	mkdirAll(t, providerDir)
	writeFile(t, filepath.Join(providerDir, "plugin.yaml"), []byte(`name: multi-provider
version: 1.0.0
type: binary
provides:
  - holomush.scene.v1.SceneService
  - holomush.scene.v1.SceneQueryService
binary-plugin:
  executable: multi-plugin`))

	fakeConn := &stubClientConn{}
	binaryHost := &mockBinaryHost{conn: fakeConn}

	reg := plugins.NewServiceRegistry()
	mgr := plugins.NewManager(pluginsDir, plugins.WithServiceRegistry(reg))
	mgr.RegisterHost(plugins.TypeBinary, binaryHost)

	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)

	svc1, err := reg.Resolve("holomush.scene.v1.SceneService")
	require.NoError(t, err)
	assert.Equal(t, "multi-provider", svc1.PluginName)

	svc2, err := reg.Resolve("holomush.scene.v1.SceneQueryService")
	require.NoError(t, err)
	assert.Equal(t, "multi-provider", svc2.PluginName)
}
