// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package setup provides the plugin subsystem lifecycle wrapper.
// It lives in a sub-package to avoid import cycles: the core plugin
// packages (plugins/core-*) import internal/plugin, so the subsystem
// that imports those packages cannot reside in internal/plugin itself.
package setup

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/lifecycle"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	tlscerts "github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/xdg"
)

// shutdownTimeout is the maximum time to wait for plugin manager shutdown.
const shutdownTimeout = 10 * time.Second

// EngineProvider provides an ABAC policy engine, the concrete attribute
// resolver that backs it, and the audit logger for recording authorization
// decisions. AttributeResolver is used by the plugin subsystem to register
// plugin-declared attribute providers so that per-action resource policies
// (e.g. resource.scene.*) resolve at evaluation time. AuditLogger satisfies
// pluginauthz.Auditor and is wired to both Evaluate surfaces (binary and Lua)
// so that spec §5 / INV-PLUGIN-25 ("exactly one host-stamped audit event per
// plugin per-action decision") is satisfied in production.
type EngineProvider interface {
	Engine() types.AccessPolicyEngine
	// AttributeResolver returns the *attribute.Resolver that was passed to
	// policy.NewEngine during ABAC stack construction. Registering a provider
	// on this resolver immediately makes its attributes visible to the engine.
	AttributeResolver() *attribute.Resolver
	// AuditLogger returns the audit.Logger from the ABAC stack. It satisfies
	// pluginauthz.Auditor and is passed to goplugin.WithAuditLogger and
	// hostfunc.WithAuditLogger so both Evaluate surfaces emit audit events
	// (INV-PLUGIN-25). May return nil when audit logging is disabled.
	AuditLogger() pluginauthz.Auditor
}

// PolicyInstallerProvider provides a plugin policy installer.
type PolicyInstallerProvider interface {
	PolicyInstaller() *plugins.PolicyInstaller
}

// PluginProviderSetter sets the plugin registry on the ABAC plugin provider.
type PluginProviderSetter interface {
	PluginProvider() *attribute.PluginProvider
}

// WorldServiceProvider provides the world service.
type WorldServiceProvider interface {
	Service() *world.Service
}

// SessionProvider provides session access for host functions.
type SessionProvider interface {
	SessionStore() session.Access
}

// AdminDepsProvider provides the dependencies needed for admin command
// registration. This decouples the plugin subsystem from direct auth imports.
type AdminDepsProvider interface {
	AdminDeps() handlers.AdminDeps
}

// PluginSubsystemConfig configures the plugin subsystem.
//
// Focus-delta delivery note: per-connection focus deltas are driven inside
// focus.Coordinator for BOTH binary and Lua plugin runtimes (INV-SCENE-38). The
// plugin host itself no longer carries a ConnectionSender — that field was
// removed as part of the focus-delta coordinator unification (holomush-66228).
// The host still receives StreamRegistry so the Lua hostfunc session-stream
// contribution path (auto_focus_on_join → hostfunc.StreamRegistry call) can
// look up a character's active session streams.
type PluginSubsystemConfig struct {
	DataDir            string
	DatabaseConnStr    string   // PostgreSQL connection string for schema provisioning
	CertsDir           string   // path to game certs directory (for loading CA)
	GameID             string   // game ID for cert SANs
	TrustAllowlist     []string // server-side plugin trust escalation allowlist
	ABAC               EngineProvider
	PolicyInst         PolicyInstallerProvider
	PluginProv         PluginProviderSetter
	World              WorldServiceProvider
	Sessions           SessionProvider
	AdminDeps          AdminDepsProvider
	Registry           *lifecycle.ReadinessRegistry
	StreamRegistry     plugins.StreamRegistry
	LuaTimeout         time.Duration // per-invocation CPU deadline for Lua plugins
	LuaRegistryMaxSize int           // max Lua registry size per plugin state
	// VerbRegistry is seeded by BootstrapVerbRegistry in core.go and passed
	// through so the plugin manager can call WithVerbRegistry(). Required by
	// Task 20's nil check (INV-EVENTBUS-11), but safe to thread now.
	VerbRegistry *core.VerbRegistry
	// PluginConfigOverrides maps plugin name → (config key → value), merged
	// over each plugin's manifest config defaults at init (override wins).
	// Opaque to the host. Empty in production; tests/ops populate it. Keys
	// MUST be declared in the target plugin's manifest config schema (else
	// PLUGIN_CONFIG_UNKNOWN_KEY at load).
	PluginConfigOverrides map[string]map[string]string
}

