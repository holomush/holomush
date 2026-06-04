// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/plugin/mocks"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
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

	mgr, mgrErr := plugins.NewManager(filepath.Join(dir, "plugins"), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	manifests, err := mgr.Discover(context.Background())
	// Should succeed but only return valid plugin
	require.NoError(t, err)
	assert.Len(t, manifests, 1, "len(manifests) should be 1 (valid only)")
}

func TestManagerDiscoverEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, manifests, "len(manifests) should be 0 for empty directory")
}

func TestManagerDiscoverNonExistentDirectory(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "non-existent-plugins")

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.Equal(t, plugins.TypeBinary, manifests[0].Manifest.Type)
}

func TestManagerListPluginsNoPluginsLoaded(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	// Strict by default: a broken Lua plugin is a hard error.
	require.Error(t, err, "LoadAll() should fail when a plugin has load errors")

	plugins := mgr.ListPlugins()
	assert.Empty(t, plugins, "ListPlugins() should be empty (bad Lua syntax)")
}

func TestLoadAllSkipsBrokenPluginsWhenGracefulDegradationEnabled(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// One good plugin, one broken
	goodDir := filepath.Join(pluginsDir, "good-lua")
	mkdirAll(t, goodDir)
	writeFile(t, filepath.Join(goodDir, "plugin.yaml"), []byte("name: good-lua\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(goodDir, "main.lua"), []byte("return {}"))

	badDir := filepath.Join(pluginsDir, "bad-lua")
	mkdirAll(t, badDir)
	writeFile(t, filepath.Join(badDir, "plugin.yaml"), []byte("name: bad-lua\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(badDir, "main.lua"), []byte("function broken"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithLuaHost(luaHost),
		plugins.WithGracefulDegradation(),
		plugins.WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	// Graceful degradation: errors are logged but LoadAll succeeds.
	require.NoError(t, err)

	loaded := mgr.ListPlugins()
	assert.Contains(t, loaded, "good-lua")
	assert.NotContains(t, loaded, "bad-lua")
}

func TestManagerCloseWithoutLuaHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

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
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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
	m, mgrErr := plugins.NewManager("/nonexistent", plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	assert.False(t, m.IsPluginLoaded("echo-bot"), "no plugins loaded yet")
}

func TestManagerGetLoadedPluginReturnsFalseWhenNotLoaded(t *testing.T) {
	m, mgrErr := plugins.NewManager("/nonexistent", plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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
	m, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(host), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	require.NoError(t, m.LoadAll(context.Background()))

	dp, ok := m.GetLoadedPlugin("echo-bot")
	require.True(t, ok, "should find loaded plugin")
	assert.Equal(t, "echo-bot", dp.Manifest.Name)
	assert.Equal(t, "1.0.0", dp.Manifest.Version)

	require.NoError(t, m.Close(context.Background()))
}

func TestManagerWithServiceRegistryReturnsConfiguredRegistry(t *testing.T) {
	reg := plugins.NewServiceRegistry()
	m, mgrErr := plugins.NewManager("/nonexistent", plugins.WithServiceRegistry(reg), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	assert.Same(t, reg, m.Registry(), "Registry() should return the configured service registry")
}

func TestManagerRegistryReturnsNilWhenNotConfigured(t *testing.T) {
	m, mgrErr := plugins.NewManager("/nonexistent", plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithServiceRegistry(reg), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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
func (h *mockBinaryHost) Plugins() []string                            { return h.pluginList }
func (h *mockBinaryHost) PluginEmitRegistry(_ string) ([]string, bool) { return nil, false }
func (h *mockBinaryHost) Close(_ context.Context) error                { return h.closeErr }
func (h *mockBinaryHost) PluginConn(_ string) (grpc.ClientConnInterface, error) {
	return h.conn, h.connErr
}

func (h *mockBinaryHost) QuerySessionStreams(_ context.Context, _ string, _ plugins.SessionStreamsRequest) ([]string, error) {
	return nil, nil
}

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

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithServiceRegistry(reg), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithServiceRegistry(reg), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	mgr.RegisterHost(plugins.TypeBinary, mockHost)

	// LoadAll is strict by default — a host that can't satisfy provides is a hard error.
	err := mgr.LoadAll(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, plugins.CodeHostMissingConnProvider)

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
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithServiceRegistry(reg), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
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

// fakeAliasSeederMgr is an in-memory AliasSeeder for manager tests.
type fakeAliasSeederMgr struct {
	existing map[string]string
}

func (f *fakeAliasSeederMgr) GetSystemAliases(_ context.Context) (map[string]string, error) {
	result := make(map[string]string, len(f.existing))
	for k, v := range f.existing {
		result[k] = v
	}
	return result, nil
}

func (f *fakeAliasSeederMgr) SetSystemAlias(_ context.Context, alias, cmd, _, _ string) error {
	if f.existing == nil {
		f.existing = make(map[string]string)
	}
	f.existing[alias] = cmd
	return nil
}

func TestManagerLoadAllSeedsAliasesFromManifests(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	commDir := filepath.Join(pluginsDir, "test-comm")
	mkdirAll(t, commDir)
	writeFile(t, filepath.Join(commDir, "plugin.yaml"), []byte(`name: test-comm
version: 1.0.0
type: lua
commands:
  - name: say
    aliases:
      - '"'
    help: Say something
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(commDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	repo := &fakeAliasSeederMgr{existing: make(map[string]string)}
	cache := command.NewAliasCache()

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithLuaHost(luaHost),
		plugins.WithAliasSeeder(repo, cache),
		plugins.WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, mgrErr)

	err := mgr.LoadAll(context.Background())
	require.NoError(t, err)

	// Verify alias was persisted to the repo.
	aliases, repoErr := repo.GetSystemAliases(context.Background())
	require.NoError(t, repoErr)
	assert.Equal(t, "say", aliases[`"`], "alias should map to say command")

	// Verify alias was loaded into the cache.
	cached, found := cache.GetSystemAlias(`"`)
	require.True(t, found, "cache should contain the seeded alias")
	assert.Equal(t, "say", cached)
}

// TestManagerLoadAllSeedsAliasesDeterministicallyAcrossLoads verifies that
// cross-plugin duplicate alias resolution is stable across multiple LoadAll
// cycles. The Manager uses loadedOrder (slice) rather than iterating m.loaded
// (map) to preserve DAG/priority load order. Without this, Go's randomized
// map iteration would cause the "first plugin wins" contract to pick different
// winners on different runs.
func TestManagerLoadAllSeedsAliasesDeterministicallyAcrossLoads(t *testing.T) {
	const iterations = 25

	writeConflictingPlugins := func(t *testing.T, pluginsDir string) {
		// Two plugins both declare alias `"`. Priority determines load order
		// (lower first), so alpha should always win.
		alphaDir := filepath.Join(pluginsDir, "alpha")
		mkdirAll(t, alphaDir)
		writeFile(t, filepath.Join(alphaDir, "plugin.yaml"), []byte(`name: alpha
version: 1.0.0
type: lua
priority: 10
commands:
  - name: say
    aliases:
      - '"'
    help: Say something
lua-plugin:
  entry: main.lua`))
		writeFile(t, filepath.Join(alphaDir, "main.lua"), []byte("function on_event(e) end"))

		bravoDir := filepath.Join(pluginsDir, "bravo")
		mkdirAll(t, bravoDir)
		writeFile(t, filepath.Join(bravoDir, "plugin.yaml"), []byte(`name: bravo
version: 1.0.0
type: lua
priority: 20
commands:
  - name: shout
    aliases:
      - '"'
    help: Shout something
lua-plugin:
  entry: main.lua`))
		writeFile(t, filepath.Join(bravoDir, "main.lua"), []byte("function on_event(e) end"))
	}

	winners := make(map[string]int)
	for i := 0; i < iterations; i++ {
		dir := t.TempDir()
		pluginsDir := filepath.Join(dir, "plugins")
		writeConflictingPlugins(t, pluginsDir)

		luaHost := pluginlua.NewHost()
		repo := &fakeAliasSeederMgr{existing: make(map[string]string)}
		cache := command.NewAliasCache()

		mgr, mgrErr := plugins.NewManager(
			pluginsDir,
			plugins.WithLuaHost(luaHost),
			plugins.WithAliasSeeder(repo, cache),
			plugins.WithVerbRegistry(core.NewVerbRegistry()),
		)
		require.NoError(t, mgrErr)
		require.NoError(t, mgr.LoadAll(context.Background()))
		_ = luaHost.Close(context.Background())

		winners[repo.existing[`"`]]++
	}
	assert.Len(t, winners, 1, "alias winner must be deterministic across loads, got %v", winners)
	assert.Equal(t, iterations, winners["say"], "alpha (lower priority) should always win the alias")
}

func TestManagerLoadAllWithoutAliasSeederSkipsSeeding(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	commDir := filepath.Join(pluginsDir, "test-comm")
	mkdirAll(t, commDir)
	writeFile(t, filepath.Join(commDir, "plugin.yaml"), []byte(`name: test-comm
version: 1.0.0
type: lua
commands:
  - name: say
    aliases:
      - '"'
    help: Say something
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(commDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	// No WithAliasSeeder — seeding should be silently skipped.
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "LoadAll without alias seeder should not error")

	loaded := mgr.ListPlugins()
	assert.Contains(t, loaded, "test-comm", "plugin should still load without alias seeder")
}

// CollectResourceTypes is the exported test seam that backs the
// cross-plugin resource type collection used during LoadAll.

func TestCollectResourceTypesIncludesCoreTypes(t *testing.T) {
	known := plugins.CollectResourceTypes(nil)
	assert.True(t, known["character"], "core type 'character' must be included")
	assert.True(t, known["location"], "core type 'location' must be included")
	assert.True(t, known["command"], "core type 'command' must be included")
}

func TestCollectResourceTypesMergesPluginDeclaredTypes(t *testing.T) {
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "p1", ResourceTypes: []string{"widget"}}},
		{Manifest: &plugins.Manifest{Name: "p2", ResourceTypes: []string{"gadget", "gizmo"}}},
	}
	known := plugins.CollectResourceTypes(discovered)
	assert.True(t, known["widget"], "plugin-declared 'widget' should be present")
	assert.True(t, known["gadget"], "plugin-declared 'gadget' should be present")
	assert.True(t, known["gizmo"], "plugin-declared 'gizmo' should be present")
	assert.True(t, known["character"], "core types should still be present after merge")
	assert.True(t, known["location"], "core types should still be present after merge")
}

func TestCollectResourceTypesReturnsNewMapPerCall(t *testing.T) {
	first := plugins.CollectResourceTypes(nil)
	first["mutated"] = true
	second := plugins.CollectResourceTypes(nil)
	assert.False(t, second["mutated"], "subsequent calls must not see prior mutations")
}

// CollectActions is the exported test seam that backs the
// cross-plugin action collection used during LoadAll.

func TestCollectActionsIncludesCoreActions(t *testing.T) {
	known := plugins.CollectActions(nil)
	for _, action := range []string{"read", "write", "emit", "enter", "use", "delete", "execute", "admin"} {
		assert.True(t, known[action], "core action %q must be included", action)
	}
}

func TestCollectActionsMergesExplicitManifestActions(t *testing.T) {
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "p1", Actions: []string{"join"}}},
		{Manifest: &plugins.Manifest{Name: "p2", Actions: []string{"leave", "vote"}}},
	}
	known := plugins.CollectActions(discovered)
	assert.True(t, known["join"], "declared 'join' should be present")
	assert.True(t, known["leave"], "declared 'leave' should be present")
	assert.True(t, known["vote"], "declared 'vote' should be present")
	assert.True(t, known["read"], "core actions should still be present after merge")
}

func TestCollectActionsDeduplicatesAcrossPlugins(t *testing.T) {
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "p1", Actions: []string{"join"}}},
		{Manifest: &plugins.Manifest{Name: "p2", Actions: []string{"join"}}},
	}
	known := plugins.CollectActions(discovered)
	assert.True(t, known["join"], "'join' declared by two plugins should be present once")
}

func TestCollectActionsReturnsNewMapPerCall(t *testing.T) {
	first := plugins.CollectActions(nil)
	first["mutated"] = true
	second := plugins.CollectActions(nil)
	assert.False(t, second["mutated"], "subsequent calls must not see prior mutations")
}

func TestCollectActionsIgnoresCapabilityActionsNotInActionsField(t *testing.T) {
	// Only the explicit Actions manifest field feeds CollectActions.
	// Action strings in command capabilities are NOT auto-promoted.
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{
			Name: "p1",
			Commands: []plugins.CommandSpec{
				{Name: "channel", Capabilities: []command.Capability{
					{Action: "join", Resource: "channel"},
				}},
			},
			// No Actions field declared.
		}},
	}
	known := plugins.CollectActions(discovered)
	assert.False(t, known["join"], "'join' in capabilities but not in actions field must not appear")
}

// Semantic capability validation: loadPlugin must reject manifests whose
// commands declare capabilities on resource types that aren't in the
// cross-plugin known set.

func TestManagerLoadAllRejectsCommandCapabilityOnUnknownResourceType(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "alien-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: alien-plugin
version: 1.0.0
type: lua
commands:
  - name: probe
    capabilities:
      - action: read
        resource: alien
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.Error(t, err, "load should fail when capability targets an unknown resource type")
	assert.Contains(t, err.Error(), "alien")
	assert.Empty(t, mgr.ListPlugins(), "rejected plugin should not be listed as loaded")
}

func TestManagerLoadAllAcceptsCapabilityOnAnotherPluginsResourceType(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Plugin A declares the "widget" resource type so Plugin B's capability
	// referencing it becomes valid in the cross-plugin known set.
	declarerDir := filepath.Join(pluginsDir, "widget-declarer")
	mkdirAll(t, declarerDir)
	writeFile(t, filepath.Join(declarerDir, "plugin.yaml"), []byte(`name: widget-declarer
version: 1.0.0
type: binary
resource_types: [widget]
binary-plugin:
  executable: widget-declarer`))

	// Plugin B is a Lua consumer that needs the "widget" type known.
	consumerDir := filepath.Join(pluginsDir, "widget-consumer")
	mkdirAll(t, consumerDir)
	writeFile(t, filepath.Join(consumerDir, "plugin.yaml"), []byte(`name: widget-consumer
version: 1.0.0
type: lua
commands:
  - name: peek
    capabilities:
      - action: read
        resource: widget
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(consumerDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	// No binary host registered — declarer is silently skipped, but its
	// resource_types still feed CollectResourceTypes during Phase 2.
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "consumer should validate against declarer's resource type")
	assert.Contains(t, mgr.ListPlugins(), "widget-consumer")
}

// Semantic action validation: loadPlugin must reject manifests whose commands
// declare capabilities on actions that aren't in the cross-plugin known set.

func TestManagerLoadAllRejectsCommandCapabilityOnUnknownAction(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "channel-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: channel-plugin
version: 1.0.0
type: lua
commands:
  - name: channel
    capabilities:
      - action: join
        resource: location
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.Error(t, err, "load should fail when capability uses an undeclared action")
	assert.Contains(t, err.Error(), "join")
	assert.Empty(t, mgr.ListPlugins(), "no plugins should be registered after a load failure")
}

func TestManagerLoadAllAcceptsCapabilityWithDeclaredAction(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "channel-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: channel-plugin
version: 1.0.0
type: lua
actions: [join, leave]
commands:
  - name: channel
    capabilities:
      - action: join
        resource: location
      - action: leave
        resource: location
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "load should succeed when action is declared in the plugin manifest")
	assert.Contains(t, mgr.ListPlugins(), "channel-plugin")
}

func TestManagerLoadAllAcceptsCapabilityOnAnotherPluginsAction(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Plugin A declares the "join" action so Plugin B's capability is valid.
	declarerDir := filepath.Join(pluginsDir, "action-declarer")
	mkdirAll(t, declarerDir)
	writeFile(t, filepath.Join(declarerDir, "plugin.yaml"), []byte(`name: action-declarer
version: 1.0.0
type: binary
actions: [join]
binary-plugin:
  executable: action-declarer`))

	// Plugin B uses "join" declared by Plugin A.
	consumerDir := filepath.Join(pluginsDir, "action-consumer")
	mkdirAll(t, consumerDir)
	writeFile(t, filepath.Join(consumerDir, "plugin.yaml"), []byte(`name: action-consumer
version: 1.0.0
type: lua
commands:
  - name: channel
    capabilities:
      - action: join
        resource: location
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(consumerDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	// No binary host — declarer is silently skipped, but its actions still
	// feed CollectActions during Phase 2.
	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "consumer should validate against declarer's action")
	assert.Contains(t, mgr.ListPlugins(), "action-consumer")
}

func TestManagerLoadAllAcceptsPluginRedeclaringCoreAction(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "reader-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: reader-plugin
version: 1.0.0
type: lua
actions: [read]
commands:
  - name: look
    capabilities:
      - action: read
        resource: location
lua-plugin:
  entry: main.lua`))
	writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "re-declaring a core action in the actions field should not prevent loading")
	assert.Contains(t, mgr.ListPlugins(), "reader-plugin")
}

// WithTrustAllowlist is plumbed through but only takes effect when policies
// install. The option itself is verified by ensuring no panic / no behavior
// change for plugins that don't request escalation.

func TestManagerWithTrustAllowlistDoesNotInterfereWithBasicLoad(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithTrustAllowlist([]string{"trusted-one", "trusted-two"}),
		plugins.WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, mgrErr)
	// LoadAll on an empty plugins dir should still succeed.
	require.NoError(t, mgr.LoadAll(context.Background()))
}

// A trust-allowlisted plugin name that does not match any discovered plugin
// is almost certainly a typo or stale operator config — it silently grants
// no trust to the intended plugin, and reserves the allowlist slot for a
// future plugin that might be crafted with that name. LoadAll MUST log a
// slog.Warn naming each unknown entry so the misconfiguration surfaces.
func TestManagerLoadAllWarnsOnTrustAllowlistedPluginNotDiscovered(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	// Capture slog output for the duration of LoadAll.
	var buf bytes.Buffer
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(oldDefault)

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithTrustAllowlist([]string{"ghost-plugin", "phantom-plugin"}),
		plugins.WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, mgrErr)
	require.NoError(t, mgr.LoadAll(context.Background()))

	logs := buf.String()
	// A single warning enumerating the unknown names is acceptable; so is
	// one warning per name. Either way the operator needs to see each name.
	assert.True(t, strings.Contains(logs, "level=WARN"),
		"expected a slog.Warn entry, got: %s", logs)
	assert.Contains(t, logs, "ghost-plugin",
		"warning should mention unknown trust-allowlisted plugin name")
	assert.Contains(t, logs, "phantom-plugin",
		"warning should mention unknown trust-allowlisted plugin name")
}

// When every trust-allowlisted name matches a discovered plugin, LoadAll
// MUST NOT emit an "unknown trust-allowlisted plugin" warning.
func TestManagerLoadAllDoesNotWarnWhenAllowlistMatchesDiscoveredPlugin(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Minimal valid Lua plugin named "trusted-one".
	pluginDir := filepath.Join(pluginsDir, "trusted-one")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(
		"name: trusted-one\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua",
	))
	writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte("function on_event(e) end"))

	var buf bytes.Buffer
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(oldDefault)

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithLuaHost(luaHost),
		plugins.WithTrustAllowlist([]string{"trusted-one"}),
		plugins.WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, mgrErr)
	require.NoError(t, mgr.LoadAll(context.Background()))

	assert.NotContains(t, buf.String(), "trust-allowlisted plugin not discovered",
		"no unknown-allowlist warning should fire when every entry matches")
}

// LoadAll strict mode error joining: a single failing plugin produces a
// joined error with PLUGIN_LOAD_FAILED code. Multiple failures are joined.

func TestManagerLoadAllStrictModeJoinsMultipleErrors(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Two broken Lua plugins. Strict mode collects both errors before
	// returning a joined failure with the PLUGIN_LOAD_FAILED code and a
	// failed_count context attribute reflecting both failures.
	for _, name := range []string{"broken-one", "broken-two"} {
		pluginDir := filepath.Join(pluginsDir, name)
		mkdirAll(t, pluginDir)
		writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(
			"name: "+name+"\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua",
		))
		writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte("function broken"))
	}

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.Error(t, err, "strict mode should fail when plugins have load errors")

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "joined error should be an oops error")
	assert.Equal(t, "PLUGIN_LOAD_FAILED", oopsErr.Code(),
		"joined load failure should carry PLUGIN_LOAD_FAILED code")
	assert.Equal(t, 2, oopsErr.Context()["failed_count"],
		"failed_count should match the number of broken plugins")
	assert.Empty(t, mgr.ListPlugins(), "no plugins should remain loaded after strict-mode failure")
}

// stubAttributeResolverClient lets tests drive GetSchema/ResolveResource
// behavior for the manager's discoverAndRegisterAttributes path.
type stubAttributeResolverClient struct {
	schemaResp *pluginv1.GetSchemaResponse
	schemaErr  error
}

func (s *stubAttributeResolverClient) GetSchema(_ context.Context, _ *pluginv1.GetSchemaRequest, _ ...grpc.CallOption) (*pluginv1.GetSchemaResponse, error) {
	return s.schemaResp, s.schemaErr
}

func (s *stubAttributeResolverClient) ResolveResource(_ context.Context, _ *pluginv1.ResolveResourceRequest, _ ...grpc.CallOption) (*pluginv1.ResolveResourceResponse, error) {
	return nil, nil
}

// arBinaryHost extends mockBinaryHost with AttributeResolverProvider so the
// manager can exercise schema discovery during loadPlugin.
type arBinaryHost struct {
	mockBinaryHost
	arClient pluginv1.AttributeResolverServiceClient
}

func (h *arBinaryHost) AttributeResolverClient(_ string) pluginv1.AttributeResolverServiceClient {
	return h.arClient
}

func TestManagerLoadAllFailsWhenSchemaDiscoveryReturnsError(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "broken-schema")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: broken-schema
version: 1.0.0
type: binary
resource_types: [widget]
binary-plugin:
  executable: broken-schema`))

	host := &arBinaryHost{
		mockBinaryHost: mockBinaryHost{conn: &stubClientConn{}},
		arClient:       &stubAttributeResolverClient{schemaErr: errors.New("schema rpc failed")},
	}

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	mgr.RegisterHost(plugins.TypeBinary, host)

	err := mgr.LoadAll(context.Background())
	require.Error(t, err, "schema discovery failure should be a hard load error")
	assert.Contains(t, err.Error(), "schema discovery failed")
	assert.Empty(t, mgr.ListPlugins(), "plugin should not be marked loaded after rollback")
}

func TestManagerLoadAllFailsWhenSchemaMissingDeclaredResourceType(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "missing-rt")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: missing-rt
version: 1.0.0
type: binary
resource_types: [widget]
binary-plugin:
  executable: missing-rt`))

	// GetSchema returns a schema for "gadget" — the manifest declared "widget"
	// so the cross-check inside discoverAndRegisterAttributes should reject it.
	host := &arBinaryHost{
		mockBinaryHost: mockBinaryHost{conn: &stubClientConn{}},
		arClient: &stubAttributeResolverClient{
			schemaResp: &pluginv1.GetSchemaResponse{
				ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
					"gadget": {Attributes: map[string]pluginv1.AttributeType{}},
				},
			},
		},
	}

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	mgr.RegisterHost(plugins.TypeBinary, host)

	err := mgr.LoadAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "widget")
}

