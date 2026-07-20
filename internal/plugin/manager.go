// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"sort"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/focuscontract"
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

// managerConfig is the construction-time holder every ManagerOption writes
// into. It exists because ManagerOption is `func(*Manager)` (frozen by D-07),
// so each option needs somewhere on the Manager to record its value between
// the option loop and the point NewManager builds the three units.
//
// Nothing reads it after NewManager returns. The embedded LoaderConfig is
// handed straight to NewPluginLoader; pluginRepo/retentionDays feed the
// IdentityStore.
type managerConfig struct {
	LoaderConfig

	pluginRepo       store.PluginRepo
	retentionDays    int  // plugin row TTL (days); 0 = sweep disabled; default 3
	retentionDaysSet bool // true iff WithRetentionDays was called explicitly
}

// Manager is the facade over the three units ARCH-02 decomposed the former god
// object into. It holds no plugin state of its own: every exported method is a
// one-line forward into the unit that owns the state it touches.
//
//   - loader   — discovery, ordering, host wiring, load orchestration, teardown
//   - runtime  — the loaded-plugin registry, delivery and read-side lookups
//   - identity — the plugin name ↔ ULID registry and its persistence
//
// Each unit carries its OWN lock. No code path may hold two of the three at
// once; see PluginLoader's LOCK DISCIPLINE note for the invariant and the
// hoisting comments that maintain it.
//
// The type, NewManager's signature, the exported method set, and all eleven
// ManagerOption signatures are frozen (D-07): internal/plugin/setup,
// cmd/holomush and the integration harness compile against them unchanged.
type Manager struct {
	loader   *PluginLoader
	runtime  *PluginRuntime
	identity *IdentityStore

	// cfg is option plumbing, read exactly once — by NewManager, while
	// constructing the three units above. It is not consulted afterwards.
	cfg managerConfig
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithLuaHost sets the Lua host for the manager.
func WithLuaHost(h Host) ManagerOption {
	return func(m *Manager) {
		m.cfg.LuaHost = h
	}
}

// WithPolicyInstaller sets the policy installer for plugin ABAC policies.
func WithPolicyInstaller(pi PluginPolicyInstaller) ManagerOption {
	return func(m *Manager) {
		m.cfg.PolicyInstaller = pi
	}
}

// WithAliasSeeder configures alias seeding from plugin manifests during LoadAll.
func WithAliasSeeder(seeder AliasSeeder, cache *command.AliasCache) ManagerOption {
	return func(m *Manager) {
		m.cfg.AliasSeeder = seeder
		m.cfg.AliasCache = cache
	}
}

// WithAttributeProviderRegistrar sets a callback used to register plugin
// attribute providers with the ABAC resolver during plugin load.
func WithAttributeProviderRegistrar(fn RegisterPluginProviderFunc) ManagerOption {
	return func(m *Manager) {
		m.cfg.RegisterProvider = fn
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
		m.cfg.UnregisterProvider = fn
	}
}

// WithServiceRegistry configures the manager to use DAG-based dependency
// resolution via the provided service registry.
func WithServiceRegistry(reg *ServiceRegistry) ManagerOption {
	return func(m *Manager) {
		m.cfg.Registry = reg
	}
}

// WithTrustAllowlist sets the server-side allowlist of plugin names permitted
// to use trust escalation. A plugin's manifest trust.all_principals declaration
// only takes effect when the plugin name appears in this allowlist.
func WithTrustAllowlist(names []string) ManagerOption {
	return func(m *Manager) {
		m.cfg.TrustAllowlist = make(map[string]bool, len(names))
		for _, n := range names {
			m.cfg.TrustAllowlist[n] = true
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
		m.cfg.GracefulDegradation = true
	}
}

// WithVerbRegistry sets the VerbRegistry for plugin verb registration.
func WithVerbRegistry(reg *core.VerbRegistry) ManagerOption {
	return func(m *Manager) {
		m.cfg.VerbRegistry = reg
	}
}

// WithPluginRepo wires the IdentityRegistry's persistence layer.
// Required when the Manager will Upsert plugin rows. Without it,
// loadPlugin operates with an in-memory-only registry (test seam).
func WithPluginRepo(repo store.PluginRepo) ManagerOption {
	return func(m *Manager) { m.cfg.pluginRepo = repo }
}

// WithRetentionDays configures plugin row TTL (days). After RetentionDays
// of inactivity, a plugin row is deactivated (gc_at set) at the end of
// LoadAll. 0 disables the sweep entirely. Default: 3.
func WithRetentionDays(days int) ManagerOption {
	return func(m *Manager) {
		m.cfg.retentionDays = days
		m.cfg.retentionDaysSet = true
	}
}

// Registry returns the service registry, or nil if not configured.
func (m *Manager) Registry() *ServiceRegistry {
	return m.loader.Registry()
}

// NewManager creates a plugin manager.
//
// INV-EVENTBUS-11: callers MUST supply a non-nil VerbRegistry via
// WithVerbRegistry. Construction returns ErrMissingVerbRegistry when the
// option is omitted so plugin-declared verbs always have a place to land.
func NewManager(pluginsDir string, opts ...ManagerOption) (*Manager, error) {
	m := &Manager{
		cfg: managerConfig{
			LoaderConfig: LoaderConfig{
				PluginsDir: pluginsDir,
				CapVocab:   DefaultCapabilityVocabulary(),
			},
		},
		// The runtime is constructed BEFORE the option loop, unlike the
		// identity store and the loader below: no ManagerOption feeds it, and
		// WithLuaHost's capability caching (below) needs it already present. It
		// owns its own maps, so there is nothing for an option to configure.
		runtime: NewPluginRuntime(),
	}
	for _, opt := range opts {
		opt(m)
	}
	// Default retentionDays to 3 when WithRetentionDays was not called.
	if !m.cfg.retentionDaysSet {
		m.cfg.retentionDays = 3
	}
	// If WithLuaHost was used, cache its capabilities for the same lookup path.
	if m.cfg.LuaHost != nil {
		m.runtime.CacheHostCapabilities(m.cfg.LuaHost)
	}

	// Construct the identity registry AFTER the option loop, so WithPluginRepo
	// and WithRetentionDays have already recorded their values and the
	// retentionDays default above has been applied. Construction order within
	// NewManager is fixed and deterministic: the runtime unit, then options,
	// then defaults, then host-capability caching, then the identity store,
	// then the VerbRegistry guard, then the loader.
	m.identity = NewIdentityStore(m.cfg.pluginRepo, m.cfg.retentionDays)
	if err := m.identity.Bootstrap(context.Background()); err != nil {
		return nil, err
	}

	if m.cfg.VerbRegistry == nil {
		return nil, ErrMissingVerbRegistry
	}

	// The loader is built last: it orchestrates the other two units, so both
	// must exist first. It is built only on the success path, after the
	// VerbRegistry guard, so a rejected construction never produces a
	// half-wired loader.
	m.loader = NewPluginLoader(m.cfg.LoaderConfig, m.runtime, m.identity)
	return m, nil
}

// RegisterHost registers a host implementation for a plugin type.
// Must be called before LoadAll. Panics if host is nil.
func (m *Manager) RegisterHost(hostType Type, host Host) {
	m.loader.RegisterHost(hostType, host)
}

// DeliverCommand routes a command to the correct host for the named plugin.
func (m *Manager) DeliverCommand(ctx context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return m.runtime.DeliverCommand(ctx, pluginName, cmd)
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
	return m.runtime.BeginServiceDispatch(ctx, pluginName, actor, ownerPlayerID)
}

// ConfigureEventEmitter wires the shared plugin event emitter to the provided
// EventBus publisher. Production startup MUST call this before plugin response
// events are routed through the manager.
func (m *Manager) ConfigureEventEmitter(publisher eventbus.Publisher, opts ...EmitterOption) {
	m.loader.ConfigureEventEmitter(publisher, opts...)
}

// ConfigureFocusDeps injects the focus coordinator and history reader into all
// registered hosts. Production startup MUST call this before plugins handle
// focus-related RPCs or host functions.
func (m *Manager) ConfigureFocusDeps(fc focuscontract.Coordinator, hr HistoryReader) {
	m.loader.ConfigureFocusDeps(fc, hr)
}

// ConfigureReadbackDecryptor injects the read-back decryptor into all
// registered hosts that implement ReadbackDepsConfigurer. Production startup
// MUST call this before plugins issue DecryptOwnAuditRows RPCs.
func (m *Manager) ConfigureReadbackDecryptor(d ReadbackDecryptor) {
	m.loader.ConfigureReadbackDecryptor(d)
}

// ConfigureSettingsDeps injects the plugin-partitioned settings stores into all
// registered hosts that implement SettingsDepsConfigurer. Production startup
// MUST call this before plugins issue GetSetting / SetSetting RPCs (or the Lua
// equivalents).
func (m *Manager) ConfigureSettingsDeps(
	player settings.PlayerSettingsStore,
	character settings.CharacterSettingsStore,
	game settings.GameSettings,
) {
	m.loader.ConfigureSettingsDeps(player, character, game)
}

// DeliverEvent routes an event to the correct host for the named plugin.
func (m *Manager) DeliverEvent(ctx context.Context, pluginName string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return m.runtime.DeliverEvent(ctx, pluginName, event)
}

// EmitPluginEvent routes a plugin-owned emit request through the shared host
// emitter so manifests are validated and host-owned event fields are stamped
// consistently across command and subscriber paths.
func (m *Manager) EmitPluginEvent(ctx context.Context, pluginName string, event pluginsdk.EmitEvent) error {
	return m.runtime.EmitPluginEvent(ctx, pluginName, event)
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
	return m.loader.Discover(ctx)
}

// LoadAll discovers and loads all plugins in the plugins directory.
//
// Strict by default: if any plugin fails to load, LoadAll attempts all
// remaining plugins, then returns a joined error describing every failure.
// Use WithGracefulDegradation() to opt into logging them instead.
func (m *Manager) LoadAll(ctx context.Context) error {
	return m.loader.LoadAll(ctx)
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
// registration time; results are cached on the runtime unit.
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
	return m.loader.BuildFocusRedirects(registry)
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

// prioritySort orders plugins by load priority (lower values load first). It is
// the no-registry fallback path for resolveLoadOrder.
func prioritySort(discovered []*DiscoveredPlugin) []*DiscoveredPlugin {
	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].Manifest.EffectivePriority() < discovered[j].Manifest.EffectivePriority()
	})
	return discovered
}