// PluginSubsystem manages the plugin Manager, Lua host, core plugin
// registration, and the command registry.
type PluginSubsystem struct {
	cfg               PluginSubsystemConfig
	manager           *plugins.Manager
	luaHost           *pluginlua.Host // built in Start; SessionAdmin backing wired late via ConfigureSystemBroadcaster
	cmdRegistry       *command.Registry
	commandQuerier    *commandquery.Querier
	registry          *plugins.ServiceRegistry
	worldConn         *plugins.InProcessConn
	schemaProvisioner *plugins.SchemaProvisioner
	health            *lifecycle.HealthTracker
	aliasPool         *pgxpool.Pool
	aliasRepo         *store.PostgresAliasRepository
	aliasCache        *command.AliasCache
}

// NewPluginSubsystem creates a plugin subsystem configured with cfg.
// The returned subsystem holds configuration only and does not allocate or start any runtime resources.
func NewPluginSubsystem(cfg PluginSubsystemConfig) *PluginSubsystem {
	return &PluginSubsystem{cfg: cfg}
}

// ID returns SubsystemPlugins.
func (s *PluginSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemPlugins }

// DependsOn returns all subsystems that must start before plugins.
func (s *PluginSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemWorld,
		lifecycle.SubsystemAuth,
	}
}

