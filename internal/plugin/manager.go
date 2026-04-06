// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Manager discovers and manages plugin lifecycle.
type Manager struct {
	pluginsDir      string
	luaHost         Host
	hosts           map[Type]Host   // host registry keyed by plugin type
	pluginHosts     map[string]Host // maps plugin name → owning host
	policyInstaller PluginPolicyInstaller
	registry        *ServiceRegistry // optional, enables DAG resolution
	loaded          map[string]*DiscoveredPlugin
	mu              sync.RWMutex
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithLuaHost sets the Lua host for the manager.
func WithLuaHost(h Host) ManagerOption {
	return func(m *Manager) {
		m.luaHost = h
	}
}

// WithPolicyInstaller sets the policy installer for plugin ABAC policies.
func WithPolicyInstaller(pi PluginPolicyInstaller) ManagerOption {
	return func(m *Manager) {
		m.policyInstaller = pi
	}
}

// WithServiceRegistry configures the manager to use DAG-based dependency
// resolution via the provided service registry.
func WithServiceRegistry(reg *ServiceRegistry) ManagerOption {
	return func(m *Manager) {
		m.registry = reg
	}
}

// Registry returns the service registry, or nil if not configured.
func (m *Manager) Registry() *ServiceRegistry {
	return m.registry
}

// NewManager creates a plugin manager.
func NewManager(pluginsDir string, opts ...ManagerOption) *Manager {
	m := &Manager{
		pluginsDir:  pluginsDir,
		loaded:      make(map[string]*DiscoveredPlugin),
		hosts:       make(map[Type]Host),
		pluginHosts: make(map[string]Host),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// RegisterHost registers a host implementation for a plugin type.
// Must be called before LoadAll. Panics if host is nil.
func (m *Manager) RegisterHost(hostType Type, host Host) {
	if host == nil {
		panic("RegisterHost: host must not be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hosts[hostType] = host
}

// DeliverCommand routes a command to the correct host for the named plugin.
func (m *Manager) DeliverCommand(ctx context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	m.mu.RLock()
	host, ok := m.pluginHosts[pluginName]
	m.mu.RUnlock()

	if !ok {
		return nil, oops.In("manager").With("plugin", pluginName).New("plugin not loaded or unknown")
	}
	resp, err := host.DeliverCommand(ctx, pluginName, cmd)
	if err != nil {
		return nil, oops.In("manager").With("plugin", pluginName).With("operation", "deliver_command").Wrap(err)
	}
	return resp, nil
}

// DeliverEvent routes an event to the correct host for the named plugin.
func (m *Manager) DeliverEvent(ctx context.Context, pluginName string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	m.mu.RLock()
	host, ok := m.pluginHosts[pluginName]
	m.mu.RUnlock()

	if !ok {
		return nil, oops.In("manager").With("plugin", pluginName).New("plugin not loaded or unknown")
	}
	emits, err := host.DeliverEvent(ctx, pluginName, event)
	if err != nil {
		return nil, oops.In("manager").With("plugin", pluginName).With("operation", "deliver_event").Wrap(err)
	}
	return emits, nil
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
		return nil, oops.In("manager").With("dir", m.pluginsDir).Hint("failed to read plugins directory").Wrap(err)
	}

	plugins := make([]*DiscoveredPlugin, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(m.pluginsDir, entry.Name())
		manifestPath := filepath.Join(pluginDir, "plugin.yaml")

		data, err := os.ReadFile(filepath.Clean(manifestPath))
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
// When a ServiceRegistry is configured (via WithServiceRegistry), LoadAll uses
// DAG-based dependency resolution to determine load order. If resolution fails
// (e.g. circular dependency or unsatisfied requires), it falls back to priority
// sort and logs a warning.
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

	ordered := m.resolveLoadOrder(discovered)

	for _, dp := range ordered {
		if err := m.loadPlugin(ctx, dp); err != nil {
			slog.Error("failed to load plugin",
				"plugin", dp.Manifest.Name,
				"priority", dp.Manifest.EffectivePriority(),
				"error", err)
			continue
		}
	}

	return nil
}

// resolveLoadOrder returns plugins in the order they should be loaded.
// When a registry is configured, it uses DAG-based dependency resolution.
// Falls back to priority sort if DAG resolution fails or no registry is set.
func (m *Manager) resolveLoadOrder(discovered []*DiscoveredPlugin) []*DiscoveredPlugin {
	if m.registry != nil {
		serverServices := m.registry.List()
		serverServiceNames := make([]string, 0, len(serverServices))
		for _, svc := range serverServices {
			serverServiceNames = append(serverServiceNames, svc.Name)
		}

		ordered, err := ResolveDependencyOrder(discovered, serverServiceNames)
		if err == nil {
			return ordered
		}
		slog.Warn("DAG dependency resolution failed, falling back to priority sort",
			"error", err)
	}

	// Default: sort by load priority (lower values load first).
	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].Manifest.EffectivePriority() < discovered[j].Manifest.EffectivePriority()
	})
	return discovered
}

// loadPlugin loads a single discovered plugin.
//
// Design: Returns nil (not error) for unsupported configurations to support
// graceful degradation. This allows running without Lua support or before
// binary plugin support is implemented. The warning logs provide visibility.
func (m *Manager) loadPlugin(ctx context.Context, dp *DiscoveredPlugin) error {
	// Resolve the host for this plugin type.
	// For backward compatibility, TypeLua falls back to the dedicated luaHost field.
	var host Host
	switch dp.Manifest.Type {
	case TypeLua:
		host = m.hosts[TypeLua]
		if host == nil {
			host = m.luaHost // backward compatibility
		}
		if host == nil {
			slog.Warn("no Lua host configured, skipping Lua plugin",
				"plugin", dp.Manifest.Name)
			return nil
		}
	case TypeBinary:
		host = m.hosts[TypeBinary]
		if host == nil {
			slog.Warn("binary plugins not yet supported, skipping",
				"plugin", dp.Manifest.Name)
			return nil
		}
	default:
		slog.Warn("unknown plugin type, skipping",
			"plugin", dp.Manifest.Name,
			"type", dp.Manifest.Type)
		return nil
	}

	// Reject duplicate plugin names before loading to prevent the second plugin
	// from overwriting the first in the manager maps while leaving the original
	// loaded inside its host but unreachable.
	m.mu.RLock()
	_, duplicate := m.loaded[dp.Manifest.Name]
	m.mu.RUnlock()
	if duplicate {
		return oops.In("manager").With("plugin", dp.Manifest.Name).With("operation", "load").
			Errorf("plugin %q is already loaded", dp.Manifest.Name)
	}

	if err := host.Load(ctx, dp.Manifest, dp.Dir); err != nil {
		return oops.In("manager").With("plugin", dp.Manifest.Name).With("operation", "load").Wrap(err)
	}

	// Install ABAC policies if present.
	if m.policyInstaller != nil && len(dp.Manifest.Policies) > 0 {
		if err := m.policyInstaller.InstallPluginPolicies(ctx, dp.Manifest.Name, dp.Manifest.Policies); err != nil {
			if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
				slog.Error("failed to rollback plugin load after policy install failure",
					"plugin", dp.Manifest.Name, "error", unloadErr)
			}
			return oops.In("manager").With("plugin", dp.Manifest.Name).Wrapf(err, "install plugin policies")
		}
	}

	// Register plugin-provided services in the service registry.
	// Service registration is best-effort: if some services fail to register
	// (e.g., duplicate provider), the plugin is still considered loaded with
	// partial Provides. This matches the graceful degradation pattern used
	// throughout the plugin system — individual failures are logged but don't
	// prevent the server from starting. Callers that need strict guarantees
	// should check the service registry directly.
	if m.registry != nil && len(dp.Manifest.Provides) > 0 {
		if connProvider, ok := host.(ServiceConnProvider); ok {
			conn, connErr := connProvider.PluginConn(dp.Manifest.Name)
			if connErr != nil {
				slog.Error("failed to get plugin connection for service registration",
					"plugin", dp.Manifest.Name, "error", connErr)
			} else {
				for _, svcName := range dp.Manifest.Provides {
					regErr := m.registry.Register(RegisteredService{
						Name:       svcName,
						Conn:       conn,
						PluginName: dp.Manifest.Name,
						PluginType: dp.Manifest.Type,
					})
					if regErr != nil {
						slog.Error("failed to register plugin service",
							"plugin", dp.Manifest.Name,
							"service", svcName,
							"error", regErr)
					}
				}
			}
		}
	}

	m.mu.Lock()
	m.loaded[dp.Manifest.Name] = dp
	m.pluginHosts[dp.Manifest.Name] = host
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

	if m.policyInstaller != nil {
		for name := range m.loaded {
			if err := m.policyInstaller.RemovePluginPolicies(ctx, name); err != nil {
				slog.Error("failed to remove plugin policies", "plugin", name, "error", err)
			}
		}
	}

	// Close all registered hosts before clearing maps so that hosts can
	// still reference loaded state during shutdown.
	for hostType, host := range m.hosts {
		if err := host.Close(ctx); err != nil {
			slog.Error("failed to close host", "type", hostType, "error", err)
		}
	}

	// Clear loaded maps after hosts are closed.
	m.loaded = make(map[string]*DiscoveredPlugin)
	m.pluginHosts = make(map[string]Host)

	// Close legacy luaHost if not already in the hosts map.
	if m.luaHost != nil {
		if _, inMap := m.hosts[TypeLua]; !inMap {
			if err := m.luaHost.Close(ctx); err != nil {
				return oops.In("manager").With("operation", "close").Hint("failed to close lua host").Wrap(err)
			}
		}
	}

	return nil
}

// IsPluginLoaded returns true if the named plugin is currently loaded.
// Implements attribute.PluginRegistry for ABAC attribute resolution.
func (m *Manager) IsPluginLoaded(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.loaded[name]
	return ok
}

// GetLoadedPlugin returns the discovered plugin info for the named plugin.
// Returns nil and false if the plugin is not loaded.
func (m *Manager) GetLoadedPlugin(name string) (*DiscoveredPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dp, ok := m.loaded[name]
	return dp, ok
}

// RegisterPluginCommands iterates all loaded plugins and registers their
// manifest-declared commands into the given command registry. This ensures
// the dispatcher can route plugin-backed commands via registry.Get().
func (m *Manager) RegisterPluginCommands(registry *command.Registry) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, dp := range m.loaded {
		for _, cmdSpec := range dp.Manifest.Commands {
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name:         cmdSpec.Name,
				PluginName:   dp.Manifest.Name,
				Capabilities: cmdSpec.Capabilities,
				Help:         cmdSpec.Help,
				Usage:        cmdSpec.Usage,
				HelpText:     cmdSpec.HelpText,
				Source:       dp.Manifest.Name,
			})
			if err != nil {
				slog.Warn("failed to create command entry for plugin command",
					"plugin", dp.Manifest.Name,
					"command", cmdSpec.Name,
					"error", err)
				continue
			}
			if regErr := registry.Register(*entry); regErr != nil {
				slog.Warn("failed to register plugin command",
					"plugin", dp.Manifest.Name,
					"command", cmdSpec.Name,
					"error", regErr)
			}
		}
	}
}