func TestManagerLoadAllFailsWhenHostMissingAttributeResolverProvider(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "no-ar-host")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: no-ar-host
version: 1.0.0
type: binary
resource_types: [widget]
binary-plugin:
  executable: no-ar-host`))

	// mockBinaryHost does NOT implement AttributeResolverProvider; the manager
	// should reject the plugin because resource_types requires that capability.
	host := &mockBinaryHost{conn: &stubClientConn{}}

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	mgr.RegisterHost(plugins.TypeBinary, host)

	err := mgr.LoadAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AttributeResolverProvider")
	assert.Empty(t, mgr.ListPlugins())
}

func TestManagerLoadAllUnregistersAttributeProviderWhenSchemaValidationFailsAfterRegistration(t *testing.T) {
	// T35: resolver rollback completeness. When ValidateManifestPolicySchemas
	// rejects a manifest (e.g., policy references an attribute not in the
	// discovered schema), the manager must unregister the attribute providers
	// that discoverAndRegisterAttributes added moments earlier. Otherwise the
	// ABAC resolver retains a stale provider tied to a plugin that never
	// finished loading, and a subsequent retry hits "already registered".
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "bad-widget")
	mkdirAll(t, pluginDir)
	// Policy references resource.widget.tipe but the schema exposes "type".
	// ValidateManifestPolicySchemas rejects this at load time, after
	// discoverAndRegisterAttributes has already registered the widget provider.
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: bad-widget
version: 1.0.0
type: binary
resource_types: [widget]
binary-plugin:
  executable: bad-widget
policies:
  - name: widget-read-typo
    dsl: |
      permit(principal is character, action in ["read"], resource is widget)
      when { resource.widget.tipe == "normal" };
`))

	host := &arBinaryHost{
		mockBinaryHost: mockBinaryHost{conn: &stubClientConn{}},
		arClient: &stubAttributeResolverClient{
			schemaResp: &pluginv1.GetSchemaResponse{
				ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
					"widget": {Attributes: map[string]pluginv1.AttributeType{
						"type": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					}},
				},
			},
		},
	}

	var registered []string
	var unregistered []string
	registrar := func(p *plugins.PluginAttributeProvider) error {
		registered = append(registered, p.Namespace())
		return nil
	}
	unregistrar := func(namespace string) bool {
		unregistered = append(unregistered, namespace)
		return true
	}

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithAttributeProviderRegistrar(registrar),
		plugins.WithAttributeProviderUnregistrar(unregistrar),
		plugins.WithVerbRegistry(core.NewVerbRegistry()),
	)
	require.NoError(t, mgrErr)
	mgr.RegisterHost(plugins.TypeBinary, host)

	err := mgr.LoadAll(context.Background())
	require.Error(t, err, "manifest with schema-mismatched policy must fail to load")
	assert.Contains(t, err.Error(), "validate manifest policy schemas")

	// Assert rollback: the provider registered during
	// discoverAndRegisterAttributes was also unregistered.
	assert.Equal(t, []string{"widget"}, registered,
		"provider must be registered before the validation error")
	assert.Equal(t, []string{"widget"}, unregistered,
		"rollback must unregister every provider that was registered")
	assert.Empty(t, mgr.ListPlugins(),
		"plugin must not be marked loaded after validation rollback")
}