// Start builds the full plugin stack and command registry.
// codecov:ignore — tested by integration and E2E tests
func (s *PluginSubsystem) Start(ctx context.Context) error {
	// 1. Resolve plugin directory.
	pluginsDir, err := s.resolvePluginsDir()
	if err != nil {
		return err
	}

	sessionStore := s.cfg.Sessions.SessionStore()

	// 2. Create capability registry for requires-based Lua function injection.
	// Capability modules will be registered here as their service dependencies
	// become available. The registry is wired into the hostfunc bridge so that
	// any capabilities registered before or after New() will be injected at
	// plugin delivery time.
	capRegistry := hostfunc.NewCapabilityRegistry()
	capRegistry.Register("holomush.plugin.v1.AuditService", hostfunc.NewAuditCapability())

	// Create hostfunc bridge.
	// Wire the audit logger so holomush.evaluate Lua calls emit INV-PLUGIN-25 host-stamped
	// audit events. The logger satisfies pluginauthz.Auditor; nil is safe (audit
	// is skipped) but should not occur in production since ABACSubsystem always
	// constructs an audit.Logger during Start().
	hostFuncOpts := []hostfunc.Option{
		hostfunc.WithEngine(s.cfg.ABAC.Engine()),
		hostfunc.WithAuditLogger(s.cfg.ABAC.AuditLogger()),
		hostfunc.WithWorldService(s.cfg.World.Service()),
		hostfunc.WithSessionAccess(sessionStore),
		hostfunc.WithCapabilities(capRegistry),
		// Qualifies domain-relative stream refs before the ambient
		// query_stream_history ABAC gate (holomush-xakba).
		hostfunc.WithGameID(s.cfg.GameID),
	}
	if s.cfg.StreamRegistry != nil {
		hostFuncOpts = append(hostFuncOpts, hostfunc.WithStreamRegistry(s.cfg.StreamRegistry))
	}
	hostFuncs := hostfunc.New(nil, hostFuncOpts...) // KV store not yet available

	// 3. Create Lua host.
	luaHost := pluginlua.NewHostWithFunctions(
		hostFuncs,
		pluginlua.WithCPUTimeout(s.cfg.LuaTimeout),
		pluginlua.WithStateFactory(pluginlua.NewStateFactory(
			pluginlua.WithRegistryMaxSize(s.cfg.LuaRegistryMaxSize),
		)),
		// Thread per-plugin config overrides so the Lua host can compute the
		// merged map at Load time (INV-PLUGIN-3: identical merged map for both
		// binary and Lua runtimes via plugins.MergePluginConfig).
		pluginlua.WithPluginConfigOverrides(s.cfg.PluginConfigOverrides),
		// Wire the attribute resolver so stampDispatch populates
		// DispatchContext.Attributes (notably "location") at delivery time
		// (holomush-eykuh.3). Same instance as the binary host below
		// (plugin-runtime-symmetry); it is the resolver the ABAC engine reads.
		pluginlua.WithDispatchAttributeResolver(s.cfg.ABAC.AttributeResolver()),
	)
	// Retain the Lua host so the gRPC subsystem can wire the SessionAdmin
	// broadcast backing late, once the event appender exists
	// (ConfigureSystemBroadcaster, holomush-eykuh.4.2).
	s.luaHost = luaHost

	// 4. Create service registry for proto service resolution.
	s.registry = plugins.NewServiceRegistry()

	// 4a. Register WorldService as a server-internal service.
	worldConn, worldConnErr := newWorldInProcessConn(s.cfg.World.Service())
	if worldConnErr != nil {
		return oops.Code("WORLD_INPROCESS_CONN_FAILED").Wrap(worldConnErr)
	}
	s.worldConn = worldConn

	// cleanupOnError closes partially initialized resources when startup fails.
	// Order matters: alias pool first because it was opened latest and holds
	// PG connections; then schema provisioner; then world conn.
	cleanupOnError := func() {
		if s.aliasPool != nil {
			s.aliasPool.Close()
			s.aliasPool = nil
			s.aliasRepo = nil
			s.aliasCache = nil
		}
		if s.schemaProvisioner != nil {
			s.schemaProvisioner.Close()
			s.schemaProvisioner = nil
		}
		_ = s.worldConn.Close() //nolint:errcheck // best-effort cleanup
		s.worldConn = nil
	}

	if regErr := s.registry.Register(plugins.RegisteredService{
		Name:       "holomush.world.v1.WorldService",
		Conn:       worldConn,
		PluginType: plugins.TypeServerInternal(),
	}); regErr != nil {
		cleanupOnError()
		return oops.Code("WORLD_SERVICE_REGISTER_FAILED").Wrap(regErr)
	}

	// 5. (core plugins have all been migrated to Lua — no in-process host needed)

	// 6. Wrap hosts with OTel instrumentation.
	instrumentedLuaHost, luaMWErr := plugins.NewHostMiddleware(
		luaHost, otel.GetTracerProvider(), otel.GetMeterProvider(),
	)
	if luaMWErr != nil {
		cleanupOnError()
		return oops.Code("LUA_HOST_MW_FAILED").Wrap(luaMWErr)
	}

	// Create schema provisioner for binary plugins with postgres storage.
	var schemaProvisioner *plugins.SchemaProvisioner
	if s.cfg.DatabaseConnStr != "" {
		schemaProvisioner = plugins.NewSchemaProvisioner(s.cfg.DatabaseConnStr)
		if spErr := schemaProvisioner.Init(ctx); spErr != nil {
			cleanupOnError()
			return oops.Code("SCHEMA_PROVISIONER_INIT_FAILED").Wrap(spErr)
		}
		s.schemaProvisioner = schemaProvisioner
	}

	// Create binary plugin host (subprocess plugins via hashicorp/go-plugin).
	var hostOpts []goplugin.HostOption
	hostOpts = append(
		hostOpts,
		goplugin.WithSchemaProvisioner(schemaProvisioner),
		goplugin.WithServiceRegistry(s.registry),
		// Game ID for stream qualification, wired unconditionally (independent of
		// WithCA/mTLS) so stream.history works for no-mTLS binary plugins (holomush-xakba).
		goplugin.WithGameID(s.cfg.GameID),
		// Wire the ABAC engine so host.v1 EvalService.Evaluate resolves (holomush-8kkv5.18).
		// This MUST use the same engine instance as s.cfg.ABAC.Engine() above — the Lua
		// hostfunc bridge (hostfunc.WithEngine) is wired from the same call, and the
		// attribute resolver registered on it is the same one returned by
		// s.cfg.ABAC.AttributeResolver() below. One engine+resolver pair for both surfaces.
		goplugin.WithEngine(s.cfg.ABAC.Engine()),
		// Wire the audit logger so host.v1 EvalService.Evaluate emits INV-PLUGIN-25 host-stamped
		// audit events for binary plugins. Same logger instance as the Lua surface above
		// (both call s.cfg.ABAC.AuditLogger()), satisfying spec §5 / INV-PLUGIN-25.
		goplugin.WithAuditLogger(s.cfg.ABAC.AuditLogger()),
		// Thread per-plugin config overrides from PluginSubsystemConfig into the
		// binary host so overrideFor() can look them up at plugin init time.
		goplugin.WithConfigOverrides(s.cfg.PluginConfigOverrides),
		// Wire the attribute resolver so stampDispatch populates
		// DispatchContext.Attributes (notably "location") at delivery time for
		// binary plugins (holomush-eykuh.3). Same resolver instance as the Lua
		// host above (plugin-runtime-symmetry).
		goplugin.WithDispatchAttributeResolver(s.cfg.ABAC.AttributeResolver()),
	)

	if s.cfg.CertsDir != "" {
		ca, caErr := tlscerts.LoadCA(s.cfg.CertsDir)
		if caErr != nil {
			slog.WarnContext(ctx, "plugin mTLS disabled: could not load CA", "error", caErr)
		} else {
			hostOpts = append(hostOpts, goplugin.WithCA(ca, s.cfg.GameID))
			slog.InfoContext(ctx, "plugin mTLS enabled", "certs_dir", s.cfg.CertsDir)
		}
	}

	binaryHost := goplugin.NewHost(hostOpts...)
	// Wire the session stream registry so the served stream.subscription
	// capability (AddSessionStream/RemoveSessionStream) reaches the same host
	// SessionStreamRegistry the Lua hostfunc path uses (plugin-runtime-symmetry).
	if s.cfg.StreamRegistry != nil {
		binaryHost.SetStreamRegistry(s.cfg.StreamRegistry)
	}
	instrumentedBinaryHost, binaryMWErr := plugins.NewHostMiddleware(
		binaryHost, otel.GetTracerProvider(), otel.GetMeterProvider(),
	)
	if binaryMWErr != nil {
		cleanupOnError()
		return oops.Code("BINARY_HOST_MW_FAILED").Wrap(binaryMWErr)
	}

	// 7. Create alias repo and cache for plugin manifest alias seeding.
	if s.cfg.DatabaseConnStr != "" {
		aliasPool, aliasPoolErr := pgxpool.New(ctx, s.cfg.DatabaseConnStr)
		if aliasPoolErr != nil {
			cleanupOnError()
			return oops.Code("ALIAS_POOL_FAILED").Wrap(aliasPoolErr)
		}
		s.aliasPool = aliasPool
		s.aliasRepo = store.NewPostgresAliasRepository(aliasPool)
		s.aliasCache = command.NewAliasCache()
	}

	// 8. Create Manager, register hosts.
	policyInstaller := s.cfg.PolicyInst.PolicyInstaller()
	// Always overwrite the installer's allowlist (including nil/empty) so a
	// reused installer doesn't carry stale state from a prior configuration.
	// Removing entries from PluginTrustAllowlist must take effect on the
	// next start, not require a restart of the installer instance.
	policyInstaller.SetTrustAllowlist(s.cfg.TrustAllowlist)
	// Wire attribute-provider registrar/unregistrar so plugin-declared
	// resource_types (e.g. core-scenes' "scene" namespace) register on the
	// live ABAC resolver during plugin load. Without this callback the
	// manager's discoverAndRegisterAttributes guard skips registration
	// and resource.scene.* resolves to empty — causing per-action owner
	// policies (pause-own-scene, update-own-scene, etc.) to default-deny.
	resolver := s.cfg.ABAC.AttributeResolver()
	managerOpts := []plugins.ManagerOption{
		plugins.WithLuaHost(instrumentedLuaHost),
		plugins.WithPolicyInstaller(policyInstaller),
		plugins.WithTrustAllowlist(s.cfg.TrustAllowlist),
		plugins.WithServiceRegistry(s.registry),
		plugins.WithVerbRegistry(s.cfg.VerbRegistry),
		plugins.WithAttributeProviderRegistrar(func(p *plugins.PluginAttributeProvider) error {
			return resolver.RegisterProvider(p)
		}),
		plugins.WithAttributeProviderUnregistrar(func(namespace string) bool {
			return resolver.UnregisterProvider(namespace)
		}),
	}
	if s.aliasRepo != nil && s.aliasCache != nil {
		managerOpts = append(managerOpts, plugins.WithAliasSeeder(s.aliasRepo, s.aliasCache))
	}
	mgr, mgrErr := plugins.NewManager(pluginsDir, managerOpts...)
	if mgrErr != nil {
		cleanupOnError()
		return oops.In("plugin-subsystem").Wrap(mgrErr)
	}
	s.manager = mgr
	s.manager.RegisterHost(plugins.TypeBinary, instrumentedBinaryHost)

	// 9. Set ABAC plugin provider registry.
	s.cfg.PluginProv.PluginProvider().SetRegistry(s.manager)

	// 10. Load all discovered plugins (alias seeding happens inside LoadAll).
	// Strict by default — any plugin failure aborts startup so configuration
	// errors fail fast and visibly. Use plugins.WithGracefulDegradation() in
	// the manager options (intended for local dev only) to log and continue.
	if loadErr := s.manager.LoadAll(ctx); loadErr != nil {
		slog.ErrorContext(ctx, "failed to load plugins", "error", loadErr)
		return oops.In("plugin-subsystem").Wrapf(loadErr, "loading plugins")
	}

	// Close the schema provisioner pool — it's only needed during plugin loading.
	// Individual plugin pools remain open for runtime queries.
	if s.schemaProvisioner != nil {
		s.schemaProvisioner.Close()
	}

	// 11. Initialize health tracker and register with readiness registry.
	s.health = lifecycle.NewHealthTracker(lifecycle.TrackerConfig{
		SubsystemName: lifecycle.SubsystemPlugins.String(),
	})
	if s.cfg.Registry != nil {
		s.cfg.Registry.Register(lifecycle.SubsystemPlugins, s)
	}

	// 12. Create command registry, register built-in + admin handlers.
	s.cmdRegistry = command.NewRegistry()
	handlers.RegisterAll(s.cmdRegistry)
	adminDeps := s.cfg.AdminDeps.AdminDeps()
	adminDeps.PluginLister = s.manager
	handlers.RegisterAdmin(s.cmdRegistry, adminDeps)

	// Register plugin-provided commands.
	s.manager.RegisterPluginCommands(s.cmdRegistry)

	// Build the shared command querier now that both the registry and alias cache
	// are fully populated. The alias cache may be nil when DatabaseConnStr is empty
	// (no DB configured). commandquery.New accepts a nil AliasLister *interface*,
	// but s.aliasCache is a typed *command.AliasCache: passing a typed-nil pointer
	// into the interface parameter would yield a non-nil interface whose method set
	// dereferences the nil receiver (panic in ListSystemAliases). Convert to the
	// interface only when non-nil so the querier sees a genuine nil interface.
	// This MUST happen after RegisterPluginCommands so plugin-declared commands are
	// visible to the querier. hostFuncs was constructed at line ~193, before the
	// registry existed — SetCommandQuerier late-binds the querier into the Lua host.
	var aliasLister commandquery.AliasLister
	if s.aliasCache != nil {
		aliasLister = s.aliasCache
	}
	s.commandQuerier = commandquery.New(s.cmdRegistry, s.cfg.ABAC.Engine(), aliasLister)
	hostFuncs.SetCommandQuerier(s.commandQuerier)
	// Late-bind the querier into the binary host (parity with the Lua hostfunc
	// SetCommandQuerier above; plugin-runtime-symmetry). binaryHost is constructed
	// earlier in Start() before the registry and querier exist, so the option
	// cannot be threaded at construction time — use the setter instead.
	binaryHost.SetCommandQuerier(s.commandQuerier)

	slog.InfoContext(ctx, "plugin subsystem started", "plugins_dir", pluginsDir)
	return nil
}

