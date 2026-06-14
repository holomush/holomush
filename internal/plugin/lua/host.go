// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/luabridge"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/settings"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Compile-time interface checks.
var (
	_ plugins.Host                   = (*Host)(nil)
	_ plugins.FocusDepsConfigurer    = (*Host)(nil)
	_ plugins.ReadbackDepsConfigurer = (*Host)(nil)
	_ plugins.SettingsDepsConfigurer = (*Host)(nil)
	_ plugins.PluginGrantsConfigurer = (*Host)(nil)
)

// luaPlugin holds compiled Lua code for a plugins.
type luaPlugin struct {
	manifest     *plugins.Manifest
	code         string          // Lua source (compiled at load time in future)
	emitRegistry []string        // INV-PLUGIN-32: populated during Load capture pass; nil when crypto.emits empty
	endpoint     *pluginEndpoint // per-plugin bufconn endpoint serving host.v1 LuaDefaultSet; nil when hostFuncs is nil
}

// Host manages Lua plugins.
type Host struct {
	factory              *StateFactory
	hostFuncs            *hostfunc.Functions
	hostCapAdapter       hostcap.HostCapabilities // adapter wrapping hostFuncs; non-nil iff hostFuncs is non-nil
	plugins              map[string]*luaPlugin
	mu                   sync.RWMutex
	closed               bool
	cpuTimeout           time.Duration // per-invocation deadline applied via context.WithTimeout
	configOverrides      map[string]map[string]string
	mergedConfigs        map[string]map[string]string
	bridgeEnabledPlugins map[string]bool // opt-in allowlist for host-cap bridge injection; empty = production (no bridge)
	// dispatchAttrResolver resolves the acting character's host-vouched dispatch
	// attributes (notably "location") at delivery time, populating
	// DispatchContext.Attributes (holomush-eykuh.3). Nil leaves Attributes nil,
	// fail-closed at scope-enforcement time. Mirrors goplugin.Host's field
	// (plugin-runtime-symmetry). Wired at construction via
	// WithDispatchAttributeResolver.
	dispatchAttrResolver pluginauthz.AttributeResolver
	// pluginGrants is the per-plugin least-privilege grant set from the
	// resolver (holomush-eykuh.4.7). A nil map means "not set" → fall back
	// to manifest.RequiredCapabilities() at delivery time (no-registry path,
	// existing tests). A non-nil map is authoritative: only tokens present
	// in pluginGrants[name] are passed to RegisterHostCaps.
	pluginGrants map[string][]string
	// sessionAdmin is the broadcast/disconnect backing threaded into the
	// hostCapAdapter so the brokered SessionAdminService serves real broadcasts.
	// Wired at construction (WithSessionAdmin) or late (SetSessionAdmin);
	// production wires it late, after the event appender exists
	// (holomush-eykuh.4.2). nil ⇒ the sessionAdminServer fails closed.
	sessionAdmin hostcap.SessionAdmin
}

// HostOption customizes Host construction.
type HostOption func(*Host)

// WithCPUTimeout sets the per-invocation deadline applied to every
// CallByParam dispatched through Host.invoke. Zero disables the cap
// (unchanged context inheritance). Recommend the caller pass a positive
// duration in production; zero is allowed only for tests.
func WithCPUTimeout(d time.Duration) HostOption {
	return func(h *Host) { h.cpuTimeout = d }
}

// WithStateFactory replaces the default StateFactory. Used by callers
// that need a factory with non-default options (e.g. RegistryMaxSize).
func WithStateFactory(f *StateFactory) HostOption {
	return func(h *Host) { h.factory = f }
}

// WithHostCapBridge opts the named plugins into the host-capability bridge path.
// Only plugins in this allowlist receive luabridge.RegisterHostCaps injection in
// DeliverEvent; all other plugins continue using the legacy hostfunc shim path
// unchanged. The default (empty allowlist) means NO plugin uses the bridge,
// keeping production behaviour identical.
//
// This is intentionally test-fixture-only for this sub-spec (Phase 2 T9). Full
// production migration is tracked in sub-spec 5 / holomush-eykuh.4.
func WithHostCapBridge(pluginNames ...string) HostOption {
	return func(h *Host) {
		if h.bridgeEnabledPlugins == nil {
			h.bridgeEnabledPlugins = make(map[string]bool, len(pluginNames))
		}
		for _, name := range pluginNames {
			h.bridgeEnabledPlugins[name] = true
		}
	}
}

// WithPluginConfigOverrides threads the server-provided per-plugin config
// overrides into the Lua host (mirrors the binary host's configOverrides).
// The overrides are merged against each plugin's manifest defaults at Load
// time via plugins.MergePluginConfig and stashed in h.mergedConfigs, which
// is then injected into the hostfunc bridge before each per-delivery Register.
func WithPluginConfigOverrides(o map[string]map[string]string) HostOption {
	// Defensively deep-copy: the caller retains ownership of o, and a later
	// mutation must not race with Load reading h.configOverrides.
	cloned := make(map[string]map[string]string, len(o))
	for name, cfg := range o {
		cloned[name] = maps.Clone(cfg)
	}
	return func(h *Host) { h.configOverrides = cloned }
}

