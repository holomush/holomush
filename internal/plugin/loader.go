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
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// LoaderConfig carries the construction-time settings a PluginLoader needs.
//
// It exists so the loader is constructible without a Manager: NewManager's
// ManagerOption loop records into this shape and hands it over, while a test
// (or any future caller) can populate it directly. Every field here was a
// Manager field before ARCH-02's third extraction.
type LoaderConfig struct {
	// PluginsDir is the directory Discover walks for plugin.yaml manifests.
	PluginsDir string
	// LuaHost is the legacy dedicated Lua host. It is deliberately NOT folded
	// into Hosts[TypeLua]; loadPlugin falls back to it and TestLoadPlugin
	// mirrors that fallback. Collapsing the two is tracked separately.
	LuaHost Host
	// PolicyInstaller installs and removes plugin-declared ABAC policies.
	PolicyInstaller PluginPolicyInstaller
	// RegisterProvider / UnregisterProvider are the ABAC attribute-provider
	// callbacks. They SHOULD be wired as a pair; a set registrar with a nil
	// unregistrar logs a warning on rollback rather than failing.
	RegisterProvider   RegisterPluginProviderFunc
	UnregisterProvider UnregisterPluginProviderFunc
	// Registry enables DAG-based dependency resolution. Nil selects the
	// priority-sort fallback with a nil Grants map.
	Registry *ServiceRegistry
	// CapVocab is the controlled host-capability vocabulary. Nil falls back to
	// DefaultCapabilityVocabulary inside resolveLoadOrder.
	CapVocab *CapabilityVocabulary
	// TrustAllowlist gates server-side trust escalation by plugin name.
	TrustAllowlist map[string]bool
	// GracefulDegradation lets LoadAll log per-plugin load failures instead of
	// aborting startup. It does NOT relax DAG resolution, which is always
	// fail-closed (INV-PLUGIN-43).
	GracefulDegradation bool
	// AliasSeeder / AliasCache drive manifest alias seeding at the end of
	// LoadAll. Seeding runs only when both are non-nil.
	AliasSeeder AliasSeeder
	AliasCache  *command.AliasCache
	// VerbRegistry receives plugin-declared verb registrations. NewManager
	// rejects a nil registry (ErrMissingVerbRegistry, INV-EVENTBUS-11); the
	// loader itself does not re-check, matching pre-extraction behavior where
	// the guard lived only in NewManager.
	VerbRegistry *core.VerbRegistry
}

// PluginLoader owns the load-time half of plugin management: manifest
// discovery, dependency ordering, host registration and late-bound dependency
// wiring, per-plugin load orchestration, and the two teardown paths.
//
// It is one of ARCH-02's three units, alongside PluginRuntime (the loaded
// registry and delivery surface) and IdentityStore (the name ↔ ULID registry).
// Manager is a facade over the three; the loader holds NO backpointer to it.
//
// # Lock discipline
//
// The loader carries the mutex that used to be Manager.mu, and it guards
// exactly the state that mutex guarded after 08-04 and 08-06 took their fields
// away: hosts, luaHost and loadedOrder. It is not a new lock — it is the
// original one travelling with its remaining state.
//
// No code path may hold the loader's lock and either sibling unit's lock at the
// same time. Every call into runtime or identity is made with the loader's lock
// released; where a pre-extraction critical section spanned two clusters, the
// cross-unit call is hoisted out and the hoist is documented at the call site.
// Because no path holds two of the three locks, no lock ordering exists to
// violate.
//
// # Teardown ownership
//
// Close and UnloadPlugin live here rather than on PluginRuntime. Both are
// inverses of load-unit operations (Close inverts LoadAll, UnloadPlugin inverts
// loadPlugin) and both need policyInstaller, hosts and luaHost, which are
// load-only collaborators. Placing them on the runtime unit would force it to
// hold those three fields purely for teardown and would give the package two
// orchestrators pointing at each other — the coupling ARCH-02 exists to remove.
type PluginLoader struct {
	pluginsDir          string
	luaHost             Host
	hosts               map[Type]Host // host registry keyed by plugin type
	policyInstaller     PluginPolicyInstaller
	registerProvider    RegisterPluginProviderFunc   // optional, registers plugin attribute providers
	unregisterProvider  UnregisterPluginProviderFunc // optional, unregisters plugin attribute providers on rollback
	registry            *ServiceRegistry             // optional, enables DAG resolution
	capVocab            *CapabilityVocabulary        // controlled host-capability vocabulary
	trustAllowlist      map[string]bool              // server-side trust escalation allowlist
	gracefulDegradation bool                         // if true, LoadAll continues despite plugin failures
	aliasSeeder         AliasSeeder
	aliasCache          *command.AliasCache
	verbRegistry        *core.VerbRegistry
	loadedOrder         []*DiscoveredPlugin // preserves DAG/priority load order for deterministic iteration
	mu                  sync.RWMutex

	// runtime and identity are the sibling units the loader orchestrates.
	// They are held as concrete pointers rather than consumer-side interfaces:
	// the loader uses eleven runtime operations (two of them unexported, one
	// passed as a method value to the event emitter) and six identity
	// operations, which is past the point where a narrow interface clarifies
	// anything. All three types live in this package, so a concrete pointer
	// introduces no import edge and no visibility the loader did not already
	// have. What matters for D-02 is the absence of a *Manager backpointer.
	runtime  *PluginRuntime
	identity *IdentityStore
}

