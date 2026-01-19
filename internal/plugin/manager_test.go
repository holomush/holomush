package plugin_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)

// Helper functions for creating test fixtures with secure permissions.
func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0750); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
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
	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte("function on_event(e) end"))

	mgr := plugin.NewManager(filepath.Join(dir, "plugins"))
	manifests, err := mgr.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(manifests) != 1 {
		t.Fatalf("len(manifests) = %d, want 1", len(manifests))
	}

	if manifests[0].Manifest.Name != "echo-bot" {
		t.Errorf("Name = %q, want %q", manifests[0].Manifest.Name, "echo-bot")
	}
	if manifests[0].Dir != echoDir {
		t.Errorf("Dir = %q, want %q", manifests[0].Dir, echoDir)
	}
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
	writeFile(t, filepath.Join(validDir, "plugin.yaml"), []byte(validManifest))
	writeFile(t, filepath.Join(validDir, "main.lua"), []byte(""))

	// Create invalid plugin (bad YAML)
	invalidDir := filepath.Join(pluginsDir, "invalid")
	mkdirAll(t, invalidDir)
	writeFile(t, filepath.Join(invalidDir, "plugin.yaml"), []byte("invalid: ["))

	mgr := plugin.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())

	// Should succeed but only return valid plugin
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(manifests) != 1 {
		t.Errorf("len(manifests) = %d, want 1 (valid only)", len(manifests))
	}
}

func TestManager_Discover_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugin.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(manifests) != 0 {
		t.Errorf("len(manifests) = %d, want 0 for empty directory", len(manifests))
	}
}

func TestManager_Discover_NonExistentDirectory(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "non-existent-plugins")

	mgr := plugin.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v (should handle non-existent dir gracefully)", err)
	}

	if len(manifests) != 0 {
		t.Errorf("len(manifests) = %d, want 0 for non-existent directory", len(manifests))
	}
}

func TestManager_Discover_SkipsFilesNotDirectories(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	// Create a file (not directory) in plugins dir - should be skipped
	writeFile(t, filepath.Join(pluginsDir, "not-a-plugin.txt"), []byte("hello"))

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

	mgr := plugin.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(manifests) != 1 {
		t.Errorf("len(manifests) = %d, want 1 (files should be skipped)", len(manifests))
	}
}

func TestManager_Discover_SkipsDirWithoutManifest(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create directory without plugin.yaml
	noManifestDir := filepath.Join(pluginsDir, "no-manifest")
	mkdirAll(t, noManifestDir)
	// Only create a lua file, no plugin.yaml
	writeFile(t, filepath.Join(noManifestDir, "main.lua"), []byte(""))

	mgr := plugin.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(manifests) != 0 {
		t.Errorf("len(manifests) = %d, want 0 (dir without manifest should be skipped)", len(manifests))
	}
}

func TestManager_Discover_MultiplePlugins(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	plugins := []struct {
		name    string
		version string
	}{
		{"alpha-plugin", "1.0.0"},
		{"beta-plugin", "2.0.0"},
		{"gamma-plugin", "3.0.0"},
	}

	for _, p := range plugins {
		pluginDir := filepath.Join(pluginsDir, p.name)
		mkdirAll(t, pluginDir)
		manifest := "name: " + p.name + "\nversion: " + p.version + "\ntype: lua\nlua-plugin:\n  entry: main.lua"
		writeFile(t, filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest))
		writeFile(t, filepath.Join(pluginDir, "main.lua"), []byte(""))
	}

	mgr := plugin.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(manifests) != 3 {
		t.Fatalf("len(manifests) = %d, want 3", len(manifests))
	}

	// Sort by name for deterministic comparison
	names := make([]string, 0, len(manifests))
	for _, m := range manifests {
		names = append(names, m.Manifest.Name)
	}
	sort.Strings(names)

	expected := []string{"alpha-plugin", "beta-plugin", "gamma-plugin"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("names[%d] = %q, want %q", i, name, expected[i])
		}
	}
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
	writeFile(t, filepath.Join(binaryDir, "plugin.yaml"), []byte(manifest))

	mgr := plugin.NewManager(pluginsDir)
	manifests, err := mgr.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(manifests) != 1 {
		t.Fatalf("len(manifests) = %d, want 1", len(manifests))
	}

	if manifests[0].Manifest.Type != plugin.TypeBinary {
		t.Errorf("Type = %v, want %v", manifests[0].Manifest.Type, plugin.TypeBinary)
	}
}