// WithDispatchAttributeResolver configures the host with the resolver used to
// populate DispatchContext.Attributes (notably "location") at command/event
// delivery (holomush-eykuh.3). Without it, dispatch Attributes stay nil —
// fail-closed at scope-enforcement time. The binary host has the same option
// (plugin-runtime-symmetry). Satisfied by access/policy/attribute.Resolver.
func WithDispatchAttributeResolver(r pluginauthz.AttributeResolver) HostOption {
	return func(h *Host) { h.dispatchAttrResolver = r }
}

// WithPluginGrants threads the resolver's per-plugin grant set into the host.
// When set (non-nil), it is the single least-privilege authority for what
// capability tokens are injected into Lua at delivery time via RegisterHostCaps
// — only tokens present in grants[pluginName] are passed to RegisterHostCaps.
//
// A nil map (the default) means the host falls back to
// manifest.RequiredCapabilities() at delivery time, preserving backward
// compatibility for the no-registry path and existing tests.
//
// The binary host has a symmetric option (plugin-runtime-symmetry).
func WithPluginGrants(grants map[string][]string) HostOption {
	return func(h *Host) { h.pluginGrants = plugins.CloneGrants(grants) }
}

// SetPluginGrants implements plugins.PluginGrantsConfigurer. The Manager calls
// this before starting plugin loads when a resolver is configured, so that
// every subsequent delivery uses the grant set rather than the manifest.
// A nil grants map restores the nil state (manifest fallback).
func (h *Host) SetPluginGrants(grants map[string][]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pluginGrants = plugins.CloneGrants(grants)
}

// grantedSubset returns the elements of requested that appear in the granted
// set. When granted is nil or empty the result is nil (no caps injected on
// an explicitly empty grant). This helper is used by both delivery paths that
// call RegisterHostCaps so the grant filter is applied in one place.
func grantedSubset(requested, granted []string) []string {
	if len(granted) == 0 {
		return nil
	}
	grantSet := make(map[string]bool, len(granted))
	for _, g := range granted {
		grantSet[g] = true
	}
	out := make([]string, 0, len(requested))
	for _, r := range requested {
		if grantSet[r] {
			out = append(out, r)
		}
	}
	return out
}

// WithSessionAdmin configures the host with the broadcast/disconnect backing for
// the brokered SessionAdminService (holomush-eykuh.4.2). Production wires the
// backing late via SetSessionAdmin (the event appender does not exist at host
// construction); this option is the construction-time equivalent for tests and
// in-process wiring. nil leaves the sessionAdminServer fail-closed.
func WithSessionAdmin(sa hostcap.SessionAdmin) HostOption {
	return func(h *Host) { h.sessionAdmin = sa }
}

