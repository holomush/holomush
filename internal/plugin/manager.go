// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// RegisterPluginProviderFunc is a callback that registers a PluginAttributeProvider
// with the ABAC attribute resolver. The server wiring layer provides a closure
// that calls resolver.RegisterProvider(provider).
type RegisterPluginProviderFunc func(provider *PluginAttributeProvider) error

// UnregisterPluginProviderFunc is a callback that removes a plugin attribute
// provider from the ABAC attribute resolver by namespace. The server wiring
// layer provides a closure that calls resolver.UnregisterProvider(namespace).
//
// Used during plugin load rollback to unwind provider registrations when a
// later load-time step (schema validation, policy install) fails. If the
// registrar callback is set but the unregistrar is nil, the manager logs a
// warning on rollback — the resolver will retain a stale reference to a
// plugin that never finished loading.
type UnregisterPluginProviderFunc func(namespace string) bool

// Error codes returned by the plugin manager. Tests should check these via
// errutil.AssertErrorCode rather than substring matching error messages.
const (
	// CodeHostMissingConnProvider is returned when a plugin declares
	// `provides:` but the host implementation does not satisfy the optional
	// ServiceConnProvider interface needed to expose plugin gRPC services.
	CodeHostMissingConnProvider = "PLUGIN_HOST_MISSING_CONN_PROVIDER"
)