func TestManagerLoadAllRegistersAttributeProviderViaCallback(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	pluginDir := filepath.Join(pluginsDir, "good-widget")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(`name: good-widget
version: 1.0.0
type: binary
resource_types: [widget]
binary-plugin:
  executable: good-widget`))

	host := &arBinaryHost{
		mockBinaryHost: mockBinaryHost{conn: &stubClientConn{}},
		arClient: &stubAttributeResolverClient{
			schemaResp: &pluginv1.GetSchemaResponse{
				ResourceTypes: map[string]*pluginv1.ResourceTypeSchema{
					"widget": {Attributes: map[string]pluginv1.AttributeType{
						"type": pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING,
					}},
				},
			},
		},
	}

	var registered []*plugins.PluginAttributeProvider
	registrar := func(p *plugins.PluginAttributeProvider) error {
		registered = append(registered, p)
		return nil
	}

	mgr, mgrErr := plugins.NewManager(pluginsDir, plugins.WithAttributeProviderRegistrar(registrar), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	mgr.RegisterHost(plugins.TypeBinary, host)

	require.NoError(t, mgr.LoadAll(context.Background()))
	require.Len(t, registered, 1, "manager should register one provider per declared resource type")
	assert.Equal(t, "widget", registered[0].Namespace())
	assert.Contains(t, mgr.ListPlugins(), "good-widget")
}