// NewHost creates a new Lua plugin host without host functions.
func NewHost(opts ...HostOption) *Host {
	h := &Host{
		factory: NewStateFactory(),
		plugins: make(map[string]*luaPlugin),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// NewHostWithFunctions creates a Lua plugin host with host functions.
// The host functions enable plugins to call holomush.* APIs like log(), new_request_id(), and kv_*.
// Also constructs a hostcap.HostCapabilities adapter over hf so per-plugin bufconn
// endpoints (created at Load time) serve the host.v1 LuaDefaultSet capability servers
// through the same Functions backing (INV-PLUGIN-49).
// Panics if hf is nil (consistent with hostfunc.New).
func NewHostWithFunctions(hf *hostfunc.Functions, opts ...HostOption) *Host {
	if hf == nil {
		panic("lua.NewHostWithFunctions: hostFuncs cannot be nil")
	}
	h := &Host{
		factory:   NewStateFactory(),
		hostFuncs: hf,
		plugins:   make(map[string]*luaPlugin),
	}
	for _, opt := range opts {
		opt(h)
	}
	// Build the adapter AFTER options apply so WithSessionAdmin (and any future
	// adapter-affecting option) is reflected in the per-plugin capability servers.
	h.hostCapAdapter = newLuaHostCapAdapterWithSessionAdmin(hf, h.sessionAdmin)
	return h
}

// HostCapabilitiesAdapter exposes the Lua runtime's hostcap.HostCapabilities
// adapter (the *hostfunc.Functions-backed identity/settings/world surface the
// per-plugin bufconn endpoint serves). It is the SAME adapter the production
// LuaDefaultSet endpoint consumes; returning it lets the cross-runtime parity
// test (test/integration/pluginparity, INV-PLUGIN-49) drive the real Lua
// runtime adapter against the same hostcap servers the binary runtime uses,
// rather than a stand-in. Returns nil when the host was built without host
// functions (NewHost rather than NewHostWithFunctions). Intended for tests and
// in-process wiring; production paths reach the adapter via the endpoint.
func (h *Host) HostCapabilitiesAdapter() hostcap.HostCapabilities {
	return h.hostCapAdapter
}

// SetFocusCoordinator injects the focus coordinator into the underlying
// hostfunc bridge via a coordinatorFocusOpsAdapter that satisfies hostfunc.FocusOps.
// Phase 5 methods (SetConnectionFocus, AutoFocusOnJoin, IsAnyConnFocused) are
// stubbed here until T14-T16 add real implementations to the Coordinator.
//
// A nil fc clears the FocusOps binding rather than wrapping nil — every
// adapter method would otherwise NPE on its first call.
func (h *Host) SetFocusCoordinator(fc focus.Coordinator) {
	if h.hostFuncs == nil {
		return
	}
	if fc == nil {
		h.hostFuncs.SetFocusOps(nil)
		return
	}
	h.hostFuncs.SetFocusOps(&coordinatorFocusOpsAdapter{c: fc})
}

// SetSettingsStores injects the plugin-partitioned settings stores into the
// underlying hostfunc bridge via a settingsStoresOpsAdapter that satisfies
// hostfunc.SettingsOps, so the Lua get_setting / set_setting hostfuncs reach the
// SAME stores the binary GetSetting / SetSetting RPCs use (plugin-runtime-
// symmetry, INV-PLUGIN-27). Implements plugins.SettingsDepsConfigurer; invoked by
// Manager.ConfigureSettingsDeps via findOptional during gRPC subsystem Start.
//
// If any store is nil the binding is cleared rather than wrapping a partial set
// of stores — a half-wired adapter would nil-deref inside scopedFor's For()
// calls. Clearing makes the affected scopes fail closed at the hostfunc layer.
func (h *Host) SetSettingsStores(
	player settings.PlayerSettingsStore,
	character settings.CharacterSettingsStore,
	game settings.GameSettings,
) {
	if h.hostFuncs == nil {
		return
	}
	if player == nil || character == nil || game == nil {
		h.hostFuncs.SetSettingsOps(nil)
		return
	}
	h.hostFuncs.SetSettingsOps(&settingsStoresOpsAdapter{
		player:    player,
		character: character,
		game:      game,
	})
}

// SetHistoryReader injects the history reader into the underlying hostfunc bridge.
func (h *Host) SetHistoryReader(hr plugins.HistoryReader) {
	if h.hostFuncs != nil {
		h.hostFuncs.SetHistoryReader(hr)
	}
}

// SetSessionAdmin injects the broadcast/disconnect backing into the host-cap
// adapter so the brokered SessionAdminService serves real broadcasts
// (holomush-eykuh.4.2). Called once during startup wiring, before any plugin
// dispatch — the event appender the backing wraps is not available until the
// EventBus subsystem starts (after the plugin subsystem builds this host), so a
// construction-time option cannot reach it. Same startup-ordered late-binding
// contract as SetHistoryReader / SetEventEmitter. No-op when the host was built
// without host functions (NewHost path — no capability adapter).
func (h *Host) SetSessionAdmin(sa hostcap.SessionAdmin) {
	h.sessionAdmin = sa
	if a, ok := h.hostCapAdapter.(*luaHostCapAdapter); ok {
		a.setSessionAdmin(sa)
	}
}

// SetReadbackDecryptor injects the read-back decryptor into the hostfunc bridge,
// adapting the per-row plugins.ReadbackDecryptor to the batch-oriented
// hostfunc.AuditDecryptor so Lua plugins can call decrypt_own_audit_rows.
func (h *Host) SetReadbackDecryptor(d plugins.ReadbackDecryptor) {
	if h.hostFuncs == nil {
		return
	}
	if d == nil {
		h.hostFuncs.SetAuditDecryptor(nil)
		return
	}
	h.hostFuncs.SetAuditDecryptor(&readbackDecryptorAdapter{d: d})
}

// Load reads and validates a Lua plugins.
func (h *Host) Load(ctx context.Context, manifest *plugins.Manifest, dir string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").New("host is closed")
	}

	entryPath := filepath.Join(dir, manifest.LuaPlugin.Entry)

	// Verify resolved path is within the plugin directory (prevent path traversal).
	// Use EvalSymlinks to resolve symlinks and prevent symlink-based escapes.
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").With("dir", dir).Hint("cannot resolve plugin directory").Wrap(err)
	}
	realEntry, err := filepath.EvalSymlinks(entryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").With("path", entryPath).Hint("plugin entry file not found").Wrap(err)
		}
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").With("path", entryPath).Hint("cannot resolve entry path").Wrap(err)
	}
	// Use filepath.Rel for robust cross-platform path containment check.
	rel, err := filepath.Rel(realDir, realEntry)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").With("entry", manifest.LuaPlugin.Entry).New("plugin entry path escapes plugin directory")
	}

	// Use realEntry (resolved symlink) for ReadFile to prevent TOCTOU attacks.
	code, err := os.ReadFile(realEntry)
	if err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").With("path", realEntry).Hint("failed to read entry file").Wrap(err)
	}

	// Branch the Load pass on whether INV-PLUGIN-32 capture is needed.
	//
	// Plugins WITHOUT non-empty crypto.emits: existing syntax-check
	// throwaway state (no hostfuncs). Unchanged from today.
	//
	// Plugins WITH non-empty crypto.emits: capture-and-validate pass
	// (hostfuncs registered including register_emit_type). The captured
	// registry is stored on luaPlugin for the validator
	// (manager.go::loadPlugin reads via Host.PluginEmitRegistry).
	var emitRegistry []string
	L, err := h.factory.NewState(ctx)
	if err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").Hint("failed to create validation state").Wrap(err)
	}
	defer L.Close()

	if manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0 {
		// INV-PLUGIN-32 capture pass. Install ONLY register_emit_type so the
		// pass is side-effect-isolated: top-level plugin code can register
		// emit types but cannot call kv_set, create_location, or any
		// other holomush.* hostfunc. Exposing the full surface here
		// would persist substrate mutations even if validation later
		// rejects the plugin (host.Unload rolls back manager-level
		// plugin state but not substrate KV/world side effects).
		// Per-delivery state still gets the full hostfunc surface via
		// Functions.Register.
		reg := hostfunc.NewLuaEmitRegistry()
		mod := L.NewTable()
		hostfunc.RegisterEmitTypeFuncs(L, mod, reg)
		L.SetGlobal("holomush", mod)
		err = L.DoString(string(code))
		if err != nil {
			return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").
				With("entry", manifest.LuaPlugin.Entry).
				Hint("INV-PLUGIN-32 capture pass execution error").Wrap(err)
		}
		emitRegistry = reg.Types()
	} else {
		// Existing syntax-check pass — no hostfuncs registered.
		err = L.DoString(string(code))
		if err != nil {
			return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").
				With("entry", manifest.LuaPlugin.Entry).
				Hint("syntax error").Wrap(err)
		}
	}

	// INV-PLUGIN-3: compute and stash the merged config for this plugin so every
	// per-delivery Register call injects an identical map to what the binary
	// host delivers via ServiceConfig.PluginConfig. Fail-loud on error
	// (same posture as goplugin/host.go:Load). Plugins with no config schema
	// produce an empty map; the nil override case is handled by MergePluginConfig.
	if len(manifest.Config) > 0 {
		merged, mergeErr := plugins.MergePluginConfig(manifest.Config, h.configOverrides[manifest.Name])
		if mergeErr != nil {
			return oops.In("lua").With("plugin", manifest.Name).With("operation", "merge_plugin_config").Wrap(mergeErr)
		}
		if h.mergedConfigs == nil {
			h.mergedConfigs = map[string]map[string]string{}
		}
		h.mergedConfigs[manifest.Name] = merged
	} else if h.mergedConfigs != nil {
		// A reload that drops the manifest config block must clear any stale
		// merged entry, else old values get injected on later deliveries.
		delete(h.mergedConfigs, manifest.Name)
	}

	// Create the per-plugin bufconn endpoint once per plugin (not per VM delivery).
	// The endpoint serves the host.v1 LuaDefaultSet capability servers over an
	// in-process connection that the bridge will capture for gRPC stub calls.
	// When hostFuncs is nil (NewHost path — no capabilities configured) the
	// adapter is also nil, so we skip endpoint creation; the plugin runs without
	// the host-brokered capability path (safe: no bridge will wire the nil conn).
	var ep *pluginEndpoint
	if h.hostCapAdapter != nil {
		ep, err = newPluginEndpoint(h.hostCapAdapter, manifest)
		if err != nil {
			return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").
				Hint("failed to create bufconn endpoint").Wrap(err)
		}
	}

	// Close any existing endpoint for this plugin name before overwriting the map
	// entry. This handles reloads: a prior Load may have created a running
	// *grpc.Server + bufconn listener (goroutine + fd) that must be stopped before
	// the new endpoint takes over. We close here — after the new endpoint is
	// successfully created — so a Load that fails partway does NOT tear down a
	// still-good endpoint from the previous load.
	if existing, ok := h.plugins[manifest.Name]; ok && existing.endpoint != nil {
		_ = existing.endpoint.Close() //nolint:errcheck // superseded on reload; best-effort cleanup
	}
	h.plugins[manifest.Name] = &luaPlugin{
		manifest:     manifest,
		code:         string(code),
		emitRegistry: emitRegistry,
		endpoint:     ep,
	}

	return nil
}

