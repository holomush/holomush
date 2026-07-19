// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/focuscontract"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
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

	// CodeMissingVerbRegistry is returned by NewManager when no
	// VerbRegistry has been configured via WithVerbRegistry. INV-EVENTBUS-11.
	CodeMissingVerbRegistry = "MISSING_VERB_REGISTRY"
)

// ErrMissingVerbRegistry is returned by NewManager when no VerbRegistry has
// been configured via WithVerbRegistry. INV-EVENTBUS-11: every plugin manager MUST
// be constructed with a non-nil VerbRegistry so plugin-declared verbs and
// host-owned event types resolve through a single shared source of truth.
var ErrMissingVerbRegistry = oops.Code(CodeMissingVerbRegistry).
	Errorf("plugin manager requires a VerbRegistry; pass WithVerbRegistry(...)")

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
	capVocab            *CapabilityVocabulary        // controlled host-capability vocabulary; defaulted in NewManager
	trustAllowlist      map[string]bool              // server-side trust escalation allowlist
	gracefulDegradation bool                         // if true, LoadAll continues despite plugin failures
	aliasSeeder         AliasSeeder
	aliasCache          *command.AliasCache
	verbRegistry        *core.VerbRegistry
	eventEmitter        *PluginEventEmitter
	loaded              map[string]*DiscoveredPlugin
	inflight            map[string]*DiscoveredPlugin
	loadedOrder         []*DiscoveredPlugin // preserves DAG/priority load order for deterministic iteration
	mu                  sync.RWMutex

	// Identity registry: name ↔ ULID maps populated at bootstrap from the
	// plugins table; mutated on load/unload. nameByID resolves three
	// populations (active plugins + historical plugins + system sentinels);
	// activeByName resolves only currently-loaded plugins. Both are
	// guarded by the existing m.mu RWMutex.
	pluginRepo       store.PluginRepo
	nameByID         map[ulid.ULID]string
	activeByName     map[string]ulid.ULID
	retentionDays    int  // plugin row TTL (days); 0 = sweep disabled; default 3
	retentionDaysSet bool // true iff WithRetentionDays was called explicitly
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

// WithVerbRegistry sets the VerbRegistry for plugin verb registration.
func WithVerbRegistry(reg *core.VerbRegistry) ManagerOption {
	return func(m *Manager) {
		m.verbRegistry = reg
	}
}

// WithPluginRepo wires the IdentityRegistry's persistence layer.
// Required when the Manager will Upsert plugin rows. Without it,
// loadPlugin operates with an in-memory-only registry (test seam).
func WithPluginRepo(repo store.PluginRepo) ManagerOption {
	return func(m *Manager) { m.pluginRepo = repo }
}

// WithRetentionDays configures plugin row TTL (days). After RetentionDays
// of inactivity, a plugin row is deactivated (gc_at set) at the end of
// LoadAll. 0 disables the sweep entirely. Default: 3.
func WithRetentionDays(days int) ManagerOption {
	return func(m *Manager) {
		m.retentionDays = days
		m.retentionDaysSet = true
	}
}

// Registry returns the service registry, or nil if not configured.
func (m *Manager) Registry() *ServiceRegistry {
	return m.registry
}