// newTestManager creates a Manager with a temp dir for unit tests.
func newTestManager(t *testing.T) *plugins.Manager {
	t.Helper()
	mgr, err := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, err)
	return mgr
}

// loadPlugin registers a fake plugin manifest with the manager.
// sessionStreams controls whether Manifest.SessionStreams is true.
func loadPlugin(t *testing.T, m *plugins.Manager, name string, plugType plugins.Type, sessionStreams bool) {
	t.Helper()
	manifest := &plugins.Manifest{
		Name:           name,
		Version:        "1.0.0",
		Type:           plugType,
		SessionStreams: sessionStreams,
	}
	if plugType == plugins.TypeLua {
		manifest.LuaPlugin = &plugins.LuaConfig{Entry: "main.lua"}
	}
	if plugType == plugins.TypeBinary {
		manifest.BinaryPlugin = &plugins.BinaryConfig{Executable: "plugin"}
	}
	m.TestLoadPlugin(name, manifest)
}

func TestManagerQuerySessionStreamsReturnsNilWhenNoOptedInPlugins(t *testing.T) {
	m := newTestManager(t)
	result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
		CharacterID: "char-1",
		PlayerID:    "player-1",
		SessionID:   "sess-1",
	})
	assert.Nil(t, result)
}

func TestManagerQuerySessionStreamsMergesContributionsFromMultiplePlugins(t *testing.T) {
	m := newTestManager(t)

	hostA := mocks.NewMockHost(t)
	hostA.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	hostA.EXPECT().QuerySessionStreams(mock.Anything, "plugin-a", mock.Anything).
		Return([]string{"channel:abc", "channel:shared"}, nil)
	m.RegisterHost(plugins.TypeLua, hostA)

	hostB := mocks.NewMockHost(t)
	hostB.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	hostB.EXPECT().QuerySessionStreams(mock.Anything, "plugin-b", mock.Anything).
		Return([]string{"channel:shared", "channel:def"}, nil) // channel:shared is a duplicate
	m.RegisterHost(plugins.TypeBinary, hostB)

	loadPlugin(t, m, "plugin-a", plugins.TypeLua, true) // session_streams: true
	loadPlugin(t, m, "plugin-b", plugins.TypeBinary, true)

	result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
		CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
	})

	assert.ElementsMatch(t, []string{"channel:abc", "channel:shared", "channel:def"}, result)
}