// Unload removes a plugins.
func (h *Host) Unload(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	p, ok := h.plugins[name]
	if !ok {
		return oops.In("lua").With("plugin", name).With("operation", "unload").New("plugin not loaded")
	}
	if p.endpoint != nil {
		// Close the endpoint before deleting the map entry. The entry is deleted
		// in the same locked section, so this is the only Close call for this
		// endpoint — no risk of double-close.
		_ = p.endpoint.Close() //nolint:errcheck // best-effort cleanup; plugin is being removed regardless
	}
	delete(h.plugins, name)
	return nil
}

// DeliverEvent executes the plugin's event handler.
// For command events, it calls on_command(ctx) if defined, falling back to on_event(event).
// For non-command events, it calls on_event(event).
//
// Partial Success Behavior: If the plugin returns emit events with validation errors (e.g.,
// missing required fields), those specific events are skipped and logged as warnings, but
// valid events are still returned. This ensures plugin bugs don't break game uptime while
// still providing visibility into issues via logs. The returned error is only non-nil for
// critical failures (plugin not found, Lua execution errors), not for emit validation issues.
func (h *Host) DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.mu.RLock()
	p, ok := h.plugins[name]
	if !ok {
		h.mu.RUnlock()
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_event").New("plugin not loaded")
	}
	code := p.code
	requires := p.manifest.RequiredServiceNames()
	// Snapshot capability tokens and the endpoint under the read lock alongside
	// the other snapshots. Both p.endpoint and the manifest slice are
	// written only under h.mu.Lock in Load/Unload, so reading them here under
	// RLock is safe. We snapshot rather than keep p alive to avoid holding
	// the lock during the (potentially slow) hostFuncs.Register call.
	declaredCaps := p.manifest.RequiredCapabilities()
	if h.pluginGrants != nil {
		// pluginGrants is set (resolver path) — filter to the granted subset.
		declaredCaps = grantedSubset(declaredCaps, h.pluginGrants[name])
	}
	endpoint := p.endpoint // nil when hostFuncs is nil (NewHost path)
	// Snapshot the merged config under the read lock: Load mutates
	// h.mergedConfigs under h.mu, so reading it unlocked below races
	// (concurrent map read/write panic). Shallow clone suffices — Load
	// replaces inner maps wholesale, never mutating one in place.
	cfgSnapshot := maps.Clone(h.mergedConfigs)
	bridgeEnabled := h.bridgeEnabledPlugins[name] // false when map is nil or plugin not in set
	h.mu.RUnlock()

	// Create fresh state for this event
	L, err := h.factory.NewState(ctx)
	if err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_event").Hint("failed to create state").Wrap(err)
	}
	defer L.Close()

	// Stamp the host-vouched dispatch subject before the Lua state's context is
	// set, so in-VM hostfuncs inherit it (INV-PLUGIN-51). Only character actors
	// are vouched; see stampDispatch.
	ctx = h.stampDispatch(ctx)

	// Set context on the Lua state so host functions can inherit it
	L.SetContext(ctx)

	// Register host functions if available
	if h.hostFuncs != nil {
		h.hostFuncs.SetPluginConfigs(cfgSnapshot)
		h.hostFuncs.Register(L, name, requires...)
	}

	// For plugins that have opted into the host-cap bridge path, inject the
	// generated capability tables after the legacy hostfunc shim has run.
	// The bridge's no-clobber logic (RegisterHostCaps) prevents double-injection
	// for any global the legacy shim already set (spec §5).
	//
	// Guard: endpoint is nil when hostFuncs is nil (NewHost path — no capability
	// infrastructure configured); the bridge requires a real conn.
	if bridgeEnabled && endpoint != nil {
		luabridge.RegisterHostCaps(L, endpoint.Conn(), name, declaredCaps)
	}

	// Load plugin code
	if err := L.DoString(code); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_event").Hint("failed to load code").Wrap(err)
	}

	// Check if on_event exists
	onEvent := L.GetGlobal("on_event")
	if onEvent.Type() == lua.LTNil {
		slog.DebugContext(ctx, "plugin has no handler defined",
			"plugin", name,
			"event_type", event.Type)
		return nil, nil
	}

	// Build event table
	eventTable := h.buildEventTable(L, event)

	// Call on_event(event) via invoke for CPU-deadline + watchdog protection.
	if err := h.invoke(ctx, L, name, "on_event", lua.P{
		Fn:      onEvent,
		NRet:    1,
		Protect: true,
	}, eventTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_event").Wrap(err)
	}

	// Get return value
	ret := L.Get(-1)
	L.Pop(1)

	emits, validationErrs := h.parseEmitEvents(ret)
	if len(validationErrs) > 0 {
		slog.WarnContext(ctx, "plugin emit validation errors",
			"plugin", name,
			"error_count", len(validationErrs),
			"errors", validationErrs)
	}
	return emits, nil
}