// NewManager creates a plugin manager.
//
// INV-EVENTBUS-11: callers MUST supply a non-nil VerbRegistry via
// WithVerbRegistry. Construction returns ErrMissingVerbRegistry when the
// option is omitted so plugin-declared verbs always have a place to land.
func NewManager(pluginsDir string, opts ...ManagerOption) (*Manager, error) {
	m := &Manager{
		pluginsDir:  pluginsDir,
		capVocab:    DefaultCapabilityVocabulary(),
		loaded:      make(map[string]*DiscoveredPlugin),
		inflight:    make(map[string]*DiscoveredPlugin),
		hosts:       make(map[Type]Host),
		hostCaps:    make(map[Host]hostCapabilities),
		pluginHosts: make(map[string]Host),
	}
	for _, opt := range opts {
		opt(m)
	}
	// Default retentionDays to 3 when WithRetentionDays was not called.
	if !m.retentionDaysSet {
		m.retentionDays = 3
	}
	// If WithLuaHost was used, cache its capabilities for the same lookup path.
	if m.luaHost != nil {
		m.hostCaps[m.luaHost] = discoverCapabilities(m.luaHost)
	}

	// Initialize the identity registry cache.
	m.nameByID = make(map[ulid.ULID]string)
	m.activeByName = make(map[string]ulid.ULID)

	// Step 1: register system sentinels first (not in activeByName, not
	// in the plugins table — different identity domain).
	m.nameByID[core.SystemActorULID] = "system"
	m.nameByID[core.WorldServiceActorULID] = "world-service"

	// Step 2: load existing plugin rows from persistence. Reject sentinel
	// collisions defensively.
	if m.pluginRepo != nil {
		ctx := context.Background()
		rows, err := m.pluginRepo.ListAll(ctx)
		if err != nil {
			return nil, oops.Code("PLUGIN_MANAGER_BOOTSTRAP").Wrap(err)
		}
		for i := range rows {
			row := &rows[i]
			if core.IsSentinelULID(row.ID) {
				return nil, oops.Code("PLUGIN_ROW_USES_SENTINEL_ID").
					With("name", row.Name).
					With("id", row.ID.String()).
					Errorf("plugin row uses a reserved sentinel ULID")
			}
			m.nameByID[row.ID] = row.Name
			if row.GcAt == nil {
				m.activeByName[row.Name] = row.ID
			}
		}
	}

	if m.verbRegistry == nil {
		return nil, ErrMissingVerbRegistry
	}
	return m, nil
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
	if configurer := findOptional[IdentityRegistryConfigurer](host); configurer != nil {
		configurer.SetIdentityRegistry(m)
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

// BeginServiceDispatch resolves the named plugin's host and delegates to its
// ServiceDispatcher capability, minting a dispatch token for a host-initiated
// call into the plugin's registered gRPC services. See the ServiceDispatcher
// doc for the actor/release contract and why this is binary-only by
// construction.
//
// Typed errors: PLUGIN_NOT_LOADED when no host owns pluginName;
// SERVICE_DISPATCH_UNSUPPORTED when the owning host lacks the capability
// (e.g. the Lua host).
func (m *Manager) BeginServiceDispatch(ctx context.Context, pluginName string, actor core.Actor, ownerPlayerID string) (context.Context, func(), error) {
	m.mu.RLock()
	host, ok := m.pluginHosts[pluginName]
	var dispatcher ServiceDispatcher
	if ok {
		// hostCaps is written under m.mu in RegisterHost; read it inside the
		// same critical section that resolves the host.
		dispatcher = m.capabilitiesFor(host).dispatcher
	}
	m.mu.RUnlock()

	if !ok {
		return nil, nil, oops.Code("PLUGIN_NOT_LOADED").In("manager").
			With("plugin", pluginName).
			With("operation", "begin_service_dispatch").
			New("plugin not loaded or unknown")
	}

	if dispatcher == nil {
		return nil, nil, oops.Code("SERVICE_DISPATCH_UNSUPPORTED").In("manager").
			With("plugin", pluginName).
			With("operation", "begin_service_dispatch").
			New("plugin's host does not support service dispatch (binary plugins only)")
	}

	dispatchCtx, release, err := dispatcher.BeginServiceDispatch(ctx, pluginName, actor, ownerPlayerID)
	if err != nil {
		return nil, nil, oops.In("manager").With("plugin", pluginName).With("operation", "begin_service_dispatch").Wrap(err)
	}
	return dispatchCtx, release, nil
}

// ConfigureEventEmitter wires the shared plugin event emitter to the provided
// EventBus publisher. Production startup MUST call this before plugin response
// events are routed through the manager.
//
// Post-F1 the emitter publishes to JetStream (no core.EventStore.Append path
// remains). Callers SHOULD pass `eventBusSub.Publisher()` here; tests MAY
// inject a fake Publisher.
func (m *Manager) ConfigureEventEmitter(publisher eventbus.Publisher, opts ...EmitterOption) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventEmitter = NewPluginEventEmitter(publisher, m.lookupManifest, actorFromContext, opts...)
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

// ConfigureFocusDeps injects the focus coordinator and history reader into all
// registered hosts. Production startup MUST call this before plugins handle
// focus-related RPCs or host functions. Called from the gRPC subsystem's
// Start after creating the FocusCoordinator.
func (m *Manager) ConfigureFocusDeps(fc focuscontract.Coordinator, hr HistoryReader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, host := range m.hosts {
		if configurer := findOptional[FocusDepsConfigurer](host); configurer != nil {
			configurer.SetFocusCoordinator(fc)
			configurer.SetHistoryReader(hr)
		}
	}
	if m.luaHost != nil {
		if configurer := findOptional[FocusDepsConfigurer](m.luaHost); configurer != nil {
			configurer.SetFocusCoordinator(fc)
			configurer.SetHistoryReader(hr)
		}
	}
}

// ConfigureReadbackDecryptor injects the read-back decryptor into all
// registered hosts that implement ReadbackDepsConfigurer. Production startup
// MUST call this before plugins issue DecryptOwnAuditRows RPCs. Called from the
// gRPC subsystem's Start after the history reader (and thus the OwnerMap +
// crypto deps) is built.
func (m *Manager) ConfigureReadbackDecryptor(d ReadbackDecryptor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, host := range m.hosts {
		if configurer := findOptional[ReadbackDepsConfigurer](host); configurer != nil {
			configurer.SetReadbackDecryptor(d)
		}
	}
	if m.luaHost != nil {
		if configurer := findOptional[ReadbackDepsConfigurer](m.luaHost); configurer != nil {
			configurer.SetReadbackDecryptor(d)
		}
	}
}

// ConfigureSettingsDeps injects the plugin-partitioned settings stores into all
// registered hosts that implement SettingsDepsConfigurer. Production startup
// MUST call this before plugins issue GetSetting / SetSetting RPCs (or the Lua
// equivalents). Called from the gRPC subsystem's Start after the settings stores
// are assembled. Same late-binding pattern as ConfigureFocusDeps (holomush-iokti.7).
func (m *Manager) ConfigureSettingsDeps(
	player settings.PlayerSettingsStore,
	character settings.CharacterSettingsStore,
	game settings.GameSettings,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, host := range m.hosts {
		if configurer := findOptional[SettingsDepsConfigurer](host); configurer != nil {
			configurer.SetSettingsStores(player, character, game)
		}
	}
	if m.luaHost != nil {
		if configurer := findOptional[SettingsDepsConfigurer](m.luaHost); configurer != nil {
			configurer.SetSettingsStores(player, character, game)
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
	return emitter.Emit(ctx, pluginName, emitIntentFromEmitEvent(event))
}

// emitIntentFromEmitEvent maps a plugin-return EmitEvent onto the host-facing
// EmitIntent. This is the single construction site for the Lua and binary
// return-value emit paths (both reach the shared emitter through here); routing
// it through one function lets TestEmitIntentFromEmitEventCarriesEveryField
// assert by reflection that every EmitIntent field is populated, so a field
// added to EmitIntent cannot silently stay zero on these paths (holomush-av954).
//
// EmitEvent is the plugin-return shape (Stream is the legacy field name);
// EmitIntent is the host-facing shape (Subject). F5 migrates plugin code to
// Subject natively.
func emitIntentFromEmitEvent(event pluginsdk.EmitEvent) pluginsdk.EmitIntent {
	return pluginsdk.EmitIntent{
		Subject:   event.Stream,
		Type:      event.Type,
		Payload:   event.Payload,
		Sensitive: event.Sensitive,
	}
}

// DiscoveredPlugin contains a manifest and its directory.
type DiscoveredPlugin struct {
	Manifest *Manifest
	Dir      string
}

// Discover finds all valid plugins in the plugins directory.
// Invalid plugins are logged and skipped.
func (m *Manager) Discover(ctx context.Context) ([]*DiscoveredPlugin, error) {
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
			slog.WarnContext(ctx, "skipping plugin without manifest",
				"dir", entry.Name(),
				"error", err)
			continue
		}

		manifest, err := ParseManifest(data)
		if err != nil {
			slog.WarnContext(ctx, "skipping plugin with invalid manifest",
				"dir", entry.Name(),
				"error", err)
			continue
		}

		// Static crypto-section validation. Failure means the manifest is
		// internally malformed (unknown sensitivity, duplicate emit, etc.)
		// and we skip the plugin entirely, mirroring the ParseManifest path.
		if err := ValidateCrypto(manifest); err != nil {
			slog.WarnContext(ctx, "skipping plugin with invalid crypto section",
				"dir", entry.Name(),
				"plugin", manifest.Name,
				"error", err)
			continue
		}

		plugins = append(plugins, &DiscoveredPlugin{
			Manifest: manifest,
			Dir:      pluginDir,
		})
	}

	// Filter out plugins whose cross-plugin refs don't resolve. Iterate to a
	// fixed point: a plugin's refs MUST resolve against the FINAL accepted
	// set, not the initial discovery set, otherwise plugin-b can resolve
	// against plugin-a in the same pass that filters plugin-a out.
	resolved := plugins
	for {
		emitRegistry := make(map[string][]CryptoEmit, len(resolved))
		for _, dp := range resolved {
			if dp.Manifest.Crypto != nil {
				emitRegistry[dp.Manifest.Name] = dp.Manifest.Crypto.Emits
			}
		}
		next := resolved[:0]
		for _, dp := range resolved {
			if err := ResolveCryptoRefs(dp.Manifest, emitRegistry); err != nil {
				slog.WarnContext(ctx, "skipping plugin with unresolvable crypto refs",
					"plugin", dp.Manifest.Name,
					"dir", dp.Dir,
					"error", err)
				continue
			}
			next = append(next, dp)
		}
		if len(next) == len(resolved) {
			return next, nil
		}
		resolved = next
	}
}

// warnUnknownTrustAllowlistEntries logs a slog.Warn for each entry in the
// trust allowlist that does not match a discovered plugin name. Called from
// LoadAll after Discover. Intended to surface operator typos or stale config
// that would otherwise silently fail to grant trust to the intended plugin
// — or reserve the allowlist slot for a future crafted plugin with that name.
func (m *Manager) warnUnknownTrustAllowlistEntries(discovered []*DiscoveredPlugin) {
	if len(m.trustAllowlist) == 0 {
		return
	}
	discoveredNames := make(map[string]bool, len(discovered))
	for _, dp := range discovered {
		discoveredNames[dp.Manifest.Name] = true
	}
	// Sort so log output is deterministic across runs.
	unknown := make([]string, 0, len(m.trustAllowlist))
	for name := range m.trustAllowlist {
		if !discoveredNames[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return
	}
	sort.Strings(unknown)
	for _, name := range unknown {
		slog.Warn("trust-allowlisted plugin not discovered",
			"plugin", name,
			"hint", "check for typos in plugin_trust_allowlist config or remove stale entries")
	}
}

// LoadAll discovers and loads all plugins in the plugins directory.
//
// When a ServiceRegistry is configured (via WithServiceRegistry), LoadAll uses
// DAG-based dependency resolution to determine load order. Resolution failure
// (a cycle or a non-optional unsatisfied dependency) is a fatal boot error
// (fail-closed, INV-PLUGIN-43): LoadAll returns it before loading any plugin.
// With no registry, load order falls back to priority sort.
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

	// Surface trust-allowlist misconfigurations: an allowlisted name that
	// matches no discovered plugin is almost certainly a typo or stale
	// config. Left silent, it either grants no trust to the plugin the
	// operator intended, or reserves the slot for a crafted future plugin
	// with that name. Warn per unknown entry so the operator sees it.
	m.warnUnknownTrustAllowlistEntries(discovered)

	// Phase 2: Collect cross-plugin context.
	knownResourceTypes := CollectResourceTypes(discovered)
	knownActions := CollectActions(discovered)

	// Phase 3: Resolve load order.
	res, err := m.resolveLoadOrder(discovered)
	if err != nil {
		return err
	}
	ordered := res.Ordered

	// Thread the resolver grant set into all registered hosts so each host's
	// delivery shim can use grants as the single least-privilege authority.
	// res.Grants is nil on the no-registry path — hosts treat nil as "fall back
	// to manifest.RequiredCapabilities()" preserving existing behavior.
	if res.Grants != nil {
		for _, h := range m.hosts {
			if gc := findOptional[PluginGrantsConfigurer](h); gc != nil {
				gc.SetPluginGrants(res.Grants)
			}
		}
		if m.luaHost != nil {
			if gc := findOptional[PluginGrantsConfigurer](m.luaHost); gc != nil {
				gc.SetPluginGrants(res.Grants)
			}
		}
	}

	// Phase 4: Load each plugin with full context.
	var loadErrors []error
	for _, dp := range ordered {
		if err := m.loadPlugin(ctx, dp, knownResourceTypes, knownActions); err != nil {
			slog.ErrorContext(ctx, "failed to load plugin",
				"plugin", dp.Manifest.Name,
				"priority", dp.Manifest.EffectivePriority(),
				"error", err)
			loadErrors = append(loadErrors,
				oops.With("plugin", dp.Manifest.Name).Wrap(err))
		}
	}

	if len(loadErrors) > 0 {
		// gracefulDegradation governs per-plugin LOAD failures only. DAG
		// resolution (resolveLoadOrder → defaultResolvePolicy) is always
		// fail-closed and is NOT subject to this flag; that domain is
		// defaultResolvePolicy's responsibility.
		if m.gracefulDegradation {
			slog.WarnContext(ctx, "plugin loading completed with errors (graceful degradation enabled)",
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
			slog.ErrorContext(ctx, "failed to seed plugin aliases", "error", err)
		}
	}

	// w9ml T8: GC sweep — runs AFTER all loads have refreshed last_seen_at,
	// so a plugin loaded in this cycle is never swept in the same cycle
	// (INV-PLUGIN-16). Skipped on the graceful-degradation early return path
	// because partial-load failures may leave last_seen_at stale.
	if m.pluginRepo != nil && m.retentionDays > 0 {
		swept, err := m.pluginRepo.SweepInactive(ctx, m.retentionDays)
		if err != nil {
			return oops.Code("PLUGIN_MANAGER_SWEEP").Wrap(err)
		}
		for i := range swept {
			row := &swept[i]
			m.mu.Lock()
			delete(m.activeByName, row.Name)
			// nameByID intentionally retained for historical resolution.
			m.mu.Unlock()
			slog.InfoContext(
				ctx,
				"plugin.gc",
				"name", row.Name,
				"id", row.ID.String(),
				"last_seen_at", row.LastSeenAt.Format(time.RFC3339),
			)
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
	dispatcher   ServiceDispatcher         // nil if host doesn't support
}

// discoverCapabilities walks a chain of Host wrappers (via the optional Unwrap
// method) to find implementations of optional interfaces. Called once at host
// registration time; results are cached on the Manager.
func discoverCapabilities(h Host) hostCapabilities {
	return hostCapabilities{
		connProvider: findOptional[ServiceConnProvider](h),
		arProvider:   findOptional[AttributeResolverProvider](h),
		dispatcher:   findOptional[ServiceDispatcher](h),
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

// CollectActions builds the full set of known ABAC actions: core actions plus
// all actions explicitly declared across discovered plugins. This cross-plugin
// context is needed for semantic validation during loadPlugin. Exported as a
// test seam so callers can verify the merge logic without driving LoadAll.
func CollectActions(discovered []*DiscoveredPlugin) map[string]bool {
	known := command.CoreActions()
	for _, dp := range discovered {
		for _, a := range dp.Manifest.Actions {
			known[a] = true
		}
	}
	return known
}

// CollectFocusRedirects merges every discovered plugin's focus_redirects into a
// verb-keyed command.FocusRedirectTable. It validates that each target_command
// is a registered command and that no (verb, focus_kind) pair is claimed by two
// plugins — both are fail-closed startup errors. Verb keys are trimmed so a
// whitespace-padded manifest verb matches the parser-trimmed dispatch token.
// Exported as a test seam (mirrors CollectResourceTypes / CollectActions).
func CollectFocusRedirects(discovered []*DiscoveredPlugin, registry *command.Registry) (command.FocusRedirectTable, error) {
	table := command.FocusRedirectTable{}
	for _, dp := range discovered {
		for i := range dp.Manifest.FocusRedirects {
			fr := &dp.Manifest.FocusRedirects[i]
			if _, ok := registry.Get(fr.TargetCommand); !ok {
				return nil, oops.Code("FOCUS_REDIRECT_UNKNOWN_TARGET").
					With("plugin", dp.Manifest.Name).
					With("target_command", fr.TargetCommand).
					Errorf("focus_redirect target command %q is not a registered command", fr.TargetCommand)
			}
			for _, verbRaw := range fr.Verbs {
				verb := strings.TrimSpace(verbRaw)
				byKind := table[verb]
				if byKind == nil {
					byKind = map[string]string{}
					table[verb] = byKind
				}
				if existing, dup := byKind[fr.FocusKind]; dup {
					return nil, oops.Code("FOCUS_REDIRECT_DUPLICATE").
						With("verb", verb).With("focus_kind", fr.FocusKind).
						With("existing_target", existing).With("plugin", dp.Manifest.Name).
						Errorf("duplicate focus_redirect for verb %q + focus_kind %q", verb, fr.FocusKind)
				}
				byKind[fr.FocusKind] = fr.TargetCommand
			}
		}
	}
	return table, nil
}

// BuildFocusRedirects collects redirects from the loaded plugin set in
// deterministic load order. Thin wrapper over CollectFocusRedirects used by the
// dispatcher wiring.
func (m *Manager) BuildFocusRedirects(registry *command.Registry) (command.FocusRedirectTable, error) {
	return CollectFocusRedirects(m.loadedOrder, registry)
}

// resolvePolicy decides loader behavior from a structured resolve result.
// The concrete function is swappable so a future gracefulDegradation quarantine
// strategy can replace defaultResolvePolicy at this single call site without
// touching the resolver. Today there is only one policy: fail-closed.
type resolvePolicy func(*ResolveResult) error

// defaultResolvePolicy is fail-closed (INV-PLUGIN-43): any non-optional
// unsatisfied dependency or cycle is fatal. DUPLICATE_* errors are bare Go
// errors returned by ResolveDependencyOrder before the result is built, so they
// are NOT visible here and are not a policy case.
func defaultResolvePolicy(res *ResolveResult) error {
	if len(res.Unsatisfied) > 0 || len(res.Cycles) > 0 {
		return oops.Code("PLUGIN_DEPENDENCY_UNSATISFIED").
			With("unsatisfied", res.Unsatisfied).With("cycles", res.Cycles).
			Errorf("plugin dependency resolution failed; fail-closed (INV-PLUGIN-43)")
	}
	return nil
}

// applyResolvePolicy applies p to res and returns the resulting error (nil on success).
func applyResolvePolicy(res *ResolveResult, p resolvePolicy) error { return p(res) }

// resolveLoadOrder resolves plugins into load order with their grant sets.
// When a registry is configured, it uses DAG-based dependency resolution and
// fails the boot (fail-closed, INV-PLUGIN-43) on any non-optional unsatisfied
// dependency or cycle. With no registry, it falls back to priority sort and
// returns a result with a nil Grants map (no-registry path: hosts fall back
// to manifest-derived caps).
func (m *Manager) resolveLoadOrder(discovered []*DiscoveredPlugin) (*ResolveResult, error) {
	if m.registry == nil {
		// No registry: priority sort only; Grants is nil. On the nil-Grants
		// path BOTH runtime shims fall back to the SAME source —
		// manifest.RequiredCapabilities() — with no per-runtime divergence
		// (ADR holomush-vpg8l). This is the backward-compat fallback, not an
		// endorsement of any per-runtime gating: INV-PLUGIN-45 forbids
		// divergence, and the shared fallback satisfies that by construction.
		return &ResolveResult{Ordered: prioritySort(discovered)}, nil
	}

	serverServices := m.registry.List()
	serverServiceNames := make([]string, 0, len(serverServices))
	for _, svc := range serverServices {
		serverServiceNames = append(serverServiceNames, svc.Name)
	}

	vocab := m.capVocab
	if vocab == nil {
		vocab = DefaultCapabilityVocabulary()
	}

	res, err := ResolveDependencyOrder(discovered, serverServiceNames, vocab)
	if err != nil {
		return nil, oops.Code("PLUGIN_DEPENDENCY_RESOLVE_FAILED").Wrap(err)
	}
	if err := applyResolvePolicy(res, defaultResolvePolicy); err != nil {
		return nil, err
	}
	return res, nil
}

// prioritySort orders plugins by load priority (lower values load first). It is
// the no-registry fallback path for resolveLoadOrder.
func prioritySort(discovered []*DiscoveredPlugin) []*DiscoveredPlugin {
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

// computeHashes returns sha256 of the plugin's plugin.yaml bytes (always)
// and its executable artifact:
//   - TypeBinary:  sha256 of the executable file at BinaryPlugin.Executable.
//   - TypeLua:     sha256 of deterministic concatenation of *.lua files
//     (sorted by relative path within Dir; rel-path NUL contents
//     NUL between files).
//   - TypeSetting: nil (no executable artifact).
//
// Hashes feed PluginRepo.Upsert for drift detection; manifest_hash is
// always non-nil, content_hash is nil only for setting plugins.
func (m *Manager) computeHashes(dp *DiscoveredPlugin) (manifestHash, contentHash []byte, err error) {
	mfBytes, err := os.ReadFile(filepath.Join(dp.Dir, "plugin.yaml"))
	if err != nil {
		return nil, nil, oops.Code("PLUGIN_HASH_MANIFEST_READ").
			With("plugin", dp.Manifest.Name).Wrap(err)
	}
	mh := sha256.Sum256(mfBytes)
	manifestHash = mh[:]

	switch dp.Manifest.Type {
	case TypeBinary:
		if dp.Manifest.BinaryPlugin == nil || dp.Manifest.BinaryPlugin.Executable == "" {
			return nil, nil, oops.Code("PLUGIN_HASH_BINARY_MISSING_EXECUTABLE").
				With("plugin", dp.Manifest.Name).
				Errorf("binary plugin must declare binary-plugin.executable")
		}
		bin, readErr := os.ReadFile(filepath.Join(dp.Dir, dp.Manifest.BinaryPlugin.Executable))
		if readErr != nil {
			return nil, nil, oops.Code("PLUGIN_HASH_BINARY_READ").
				With("plugin", dp.Manifest.Name).Wrap(readErr)
		}
		ch := sha256.Sum256(bin)
		contentHash = ch[:]
	case TypeLua:
		var luaFiles []string
		walkErr := filepath.Walk(dp.Dir, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !info.IsDir() && filepath.Ext(p) == ".lua" {
				rel, relErr := filepath.Rel(dp.Dir, p)
				if relErr != nil {
					return oops.Code("PLUGIN_HASH_LUA_REL").
						With("plugin", dp.Manifest.Name).Wrap(relErr)
				}
				luaFiles = append(luaFiles, rel)
			}
			return nil
		})
		if walkErr != nil {
			return nil, nil, oops.Code("PLUGIN_HASH_LUA_WALK").
				With("plugin", dp.Manifest.Name).Wrap(walkErr)
		}
		sort.Strings(luaFiles)
		h := sha256.New()
		for _, rel := range luaFiles {
			b, readErr := os.ReadFile(filepath.Join(dp.Dir, rel))
			if readErr != nil {
				return nil, nil, oops.Code("PLUGIN_HASH_LUA_READ").
					With("plugin", dp.Manifest.Name).With("file", rel).Wrap(readErr)
			}
			h.Write([]byte(rel))
			h.Write([]byte{0x00})
			h.Write(b)
			h.Write([]byte{0x00})
		}
		contentHash = h.Sum(nil)
	case TypeSetting:
		contentHash = nil
	default:
		return nil, nil, oops.Code("PLUGIN_HASH_UNKNOWN_TYPE").
			With("plugin", dp.Manifest.Name).
			With("type", string(dp.Manifest.Type)).
			Errorf("unknown plugin type")
	}
	return manifestHash, contentHash, nil
}

// loadPlugin loads a single discovered plugin.
//
// Design: Returns nil (not error) for unsupported configurations to support
// graceful degradation. This allows running without Lua support or before
// binary plugin support is implemented. The warning logs provide visibility.
func (m *Manager) loadPlugin(ctx context.Context, dp *DiscoveredPlugin, knownResourceTypes, knownActions map[string]bool) error {
	// Resolve the host for this plugin type first — unsupported configurations
	// are skipped here before any semantic validation so that graceful degradation
	// (e.g., no binary host configured) is not blocked by capability checks.
	// For backward compatibility, TypeLua falls back to the dedicated luaHost field.
	var host Host
	switch dp.Manifest.Type {
	case TypeLua:
		host = m.hosts[TypeLua]
		if host == nil {
			host = m.luaHost // backward compatibility
		}
		if host == nil {
			slog.WarnContext(ctx, "no Lua host configured, skipping Lua plugin",
				"plugin", dp.Manifest.Name)
			return nil
		}
	case TypeBinary:
		host = m.hosts[TypeBinary]
		if host == nil {
			slog.WarnContext(ctx, "binary plugins not yet supported, skipping",
				"plugin", dp.Manifest.Name)
			return nil
		}
	default:
		slog.WarnContext(ctx, "unknown plugin type, skipping",
			"plugin", dp.Manifest.Name,
			"type", dp.Manifest.Type)
		return nil
	}

	// Semantic validation: check capability resource types and actions against the full known sets.
	coreActions := command.CoreActions()
	ownActions := make(map[string]bool, len(dp.Manifest.Actions))
	for _, a := range dp.Manifest.Actions {
		ownActions[a] = true
	}
	for i := range dp.Manifest.Commands {
		cmd := &dp.Manifest.Commands[i]
		for _, cap := range cmd.Capabilities {
			if err := cap.ValidateResourceType(knownResourceTypes); err != nil {
				return oops.In("manager").With("plugin", dp.Manifest.Name).
					With("command", cmd.Name).Wrap(err)
			}
			if err := cap.ValidateAction(knownActions); err != nil {
				return oops.In("manager").With("plugin", dp.Manifest.Name).
					With("command", cmd.Name).Wrap(err)
			}
			if !coreActions[cap.Action] && !ownActions[cap.Action] {
				slog.WarnContext(ctx, "capability uses action not declared by this plugin",
					"plugin", dp.Manifest.Name,
					"command", cmd.Name,
					"action", cap.Action)
			}
		}
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

	// w9ml T6: compute hashes, Upsert into plugins table, populate cache.
	// Hash computation only runs when pluginRepo is wired; tests that construct
	// Manager without WithPluginRepo take the else branch and bypass computeHashes.
	var pluginID ulid.ULID
	var drift *store.DriftReport
	if m.pluginRepo != nil {
		manifestHash, contentHash, hashErr := m.computeHashes(dp)
		if hashErr != nil {
			return hashErr
		}
		id, d, upsertErr := m.pluginRepo.Upsert(ctx, store.PluginUpsertInput{
			Name:         dp.Manifest.Name,
			DisplayName:  dp.Manifest.Name,
			Version:      dp.Manifest.Version,
			ManifestHash: manifestHash,
			ContentHash:  contentHash,
		})
		if upsertErr != nil {
			return oops.In("manager").With("plugin", dp.Manifest.Name).Wrap(upsertErr)
		}
		pluginID, drift = id, d
	} else {
		pluginID = idgen.New()
	}

	// Cache mutation BEFORE host.Load — downstream code may emit during Load
	// and needs to resolve plugin name via IDByName.
	m.mu.Lock()
	m.nameByID[pluginID] = dp.Manifest.Name
	m.activeByName[dp.Manifest.Name] = pluginID
	m.mu.Unlock()

	// Roll back the cache mutation if any subsequent step fails. loadPlugin
	// returns a bare `error` (not a named return), so we cannot use
	// `defer func() { if err != nil ... }()` — closure would capture the
	// wrong `err` after shadowing in subsequent if-blocks. Use an explicit
	// rollback flag set by the success path.
	var loadPluginCommitted bool
	defer func() {
		if !loadPluginCommitted {
			m.mu.Lock()
			delete(m.nameByID, pluginID)
			delete(m.activeByName, dp.Manifest.Name)
			m.mu.Unlock()
		}
	}()

	// Drift logging (no decision logic — log and continue per spec).
	if drift != nil {
		slog.InfoContext(
			ctx,
			"plugin.drift",
			"name", dp.Manifest.Name,
			"old_manifest_hash", hex.EncodeToString(drift.OldManifestHash),
			"new_manifest_hash", hex.EncodeToString(drift.NewManifestHash),
			"old_content_hash", hex.EncodeToString(drift.OldContentHash),
			"new_content_hash", hex.EncodeToString(drift.NewContentHash),
			"version_before", drift.VersionBefore,
			"version_after", drift.VersionAfter,
		)
	}

	if err := host.Load(ctx, dp.Manifest, dp.Dir); err != nil {
		return oops.In("manager").With("plugin", dp.Manifest.Name).With("operation", "load").Wrap(err)
	}

	// INV-PLUGIN-32: manifest emit-type startup validation. Scope per INV-PLUGIN-33:
	// only plugins with non-empty crypto.emits participate.
	if dp.Manifest.Crypto != nil && len(dp.Manifest.Crypto.Emits) > 0 {
		registered, ok := host.PluginEmitRegistry(dp.Manifest.Name)
		if !ok {
			// Roll back the successful host.Load so the host's plugin
			// table (and any live subprocess / gRPC client for binary
			// plugins) does not leak after fail-closed rejection.
			if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
				slog.ErrorContext(ctx, "failed to rollback plugin load after PluginEmitRegistry not-found",
					"plugin", dp.Manifest.Name, "error", unloadErr)
			}
			return oops.Code("PLUGIN_EMIT_REGISTRY_UNAVAILABLE").
				In("manager").With("plugin", dp.Manifest.Name).
				Errorf("host loaded plugin but PluginEmitRegistry returned not-found")
		}
		declared := manifestDeclaredEmitTypes(dp.Manifest)
		mismatch := ValidateEmitTypeSetEquality(declared, registered)
		if mismatch.HasMismatch() {
			// Roll back the successful host.Load so the host's plugin
			// table (and any live subprocess / gRPC client for binary
			// plugins) does not leak after fail-closed rejection.
			if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
				slog.ErrorContext(ctx, "failed to rollback plugin load after INV-PLUGIN-32 mismatch",
					"plugin", dp.Manifest.Name, "error", unloadErr)
			}
			return oops.Code("EVENT_TYPE_REGISTRY_MISMATCH").
				In("manager").With("plugin", dp.Manifest.Name).
				With("declared_but_unregistered", mismatch.DeclaredButUnregistered).
				With("registered_but_undeclared", mismatch.RegisteredButUndeclared).
				Errorf("plugin crypto.emits manifest does not match registered emit-type set (INV-PLUGIN-32)")
		}
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
			slog.ErrorContext(ctx, "failed to rollback plugin load after schema validation failure",
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
				slog.ErrorContext(ctx, "failed to rollback plugin load after policy install failure",
					"plugin", dp.Manifest.Name, "error", unloadErr)
			}
			return oops.In("manager").With("plugin", dp.Manifest.Name).Wrapf(installErr, "install plugin policies")
		}
	}

	// Check for manifest warnings (non-fatal policy coverage gaps).
	if warnings := CheckManifestWarnings(dp.Manifest); len(warnings) > 0 {
		for _, w := range warnings {
			slog.InfoContext(ctx, "manifest warning", "plugin", dp.Manifest.Name, "warning", w)
		}
	}

	// Register plugin-declared verbs in the VerbRegistry.
	for _, vs := range dp.Manifest.Verbs {
		regErr := m.verbRegistry.RegisterWithSource(core.VerbRegistration{
			Type:          vs.Type,
			Category:      vs.Category,
			Format:        vs.Format,
			Label:         vs.Label,
			DisplayTarget: displayTargetFromString(vs.DisplayTarget),
			Source:        dp.Manifest.Name,
		}, dp.Manifest.Version)
		if regErr != nil {
			// Clean up any verbs already registered from this plugin.
			m.verbRegistry.UnregisterBySource(dp.Manifest.Name)
			m.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
			if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
				slog.ErrorContext(ctx, "failed to rollback plugin load after verb registration failure",
					"plugin", dp.Manifest.Name, "error", unloadErr)
			}
			return oops.In("manager").With("plugin", dp.Manifest.Name).
				With("verb", vs.Type).Wrapf(regErr, "register plugin verb")
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

	slog.InfoContext(ctx, "loaded plugin",
		"plugin", dp.Manifest.Name,
		"type", dp.Manifest.Type,
		"version", dp.Manifest.Version)

	// w9ml T6: rollback-flag commit — see deferred rollback registered after
	// the cache-mutation block above. Setting this true makes the rollback a
	// no-op on the success path.
	loadPluginCommitted = true
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
			slog.ErrorContext(ctx, "failed to rollback plugin load after schema discovery failure",
				"plugin", pluginName, "error", unloadErr)
		}
		return nil, oops.In("manager").With("plugin", pluginName).
			Wrapf(schemaErr, "schema discovery failed")
	}

	schemas := ConvertProtoSchema(schemaResp)
	for _, rt := range dp.Manifest.ResourceTypes {
		if _, ok := schemas[rt]; !ok {
			if unloadErr := host.Unload(ctx, pluginName); unloadErr != nil {
				slog.ErrorContext(ctx, "failed to rollback plugin after schema validation failure",
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
					slog.ErrorContext(ctx, "failed to rollback plugin after attribute provider registration failure",
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

// isValidStreamName returns true if name is a valid RELATIVE plugin session
// stream reference. Plugin contributions are domain-RELATIVE dot references
// (e.g. "channel.<id>"): non-empty, no whitespace, at most 256 characters, NOT
// pre-qualified with an "events." prefix, and containing no colon.
//
// HIGH-1 fixed the prior contract, which required a colon and therefore DROPPED
// the dot-relative refs core-channels returns (the colon form was eradicated by
// holomush-rops). R3-A then tightened it to RELATIVE-ONLY: a pre-qualified
// "events." subject or a colon-style ref from a plugin is rejected so an
// arbitrary session_streams plugin cannot inject a pre-qualified FOREIGN subject
// (Qualify would pass it through unscoped). The accepted relative ref is
// qualified idempotently downstream (computeInitialFilters→toSubject→Qualify).
func isValidStreamName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for _, r := range name {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return false
		}
	}
	if strings.HasPrefix(name, "events.") || strings.Contains(name, ":") {
		return false
	}
	return true
}

// QuerySessionStreams collects plugin-contributed stream names for a session.
// Only plugins with SessionStreams: true in their manifest are queried.
// Plugin errors are logged and skipped (degraded-subscribe policy).
// Invalid stream names are dropped. Duplicate streams are deduplicated.
func (m *Manager) QuerySessionStreams(ctx context.Context, req SessionStreamsRequest) []string {
	m.mu.RLock()
	type pluginEntry struct {
		name        string
		host        Host
		emitDomains []string // manifest.Emits — the owned-namespace fence input (R3-A)
	}
	var opted []pluginEntry
	for name, dp := range m.loaded {
		if dp.Manifest.SessionStreams {
			if host, ok := m.pluginHosts[name]; ok {
				// Copy the manifest's emit domains under the lock so the fence
				// reads a stable snapshot outside it.
				emits := append([]string(nil), dp.Manifest.Emits...)
				opted = append(opted, pluginEntry{name: name, host: host, emitDomains: emits})
			}
		}
	}
	m.mu.RUnlock()

	if len(opted) == 0 {
		return nil
	}

	type result struct {
		name        string
		emitDomains []string
		streams     []string
		err         error
	}
	results := make(chan result, len(opted))
	for _, p := range opted {
		p := p
		go func() {
			streams, err := p.host.QuerySessionStreams(ctx, p.name, req)
			select {
			case results <- result{name: p.name, emitDomains: p.emitDomains, streams: streams, err: err}:
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
			// R3-A establishment-path namespace fence: run the SAME
			// pluginauthz.AuthorizePluginStreamContribution the mid-session
			// stream.subscription guard uses, so the two contribution paths
			// cannot diverge. A ref outside the plugin's owned emit domains
			// (a forbidden system/audit/crypto or a foreign/cross-game domain)
			// is DROPPED + logged before it can reach the Subscribe filter plan.
			if fenceErr := pluginauthz.AuthorizePluginStreamContribution(r.name, r.emitDomains, s); fenceErr != nil {
				slog.WarnContext(ctx, "plugin stream contribution failed namespace fence — dropping",
					"plugin", r.name,
					"stream", s,
					"error", fenceErr)
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
				slog.ErrorContext(ctx, "failed to remove plugin policies", "plugin", name, "error", err)
			}
		}
	}

	// Close all registered hosts before clearing maps so that hosts can
	// still reference loaded state during shutdown.
	for hostType, host := range m.hosts {
		if err := host.Close(ctx); err != nil {
			slog.ErrorContext(ctx, "failed to close host", "type", hostType, "error", err)
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

// AuditSubjectDeclaration pairs a plugin name with one NATS subject pattern
// drawn from the plugin's manifest audit blocks. Consumers of this shape
// feed it directly into audit.NewOwnerMap to build the host OwnerMap.
type AuditSubjectDeclaration struct {
	PluginName string
	Subject    string
}

// PluginAuditClient returns the PluginAuditService client for the named
// plugin by walking every registered host and asking each to produce one.
// Returns nil, false when no host can supply a client for the plugin —
// typically because the plugin is not a binary plugin, is not loaded, or
// did not register the service. The host audit subsystem calls this to
// resolve the client for each manifest-declared audit block.
func (m *Manager) PluginAuditClient(pluginName string) (pluginv1.PluginAuditServiceClient, bool) {
	m.mu.RLock()
	host, ok := m.pluginHosts[pluginName]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	// Use the Unwrap-aware optional lookup so middleware-wrapped hosts
	// (e.g. HostMiddleware for OTel instrumentation) still surface the
	// underlying provider. Mirrors how ServiceConnProvider is discovered
	// during plugin load.
	provider := findOptional[PluginAuditClientProvider](host)
	if provider == nil {
		return nil, false
	}
	client := provider.PluginAuditClient(pluginName)
	if client == nil {
		return nil, false
	}
	return client, true
}

// AuditSubjects returns every (plugin, subject) pair declared via
// manifest.Audit[*].Subjects across all loaded plugins. Plugins without
// audit blocks contribute nothing; duplicate subjects from the same
// plugin are de-duplicated at OwnerMap construction time, not here.
func (m *Manager) AuditSubjects() []AuditSubjectDeclaration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []AuditSubjectDeclaration
	for name, dp := range m.loaded {
		if dp.Manifest == nil {
			continue
		}
		for _, block := range dp.Manifest.Audit {
			for _, subj := range block.Subjects {
				out = append(out, AuditSubjectDeclaration{
					PluginName: name,
					Subject:    subj,
				})
			}
		}
	}
	return out
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

// manifestLookup mirrors authguard.ManifestLookup structurally so a
// signature drift on either crypto gate below is caught here at compile
// time rather than at the authguard call site.
//
// It is declared locally on purpose: internal/plugin MUST NOT import
// internal/eventbus/authguard. *Manager satisfies that interface by
// structural satisfaction, which is what lets authguard sit below plugin
// with no import edge in either direction.
type manifestLookup interface {
	PluginRequestsDecryption(pluginName, eventType string) bool
	PluginCanReadBack(pluginName, eventType string) bool
}

var _ manifestLookup = (*Manager)(nil)

// PluginRequestsDecryption returns true iff the plugin named pluginName
// has a manifest declaring eventType in its
// crypto.consumes[].requests_decryption[] list. The eventType MUST be
// in the qualified <plugin>:<event_type> form per crypto_validator's
// validation rules.
//
// Read by AuthGuard, which consumes *Manager directly as its
// ManifestLookup (Phase 3b grounding doc Decision 1).
//
// A nil receiver returns false rather than panicking. This carries the
// fail-closed contract previously held by authguard's manifestAdapter: a
// typed-nil *Manager stored in a ManifestLookup interface is not
// interface-nil, so authguard.New's AUTHGUARD_DEPENDENCY_NIL check cannot
// catch it. This is a crypto authorization gate on the decrypt path — it
// must deny, not crash.
func (m *Manager) PluginRequestsDecryption(pluginName, eventType string) bool {
	if m == nil {
		return false
	}
	manifest := m.lookupManifest(pluginName)
	if manifest == nil || manifest.Crypto == nil {
		return false
	}
	for _, consume := range manifest.Crypto.Consumes {
		for _, ref := range consume.RequestsDecryption {
			if ref == eventType {
				return true
			}
		}
	}
	return false
}

// PluginCanReadBack returns true iff pluginName's manifest declares
// crypto.emits[].readback=true for eventType. Read-back authorization
// gate g2 (plugin-readback-decrypt-design §4). Distinct from
// PluginRequestsDecryption, which reads crypto.consumes.
//
// A nil receiver returns false rather than panicking, for the same
// fail-closed reason documented on PluginRequestsDecryption.
func (m *Manager) PluginCanReadBack(pluginName, eventType string) bool {
	if m == nil {
		return false
	}
	manifest := m.lookupManifest(pluginName)
	if manifest == nil || manifest.Crypto == nil {
		return false
	}
	for i := range manifest.Crypto.Emits {
		if emitEntryMatchesWireType(manifest.Name, manifest.Crypto.Emits[i].EventType, eventType) {
			return manifest.Crypto.Emits[i].Readback
		}
	}
	return false
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

// NameByID implements IdentityRegistry.
func (m *Manager) NameByID(id ulid.ULID) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	name, ok := m.nameByID[id]
	return name, ok
}

// IDByName implements IdentityRegistry.
func (m *Manager) IDByName(name string) (ulid.ULID, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.activeByName[name]
	return id, ok
}

// displayTargetFromString converts a manifest display_target string to the
// proto enum. Returns EVENT_CHANNEL_UNSPECIFIED for unknown values (validation
// should catch these before this is called).
func displayTargetFromString(s string) corev1.EventChannel {
	switch strings.ToLower(s) {
	case "terminal":
		return corev1.EventChannel_EVENT_CHANNEL_TERMINAL
	case "state":
		return corev1.EventChannel_EVENT_CHANNEL_STATE
	case "both":
		return corev1.EventChannel_EVENT_CHANNEL_BOTH
	default:
		return corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED
	}
}

// manifestDeclaredEmitTypes extracts the event-type strings from
// manifest.Crypto.Emits for INV-PLUGIN-32 set-equality validation. Returns nil
// when manifest.Crypto is nil.
func manifestDeclaredEmitTypes(m *Manifest) []string {
	if m.Crypto == nil {
		return nil
	}
	out := make([]string, 0, len(m.Crypto.Emits))
	for _, e := range m.Crypto.Emits {
		out = append(out, e.EventType)
	}
	return out
}
