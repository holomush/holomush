// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// PluginRuntime owns the loaded-plugin registry and the runtime-delivery half
// of the plugin manager: routing events, commands and service dispatches to the
// host that owns a named plugin, and answering the read-side manifest lookups
// the host and AuthGuard depend on.
//
// It holds four maps that were previously fields on Manager, all guarded by
// Manager's general-purpose mutex:
//
//   - loaded      — successfully committed plugins, keyed by name.
//   - inflight    — plugins mid-load, keyed by name. lookupManifest falls back
//     to this map so a partially-loaded plugin's manifest gates
//     still apply during its own load.
//   - pluginHosts — plugin name → the Host that owns it.
//   - hostCaps    — optional Host interfaces, discovered once at registration.
//
// Sharing one mutex with the load-time wiring coupled the two halves of the
// Manager; breaking that coupling (D-06) is what this type exists to do. The
// runtime carries its OWN sync.RWMutex.
//
// LOCK DISCIPLINE: every method takes and releases the runtime's lock in one
// short critical section and calls nothing that acquires Manager.mu or the
// IdentityStore's lock. Callers MUST NOT invoke any of these methods while
// holding Manager.mu — no code path may hold two of the three locks at once, so
// no lock ordering exists to violate. Manager.RegisterHost,
// Manager.ConfigureEventEmitter, Manager.Close and loadPlugin's commit section
// each hoist their runtime call OUT of the surrounding m.mu section for exactly
// this reason; see the comments at those call sites.
//
// PLUGIN-RUNTIME SYMMETRY (D-20): the Lua return-value emit path and the binary
// gRPC EmitEvent path both reach the manifest gates through the single
// chokepoint in event_emitter.go::Emit. This type owns the *receiver* of the
// EmitPluginEvent wrapper and the lookupManifest func value the emitter is
// built with, but it does not — and must not — reimplement any gate. There is
// no per-runtime branch anywhere in this file.
type PluginRuntime struct {
	mu          sync.RWMutex
	loaded      map[string]*DiscoveredPlugin
	inflight    map[string]*DiscoveredPlugin
	pluginHosts map[string]Host
	hostCaps    map[Host]hostCapabilities

	// eventEmitter is the shared host emitter. It is nil until
	// Manager.ConfigureEventEmitter runs; EmitPluginEvent fails closed
	// with "plugin event emitter is not configured" until then.
	eventEmitter *PluginEventEmitter
}

// NewPluginRuntime returns an empty runtime with all four maps allocated. The
// maps are allocated here rather than lazily so the runtime is usable the
// moment it is constructed — which is what makes it independently testable
// (D-02) without a Manager, a NewManager call, or an integration harness.
func NewPluginRuntime() *PluginRuntime {
	return &PluginRuntime{
		loaded:      make(map[string]*DiscoveredPlugin),
		inflight:    make(map[string]*DiscoveredPlugin),
		pluginHosts: make(map[string]Host),
		hostCaps:    make(map[Host]hostCapabilities),
	}
}

// --- Registry mutation (called by the load-time half) -----------------------

// ClaimInflight rejects a duplicate or already-loading plugin name and marks
// the name as in-flight. Moved verbatim from loadPlugin's first critical
// section: rejecting duplicates before loading prevents the second plugin from
// overwriting the first in the manager maps while leaving the original loaded
// inside its host but unreachable.
func (r *PluginRuntime) ClaimInflight(dp *DiscoveredPlugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, duplicate := r.loaded[dp.Manifest.Name]; duplicate {
		return oops.In("manager").With("plugin", dp.Manifest.Name).With("operation", "load").
			Errorf("plugin %q is already loaded", dp.Manifest.Name)
	}
	if _, inflight := r.inflight[dp.Manifest.Name]; inflight {
		return oops.In("manager").With("plugin", dp.Manifest.Name).With("operation", "load").
			Errorf("plugin %q is already loading", dp.Manifest.Name)
	}
	r.inflight[dp.Manifest.Name] = dp
	return nil
}