// DeliverCommand executes the plugin's on_command handler with a CommandRequest.
// Returns a CommandResponse with status, output, and optional emit events.
// If on_command is not defined, returns OK with no output.
func (h *Host) DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_command").New("host is closed")
	}
	p, ok := h.plugins[name]
	if !ok {
		h.mu.RUnlock()
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_command").New("plugin not loaded")
	}
	code := p.code
	requires := p.manifest.RequiredServiceNames()
	// Snapshot the bridge inputs alongside the other per-delivery snapshots so
	// on_command handlers receive the same host-cap bridge globals as on_event
	// (intra-Lua entrypoint parity). Both p.endpoint and the manifest slice are
	// written only under h.mu.Lock in Load/Unload.
	declaredCaps := p.manifest.RequiredCapabilities()
	if h.pluginGrants != nil {
		// pluginGrants is set (resolver path) — filter to the granted subset.
		declaredCaps = grantedSubset(declaredCaps, h.pluginGrants[name])
	}
	endpoint := p.endpoint // nil when hostFuncs is nil (NewHost path)
	// Snapshot the merged config under the read lock: Load mutates
	// h.mergedConfigs under h.mu, so reading it unlocked below races
	// (concurrent map read/write panic). Shallow clone suffices — Load
	// replaces inner maps wholesale, never mutating one in place.
	cfgSnapshot := maps.Clone(h.mergedConfigs)
	bridgeEnabled := h.bridgeEnabledPlugins[name] // false when map is nil or plugin not in set
	h.mu.RUnlock()

	L, err := h.factory.NewState(ctx)
	if err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_command").Hint("failed to create state").Wrap(err)
	}
	defer L.Close()

	// Stamp the host-vouched dispatch subject before the Lua state's context is
	// set, so in-VM hostfuncs inherit it (INV-PLUGIN-51). Only character actors
	// are vouched; see stampDispatch.
	ctx = h.stampDispatch(ctx)

	L.SetContext(ctx)

	if h.hostFuncs != nil {
		h.hostFuncs.SetPluginConfigs(cfgSnapshot)
		h.hostFuncs.Register(L, name, requires...)
	}

	// Inject the host-cap bridge for opted-in plugins, mirroring DeliverEvent so
	// on_command sees the same bridge-only globals as on_event (spec §5).
	if bridgeEnabled && endpoint != nil {
		luabridge.RegisterHostCaps(L, endpoint.Conn(), name, declaredCaps)
	}

	if err := L.DoString(code); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_command").Hint("failed to load code").Wrap(err)
	}

	onCommand := L.GetGlobal("on_command")
	if onCommand.Type() == lua.LTNil {
		slog.DebugContext(ctx, "plugin has no on_command handler",
			"plugin", name,
			"command", cmd.Command)
		return pluginsdk.OK(""), nil
	}

	ctxTable := h.buildCommandRequestTable(L, cmd)

	if err := h.invoke(ctx, L, name, "on_command", lua.P{
		Fn:      onCommand,
		NRet:    1,
		Protect: true,
	}, ctxTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_command").Wrap(err)
	}

	ret := L.Get(-1)
	L.Pop(1)

	return h.parseCommandResponse(ret, name), nil
}