// NewPluginLoader builds a loader from its configuration and the two sibling
// units it orchestrates. Callers that do not need a Manager can construct one
// directly; NewManager routes its option loop through LoaderConfig.
func NewPluginLoader(cfg LoaderConfig, runtime *PluginRuntime, identity *IdentityStore) *PluginLoader {
	return &PluginLoader{
		pluginsDir:          cfg.PluginsDir,
		luaHost:             cfg.LuaHost,
		hosts:               make(map[Type]Host),
		policyInstaller:     cfg.PolicyInstaller,
		registerProvider:    cfg.RegisterProvider,
		unregisterProvider:  cfg.UnregisterProvider,
		registry:            cfg.Registry,
		capVocab:            cfg.CapVocab,
		trustAllowlist:      cfg.TrustAllowlist,
		gracefulDegradation: cfg.GracefulDegradation,
		aliasSeeder:         cfg.AliasSeeder,
		aliasCache:          cfg.AliasCache,
		verbRegistry:        cfg.VerbRegistry,
		runtime:             runtime,
		identity:            identity,
	}
}

// Registry returns the service registry, or nil if not configured.
func (l *PluginLoader) Registry() *ServiceRegistry {
	return l.registry
}

// RegisterHost registers a host implementation for a plugin type.
// Must be called before LoadAll. Panics if host is nil.
//
// Optional capabilities (ServiceConnProvider, AttributeResolverProvider) are
// discovered once at registration time by walking the host's Unwrap() chain
// (if any) and cached on the runtime unit for the host's lifetime.
func (l *PluginLoader) RegisterHost(hostType Type, host Host) {
	if host == nil {
		panic("RegisterHost: host must not be nil")
	}
	// The two runtime-unit calls are hoisted OUT of the l.mu section below.
	// hostCaps and eventEmitter live in PluginRuntime, which owns a separate
	// lock; calling into it while holding l.mu would hold two unit locks at
	// once — the one lock-ordering hazard this decomposition exists to avoid
	// (the same correction 08-04 made for UnloadPlugin's identity deletion).
	//
	// Program order is preserved: capability caching still happens before the
	// emitter is pushed into the host. Splitting the write of hosts from the
	// write of hostCaps widens an interleaving window, but RegisterHost is a
	// wiring-phase method documented as "must be called before LoadAll", and
	// capabilitiesFor already carries a discover-on-demand fallback for a
	// missing hostCaps entry.
	l.runtime.CacheHostCapabilities(host)
	emitter := l.runtime.EventEmitter()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.hosts[hostType] = host
	if emitter != nil {
		if configurer := findOptional[EventEmitterConfigurer](host); configurer != nil {
			configurer.SetEventEmitter(emitter)
		}
	}
	// The identity unit is handed to the host directly. Pre-extraction this
	// passed the *Manager, whose NameByID/IDByName are one-line forwards into
	// this same store, so the value the host observes is behaviorally
	// identical — and the loader gains no backpointer to Manager (D-02).
	if configurer := findOptional[IdentityRegistryConfigurer](host); configurer != nil {
		configurer.SetIdentityRegistry(l.identity)
	}
}