func TestManagerQuerySessionStreamsDegradeOnSinglePluginError(t *testing.T) {
	m := newTestManager(t)

	hostA := mocks.NewMockHost(t)
	hostA.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	hostA.EXPECT().QuerySessionStreams(mock.Anything, "plugin-a", mock.Anything).
		Return(nil, errors.New("db unavailable"))
	m.RegisterHost(plugins.TypeLua, hostA)

	hostB := mocks.NewMockHost(t)
	hostB.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	hostB.EXPECT().QuerySessionStreams(mock.Anything, "plugin-b", mock.Anything).
		Return([]string{"channel:abc"}, nil)
	m.RegisterHost(plugins.TypeBinary, hostB)

	loadPlugin(t, m, "plugin-a", plugins.TypeLua, true)
	loadPlugin(t, m, "plugin-b", plugins.TypeBinary, true)

	result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
		CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
	})

	assert.Equal(t, []string{"channel:abc"}, result)
}

func TestManagerQuerySessionStreamsSkipsOptedOutPlugins(t *testing.T) {
	m := newTestManager(t)

	host := mocks.NewMockHost(t)
	host.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	// QuerySessionStreams must NOT be called on opted-out plugin
	m.RegisterHost(plugins.TypeLua, host)
	loadPlugin(t, m, "plugin-a", plugins.TypeLua, false) // session_streams: false

	result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
		CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
	})
	assert.Nil(t, result)
	// testify/mock will fail the test if QuerySessionStreams was called unexpectedly
}

