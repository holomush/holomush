package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Manager discovers and manages plugin lifecycle.
type Manager struct {
	pluginsDir string
	luaHost    Host
	loaded     map[string]*DiscoveredPlugin
	mu         sync.RWMutex
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithLuaHost sets the Lua host for the manager.
func WithLuaHost(h Host) ManagerOption {
	return func(m *Manager) {
		m.luaHost = h
	}
}

// NewManager creates a plugin manager.
func NewManager(pluginsDir string, opts ...ManagerOption) *Manager {
	m := &Manager{
		pluginsDir: pluginsDir,
		loaded:     make(map[string]*DiscoveredPlugin),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// DiscoveredPlugin contains a manifest and its directory.
type DiscoveredPlugin struct {
	Manifest *Manifest
	Dir      string
}

// Discover finds all valid plugins in the plugins directory.
// Invalid plugins are logged and skipped.
func (m *Manager) Discover(_ context.Context) ([]*DiscoveredPlugin, error) {
	entries, err := os.ReadDir(m.pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No plugins directory
		}
		return nil, fmt.Errorf("failed to read plugins directory: %w", err)
	}

	var plugins []*DiscoveredPlugin
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(m.pluginsDir, entry.Name())
		manifestPath := filepath.Join(pluginDir, "plugin.yaml")

		data, err := os.ReadFile(manifestPath) //nolint:gosec // manifestPath is constructed from ReadDir entries
		if err != nil {
			slog.Warn("skipping plugin without manifest",
				"dir", entry.Name(),
				"error", err)
			continue
		}

		manifest, err := ParseManifest(data)
		if err != nil {
			slog.Warn("skipping plugin with invalid manifest",
				"dir", entry.Name(),
				"error", err)
			continue
		}

		plugins = append(plugins, &DiscoveredPlugin{
			Manifest: manifest,
			Dir:      pluginDir,
		})
	}

	return plugins, nil
}

// LoadAll discovers and loads all plugins in the plugins directory.
// Invalid plugins are logged and skipped.
//
// Design: LoadAll uses graceful degradation - individual plugin failures are
// logged as warnings but don't fail the entire load. This allows the server to
// start even if some plugins have issues. Callers who need strict loading should
// use Discover + loadPlugin individually with error checking.
func (m *Manager) LoadAll(ctx context.Context) error {
	discovered, err := m.Discover(ctx)
	if err != nil {
		return err
	}

	for _, dp := range discovered {
		if err := m.loadPlugin(ctx, dp); err != nil {
			slog.Error("failed to load plugin",
				"plugin", dp.Manifest.Name,
				"error", err)
			continue
		}
	}

	return nil
}

// loadPlugin loads a single discovered plugin.
//
// Design: Returns nil (not error) for unsupported configurations to support
// graceful degradation. This allows running without Lua support or before
// binary plugin support is implemented. The warning logs provide visibility.
func (m *Manager) loadPlugin(ctx context.Context, dp *DiscoveredPlugin) error {
	switch dp.Manifest.Type {
	case TypeLua:
		if m.luaHost == nil {
			// Design: Allow running without Lua host configured (graceful degradation).
			slog.Warn("no Lua host configured, skipping Lua plugin",
				"plugin", dp.Manifest.Name)
			return nil
		}
		if err := m.luaHost.Load(ctx, dp.Manifest, dp.Dir); err != nil {
			return fmt.Errorf("load plugin %s: %w", dp.Manifest.Name, err)
		}
	case TypeBinary:
		// Binary plugins require go-plugin host (not yet implemented)
		slog.Warn("binary plugins not yet supported, skipping",
			"plugin", dp.Manifest.Name)
		return nil
	default:
		// Unknown types should be rejected by Manifest.Validate, but handle defensively.
		slog.Warn("unknown plugin type, skipping",
			"plugin", dp.Manifest.Name,
			"type", dp.Manifest.Type)
		return nil
	}

	m.mu.Lock()
	m.loaded[dp.Manifest.Name] = dp
	m.mu.Unlock()

	slog.Info("loaded plugin",
		"plugin", dp.Manifest.Name,
		"type", dp.Manifest.Type,
		"version", dp.Manifest.Version)

	return nil
}

// ListPlugins returns names of all loaded plugins.
func (m *Manager) ListPlugins() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.loaded))
	for name := range m.loaded {
		names = append(names, name)
	}

	// Sort for deterministic output
	sort.Strings(names)
	return names
}

// Close shuts down the manager and all loaded plugins.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear loaded map first to ensure consistent state even if close fails.
	m.loaded = make(map[string]*DiscoveredPlugin)

	if m.luaHost != nil {
		if err := m.luaHost.Close(ctx); err != nil {
			return fmt.Errorf("close lua host: %w", err)
		}
	}

	return nil
}