// ConfigureEventEmitter wires the shared plugin event emitter to the provided
// EventBus publisher. Production startup MUST call this before plugin response
// events are routed through the manager.
//
// Post-F1 the emitter publishes to JetStream (no core.EventStore.Append path
// remains). Callers SHOULD pass `eventBusSub.Publisher()` here; tests MAY
// inject a fake Publisher.
func (l *PluginLoader) ConfigureEventEmitter(publisher eventbus.Publisher, opts ...EmitterOption) {
	// The emitter is built with the RUNTIME's lookupManifest. That method value
	// is the emitter's only route to a plugin's manifest, and therefore the
	// data source behind every gate event_emitter.go::Emit enforces
	// (actor_kinds_claimable, emits, crypto.emits). It is a single func value
	// shared by the Lua return-value path and the binary gRPC EmitEvent path,
	// so D-20 symmetry is preserved structurally: there is no second lookup for
	// one runtime to diverge onto.
	//
	// Construction and the SetEventEmitter store happen OUTSIDE l.mu, because
	// the emitter field lives in PluginRuntime and no path may hold both locks.
	emitter := NewPluginEventEmitter(publisher, l.runtime.lookupManifest, actorFromContext, opts...)
	l.runtime.SetEventEmitter(emitter)

	l.mu.Lock()
	defer l.mu.Unlock()
	for _, host := range l.hosts {
		if configurer := findOptional[EventEmitterConfigurer](host); configurer != nil {
			configurer.SetEventEmitter(emitter)
		}
	}
	if l.luaHost != nil {
		if configurer := findOptional[EventEmitterConfigurer](l.luaHost); configurer != nil {
			configurer.SetEventEmitter(emitter)
		}
	}
}

// ConfigureFocusDeps injects the focus coordinator and history reader into all
// registered hosts. Production startup MUST call this before plugins handle
// focus-related RPCs or host functions. Called from the gRPC subsystem's
// Start after creating the FocusCoordinator.
func (l *PluginLoader) ConfigureFocusDeps(fc focuscontract.Coordinator, hr HistoryReader) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, host := range l.hosts {
		if configurer := findOptional[FocusDepsConfigurer](host); configurer != nil {
			configurer.SetFocusCoordinator(fc)
			configurer.SetHistoryReader(hr)
		}
	}
	if l.luaHost != nil {
		if configurer := findOptional[FocusDepsConfigurer](l.luaHost); configurer != nil {
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
func (l *PluginLoader) ConfigureReadbackDecryptor(d ReadbackDecryptor) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, host := range l.hosts {
		if configurer := findOptional[ReadbackDepsConfigurer](host); configurer != nil {
			configurer.SetReadbackDecryptor(d)
		}
	}
	if l.luaHost != nil {
		if configurer := findOptional[ReadbackDepsConfigurer](l.luaHost); configurer != nil {
			configurer.SetReadbackDecryptor(d)
		}
	}
}

// ConfigureSettingsDeps injects the plugin-partitioned settings stores into all
// registered hosts that implement SettingsDepsConfigurer. Production startup
// MUST call this before plugins issue GetSetting / SetSetting RPCs (or the Lua
// equivalents). Called from the gRPC subsystem's Start after the settings stores
// are assembled. Same late-binding pattern as ConfigureFocusDeps (holomush-iokti.7).
func (l *PluginLoader) ConfigureSettingsDeps(
	player settings.PlayerSettingsStore,
	character settings.CharacterSettingsStore,
	game settings.GameSettings,
) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, host := range l.hosts {
		if configurer := findOptional[SettingsDepsConfigurer](host); configurer != nil {
			configurer.SetSettingsStores(player, character, game)
		}
	}
	if l.luaHost != nil {
		if configurer := findOptional[SettingsDepsConfigurer](l.luaHost); configurer != nil {
			configurer.SetSettingsStores(player, character, game)
		}
	}
}