func TestManagerQuerySessionStreamsDropsInvalidStreamNames(t *testing.T) {
	m := newTestManager(t)
	host := mocks.NewMockHost(t)
	host.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	host.EXPECT().QuerySessionStreams(mock.Anything, "plugin-a", mock.Anything).
		Return([]string{
			"",              // empty — invalid
			"nocolon",       // no colon — invalid
			"has space:abc", // whitespace — invalid
			"channel:valid", // valid
		}, nil)
	m.RegisterHost(plugins.TypeLua, host)
	loadPlugin(t, m, "plugin-a", plugins.TypeLua, true)

	result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
		CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
	})
	assert.Equal(t, []string{"channel:valid"}, result)
}

func TestManagerQuerySessionStreamsReturnsEarlyOnContextCancellation(t *testing.T) {
	m := newTestManager(t)
	host := mocks.NewMockHost(t)
	host.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	// Plugin blocks forever — context cancellation should rescue us.
	// Use Maybe() since the goroutine may not start before context is cancelled.
	host.EXPECT().QuerySessionStreams(mock.Anything, "plugin-a", mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, _ plugins.SessionStreamsRequest) ([]string, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}).Maybe()
	m.RegisterHost(plugins.TypeLua, host)
	loadPlugin(t, m, "plugin-a", plugins.TypeLua, true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result := m.QuerySessionStreams(ctx, plugins.SessionStreamsRequest{
		CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
	})
	// Should return empty/partial results instead of blocking
	assert.Empty(t, result)
}

func TestManagerLoadAllRegistersVerbsFromManifest(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	echoDir := filepath.Join(pluginsDir, "chat-plugin")
	mkdirAll(t, echoDir)
	manifest := `name: chat-plugin
version: 1.0.0
type: lua
verbs:
  - type: whisper
    category: communication
    format: speech
    label: whispers to
    display_target: terminal
  - type: shout
    category: communication
    format: speech
    label: shouts
    display_target: both
lua-plugin:
  entry: main.lua`
	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	reg := core.NewVerbRegistry()
	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithLuaHost(luaHost),
		plugins.WithVerbRegistry(reg),
	)
	require.NoError(t, mgrErr)
	require.NoError(t, mgr.LoadAll(context.Background()))

	whisper, ok := reg.Lookup("whisper")
	require.True(t, ok, "whisper verb should be registered")
	assert.Equal(t, "communication", whisper.Category)
	assert.Equal(t, "speech", whisper.Format)
	assert.Equal(t, "whispers to", whisper.Label)
	assert.Equal(t, "chat-plugin", whisper.Source)

	shout, ok := reg.Lookup("shout")
	require.True(t, ok, "shout verb should be registered")
	assert.Equal(t, "chat-plugin", shout.Source)

	// Manifest version MUST flow into the registry's source-version map so
	// RenderingPublisher can stamp source_plugin_version on emitted events.
	// A regression to plain Register would still satisfy the Source check
	// above but break downstream rendering audit fidelity.
	assert.Equal(t, "1.0.0", reg.SourceVersion("chat-plugin"))
}