// Stop shuts down the plugin manager and server-internal connections.
// codecov:ignore — tested by integration and E2E tests
func (s *PluginSubsystem) Stop(_ context.Context) error {
	if s.manager == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.manager.Close(shutdownCtx); err != nil {
		slog.WarnContext(shutdownCtx, "error closing plugin manager", "error", err)
	}
	if s.worldConn != nil {
		if err := s.worldConn.Close(); err != nil {
			slog.WarnContext(shutdownCtx, "error closing world in-process connection", "error", err)
		}
	}
	if s.schemaProvisioner != nil {
		s.schemaProvisioner.Close()
	}
	if s.aliasPool != nil {
		s.aliasPool.Close()
	}
	return nil
}

// Manager returns the plugin Manager. Panics if called before Start().
func (s *PluginSubsystem) Manager() *plugins.Manager {
	if s.manager == nil {
		panic("plugin/setup: Manager() called before Start()")
	}
	return s.manager
}

// ConfigureSystemBroadcaster wires the SessionAdmin broadcast backing — a
// system-event emit over pub — into the Lua host so the brokered
// SessionAdminService serves real broadcasts (holomush-eykuh.4.2, decision
// holomush-t019a). It MUST be called from the gRPC subsystem's Start once the
// publisher exists: the publisher is built after the plugin subsystem starts,
// so a construction-time option cannot reach it (same late-binding rationale as
// Manager.ConfigureEventEmitter / ConfigureFocusDeps). No-op when the Lua host is
// not yet built or pub/gameID is nil (leaves the server fail-closed). The
// binary host needs no equivalent: SessionAdminService is Lua-only (not in
// hostcap.BinaryDefaultSet). Disconnect stays Unimplemented — no production
// forcible-disconnect mechanism (follow-up holomush-obo44).
func (s *PluginSubsystem) ConfigureSystemBroadcaster(pub eventbus.Publisher, gameID func() string) {
	if s.luaHost == nil || pub == nil || gameID == nil {
		return
	}
	s.luaHost.SetSessionAdmin(hostcap.NewSystemBroadcaster(pub, gameID))
}