// Plugins returns names of loaded plugins.
func (h *Host) Plugins() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	names := make([]string, 0, len(h.plugins))
	for name := range h.plugins {
		names = append(names, name)
	}
	return names
}

// PluginEmitRegistry implements plugins.Host. Returns a defensive copy so
// callers cannot mutate host-held registry state. Preserves nil-vs-empty
// semantics — plugins without crypto.emits keep their nil registry; plugins
// with crypto.emits but zero captured types get an empty (non-nil) slice.
func (h *Host) PluginEmitRegistry(name string) ([]string, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.plugins[name]
	if !ok {
		return nil, false
	}
	return append([]string(nil), p.emitRegistry...), true
}

// sessionStreamsRequestToLuaArgs is the single Lua-side site mapping
// plugins.SessionStreamsRequest onto the positional argument list passed to
// on_session_subscribe. The order MUST stay (character_id, player_id,
// session_id) to match the documented Lua signature. SessionStreamsRequest
// forks per runtime (binary marshals it onto a proto); routing the Lua marshal
// through one function lets TestSessionStreamsRequestToLuaArgsCarriesEveryField
// assert by reflection that every field is passed, so a field added here
// without wiring cannot silently miss the Lua runtime (holomush-av954).
func sessionStreamsRequestToLuaArgs(req plugins.SessionStreamsRequest) []lua.LValue {
	return []lua.LValue{
		lua.LString(req.CharacterID),
		lua.LString(req.PlayerID),
		lua.LString(req.SessionID),
	}
}