// ReleaseInflight drops the in-flight claim for a name. Idempotent.
func (r *PluginRuntime) ReleaseInflight(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, name)
}

// CommitLoaded promotes a plugin from in-flight to loaded and records its
// owning host. It reports whether the name was ALREADY in loaded, which
// loadPlugin uses to decide whether to append to loadedOrder — that slice
// stays on Manager, so the caller performs the append outside this lock.
func (r *PluginRuntime) CommitLoaded(dp *DiscoveredPlugin, host Host) (existed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, existed = r.loaded[dp.Manifest.Name]
	delete(r.inflight, dp.Manifest.Name)
	r.loaded[dp.Manifest.Name] = dp
	r.pluginHosts[dp.Manifest.Name] = host
	return existed
}

// RemoveLoaded clears a plugin's loaded and pluginHosts entries, returning the
// host that owned it (and whether one was registered). Moved verbatim from
// UnloadPlugin: the loaded entry is removed only when a host was registered,
// preserving the existing behavior exactly.
func (r *PluginRuntime) RemoveLoaded(name string) (Host, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	host, hostLoaded := r.pluginHosts[name]
	if hostLoaded {
		delete(r.loaded, name)
		delete(r.pluginHosts, name)
	}
	return host, hostLoaded
}

// Clear empties the loaded and pluginHosts maps. Called by Manager.Close after
// every host has been closed, so hosts can still reference loaded state during
// shutdown. inflight and hostCaps are deliberately left alone, matching the
// pre-extraction Close body.
func (r *PluginRuntime) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded = make(map[string]*DiscoveredPlugin)
	r.pluginHosts = make(map[string]Host)
}

// CacheHostCapabilities discovers and caches a host's optional interfaces.
// Called once per host at registration time by Manager.RegisterHost and for the
// legacy luaHost by NewManager.
func (r *PluginRuntime) CacheHostCapabilities(host Host) {
	caps := discoverCapabilities(host)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hostCaps[host] = caps
}

// SetEventEmitter installs the shared host emitter. Called by
// Manager.ConfigureEventEmitter.
func (r *PluginRuntime) SetEventEmitter(emitter *PluginEventEmitter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventEmitter = emitter
}

// EventEmitter returns the installed emitter, or nil if
// Manager.ConfigureEventEmitter has not run.
func (r *PluginRuntime) EventEmitter() *PluginEventEmitter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.eventEmitter
}

// capabilitiesFor is the lock-taking form, for callers that hold no lock —
// loadPlugin and discoverAndRegisterAttributes.
//
// Pre-extraction those two sites called Manager.capabilitiesFor while holding
// NO lock, reading m.hostCaps unsynchronized. Routing them through the
// runtime's RLock closes that latent race; hostCaps is written only at host
// registration (before LoadAll), so no observable behavior changes.
func (r *PluginRuntime) capabilitiesFor(h Host) hostCapabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilitiesForLocked(h)
}

// capabilitiesForLocked returns the cached capabilities for a host, or an empty
// hostCapabilities if the host wasn't registered (defensive — shouldn't happen
// in practice since loadPlugin only handles hosts from m.hosts/m.luaHost).
//
// Callers MUST already hold r.mu; BeginServiceDispatch reads hostCaps inside
// the same critical section that resolves the host.
func (r *PluginRuntime) capabilitiesForLocked(h Host) hostCapabilities {
	if caps, ok := r.hostCaps[h]; ok {
		return caps
	}
	// Fallback: discover on demand. Should not happen but keeps loadPlugin safe.
	return discoverCapabilities(h)
}

// --- Delivery ---------------------------------------------------------------