func TestManager_ListPlugins_NoPluginsLoaded(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugin.NewManager(pluginsDir)
	plugins := mgr.ListPlugins()

	if len(plugins) != 0 {
		t.Errorf("ListPlugins() = %v, want empty slice before any plugins loaded", plugins)
	}
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
	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte(manifest))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte("function on_event(e) end"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr := plugin.NewManager(pluginsDir, plugin.WithLuaHost(luaHost))
	err := mgr.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	plugins := mgr.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("ListPlugins() returned %d plugins, want 1", len(plugins))
	}
	if plugins[0] != "echo-bot" {
		t.Errorf("ListPlugins()[0] = %q, want %q", plugins[0], "echo-bot")
	}
}

func TestManager_LoadAll_SkipsInvalidManifests(t *testing.T) {
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

	mgr := plugin.NewManager(pluginsDir, plugin.WithLuaHost(luaHost))
	err := mgr.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll() error = %v (should skip invalid plugins)", err)
	}

	plugins := mgr.ListPlugins()
	if len(plugins) != 1 {
		t.Errorf("ListPlugins() returned %d plugins, want 1 (invalid should be skipped)", len(plugins))
	}
}

func TestManager_LoadAll_SkipsLuaPluginsWithoutHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a Lua plugin
	luaDir := filepath.Join(pluginsDir, "lua-plugin")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte("name: lua-plugin\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte(""))

	// Create manager without LuaHost - Lua plugins should be skipped
	mgr := plugin.NewManager(pluginsDir)
	err := mgr.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll() error = %v (should skip Lua plugins without host)", err)
	}

	// No plugins should be loaded since there's no LuaHost
	plugins := mgr.ListPlugins()
	if len(plugins) != 0 {
		t.Errorf("ListPlugins() = %v, want empty (no LuaHost)", plugins)
	}
}

func TestManager_LoadAll_SkipsBinaryPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a binary plugin
	binaryDir := filepath.Join(pluginsDir, "binary-plugin")
	mkdirAll(t, binaryDir)
	writeFile(t, filepath.Join(binaryDir, "plugin.yaml"), []byte("name: binary-plugin\nversion: 1.0.0\ntype: binary\nbinary-plugin:\n  executable: plugin"))

	mgr := plugin.NewManager(pluginsDir)
	err := mgr.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll() error = %v (should skip binary plugins)", err)
	}

	// Binary plugins are not yet supported
	plugins := mgr.ListPlugins()
	if len(plugins) != 0 {
		t.Errorf("ListPlugins() = %v, want empty (binary not supported)", plugins)
	}
}

func TestManager_LoadAll_FailsOnLuaSyntaxError(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a Lua plugin with syntax error
	luaDir := filepath.Join(pluginsDir, "bad-lua")
	mkdirAll(t, luaDir)
	writeFile(t, filepath.Join(luaDir, "plugin.yaml"), []byte("name: bad-lua\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(luaDir, "main.lua"), []byte("function broken"))

	luaHost := pluginlua.NewHost()
	t.Cleanup(func() { _ = luaHost.Close(context.Background()) })

	mgr := plugin.NewManager(pluginsDir, plugin.WithLuaHost(luaHost))
	err := mgr.LoadAll(context.Background())

	// LoadAll should succeed but log a warning and skip the bad plugin
	if err != nil {
		t.Fatalf("LoadAll() error = %v (should skip plugins with load errors)", err)
	}

	plugins := mgr.ListPlugins()
	if len(plugins) != 0 {
		t.Errorf("ListPlugins() = %v, want empty (bad Lua syntax)", plugins)
	}
}

func TestManager_Close_WithoutLuaHost(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	mkdirAll(t, pluginsDir)

	mgr := plugin.NewManager(pluginsDir)

	// Close should succeed even without LuaHost
	if err := mgr.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestManager_Close(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	// Create a plugin
	echoDir := filepath.Join(pluginsDir, "echo-bot")
	mkdirAll(t, echoDir)
	writeFile(t, filepath.Join(echoDir, "plugin.yaml"), []byte("name: echo-bot\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua"))
	writeFile(t, filepath.Join(echoDir, "main.lua"), []byte(""))

	luaHost := pluginlua.NewHost()
	mgr := plugin.NewManager(pluginsDir, plugin.WithLuaHost(luaHost))
	if err := mgr.LoadAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Verify plugin is loaded
	if len(mgr.ListPlugins()) != 1 {
		t.Fatal("expected 1 plugin to be loaded")
	}

	// Close manager
	if err := mgr.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// After close, ListPlugins should return empty
	if len(mgr.ListPlugins()) != 0 {
		t.Errorf("ListPlugins() after Close() = %v, want empty", mgr.ListPlugins())
	}
}