func TestManagerLoadAllRejectsPluginWithDuplicateVerbType(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	plugDir := filepath.Join(pluginsDir, "dup-plugin")
	mkdirAll(t, plugDir)
	manifest := `name: dup-plugin
version: 1.0.0
type: lua
verbs:
  - type: existing_verb
    category: communication
    format: action
    display_target: terminal
lua-plugin:
  entry: main.lua`
	writeFile(t, filepath.Join(plugDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(plugDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	reg := core.NewVerbRegistry()
	// Pre-register a verb that the plugin also declares.
	require.NoError(t, reg.RegisterWithSource(core.VerbRegistration{
		Type:          "existing_verb",
		Category:      "state",
		Format:        "snapshot",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE,
		Source:        "builtin",
	}, "host-test"))

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithLuaHost(luaHost),
		plugins.WithVerbRegistry(reg),
	)
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register plugin verb")
	errutil.AssertErrorCode(t, err, "DUPLICATE_REGISTRATION")
}

func TestManagerLoadAllCleansUpVerbsOnPartialFailure(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	plugDir := filepath.Join(pluginsDir, "partial-plugin")
	mkdirAll(t, plugDir)
	manifest := `name: partial-plugin
version: 1.0.0
type: lua
verbs:
  - type: good_verb
    category: communication
    format: action
    display_target: terminal
  - type: conflict
    category: state
    format: snapshot
    display_target: state
lua-plugin:
  entry: main.lua`
	writeFile(t, filepath.Join(plugDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(plugDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	reg := core.NewVerbRegistry()
	// Pre-register the conflict verb so the second registration fails.
	require.NoError(t, reg.RegisterWithSource(core.VerbRegistration{
		Type:          "conflict",
		Category:      "state",
		Format:        "snapshot",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE,
		Source:        "builtin",
	}, "host-test"))

	mgr, mgrErr := plugins.NewManager(
		pluginsDir,
		plugins.WithLuaHost(luaHost),
		plugins.WithVerbRegistry(reg),
	)
	require.NoError(t, mgrErr)
	err := mgr.LoadAll(context.Background())
	require.Error(t, err)

	// good_verb should have been cleaned up via UnregisterBySource.
	_, ok := reg.Lookup("good_verb")
	assert.False(t, ok, "good_verb should have been cleaned up after partial failure")

	// The pre-existing conflict verb should remain (owned by builtin, not the plugin).
	conflict, ok := reg.Lookup("conflict")
	require.True(t, ok, "builtin conflict verb should still exist")
	assert.Equal(t, "builtin", conflict.Source)
}

// TestPluginCanReadBack verifies Manager.PluginCanReadBack against the
// crypto.emits[].readback field (INV-CRYPTO-27).
func TestPluginCanReadBack(t *testing.T) {
	t.Parallel()
	m := newTestManagerWithManifest(t, &plugins.Manifest{
		Name: "core-scenes",
		Crypto: &plugins.CryptoSection{Emits: []plugins.CryptoEmit{
			{EventType: "scene_pose", Sensitivity: plugins.SensitivityAlways, Readback: true},
			{EventType: "scene_join_ic", Sensitivity: plugins.SensitivityNever},
		}},
	})
	assert.True(t, m.PluginCanReadBack("core-scenes", "scene_pose"))
	assert.False(t, m.PluginCanReadBack("core-scenes", "scene_join_ic"), "readback not set")
	assert.False(t, m.PluginCanReadBack("core-scenes", "unknown"), "type not emitted")
	assert.False(t, m.PluginCanReadBack("other", "scene_pose"), "wrong plugin")
}

// TestNewManagerRequiresVerbRegistry pins INV-EVENTBUS-11: every plugin manager
// MUST be constructed with a non-nil VerbRegistry. Omitting the option
// returns ErrMissingVerbRegistry rather than silently skipping verb
// registration.
func TestNewManagerRequiresVerbRegistry(t *testing.T) {
	mgr, err := plugins.NewManager(t.TempDir())
	require.Error(t, err)
	require.ErrorIs(t, err, plugins.ErrMissingVerbRegistry)
	require.Nil(t, mgr)
	errutil.AssertErrorCode(t, err, plugins.CodeMissingVerbRegistry)
}

// stubFocusCoordinator is a minimal focus.Coordinator for manager tests.
type stubFocusCoordinator struct{}

func (s *stubFocusCoordinator) JoinFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
}

func (s *stubFocusCoordinator) LeaveFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
}

func (s *stubFocusCoordinator) LeaveFocusByTarget(_ context.Context, _ session.FocusKey) (session.LeaveByTargetResult, error) {
	return session.LeaveByTargetResult{}, nil
}

func (s *stubFocusCoordinator) PresentFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
}

func (s *stubFocusCoordinator) RestoreFocus(_ context.Context, _ string) (focus.RestorePlan, error) {
	return focus.RestorePlan{}, nil
}

func (s *stubFocusCoordinator) IsAnyConnFocused(_ context.Context, _, _ ulid.ULID) (bool, error) {
	return false, nil
}

func (s *stubFocusCoordinator) RestoreConnectionFocus(_ context.Context, _ string, _ ulid.ULID) error {
	return nil
}

func (s *stubFocusCoordinator) SetConnectionFocus(_ context.Context, _ ulid.ULID, _ *session.FocusKey, _ bool) (focus.SetConnectionFocusResult, error) {
	return focus.SetConnectionFocusResult{}, nil
}

func (s *stubFocusCoordinator) AutoFocusOnJoin(_ context.Context, _, _ ulid.ULID) (focus.AutoFocusOnJoinResponse, error) {
	return focus.AutoFocusOnJoinResponse{}, nil
}

var _ focus.Coordinator = (*stubFocusCoordinator)(nil)

func TestConfigureFocusDepsInjectsCoordinatorIntoLuaHost(t *testing.T) {
	hf := hostfunc.New(nil)
	luaHost := pluginlua.NewHostWithFunctions(hf)
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

	fc := &stubFocusCoordinator{}
	var hr plugins.HistoryReader // nil — acceptable for this test

	// Must not panic; calls SetFocusCoordinator and SetHistoryReader on all
	// FocusDepsConfigurer hosts registered in the manager.
	require.NotPanics(t, func() {
		mgr.ConfigureFocusDeps(fc, hr)
	})
}

func TestConfigureFocusDepsWithNilLuaHostDoesNotPanic(t *testing.T) {
	// Manager without a Lua host — ConfigureFocusDeps must handle nil luaHost.
	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	require.NotPanics(t, func() {
		mgr.ConfigureFocusDeps(nil, nil)
	})
}

// stubReadbackDecryptor satisfies plugins.ReadbackDecryptor for the
// ConfigureReadbackDecryptor injection tests.
type stubReadbackDecryptor struct{}

func (s *stubReadbackDecryptor) DecryptOwnRow(_ context.Context, _, _ string, _ *pluginv1.AuditRow) *pluginv1.RowResult {
	return &pluginv1.RowResult{}
}

func (s *stubReadbackDecryptor) DecryptOwnRows(_ context.Context, _, _ string, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error) {
	return make([]*pluginv1.RowResult, len(rows)), nil
}

var _ plugins.ReadbackDecryptor = (*stubReadbackDecryptor)(nil)

func TestConfigureReadbackDecryptorInjectsDecryptorIntoLuaHost(t *testing.T) {
	hf := hostfunc.New(nil)
	luaHost := pluginlua.NewHostWithFunctions(hf)
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithLuaHost(luaHost), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)

	// Must not panic; calls SetReadbackDecryptor on all ReadbackDepsConfigurer
	// hosts registered in the manager.
	require.NotPanics(t, func() {
		mgr.ConfigureReadbackDecryptor(&stubReadbackDecryptor{})
	})
}

func TestConfigureReadbackDecryptorWithNilLuaHostDoesNotPanic(t *testing.T) {
	// Manager without a Lua host — ConfigureReadbackDecryptor must handle nil luaHost.
	mgr, mgrErr := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, mgrErr)
	require.NotPanics(t, func() {
		mgr.ConfigureReadbackDecryptor(nil)
	})
}