// DeliverCommand routes a command to the correct host for the named plugin.
func (r *PluginRuntime) DeliverCommand(ctx context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	r.mu.RLock()
	host, ok := r.pluginHosts[pluginName]
	r.mu.RUnlock()

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
func (r *PluginRuntime) DeliverEvent(ctx context.Context, pluginName string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	r.mu.RLock()
	host, ok := r.pluginHosts[pluginName]
	r.mu.RUnlock()

	if !ok {
		return nil, oops.In("manager").With("plugin", pluginName).New("plugin not loaded or unknown")
	}
	emits, err := host.DeliverEvent(ctx, pluginName, event)
	if err != nil {
		return nil, oops.In("manager").With("plugin", pluginName).With("operation", "deliver_event").Wrap(err)
	}
	return emits, nil
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
func (r *PluginRuntime) BeginServiceDispatch(ctx context.Context, pluginName string, actor core.Actor, ownerPlayerID string) (context.Context, func(), error) {
	r.mu.RLock()
	host, ok := r.pluginHosts[pluginName]
	var dispatcher ServiceDispatcher
	if ok {
		// hostCaps is written under r.mu in CacheHostCapabilities; read it
		// inside the same critical section that resolves the host.
		dispatcher = r.capabilitiesForLocked(host).dispatcher
	}
	r.mu.RUnlock()

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

// EmitPluginEvent routes a plugin-owned emit request through the shared host
// emitter so manifests are validated and host-owned event fields are stamped
// consistently across command and subscriber paths.
//
// D-20: this is a wrapper, not a gate. Both the Lua and the binary runtime
// reach the manifest gates inside emitter.Emit — the single chokepoint in
// event_emitter.go, which this extraction does not touch.
func (r *PluginRuntime) EmitPluginEvent(ctx context.Context, pluginName string, event pluginsdk.EmitEvent) error {
	r.mu.RLock()
	emitter := r.eventEmitter
	r.mu.RUnlock()

	if emitter == nil {
		return oops.With("plugin", pluginName).
			New("plugin event emitter is not configured")
	}
	return emitter.Emit(ctx, pluginName, emitIntentFromEmitEvent(event))
}

// QuerySessionStreams collects plugin-contributed stream names for a session.
// Only plugins with SessionStreams: true in their manifest are queried.
// Plugin errors are logged and skipped (degraded-subscribe policy).
// Invalid stream names are dropped. Duplicate streams are deduplicated.
func (r *PluginRuntime) QuerySessionStreams(ctx context.Context, req SessionStreamsRequest) []string {
	r.mu.RLock()
	type pluginEntry struct {
		name        string
		host        Host
		emitDomains []string // manifest.Emits — the owned-namespace fence input (R3-A)
	}
	var opted []pluginEntry
	for name, dp := range r.loaded {
		if dp.Manifest.SessionStreams {
			if host, ok := r.pluginHosts[name]; ok {
				// Copy the manifest's emit domains under the lock so the fence
				// reads a stable snapshot outside it.
				emits := append([]string(nil), dp.Manifest.Emits...)
				opted = append(opted, pluginEntry{name: name, host: host, emitDomains: emits})
			}
		}
	}
	r.mu.RUnlock()

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
		var res result
		select {
		case res = <-results:
		case <-ctx.Done():
			return merged
		}
		if res.err != nil {
			slog.WarnContext(ctx, "plugin stream contribution failed — skipping",
				"plugin", res.name,
				"character_id", req.CharacterID,
				"session_id", req.SessionID,
				"error", res.err)
			continue
		}
		for _, s := range res.streams {
			if !isValidStreamName(s) {
				slog.WarnContext(ctx, "plugin returned invalid stream name — dropping",
					"plugin", res.name,
					"stream", s)
				continue
			}
			// R3-A establishment-path namespace fence: run the SAME
			// pluginauthz.AuthorizePluginStreamContribution the mid-session
			// stream.subscription guard uses, so the two contribution paths
			// cannot diverge. A ref outside the plugin's owned emit domains
			// (a forbidden system/audit/crypto or a foreign/cross-game domain)
			// is DROPPED + logged before it can reach the Subscribe filter plan.
			if fenceErr := pluginauthz.AuthorizePluginStreamContribution(res.name, res.emitDomains, s); fenceErr != nil {
				slog.WarnContext(ctx, "plugin stream contribution failed namespace fence — dropping",
					"plugin", res.name,
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

// --- Read-side lookups ------------------------------------------------------

// ListPlugins returns names of all loaded plugins.
func (r *PluginRuntime) ListPlugins() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.loaded))
	for name := range r.loaded {
		names = append(names, name)
	}

	// Sort for deterministic output
	sort.Strings(names)
	return names
}

// IsPluginLoaded returns true if the named plugin is currently loaded.
// Implements attribute.PluginRegistry for ABAC attribute resolution.
func (r *PluginRuntime) IsPluginLoaded(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.loaded[name]
	return ok
}

// GetLoadedPlugin returns the discovered plugin info for the named plugin.
// Returns nil and false if the plugin is not loaded.
func (r *PluginRuntime) GetLoadedPlugin(name string) (*DiscoveredPlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	dp, ok := r.loaded[name]
	return dp, ok
}

// lookupManifest resolves a plugin name to its manifest, preferring the
// committed `loaded` map and falling back to `inflight` so a plugin
// mid-load still resolves — which is what keeps its manifest gates in force
// during its own load. The order is load-bearing: inverting or dropping the
// fallback changes which gates apply mid-load (T-8-20).
//
// A nil receiver returns nil rather than panicking. This preserves the
// pre-extraction behavior of a `&Manager{}` fixture, whose nil `loaded` map
// read back cleanly; post-extraction such a fixture has a nil runtime.
func (r *PluginRuntime) lookupManifest(name string) *Manifest {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	dp, ok := r.loaded[name]
	if !ok {
		dp, ok = r.inflight[name]
	}
	if !ok {
		return nil
	}
	return dp.Manifest
}

// PluginRequestsDecryption returns true iff the plugin named pluginName
// has a manifest declaring eventType in its
// crypto.consumes[].requests_decryption[] list. The eventType MUST be
// in the qualified <plugin>:<event_type> form per crypto_validator's
// validation rules.
//
// Read by AuthGuard, which consumes *Manager directly as its
// ManifestLookup (Phase 3b grounding doc Decision 1); Manager forwards here.
//
// A nil receiver returns false rather than panicking. This carries the
// fail-closed contract previously held by authguard's manifestAdapter: a
// typed-nil *Manager stored in a ManifestLookup interface is not
// interface-nil, so authguard.New's AUTHGUARD_DEPENDENCY_NIL check cannot
// catch it. This is a crypto authorization gate on the decrypt path — it
// must deny, not crash. The guard travelled here verbatim from *Manager in
// phase 08 plan 06; *Manager retains its own, because that is the receiver
// AuthGuard actually holds.
func (r *PluginRuntime) PluginRequestsDecryption(pluginName, eventType string) bool {
	if r == nil {
		return false
	}
	manifest := r.lookupManifest(pluginName)
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
func (r *PluginRuntime) PluginCanReadBack(pluginName, eventType string) bool {
	if r == nil {
		return false
	}
	manifest := r.lookupManifest(pluginName)
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

// AuditSubjects returns every (plugin, subject) pair declared via
// manifest.Audit[*].Subjects across all loaded plugins. Plugins without
// audit blocks contribute nothing; duplicate subjects from the same
// plugin are de-duplicated at OwnerMap construction time, not here.
func (r *PluginRuntime) AuditSubjects() []AuditSubjectDeclaration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []AuditSubjectDeclaration
	for name, dp := range r.loaded {
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

// PluginAuditClient returns the PluginAuditService client for the named
// plugin by walking every registered host and asking each to produce one.
// Returns nil, false when no host can supply a client for the plugin —
// typically because the plugin is not a binary plugin, is not loaded, or
// did not register the service. The host audit subsystem calls this to
// resolve the client for each manifest-declared audit block.
func (r *PluginRuntime) PluginAuditClient(pluginName string) (pluginv1.PluginAuditServiceClient, bool) {
	r.mu.RLock()
	host, ok := r.pluginHosts[pluginName]
	r.mu.RUnlock()
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

// RegisterPluginCommands iterates all loaded plugins and registers their
// manifest-declared commands into the given command registry. This ensures
// the dispatcher can route plugin-backed commands via registry.Get().
func (r *PluginRuntime) RegisterPluginCommands(registry *command.Registry) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, dp := range r.loaded {
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
