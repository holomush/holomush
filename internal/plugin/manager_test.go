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

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/plugin/mocks"
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

func TestManager_Discover(t *testing.T) {
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
	writeFile(t, filepath.Join(echoDir, "plugins.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte("function on_event(e) end"))

	mgr := plugins.NewManager(filepath.Join(dir, "plugins"))
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)

	require.Len(t, manifests, 1)
	assert.Equal(t, "echo-bot", manifests[0].Manifest.Name)
	assert.Equal(t, echoDir, manifests[0].Dir)
}

func TestManager_Discover_SkipsInvalidPlugins(t *testing.T) {
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
	writeFile(t, filepath.Join(validDir, "plugins.yaml"), []byte(validManifest))
	writeFile(t, filepath.Join(validDir, "main.lua"), []byte(""))

	// Create invalid plugin (bad YAML)
	invalidDir := filepath.Join(pluginsDir, "invalid")
	mkdirAll(t, invalidDir)
	writeFile(t, filepath.Join(invalidDir, "plugins.yaml"), []byte("invalid: ["))

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	// Should succeed but only return valid plugin
	require.NoError(t, err)
	assert.Len(t, manifests, 1, "len(manifests) should be 1 (valid only)")
}

func TestManager_Discover_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, manifests, "len(manifests) should be 0 for empty directory")
}

func TestManager_Discover_NonExistentDirectory(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "non-existent-plugins")

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err, "Discover() should handle non-existent dir gracefully")
	assert.Empty(t, manifests, "len(manifests) should be 0 for non-existent directory")
}

func TestManager_Discover_SkipsFilesNotDirectories(t *testing.T) {
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
	writeFile(t, filepath.Join(validDir, "plugins.yaml"), []byte(validManifest))
	writeFile(t, filepath.Join(validDir, "main.lua"), []byte(""))

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	assert.Len(t, manifests, 1, "len(manifests) should be 1 (files should be skipped)")
}

func TestManager_Discover_SkipsDirWithoutManifest(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create directory without plugins.yaml
	noManifestDir := filepath.Join(pluginsDir, "no-manifest")
	mkdirAll(t, noManifestDir)
	// Only create a lua file, no plugins.yaml
	writeFile(t, filepath.Join(noManifestDir, "main.lua"), []byte(""))

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, manifests, "len(manifests) should be 0 (dir without manifest should be skipped)")
}

func TestManager_Discover_MultiplePlugins(t *testing.T) {
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
		writeFile(t, filepath.Join(pluginDir, "plugins.yaml"), []byte(manifest))
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

func TestManager_Discover_BinaryPlugin(t *testing.T) {
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
	writeFile(t, filepath.Join(binaryDir, "plugins.yaml"), []byte(manifest))

	mgr := plugins.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.Equal(t, plugins.TypeBinary, manifests[0].Manifest.Type)
}

func TestManager_ListPlugins_NoPluginsLoaded(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugins.NewManager(pluginsDir)
	plugins := mgr.ListPlugins()
	assert.Empty(t, plugins, "ListPlugins() should return empty slice before any plugins loaded")
}

func TestManager_LoadAll_LuaPlugins(t *testing.T) {
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
	writeFile(t, filepath.Join(echoDir, "plugins.yaml"), []byte(manifest))
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

func TestManager_LoadAll_SkipsInvalidManifests(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create valid plugin
	validDir := filepath.Join(pluginsDir, "valid")
	mkdirAll(t, validDir)
	writeFile(t, filepath.Join(validDir, "plugins.yaml"), []byte("name: valid\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(validDir, "main.lua"), []byte(""))

	// Create invalid plugin
	invalidDir := filepath.Join(pluginsDir, "invalid")
	mkdirAll(t, invalidDir)
	writeFile(t, filepath.Join(invalidDir, "plugins.yaml"), []byte("invalid yaml ["))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(luaHost))
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "LoadAll() should skip invalid plugins")

	plugins := mgr.ListPlugins()
	assert.Len(t, plugins, 1, "ListPlugins() should return 1 (invalid should be skipped)")
}

func TestManager_LoadAll_SkipsLuaPluginsWithoutHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a Lua plugin
	luaDir := filepath.Join(pluginsDir, "lua-plugin")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugins.yaml"), []byte("name: lua-plugin\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte(""))

	// Create manager without LuaHost - Lua plugins should be skipped
	mgr := plugins.NewManager(pluginsDir)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "LoadAll() should skip Lua plugins without host")

	// No plugins should be loaded since there's no LuaHost
	plugins := mgr.ListPlugins()
	assert.Empty(t, plugins, "ListPlugins() should be empty (no LuaHost)")
}

func TestManager_LoadAll_SkipsBinaryPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a binary plugin
	binaryDir := filepath.Join(pluginsDir, "binary-plugin")
	mkdirAll(t, binaryDir)
	writeFile(t, filepath.Join(binaryDir, "plugins.yaml"), []byte("name: binary-plugin\nversion: 1.0.0\ntype: binary\nbinary-plugin:\n  executable: plugin"))

	mgr := plugins.NewManager(pluginsDir)
	err := mgr.LoadAll(context.Background())
	require.NoError(t, err, "LoadAll() should skip binary plugins")

	// Binary plugins are not yet supported
	plugins := mgr.ListPlugins()
	assert.Empty(t, plugins, "ListPlugins() should be empty (binary not supported)")
}

func TestManager_LoadAll_FailsOnLuaSyntaxError(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a Lua plugin with syntax error
	luaDir := filepath.Join(pluginsDir, "bad-lua")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugins.yaml"), []byte("name: bad-lua\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
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

func TestManager_Close_WithoutLuaHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugins.NewManager(pluginsDir)

	// Close should succeed even without LuaHost
	assert.NoError(t, mgr.Close(context.Background()))
}

func TestManager_Close(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a plugin
	echoDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, echoDir)
	writeFile(t, filepath.Join(echoDir, "plugins.yaml"), []byte("name: echo-bot\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
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

func TestManager_Close_PropagatesHostError(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a plugin
	echoDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, echoDir)
	writeFile(t, filepath.Join(echoDir, "plugins.yaml"), []byte("name: echo-bot\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
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