// ListPlugins returns names of all loaded plugins.
func (m *Manager) ListPlugins() []string {
	return m.runtime.ListPlugins()
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
	return m.runtime.QuerySessionStreams(ctx, req)
}

// Close shuts down the manager and all loaded plugins.
func (m *Manager) Close(ctx context.Context) error {
	return m.loader.Close(ctx)
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
	return m.runtime.PluginAuditClient(pluginName)
}

// AuditSubjects returns every (plugin, subject) pair declared via
// manifest.Audit[*].Subjects across all loaded plugins. Plugins without
// audit blocks contribute nothing; duplicate subjects from the same
// plugin are de-duplicated at OwnerMap construction time, not here.
func (m *Manager) AuditSubjects() []AuditSubjectDeclaration {
	return m.runtime.AuditSubjects()
}

// IsPluginLoaded returns true if the named plugin is currently loaded.
// Implements attribute.PluginRegistry for ABAC attribute resolution.
func (m *Manager) IsPluginLoaded(name string) bool {
	return m.runtime.IsPluginLoaded(name)
}

// GetLoadedPlugin returns the discovered plugin info for the named plugin.
// Returns nil and false if the plugin is not loaded.
func (m *Manager) GetLoadedPlugin(name string) (*DiscoveredPlugin, bool) {
	return m.runtime.GetLoadedPlugin(name)
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
	return m.runtime.PluginRequestsDecryption(pluginName, eventType)
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
	return m.runtime.PluginCanReadBack(pluginName, eventType)
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
	m.runtime.RegisterPluginCommands(registry)
}

// NameByID implements IdentityRegistry.
func (m *Manager) NameByID(id ulid.ULID) (string, bool) {
	return m.identity.NameByID(id)
}

// IDByName implements IdentityRegistry.
func (m *Manager) IDByName(name string) (ulid.ULID, bool) {
	return m.identity.IDByName(name)
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