// Manager discovers and manages plugin lifecycle.
type Manager struct {
	pluginsDir          string
	luaHost             Host
	hosts               map[Type]Host             // host registry keyed by plugin type
	hostCaps            map[Host]hostCapabilities // optional interfaces, cached at registration
	pluginHosts         map[string]Host           // maps plugin name → owning host
	policyInstaller     PluginPolicyInstaller
	registerProvider    RegisterPluginProviderFunc   // optional, registers plugin attribute providers
	unregisterProvider  UnregisterPluginProviderFunc // optional, unregisters plugin attribute providers on rollback
	registry            *ServiceRegistry             // optional, enables DAG resolution
	trustAllowlist      map[string]bool              // server-side trust escalation allowlist
	gracefulDegradation bool                         // if true, LoadAll continues despite plugin failures
	aliasSeeder         AliasSeeder
	aliasCache          *command.AliasCache
	eventEmitter        *PluginEventEmitter
	loaded              map[string]*DiscoveredPlugin
	inflight            map[string]*DiscoveredPlugin
	loadedOrder         []*DiscoveredPlugin // preserves DAG/priority load order for deterministic iteration
	mu                  sync.RWMutex
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

// WithAliasSeeder configures alias seeding from plugin manifests during LoadAll.
func WithAliasSeeder(seeder AliasSeeder, cache *command.AliasCache) ManagerOption {
	return func(m *Manager) {
		m.aliasSeeder = seeder
		m.aliasCache = cache
	}
}

// WithAttributeProviderRegistrar sets a callback used to register plugin
// attribute providers with the ABAC resolver during plugin load.
func WithAttributeProviderRegistrar(fn RegisterPluginProviderFunc) ManagerOption {
	return func(m *Manager) {
		m.registerProvider = fn
	}
}

// WithAttributeProviderUnregistrar sets a callback used to remove plugin
// attribute providers from the ABAC resolver when a plugin load fails
// after provider registration has already occurred. Server wiring SHOULD
// pass both WithAttributeProviderRegistrar and WithAttributeProviderUnregistrar
// as a pair — otherwise failed loads leak dangling providers into the
// resolver registry.
func WithAttributeProviderUnregistrar(fn UnregisterPluginProviderFunc) ManagerOption {
	return func(m *Manager) {
		m.unregisterProvider = fn
	}
}

// WithServiceRegistry configures the manager to use DAG-based dependency
// resolution via the provided service registry.
func WithServiceRegistry(reg *ServiceRegistry) ManagerOption {
	return func(m *Manager) {
		m.registry = reg
	}
}

// WithTrustAllowlist sets the server-side allowlist of plugin names permitted
// to use trust escalation. A plugin's manifest trust.all_principals declaration
// only takes effect when the plugin name appears in this allowlist.
func WithTrustAllowlist(names []string) ManagerOption {
	return func(m *Manager) {
		m.trustAllowlist = make(map[string]bool, len(names))
		for _, n := range names {
			m.trustAllowlist[n] = true
		}
	}
}

// WithGracefulDegradation enables graceful degradation for LoadAll: individual
// plugin failures are logged as warnings rather than aborting server startup.
//
// This is intended for local development iteration on broken plugins.
// Production servers should leave this disabled (the default) so that
// configuration errors fail fast and visibly.
func WithGracefulDegradation() ManagerOption {
	return func(m *Manager) {
		m.gracefulDegradation = true
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
		inflight:    make(map[string]*DiscoveredPlugin),
		hosts:       make(map[Type]Host),
		hostCaps:    make(map[Host]hostCapabilities),
		pluginHosts: make(map[string]Host),
	}
	for _, opt := range opts {
		opt(m)
	}
	// If WithLuaHost was used, cache its capabilities for the same lookup path.
	if m.luaHost != nil {
		m.hostCaps[m.luaHost] = discoverCapabilities(m.luaHost)
	}
	return m
}

// RegisterHost registers a host implementation for a plugin type.
// Must be called before LoadAll. Panics if host is nil.
//
// Optional capabilities (ServiceConnProvider, AttributeResolverProvider) are
// discovered once at registration time by walking the host's Unwrap() chain
// (if any) and cached on the Manager for the host's lifetime.
func (m *Manager) RegisterHost(hostType Type, host Host) {
	if host == nil {
		panic("RegisterHost: host must not be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hosts[hostType] = host
	m.hostCaps[host] = discoverCapabilities(host)
	if m.eventEmitter != nil {
		if configurer := findOptional[EventEmitterConfigurer](host); configurer != nil {
			configurer.SetEventEmitter(m.eventEmitter)
		}
	}
}

// capabilitiesFor returns the cached capabilities for a host, or an empty
// hostCapabilities if the host wasn't registered (defensive — shouldn't happen
// in practice since loadPlugin only handles hosts from m.hosts/m.luaHost).
func (m *Manager) capabilitiesFor(h Host) hostCapabilities {
	if caps, ok := m.hostCaps[h]; ok {
		return caps
	}
	// Fallback: discover on demand. Should not happen but keeps loadPlugin safe.
	return discoverCapabilities(h)
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

// ConfigureEventEmitter wires the shared plugin event emitter to the provided
// host event store. Production startup MUST call this before plugin response
// events are routed through the manager.
func (m *Manager) ConfigureEventEmitter(store core.EventStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventEmitter = NewPluginEventEmitter(store, m.lookupManifest, actorFromContext)
	for _, host := range m.hosts {
		if configurer := findOptional[EventEmitterConfigurer](host); configurer != nil {
			configurer.SetEventEmitter(m.eventEmitter)
		}
	}
	if m.luaHost != nil {
		if configurer := findOptional[EventEmitterConfigurer](m.luaHost); configurer != nil {
			configurer.SetEventEmitter(m.eventEmitter)
		}
	}
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

// EmitPluginEvent routes a plugin-owned emit request through the shared host
// emitter so manifests are validated and host-owned event fields are stamped
// consistently across command and subscriber paths.
func (m *Manager) EmitPluginEvent(ctx context.Context, pluginName string, event pluginsdk.EmitEvent) error {
	m.mu.RLock()
	emitter := m.eventEmitter
	m.mu.RUnlock()

	if emitter == nil {
		return oops.With("plugin", pluginName).
			New("plugin event emitter is not configured")
	}
	return emitter.Emit(ctx, pluginName, pluginsdk.EmitIntent(event))
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
//
// When a ServiceRegistry is configured (via WithServiceRegistry), LoadAll uses
// DAG-based dependency resolution to determine load order. If resolution fails
// (e.g. circular dependency or unsatisfied requires), it falls back to priority
// sort and logs a warning.
//
// Strict by default: if any plugin fails to load, LoadAll attempts all
// remaining plugins, then returns a joined error describing every failure.
// Production servers should fail fast on broken plugin configuration.
//
// Use WithGracefulDegradation() to opt into the legacy behavior where
// individual plugin failures are logged but don't fail the overall load.
// This is intended for local development iteration on broken plugins.
func (m *Manager) LoadAll(ctx context.Context) error {
	// Phase 1: Discover — structural validation only.
	discovered, err := m.Discover(ctx)
	if err != nil {
		return err
	}

	// Phase 2: Collect cross-plugin context.
	knownResourceTypes := CollectResourceTypes(discovered)

	// Phase 3: Resolve load order.
	ordered := m.resolveLoadOrder(discovered)

	// Phase 4: Load each plugin with full context.
	var loadErrors []error
	for _, dp := range ordered {
		if err := m.loadPlugin(ctx, dp, knownResourceTypes); err != nil {
			slog.Error("failed to load plugin",
				"plugin", dp.Manifest.Name,
				"priority", dp.Manifest.EffectivePriority(),
				"error", err)
			loadErrors = append(loadErrors,
				oops.With("plugin", dp.Manifest.Name).Wrap(err))
		}
	}

	if len(loadErrors) > 0 {
		if m.gracefulDegradation {
			slog.Warn("plugin loading completed with errors (graceful degradation enabled)",
				"failed_count", len(loadErrors))
			return nil
		}
		return oops.Code("PLUGIN_LOAD_FAILED").
			With("failed_count", len(loadErrors)).
			Wrap(errors.Join(loadErrors...))
	}

	// Seed aliases from loaded plugin manifests.
	if m.aliasSeeder != nil && m.aliasCache != nil {
		if err := m.seedAliases(ctx); err != nil {
			slog.Error("failed to seed plugin aliases", "error", err)
		}
	}

	return nil
}

// seedAliases collects alias declarations from all loaded plugin manifests
// and seeds them into the database. Iterates loadedOrder (not the map) to
// preserve DAG/priority load order — this makes cross-plugin duplicate
// resolution deterministic across restarts.
func (m *Manager) seedAliases(ctx context.Context) error {
	m.mu.RLock()
	ordered := make([]*DiscoveredPlugin, len(m.loadedOrder))
	copy(ordered, m.loadedOrder)
	m.mu.RUnlock()

	aliases := CollectManifestAliases(ordered)
	if len(aliases) == 0 {
		return nil
	}

	return SeedManifestAliases(ctx, aliases, m.aliasSeeder, m.aliasCache)
}

// hostCapabilities holds the optional interface implementations discovered for
// a Host. Cached at registration time to avoid repeated wrapper-chain walks.
type hostCapabilities struct {
	connProvider ServiceConnProvider       // nil if host doesn't support
	arProvider   AttributeResolverProvider // nil if host doesn't support
}

// discoverCapabilities walks a chain of Host wrappers (via the optional Unwrap
// method) to find implementations of optional interfaces. Called once at host
// registration time; results are cached on the Manager.
func discoverCapabilities(h Host) hostCapabilities {
	return hostCapabilities{
		connProvider: findOptional[ServiceConnProvider](h),
		arProvider:   findOptional[AttributeResolverProvider](h),
	}
}

// findOptional returns an implementation of T from a Host or any of its
// Unwrap()-chain ancestors. Returns the zero value if no implementation
// is found in the chain.
func findOptional[T any](h Host) T {
	var zero T
	current := h
	for {
		if t, ok := any(current).(T); ok {
			return t
		}
		unwrapper, ok := current.(interface{ Unwrap() Host })
		if !ok {
			return zero
		}
		next := unwrapper.Unwrap()
		if next == nil {
			return zero
		}
		current = next
	}
}

// CollectResourceTypes builds the full set of known resource types: core types
// plus all resource_types declared across discovered plugins. This cross-plugin
// context is needed for semantic validation during loadPlugin. Exported as a
// test seam so callers can verify the merge logic without driving LoadAll.
func CollectResourceTypes(discovered []*DiscoveredPlugin) map[string]bool {
	known := command.CoreResourceTypes()
	for _, dp := range discovered {
		for _, rt := range dp.Manifest.ResourceTypes {
			known[rt] = true
		}
	}
	return known
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

// unregisterPluginProviders removes all attribute providers that were
// registered for a plugin's declared resource types. It is called during
// load rollback to keep the resolver registry consistent with the set of
// successfully loaded plugins.
//
// `upTo` limits the slice that will be unregistered — callers that fail
// partway through the registration loop pass the index of the last
// successfully-registered resource type so they unregister only what they
// actually registered.
//
// If the unregister callback is not configured but the register callback
// is, a warning is logged — the resolver will retain stale providers.
func (m *Manager) unregisterPluginProviders(pluginName string, resourceTypes []string, upTo int) {
	if m.registerProvider == nil {
		return // registrar not wired; nothing was ever registered
	}
	if upTo > len(resourceTypes) {
		upTo = len(resourceTypes)
	}
	if upTo <= 0 {
		return
	}
	if m.unregisterProvider == nil {
		slog.Warn("cannot unregister plugin attribute providers on rollback: "+
			"WithAttributeProviderRegistrar configured but WithAttributeProviderUnregistrar is not",
			"plugin", pluginName,
			"leaked_namespaces", resourceTypes[:upTo])
		return
	}
	for _, rt := range resourceTypes[:upTo] {
		_ = m.unregisterProvider(rt)
	}
}

// loadPlugin loads a single discovered plugin.
//
// Design: Returns nil (not error) for unsupported configurations to support
// graceful degradation. This allows running without Lua support or before
// binary plugin support is implemented. The warning logs provide visibility.
func (m *Manager) loadPlugin(ctx context.Context, dp *DiscoveredPlugin, knownResourceTypes map[string]bool) error {
	// Semantic validation: check capability resource types against the full known set.
	for i := range dp.Manifest.Commands {
		cmd := &dp.Manifest.Commands[i]
		for _, cap := range cmd.Capabilities {
			if err := cap.ValidateResourceType(knownResourceTypes); err != nil {
				return oops.In("manager").With("plugin", dp.Manifest.Name).
					With("command", cmd.Name).Wrap(err)
			}
		}
	}

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
	m.mu.Lock()
	if _, duplicate := m.loaded[dp.Manifest.Name]; duplicate {
		m.mu.Unlock()
		return oops.In("manager").With("plugin", dp.Manifest.Name).With("operation", "load").
			Errorf("plugin %q is already loaded", dp.Manifest.Name)
	}
	if _, inflight := m.inflight[dp.Manifest.Name]; inflight {
		m.mu.Unlock()
		return oops.In("manager").With("plugin", dp.Manifest.Name).With("operation", "load").
			Errorf("plugin %q is already loading", dp.Manifest.Name)
	}
	m.inflight[dp.Manifest.Name] = dp
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.inflight, dp.Manifest.Name)
		m.mu.Unlock()
	}()

	if err := host.Load(ctx, dp.Manifest, dp.Dir); err != nil {
		return oops.In("manager").With("plugin", dp.Manifest.Name).With("operation", "load").Wrap(err)
	}

	// Discover and register attribute providers for plugin resource types.
	// schemas is non-nil only for binary plugins that declare resource_types.
	var schemas map[string]*types.NamespaceSchema
	if len(dp.Manifest.ResourceTypes) > 0 {
		var regErr error
		schemas, regErr = m.discoverAndRegisterAttributes(ctx, host, dp)
		if regErr != nil {
			return regErr
		}
	}

	// Validate manifest policy attribute references against discovered
	// schemas BEFORE installing policies. A load-time schema mismatch
	// (e.g., policy references resource.widget.tipe but schema declares
	// "type") is a fatal load error per the Plugin ABAC Hardening spec
	// (2026-04-07).
	if valErr := ValidateManifestPolicySchemas(dp.Manifest, schemas); valErr != nil {
		// Unregister attribute providers that were added during
		// discoverAndRegisterAttributes so the resolver doesn't retain
		// dangling references to a plugin that never finished loading.
		m.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
		if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
			slog.Error("failed to rollback plugin load after schema validation failure",
				"plugin", dp.Manifest.Name, "error", unloadErr)
		}
		return oops.In("manager").With("plugin", dp.Manifest.Name).
			Wrapf(valErr, "validate manifest policy schemas")
	}

	// Install ABAC policies using manifest-aware validation when resource
	// types or trust config are present, otherwise fall back to basic install.
	if m.policyInstaller != nil && len(dp.Manifest.Policies) > 0 {
		installErr := m.policyInstaller.InstallPluginPoliciesWithManifest(ctx, dp.Manifest, dp.Manifest.Policies)
		if installErr != nil {
			// Unregister providers added during discoverAndRegisterAttributes
			// — same rationale as the schema-validation branch above.
			m.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
			if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
				slog.Error("failed to rollback plugin load after policy install failure",
					"plugin", dp.Manifest.Name, "error", unloadErr)
			}
			return oops.In("manager").With("plugin", dp.Manifest.Name).Wrapf(installErr, "install plugin policies")
		}
	}

	// Check for manifest warnings (non-fatal policy coverage gaps).
	if warnings := CheckManifestWarnings(dp.Manifest); len(warnings) > 0 {
		for _, w := range warnings {
			slog.Info(w, "plugin", dp.Manifest.Name)
		}
	}

	// Register plugin-provided services in the service registry.
	// Registration failures are treated as hard errors — dependents resolved
	// by ResolveDependencyOrder rely on the Provides contract being satisfied.
	if m.registry != nil && len(dp.Manifest.Provides) > 0 {
		connProvider := m.capabilitiesFor(host).connProvider
		if connProvider == nil {
			// Rollback attribute providers registered earlier in loadPlugin.
			m.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
			return oops.Code(CodeHostMissingConnProvider).
				In("manager").
				With("plugin", dp.Manifest.Name).
				Errorf("host does not implement ServiceConnProvider but plugin declares Provides")
		}
		conn, connErr := connProvider.PluginConn(dp.Manifest.Name)
		if connErr != nil {
			m.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
			return oops.In("manager").
				With("plugin", dp.Manifest.Name).
				Wrapf(connErr, "get plugin connection for service registration")
		}
		var registered []string
		for _, svcName := range dp.Manifest.Provides {
			regErr := m.registry.Register(RegisteredService{
				Name:       svcName,
				Conn:       conn,
				PluginName: dp.Manifest.Name,
				PluginType: dp.Manifest.Type,
			})
			if regErr != nil {
				// Unwind partial registrations.
				for _, name := range registered {
					_ = m.registry.Deregister(name) //nolint:errcheck // best-effort cleanup
				}
				m.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
				return oops.In("manager").
					With("plugin", dp.Manifest.Name).
					With("service", svcName).
					Wrapf(regErr, "register plugin service")
			}
			registered = append(registered, svcName)
		}
	}

	m.mu.Lock()
	if _, existed := m.loaded[dp.Manifest.Name]; !existed {
		m.loadedOrder = append(m.loadedOrder, dp)
	}
	delete(m.inflight, dp.Manifest.Name)
	m.loaded[dp.Manifest.Name] = dp
	m.pluginHosts[dp.Manifest.Name] = host
	m.mu.Unlock()

	slog.Info("loaded plugin",
		"plugin", dp.Manifest.Name,
		"type", dp.Manifest.Type,
		"version", dp.Manifest.Version)

	return nil
}

// discoverAndRegisterAttributes performs schema discovery for plugins that
// declare resource_types. It obtains the AttributeResolver gRPC client from the
// binary host, calls GetSchema to discover attribute schemas, validates that the
// schema covers all declared resource types, and registers proxy providers.
// It returns the discovered schemas for use by CheckManifestWarnings.
func (m *Manager) discoverAndRegisterAttributes(ctx context.Context, host Host, dp *DiscoveredPlugin) (map[string]*types.NamespaceSchema, error) {
	pluginName := dp.Manifest.Name

	arProvider := m.capabilitiesFor(host).arProvider
	if arProvider == nil {
		return nil, oops.In("manager").With("plugin", pluginName).
			Errorf("resource_types requires a host that implements AttributeResolverProvider")
	}

	arClient := arProvider.AttributeResolverClient(pluginName)
	if arClient == nil {
		return nil, oops.In("manager").With("plugin", pluginName).
			Errorf("failed to get AttributeResolver client")
	}

	schemaResp, schemaErr := arClient.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
	if schemaErr != nil {
		if unloadErr := host.Unload(ctx, pluginName); unloadErr != nil {
			slog.Error("failed to rollback plugin load after schema discovery failure",
				"plugin", pluginName, "error", unloadErr)
		}
		return nil, oops.In("manager").With("plugin", pluginName).
			Wrapf(schemaErr, "schema discovery failed")
	}

	schemas := ConvertProtoSchema(schemaResp)
	for _, rt := range dp.Manifest.ResourceTypes {
		if _, ok := schemas[rt]; !ok {
			if unloadErr := host.Unload(ctx, pluginName); unloadErr != nil {
				slog.Error("failed to rollback plugin after schema validation failure",
					"plugin", pluginName, "error", unloadErr)
			}
			return nil, oops.In("manager").With("plugin", pluginName).
				With("resource_type", rt).
				Errorf("plugin declares resource_type %q but GetSchema did not return it", rt)
		}
	}

	if m.registerProvider != nil {
		for i, rt := range dp.Manifest.ResourceTypes {
			provider := NewPluginAttributeProvider(rt, arClient, schemas[rt])
			if regErr := m.registerProvider(provider); regErr != nil {
				// Provider registration failure must be fatal — the plugin
				// declares it owns this resource type but ABAC can't resolve
				// attributes for it, so any policy targeting that type would
				// silently fail at evaluation. This is consistent with how
				// GetSchema, policy installation, and service registration
				// failures are handled.
				//
				// Rollback: unregister any providers that were added in
				// previous iterations of this loop before returning.
				m.unregisterPluginProviders(pluginName, dp.Manifest.ResourceTypes, i)
				if unloadErr := host.Unload(ctx, pluginName); unloadErr != nil {
					slog.Error("failed to rollback plugin after attribute provider registration failure",
						"plugin", pluginName, "error", unloadErr)
				}
				return nil, oops.In("manager").
					With("plugin", pluginName).
					With("resource_type", rt).
					Wrapf(regErr, "failed to register attribute provider")
			}
		}
	}

	return schemas, nil
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

// TestLoadPlugin injects a plugin directly for unit testing.
// Only available in tests (but not build-tag restricted to keep it simple).
func (m *Manager) TestLoadPlugin(name string, manifest *Manifest) {
	m.mu.Lock()
	host, ok := m.hosts[manifest.Type]
	if !ok && manifest.Type == TypeLua && m.luaHost != nil {
		host, ok = m.luaHost, true
	}
	m.mu.Unlock()

	if ok {
		if err := host.Load(context.Background(), manifest, ""); err != nil {
			panic("TestLoadPlugin: host.Load failed: " + err.Error())
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.loaded[name] = &DiscoveredPlugin{Manifest: manifest}
	if ok {
		m.pluginHosts[name] = host
	}
}

// isValidStreamName returns true if name is a valid HoloMUSH stream name.
// Stream names must be non-empty, contain at least one colon, have no whitespace,
// and be at most 256 characters long.
func isValidStreamName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for _, r := range name {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return false
		}
	}
	return strings.Contains(name, ":")
}

// QuerySessionStreams collects plugin-contributed stream names for a session.
// Only plugins with SessionStreams: true in their manifest are queried.
// Plugin errors are logged and skipped (degraded-subscribe policy).
// Invalid stream names are dropped. Duplicate streams are deduplicated.
func (m *Manager) QuerySessionStreams(ctx context.Context, req SessionStreamsRequest) []string {
	m.mu.RLock()
	type pluginEntry struct {
		name string
		host Host
	}
	var opted []pluginEntry
	for name, dp := range m.loaded {
		if dp.Manifest.SessionStreams {
			if host, ok := m.pluginHosts[name]; ok {
				opted = append(opted, pluginEntry{name, host})
			}
		}
	}
	m.mu.RUnlock()

	if len(opted) == 0 {
		return nil
	}

	type result struct {
		name    string
		streams []string
		err     error
	}
	results := make(chan result, len(opted))
	for _, p := range opted {
		p := p
		go func() {
			streams, err := p.host.QuerySessionStreams(ctx, p.name, req)
			select {
			case results <- result{name: p.name, streams: streams, err: err}:
			case <-ctx.Done():
			}
		}()
	}

	seen := make(map[string]bool)
	var merged []string
	for range opted {
		var r result
		select {
		case r = <-results:
		case <-ctx.Done():
			return merged
		}
		if r.err != nil {
			slog.WarnContext(ctx, "plugin stream contribution failed — skipping",
				"plugin", r.name,
				"character_id", req.CharacterID,
				"session_id", req.SessionID,
				"error", r.err)
			continue
		}
		for _, s := range r.streams {
			if !isValidStreamName(s) {
				slog.WarnContext(ctx, "plugin returned invalid stream name — dropping",
					"plugin", r.name,
					"stream", s)
				continue
			}
			if !seen[s] {
				seen[s] = true
				merged = append(merged, s)
			}
		}
	}
	return merged
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

func (m *Manager) lookupManifest(name string) *Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dp, ok := m.loaded[name]
	if !ok {
		dp, ok = m.inflight[name]
	}
	if !ok {
		return nil
	}
	return dp.Manifest
}

func actorFromContext(ctx context.Context, _ string) (core.Actor, error) {
	actor, ok := core.ActorFromContext(ctx)
	if !ok {
		return core.Actor{}, oops.New("plugin event actor missing from context")
	}
	return actor, nil
}

// RegisterPluginCommands iterates all loaded plugins and registers their
// manifest-declared commands into the given command registry. This ensures
// the dispatcher can route plugin-backed commands via registry.Get().
func (m *Manager) RegisterPluginCommands(registry *command.Registry) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, dp := range m.loaded {
		for i := range dp.Manifest.Commands {
			cmdSpec := &dp.Manifest.Commands[i]
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