// QuerySessionStreams calls the plugin's on_session_subscribe(character_id, player_id, session_id)
// function if defined. Returns the list of stream names the plugin wants added.
// Returns nil without error if the function is not defined.
func (h *Host) QuerySessionStreams(ctx context.Context, name string, req plugins.SessionStreamsRequest) ([]string, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, oops.In("lua").With("plugin", name).With("operation", "query_session_streams").New("host is closed")
	}
	p, ok := h.plugins[name]
	if !ok {
		h.mu.RUnlock()
		return nil, oops.In("lua").With("plugin", name).With("operation", "query_session_streams").New("plugin not loaded")
	}
	code := p.code
	requires := p.manifest.RequiredServiceNames()
	// Snapshot the bridge inputs alongside the other per-delivery snapshots so
	// on_session_subscribe handlers receive the same host-cap bridge globals as
	// on_event (intra-Lua entrypoint parity). Both p.endpoint and the manifest
	// slice are written only under h.mu.Lock in Load/Unload.
	declaredCaps := p.manifest.RequiredCapabilities()
	if h.pluginGrants != nil {
		// pluginGrants is set (resolver path) — filter to the granted subset.
		declaredCaps = grantedSubset(declaredCaps, h.pluginGrants[name])
	}
	endpoint := p.endpoint // nil when hostFuncs is nil (NewHost path)
	// Snapshot the merged config under the read lock: Load mutates
	// h.mergedConfigs under h.mu, so reading it unlocked below races
	// (concurrent map read/write panic). Shallow clone suffices — Load
	// replaces inner maps wholesale, never mutating one in place.
	cfgSnapshot := maps.Clone(h.mergedConfigs)
	bridgeEnabled := h.bridgeEnabledPlugins[name] // false when map is nil or plugin not in set
	h.mu.RUnlock()

	L, err := h.factory.NewState(ctx)
	if err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "query_session_streams").Hint("failed to create state").Wrap(err)
	}
	defer L.Close()
	L.SetContext(ctx)

	if h.hostFuncs != nil {
		h.hostFuncs.SetPluginConfigs(cfgSnapshot)
		h.hostFuncs.Register(L, name, requires...)
	}

	// Inject the host-cap bridge for opted-in plugins, mirroring DeliverEvent so
	// on_session_subscribe sees the same bridge-only globals as on_event (spec §5).
	if bridgeEnabled && endpoint != nil {
		luabridge.RegisterHostCaps(L, endpoint.Conn(), name, declaredCaps)
	}

	if err := L.DoString(code); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "query_session_streams").Hint("failed to load code").Wrap(err)
	}

	fn := L.GetGlobal("on_session_subscribe")
	if fn.Type() == lua.LTNil {
		return nil, nil
	}

	if err := h.invoke(
		ctx, L, name, "on_session_subscribe", lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		},
		sessionStreamsRequestToLuaArgs(req)...,
	); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_session_subscribe").Wrap(err)
	}

	ret := L.Get(-1)
	L.Pop(1)

	tbl, ok := ret.(*lua.LTable)
	if !ok {
		if ret.Type() == lua.LTNil {
			return nil, nil
		}
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_session_subscribe").
			Errorf("expected table return, got %s", ret.Type())
	}

	var streams []string
	tbl.ForEach(func(_ lua.LValue, v lua.LValue) {
		if s, ok := v.(lua.LString); ok {
			streams = append(streams, string(s))
		}
	})
	return streams, nil
}

// Close shuts down the host.
func (h *Host) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	// Close all per-plugin bufconn endpoints before discarding the map.
	// Each endpoint is closed exactly once: the map is nilled in the same
	// locked section, so no concurrent Load or Unload can race this iteration.
	// Best-effort: individual close errors are not collected (the host is
	// shutting down and all plugin work has stopped by convention).
	for _, p := range h.plugins {
		if p.endpoint != nil {
			_ = p.endpoint.Close() //nolint:errcheck // best-effort shutdown; errors not actionable at host close
		}
	}
	h.plugins = nil
	return nil
}

func (h *Host) buildEventTable(state *lua.LState, event pluginsdk.Event) *lua.LTable {
	t := state.NewTable()
	state.SetField(t, "id", lua.LString(event.ID))
	state.SetField(t, "stream", lua.LString(event.Stream))
	state.SetField(t, "type", lua.LString(string(event.Type)))
	state.SetField(t, "timestamp", lua.LNumber(event.Timestamp))
	state.SetField(t, "actor_kind", lua.LString(event.ActorKind.String()))
	state.SetField(t, "actor_id", lua.LString(event.ActorID))
	state.SetField(t, "payload", lua.LString(event.Payload))
	return t
}

func (h *Host) parseEmitEvents(ret lua.LValue) (emits []pluginsdk.EmitEvent, validationErrs []string) {
	if ret.Type() == lua.LTNil {
		return nil, nil
	}

	table, ok := ret.(*lua.LTable)
	if !ok {
		err := "returned non-table value: " + ret.Type().String()
		return nil, []string{err}
	}

	index := 0
	table.ForEach(func(_, v lua.LValue) {
		index++

		eventTable, ok := v.(*lua.LTable)
		if !ok {
			validationErrs = append(validationErrs,
				fmt.Sprintf("entry[%d]: expected table, got %s", index, v.Type().String()))
			return
		}

		subject := emitTableString(eventTable, "subject")
		eventType := emitTableString(eventTable, "type")
		payload := emitTableString(eventTable, "payload")

		// Validate required fields
		if subject == "" {
			validationErrs = append(validationErrs,
				fmt.Sprintf("entry[%d]: missing required 'subject' field", index))
			return
		}

		if eventType == "" {
			validationErrs = append(validationErrs,
				fmt.Sprintf("entry[%d]: missing required 'type' field (subject=%s)", index, subject))
			return
		}

		// `sensitive` is the Phase 3d per-event sensitivity claim
		// stamped by holo.emit.X(..., {sensitive=true}) and serialized
		// by hostfunc.emitFlush. Absent → default false. Wrong type
		// (e.g. a string "true") MUST be a validation error rather
		// than a silent downgrade — on a sensitivity=may manifest a
		// silent false would emit plaintext, defeating the operator-
		// set sensitivity intent. The host-side downgrade fence at
		// event_emitter.go::Emit validates correct boolean claims
		// against the plugin manifest (INV-PLUGIN-29 / INV-PLUGIN-30).
		sensitive, sensitiveOK := emitTableBool(eventTable, "sensitive")
		if !sensitiveOK {
			validationErrs = append(validationErrs,
				fmt.Sprintf("entry[%d]: sensitive MUST be a boolean (got non-boolean type, subject=%s)", index, subject))
			return
		}

		emit := pluginsdk.EmitEvent{
			// EmitEvent keeps the legacy field name Stream; F5 migrates
			// the plugin-return shape to Subject alongside other plugin
			// API updates.
			Stream:    subject,
			Type:      pluginsdk.EventType(eventType),
			Payload:   payload,
			Sensitive: sensitive,
		}
		emits = append(emits, emit)
	})

	return emits, validationErrs
}