// TestEmitPluginEventPropagatesSensitive asserts that the Manager's
// EmitPluginEvent boundary copies EmitEvent.Sensitive into the
// constructed EmitIntent.Sensitive — closing the full chain from
// Lua (or any plugin source) through to the host fence and onward
// to the published eventbus.Event.
//
// Approach: register a recordingPublisher (defined in
// event_emitter_crypto_test.go), load a fake plugin manifest that
// declares "core-test:hello" with sensitivity=may, then call
// EmitPluginEvent with EmitEvent.Sensitive=true and assert the
// recorded eventbus.Event.Sensitive=true.
func TestEmitPluginEventPropagatesSensitive(t *testing.T) {
	pub := &recordingPublisher{}

	mgr, err := plugins.NewManager(t.TempDir(), plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, err)

	// Register the manifest so the emitter's manifest lookup succeeds and
	// the Phase 3a fence sees sensitivity=may. Use TestLoadPlugin which
	// registers a manifest without requiring a real host load step.
	manifest := &plugins.Manifest{
		Name:                "core-test",
		Version:             "1.0.0",
		Type:                plugins.TypeLua,
		Emits:               []string{"location"},
		ActorKindsClaimable: []string{"plugin", "character"},
		LuaPlugin:           &plugins.LuaConfig{Entry: "main.lua"},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "core-test:hello", Sensitivity: plugins.SensitivityMay},
			},
		},
	}
	// Need a host registered for TypeLua so TestLoadPlugin can map it.
	mockLua := mocks.NewMockHost(t)
	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mgr.RegisterHost(plugins.TypeLua, mockLua)
	mgr.TestLoadPlugin("core-test", manifest)

	// Wire the shared emitter with crypto enabled so the fence runs and
	// stamps sensitive=true (manifest=may + intent.Sensitive=true → SensitivityAlways).
	mgr.ConfigureEventEmitter(pub, plugins.WithCryptoEnabled(true))

	// Emit must run with a plugin-actor context so the emitter resolves
	// an actor; ActorPlugin matches the manifest's actor_kinds_claimable.
	// Post-w9ml: Actor.ID MUST be a ULID; use a deterministic fixture.
	ctx := core.WithActor(context.Background(), core.Actor{
		Kind: core.ActorPlugin,
		ID:   ulid.MustNew(0xC0FFEE, bytes.NewReader(make([]byte, 16))).String(),
	})

	err = mgr.EmitPluginEvent(ctx, "core-test", pluginsdk.EmitEvent{
		Stream:    "location.01HXXXTESTLOC00000000000",
		Type:      "core-test:hello",
		Payload:   `{"msg":"private"}`,
		Sensitive: true,
	})
	require.NoError(t, err)

	require.Len(t, pub.events, 1)
	assert.True(t, pub.events[0].Sensitive,
		"EmitEvent.Sensitive=true MUST propagate through manager.EmitPluginEvent → EmitIntent.Sensitive → event.Sensitive")
}

// writeINVS5LuaPlugin writes a synthetic Lua plugin under pluginsDir whose
// main.lua calls register_emit_type with `registered` and whose plugin.yaml
// declares `declared` in crypto.emits. Used by INV-PLUGIN-32 manager validator tests.
func writeINVS5LuaPlugin(t *testing.T, pluginsDir, name string, declared, registered []string) {
	t.Helper()
	dir := filepath.Join(pluginsDir, name)
	mkdirAll(t, dir)

	var manifest strings.Builder
	manifest.WriteString("name: ")
	manifest.WriteString(name)
	manifest.WriteString("\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua\n")
	if len(declared) > 0 {
		manifest.WriteString("crypto:\n  emits:\n")
		for _, t := range declared {
			manifest.WriteString("    - event_type: " + t + "\n      sensitivity: never\n")
		}
	}
	writeFile(t, filepath.Join(dir, "plugin.yaml"), []byte(manifest.String()))

	var main strings.Builder
	for _, t := range registered {
		main.WriteString(`holomush.register_emit_type("` + t + "\")\n")
	}
	main.WriteString("function on_event(event) return nil end\n")
	writeFile(t, filepath.Join(dir, "main.lua"), []byte(main.String()))
}

// TestManagerLoadAll_INVS5 covers the manager-level INV-PLUGIN-32 wiring across
// mismatch (declared/registered set inequality), matching-sets (equal sets),
// and no-crypto-emits (validator gated off via INV-M1) scenarios. Each case
// shares the same setup → LoadAll → assert flow; a table form keeps future
// scenario additions cheap.
func TestManagerLoadAll_INVS5(t *testing.T) {
	tests := []struct {
		name              string
		pluginName        string
		declaredEmits     []string
		registeredEmits   []string
		expectError       bool
		expectedErrorCode string // checked when expectError is true
		assertMsg         string
	}{
		{
			name:              "mismatch fails load with canonical EVENT_TYPE_REGISTRY_MISMATCH",
			pluginName:        "mismatched",
			declaredEmits:     []string{"a", "b"},
			registeredEmits:   []string{"a"},
			expectError:       true,
			expectedErrorCode: "EVENT_TYPE_REGISTRY_MISMATCH",
			assertMsg:         "INV-PLUGIN-32 mismatch must surface as a load error",
		},
		{
			name:            "matching sets load successfully",
			pluginName:      "matched",
			declaredEmits:   []string{"a", "b"},
			registeredEmits: []string{"a", "b"},
			assertMsg:       "matched INV-PLUGIN-32 sets must load cleanly",
		},
		{
			name:       "no crypto.emits skips validator entirely (INV-M1 gate)",
			pluginName: "no-emits",
			assertMsg:  "plugins without crypto.emits must load cleanly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			pluginsDir := filepath.Join(dir, "plugins")
			writeINVS5LuaPlugin(t, pluginsDir, tt.pluginName, tt.declaredEmits, tt.registeredEmits)

			luaHost := pluginlua.NewHostWithFunctions(hostfunc.New(nil))
			t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

			mgr, mgrErr := plugins.NewManager(
				pluginsDir,
				plugins.WithLuaHost(luaHost),
				plugins.WithVerbRegistry(core.NewVerbRegistry()),
			)
			require.NoError(t, mgrErr)

			err := mgr.LoadAll(context.Background())
			if tt.expectError {
				require.Error(t, err, tt.assertMsg)
				errutil.AssertErrorCode(t, err, tt.expectedErrorCode)
				return
			}
			require.NoError(t, err, tt.assertMsg)
			require.Contains(t, mgr.ListPlugins(), tt.pluginName)
		})
	}
}