// Discover finds all valid plugins in the plugins directory.
// Invalid plugins are logged and skipped.
func (l *PluginLoader) Discover(ctx context.Context) ([]*DiscoveredPlugin, error) {
	entries, err := os.ReadDir(l.pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No plugins directory
		}
		return nil, oops.In("manager").With("dir", l.pluginsDir).Hint("failed to read plugins directory").Wrap(err)
	}

	plugins := make([]*DiscoveredPlugin, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(l.pluginsDir, entry.Name())
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
func (l *PluginLoader) warnUnknownTrustAllowlistEntries(discovered []*DiscoveredPlugin) {
	if len(l.trustAllowlist) == 0 {
		return
	}
	discoveredNames := make(map[string]bool, len(discovered))
	for _, dp := range discovered {
		discoveredNames[dp.Manifest.Name] = true
	}
	// Sort so log output is deterministic across runs.
	unknown := make([]string, 0, len(l.trustAllowlist))
	for name := range l.trustAllowlist {
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

// seedAliases collects alias declarations from all loaded plugin manifests
// and seeds them into the database. Iterates loadedOrder (not the map) to
// preserve DAG/priority load order — this makes cross-plugin duplicate
// resolution deterministic across restarts.
func (l *PluginLoader) seedAliases(ctx context.Context) error {
	l.mu.RLock()
	ordered := make([]*DiscoveredPlugin, len(l.loadedOrder))
	copy(ordered, l.loadedOrder)
	l.mu.RUnlock()

	aliases := CollectManifestAliases(ordered)
	if len(aliases) == 0 {
		return nil
	}

	return SeedManifestAliases(ctx, aliases, l.aliasSeeder, l.aliasCache)
}

// BuildFocusRedirects collects redirects from the loaded plugin set in
// deterministic load order. Thin wrapper over CollectFocusRedirects used by the
// dispatcher wiring.
func (l *PluginLoader) BuildFocusRedirects(registry *command.Registry) (command.FocusRedirectTable, error) {
	return CollectFocusRedirects(l.loadedOrder, registry)
}

// resolveLoadOrder resolves plugins into load order with their grant sets.
// When a registry is configured, it uses DAG-based dependency resolution and
// fails the boot (fail-closed, INV-PLUGIN-43) on any non-optional unsatisfied
// dependency or cycle. With no registry, it falls back to priority sort and
// returns a result with a nil Grants map (no-registry path: hosts fall back
// to manifest-derived caps).
func (l *PluginLoader) resolveLoadOrder(discovered []*DiscoveredPlugin) (*ResolveResult, error) {
	if l.registry == nil {
		// No registry: priority sort only; Grants is nil. On the nil-Grants
		// path BOTH runtime shims fall back to the SAME source —
		// manifest.RequiredCapabilities() — with no per-runtime divergence
		// (ADR holomush-vpg8l). This is the backward-compat fallback, not an
		// endorsement of any per-runtime gating: INV-PLUGIN-45 forbids
		// divergence, and the shared fallback satisfies that by construction.
		return &ResolveResult{Ordered: prioritySort(discovered)}, nil
	}

	serverServices := l.registry.List()
	serverServiceNames := make([]string, 0, len(serverServices))
	for _, svc := range serverServices {
		serverServiceNames = append(serverServiceNames, svc.Name)
	}

	vocab := l.capVocab
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
func (l *PluginLoader) unregisterPluginProviders(pluginName string, resourceTypes []string, upTo int) {
	if l.registerProvider == nil {
		return // registrar not wired; nothing was ever registered
	}
	if upTo > len(resourceTypes) {
		upTo = len(resourceTypes)
	}
	if upTo <= 0 {
		return
	}
	if l.unregisterProvider == nil {
		slog.Warn("cannot unregister plugin attribute providers on rollback: "+
			"WithAttributeProviderRegistrar configured but WithAttributeProviderUnregistrar is not",
			"plugin", pluginName,
			"leaked_namespaces", resourceTypes[:upTo])
		return
	}
	for _, rt := range resourceTypes[:upTo] {
		_ = l.unregisterProvider(rt)
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
func (l *PluginLoader) computeHashes(dp *DiscoveredPlugin) (manifestHash, contentHash []byte, err error) {
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

// discoverAndRegisterAttributes performs schema discovery for plugins that
// declare resource_types. It obtains the AttributeResolver gRPC client from the
// binary host, calls GetSchema to discover attribute schemas, validates that the
// schema covers all declared resource types, and registers proxy providers.
// It returns the discovered schemas for use by CheckManifestWarnings.
func (l *PluginLoader) discoverAndRegisterAttributes(ctx context.Context, host Host, dp *DiscoveredPlugin) (map[string]*types.NamespaceSchema, error) {
	pluginName := dp.Manifest.Name

	arProvider := l.runtime.capabilitiesFor(host).arProvider
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

	if l.registerProvider != nil {
		for i, rt := range dp.Manifest.ResourceTypes {
			provider := NewPluginAttributeProvider(rt, arClient, schemas[rt])
			if regErr := l.registerProvider(provider); regErr != nil {
				// Provider registration failure must be fatal — the plugin
				// declares it owns this resource type but ABAC can't resolve
				// attributes for it, so any policy targeting that type would
				// silently fail at evaluation. This is consistent with how
				// GetSchema, policy installation, and service registration
				// failures are handled.
				//
				// Rollback: unregister any providers that were added in
				// previous iterations of this loop before returning.
				l.unregisterPluginProviders(pluginName, dp.Manifest.ResourceTypes, i)
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
func (l *PluginLoader) LoadAll(ctx context.Context) error {
	// Phase 1: Discover — structural validation only.
	discovered, err := l.Discover(ctx)
	if err != nil {
		return err
	}

	// Surface trust-allowlist misconfigurations: an allowlisted name that
	// matches no discovered plugin is almost certainly a typo or stale
	// config. Left silent, it either grants no trust to the plugin the
	// operator intended, or reserves the slot for a crafted future plugin
	// with that name. Warn per unknown entry so the operator sees it.
	l.warnUnknownTrustAllowlistEntries(discovered)

	// Phase 2: Collect cross-plugin context.
	knownResourceTypes := CollectResourceTypes(discovered)
	knownActions := CollectActions(discovered)

	// Phase 3: Resolve load order.
	res, err := l.resolveLoadOrder(discovered)
	if err != nil {
		return err
	}
	ordered := res.Ordered

	// Thread the resolver grant set into all registered hosts so each host's
	// delivery shim can use grants as the single least-privilege authority.
	// res.Grants is nil on the no-registry path — hosts treat nil as "fall back
	// to manifest.RequiredCapabilities()" preserving existing behavior.
	if res.Grants != nil {
		for _, h := range l.hosts {
			if gc := findOptional[PluginGrantsConfigurer](h); gc != nil {
				gc.SetPluginGrants(res.Grants)
			}
		}
		if l.luaHost != nil {
			if gc := findOptional[PluginGrantsConfigurer](l.luaHost); gc != nil {
				gc.SetPluginGrants(res.Grants)
			}
		}
	}

	// Phase 4: Load each plugin with full context.
	var loadErrors []error
	for _, dp := range ordered {
		if err := l.loadPlugin(ctx, dp, knownResourceTypes, knownActions); err != nil {
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
		if l.gracefulDegradation {
			slog.WarnContext(ctx, "plugin loading completed with errors (graceful degradation enabled)",
				"failed_count", len(loadErrors))
			return nil
		}
		return oops.Code("PLUGIN_LOAD_FAILED").
			With("failed_count", len(loadErrors)).
			Wrap(errors.Join(loadErrors...))
	}

	// Seed aliases from loaded plugin manifests.
	if l.aliasSeeder != nil && l.aliasCache != nil {
		if err := l.seedAliases(ctx); err != nil {
			slog.ErrorContext(ctx, "failed to seed plugin aliases", "error", err)
		}
	}

	// w9ml T8: GC sweep — runs AFTER all loads have refreshed last_seen_at,
	// so a plugin loaded in this cycle is never swept in the same cycle
	// (INV-PLUGIN-16). Skipped on the graceful-degradation early return path
	// because partial-load failures may leave last_seen_at stale.
	swept, sweepErr := l.identity.Sweep(ctx)
	if sweepErr != nil {
		return sweepErr
	}
	for i := range swept {
		row := &swept[i]
		slog.InfoContext(
			ctx,
			"plugin.gc",
			"name", row.Name,
			"id", row.ID.String(),
			"last_seen_at", row.LastSeenAt.Format(time.RFC3339),
		)
	}

	return nil
}

// loadPlugin loads a single discovered plugin.
//
// Design: Returns nil (not error) for unsupported configurations to support
// graceful degradation. This allows running without Lua support or before
// binary plugin support is implemented. The warning logs provide visibility.
func (l *PluginLoader) loadPlugin(ctx context.Context, dp *DiscoveredPlugin, knownResourceTypes, knownActions map[string]bool) error {
	// Resolve the host for this plugin type first — unsupported configurations
	// are skipped here before any semantic validation so that graceful degradation
	// (e.g., no binary host configured) is not blocked by capability checks.
	// For backward compatibility, TypeLua falls back to the dedicated luaHost field.
	var host Host
	switch dp.Manifest.Type {
	case TypeLua:
		host = l.hosts[TypeLua]
		if host == nil {
			host = l.luaHost // backward compatibility
		}
		if host == nil {
			slog.WarnContext(ctx, "no Lua host configured, skipping Lua plugin",
				"plugin", dp.Manifest.Name)
			return nil
		}
	case TypeBinary:
		host = l.hosts[TypeBinary]
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
	if claimErr := l.runtime.ClaimInflight(dp); claimErr != nil {
		return claimErr
	}
	defer l.runtime.ReleaseInflight(dp.Manifest.Name)

	// w9ml T6: compute hashes, Upsert into plugins table, populate cache.
	// Hash computation only runs when pluginRepo is wired; tests that construct
	// Manager without WithPluginRepo take the else branch and bypass computeHashes.
	var pluginID ulid.ULID
	var drift *store.DriftReport
	if l.identity.HasRepo() {
		manifestHash, contentHash, hashErr := l.computeHashes(dp)
		if hashErr != nil {
			return hashErr
		}
		id, d, upsertErr := l.identity.Upsert(ctx, store.PluginUpsertInput{
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
	// and needs to resolve plugin name via IDByName. This takes the identity
	// store's own lock and releases it here; l.mu is NOT held across this
	// call, and the runtime commit further below remains a separate
	// acquisition of l.mu.
	l.identity.Register(pluginID, dp.Manifest.Name)

	// Roll back the cache mutation if any subsequent step fails. loadPlugin
	// returns a bare `error` (not a named return), so we cannot use
	// `defer func() { if err != nil ... }()` — closure would capture the
	// wrong `err` after shadowing in subsequent if-blocks. Use an explicit
	// rollback flag set by the success path.
	var loadPluginCommitted bool
	defer func() {
		if !loadPluginCommitted {
			l.identity.Unregister(pluginID, dp.Manifest.Name)
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
		schemas, regErr = l.discoverAndRegisterAttributes(ctx, host, dp)
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
		l.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
		if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
			slog.ErrorContext(ctx, "failed to rollback plugin load after schema validation failure",
				"plugin", dp.Manifest.Name, "error", unloadErr)
		}
		return oops.In("manager").With("plugin", dp.Manifest.Name).
			Wrapf(valErr, "validate manifest policy schemas")
	}

	// Install ABAC policies using manifest-aware validation when resource
	// types or trust config are present, otherwise fall back to basic install.
	if l.policyInstaller != nil && len(dp.Manifest.Policies) > 0 {
		installErr := l.policyInstaller.InstallPluginPoliciesWithManifest(ctx, dp.Manifest, dp.Manifest.Policies)
		if installErr != nil {
			// Unregister providers added during discoverAndRegisterAttributes
			// — same rationale as the schema-validation branch above.
			l.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
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
		regErr := l.verbRegistry.RegisterWithSource(core.VerbRegistration{
			Type:          vs.Type,
			Category:      vs.Category,
			Format:        vs.Format,
			Label:         vs.Label,
			DisplayTarget: displayTargetFromString(vs.DisplayTarget),
			Source:        dp.Manifest.Name,
		}, dp.Manifest.Version)
		if regErr != nil {
			// Clean up any verbs already registered from this plugin.
			l.verbRegistry.UnregisterBySource(dp.Manifest.Name)
			l.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
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
	if l.registry != nil && len(dp.Manifest.Provides) > 0 {
		connProvider := l.runtime.capabilitiesFor(host).connProvider
		if connProvider == nil {
			// Rollback attribute providers registered earlier in loadPlugin.
			l.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
			return oops.Code(CodeHostMissingConnProvider).
				In("manager").
				With("plugin", dp.Manifest.Name).
				Errorf("host does not implement ServiceConnProvider but plugin declares Provides")
		}
		conn, connErr := connProvider.PluginConn(dp.Manifest.Name)
		if connErr != nil {
			l.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
			return oops.In("manager").
				With("plugin", dp.Manifest.Name).
				Wrapf(connErr, "get plugin connection for service registration")
		}
		var registered []string
		for _, svcName := range dp.Manifest.Provides {
			regErr := l.registry.Register(RegisteredService{
				Name:       svcName,
				Conn:       conn,
				PluginName: dp.Manifest.Name,
				PluginType: dp.Manifest.Type,
			})
			if regErr != nil {
				// Unwind partial registrations.
				for _, name := range registered {
					_ = l.registry.Deregister(name) //nolint:errcheck // best-effort cleanup
				}
				l.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
				return oops.In("manager").
					With("plugin", dp.Manifest.Name).
					With("service", svcName).
					Wrapf(regErr, "register plugin service")
			}
			registered = append(registered, svcName)
		}
	}

	// The commit is split across the two locks. loaded/inflight/pluginHosts
	// live in PluginRuntime; loadedOrder stays on the loader (it is load-time
	// wiring, read by seedAliases and BuildFocusRedirects). Pre-extraction the
	// read of `loaded` that guards the loadedOrder append shared one critical
	// section with the writes — a coupling the research field matrix does not
	// model, because it records field ACCESS and not that an access sits inside
	// a section covering fields in another cluster.
	//
	// CommitLoaded therefore RETURNS whether the name was already loaded, and
	// the append happens under l.mu afterwards. Program order and the
	// append-once semantics are preserved; the two writes are no longer atomic
	// with respect to each other, which is inherent to D-06 and matches the
	// widening 08-04 recorded on the unload path.
	existed := l.runtime.CommitLoaded(dp, host)
	if !existed {
		l.mu.Lock()
		l.loadedOrder = append(l.loadedOrder, dp)
		l.mu.Unlock()
	}

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

// Close shuts down the manager and all loaded plugins.
func (l *PluginLoader) Close(ctx context.Context) error {
	// The loaded-name read is hoisted ABOVE the l.mu section and the map clear
	// is performed BETWEEN two l.mu sections: both live under the runtime
	// unit's own lock, and no path may hold two unit locks at once. Program
	// order is preserved exactly — policies removed, then hosts closed, then
	// maps cleared, then the legacy luaHost closed.
	//
	// ListPlugins returns the names sorted; the pre-extraction loop ranged the
	// map directly. Each name still gets exactly one RemovePluginPolicies call,
	// so this only makes shutdown logging deterministic.
	loadedNames := l.runtime.ListPlugins()

	l.mu.Lock()
	if l.policyInstaller != nil {
		for _, name := range loadedNames {
			if err := l.policyInstaller.RemovePluginPolicies(ctx, name); err != nil {
				slog.ErrorContext(ctx, "failed to remove plugin policies", "plugin", name, "error", err)
			}
		}
	}

	// Close all registered hosts before clearing maps so that hosts can
	// still reference loaded state during shutdown.
	for hostType, host := range l.hosts {
		if err := host.Close(ctx); err != nil {
			slog.ErrorContext(ctx, "failed to close host", "type", hostType, "error", err)
		}
	}
	l.mu.Unlock()

	// Clear loaded maps after hosts are closed.
	l.runtime.Clear()

	l.mu.Lock()
	defer l.mu.Unlock()
	// Close legacy luaHost if not already in the hosts map.
	if l.luaHost != nil {
		if _, inMap := l.hosts[TypeLua]; !inMap {
			if err := l.luaHost.Close(ctx); err != nil {
				return oops.In("manager").With("operation", "close").Hint("failed to close lua host").Wrap(err)
			}
		}
	}

	return nil
}