// emitTableString fetches a key from a Lua emit table and returns "" if the
// value is absent or the Lua literal "nil" (gopher-lua's String() on an
// LNilValue returns the string "nil").
func emitTableString(t *lua.LTable, key string) string {
	v := t.RawGetString(key).String()
	if v == "nil" {
		return ""
	}
	return v
}

// emitTableBool fetches a boolean key from a Lua emit table. Returns
// (value, ok). ok==true means either the key was absent (value=false,
// the documented default) OR the key carried a boolean (value=that
// boolean). ok==false signals a non-boolean, non-nil value — a
// malformed claim that callers MUST treat as a validation error rather
// than silently downgrading to false. A Lua plugin returning
// `sensitive = "true"` (string) on a `sensitivity=may` manifest must
// not silently emit as plaintext; the upstream readSensitiveOpts in
// hostfunc/stdlib.go rejects type errors at emit time, and this
// round-trip parser mirrors that fail-loud discipline so an out-of-band
// table mutation or a plugin returning a hand-built misshapen table
// surfaces as a validation error instead of a silent plaintext emit.
func emitTableBool(t *lua.LTable, key string) (value, ok bool) {
	v := t.RawGetString(key)
	switch b := v.(type) {
	case *lua.LNilType:
		return false, true // absent → default false (intended)
	case lua.LBool:
		return bool(b), true
	default:
		return false, false // wrong type → validation error
	}
}

// buildCommandRequestTable creates a Lua table from a CommandRequest.
func (h *Host) buildCommandRequestTable(state *lua.LState, cmd pluginsdk.CommandRequest) *lua.LTable {
	t := state.NewTable()
	state.SetField(t, "command", lua.LString(cmd.Command))
	state.SetField(t, "name", lua.LString(cmd.Command)) // alias: handlers may read ctx.name or ctx.command
	state.SetField(t, "args", lua.LString(cmd.Args))
	state.SetField(t, "character_id", lua.LString(cmd.CharacterID))
	state.SetField(t, "character_name", lua.LString(cmd.CharacterName))
	state.SetField(t, "location_id", lua.LString(cmd.LocationID))
	state.SetField(t, "session_id", lua.LString(cmd.SessionID))
	state.SetField(t, "player_id", lua.LString(cmd.PlayerID))
	state.SetField(t, "invoked_as", lua.LString(cmd.InvokedAs))
	// Phase 5 (5rh.14 T19) + CodeRabbit PR #4191 round 6: expose
	// connection_id so Lua command handlers can route per-connection
	// (e.g., scene focus / grid). Empty string for non-connection paths.
	state.SetField(t, "connection_id", lua.LString(cmd.ConnectionID))
	return t
}

// parseCommandResponse converts a Lua return value into a CommandResponse.
// Handles three cases: nil (OK, no output), string (OK with output), table (full response).
func (h *Host) parseCommandResponse(ret lua.LValue, pluginName string) *pluginsdk.CommandResponse {
	switch v := ret.(type) {
	case *lua.LNilType:
		return pluginsdk.OK("")
	case lua.LString:
		return pluginsdk.OK(string(v))
	case *lua.LTable:
		resp := &pluginsdk.CommandResponse{}

		if statusVal := v.RawGetString("status"); statusVal.Type() == lua.LTNumber {
			s := pluginsdk.CommandStatus(int(lua.LVAsNumber(statusVal)))
			if s < pluginsdk.CommandOK || s > pluginsdk.CommandFatal {
				s = pluginsdk.CommandOK
			}
			resp.Status = s
		}

		if outputVal := v.RawGetString("output"); outputVal.Type() == lua.LTString {
			resp.Output = lua.LVAsString(outputVal)
		}

		if eventsVal := v.RawGetString("events"); eventsVal.Type() == lua.LTTable {
			emits, validationErrs := h.parseEmitEvents(eventsVal)
			if len(validationErrs) > 0 {
				slog.Warn("plugin command emit validation errors",
					"plugin", pluginName,
					"error_count", len(validationErrs),
					"errors", validationErrs)
			}
			resp.Events = emits
		}

		return resp
	default:
		slog.Warn("plugin on_command returned unexpected type",
			"plugin", pluginName,
			"type", ret.Type().String())
		return pluginsdk.OK("")
	}
}