// CommandRegistry returns the command Registry. Panics if called before Start().
func (s *PluginSubsystem) CommandRegistry() *command.Registry {
	if s.cmdRegistry == nil {
		panic("plugin/setup: CommandRegistry() called before Start()")
	}
	return s.cmdRegistry
}

// CommandQuerier returns the shared command querier. Panics if called before Start().
// Consumed by the gRPC subsystem (holoGRPC.WithCommandQuerier) and the binary
// host.v1 CommandRegistryService (bead .8) to ensure a single
// command-visibility filter across both surfaces (INV-COMMAND-1).
func (s *PluginSubsystem) CommandQuerier() *commandquery.Querier {
	if s.commandQuerier == nil {
		panic("plugin/setup: CommandQuerier() called before Start()")
	}
	return s.commandQuerier
}

// ServiceRegistry returns the ServiceRegistry. Panics if called before Start().
func (s *PluginSubsystem) ServiceRegistry() *plugins.ServiceRegistry {
	if s.registry == nil {
		panic("plugin/setup: ServiceRegistry() called before Start()")
	}
	return s.registry
}

// AliasRepo returns the alias repository created during Start().
// Panics if called before Start().
func (s *PluginSubsystem) AliasRepo() *store.PostgresAliasRepository {
	if s.aliasRepo == nil {
		panic("plugin/setup: AliasRepo() called before Start()")
	}
	return s.aliasRepo
}

// AliasCache returns the alias cache populated during plugin loading.
// Panics if called before Start().
func (s *PluginSubsystem) AliasCache() *command.AliasCache {
	if s.aliasCache == nil {
		panic("plugin/setup: AliasCache() called before Start()")
	}
	return s.aliasCache
}

// HealthStatus reports the plugin subsystem's health tier.
// Returns Dead with reason if the subsystem has not been started.
func (s *PluginSubsystem) HealthStatus() lifecycle.HealthStatus {
	if s.health == nil {
		return lifecycle.HealthStatus{
			Tier:   lifecycle.HealthDead,
			Reason: "not started",
			Since:  time.Time{},
		}
	}
	return s.health.HealthStatus()
}

func (s *PluginSubsystem) resolvePluginsDir() (string, error) {
	if s.cfg.DataDir != "" {
		return filepath.Join(s.cfg.DataDir, "plugins"), nil
	}
	baseDir, err := xdg.DataDir()
	if err != nil {
		return "", oops.Code("PLUGINS_DIR_FAILED").With("operation", "get plugins directory").Wrap(err)
	}
	return filepath.Join(baseDir, "plugins"), nil
}
