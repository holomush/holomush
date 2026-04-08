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
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/lifecycle"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	tlscerts "github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/xdg"
)

// shutdownTimeout is the maximum time to wait for plugin manager shutdown.
const shutdownTimeout = 10 * time.Second

// EngineProvider provides an ABAC policy engine.
type EngineProvider interface {
	Engine() types.AccessPolicyEngine
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
type PluginSubsystemConfig struct {
	DataDir         string
	DatabaseConnStr string   // PostgreSQL connection string for schema provisioning
	CertsDir        string   // path to game certs directory (for loading CA)
	GameID          string   // game ID for cert SANs
	TrustAllowlist  []string // server-side plugin trust escalation allowlist
	ABAC            EngineProvider
	PolicyInst      PolicyInstallerProvider
	PluginProv      PluginProviderSetter
	World           WorldServiceProvider
	Sessions        SessionProvider
	AdminDeps       AdminDepsProvider
	Registry        *lifecycle.ReadinessRegistry
}

// PluginSubsystem manages the plugin Manager, Lua host, core plugin
// registration, and the command registry.
type PluginSubsystem struct {
	cfg               PluginSubsystemConfig
	manager           *plugins.Manager
	cmdRegistry       *command.Registry
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
	hostFuncs := hostfunc.New(nil, // KV store not yet available
		hostfunc.WithEngine(s.cfg.ABAC.Engine()),
		hostfunc.WithWorldService(s.cfg.World.Service()),
		hostfunc.WithSessionAccess(sessionStore),
		hostfunc.WithCapabilities(capRegistry),
	)

	// 3. Create Lua host.
	luaHost := pluginlua.NewHostWithFunctions(hostFuncs)

	// 4. Create service registry for proto service resolution.
	s.registry = plugins.NewServiceRegistry()

	// 4a. Register WorldService as a server-internal service.
	worldConn, worldConnErr := newWorldInProcessConn(s.cfg.World.Service())
	if worldConnErr != nil {
		return oops.Code("WORLD_INPROCESS_CONN_FAILED").Wrap(worldConnErr)
	}
	s.worldConn = worldConn

	// cleanupOnError closes partially initialized resources when startup fails.
	cleanupOnError := func() {
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
	hostOpts = append(hostOpts,
		goplugin.WithSchemaProvisioner(schemaProvisioner),
		goplugin.WithServiceRegistry(s.registry),
	)

	if s.cfg.CertsDir != "" {
		ca, caErr := tlscerts.LoadCA(s.cfg.CertsDir)
		if caErr != nil {
			slog.Warn("plugin mTLS disabled: could not load CA", "error", caErr)
		} else {
			hostOpts = append(hostOpts, goplugin.WithCA(ca, s.cfg.GameID))
			slog.Info("plugin mTLS enabled", "certs_dir", s.cfg.CertsDir)
		}
	}

	binaryHost := goplugin.NewHost(hostOpts...)
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
	managerOpts := []plugins.ManagerOption{
		plugins.WithLuaHost(instrumentedLuaHost),
		plugins.WithPolicyInstaller(policyInstaller),
		plugins.WithTrustAllowlist(s.cfg.TrustAllowlist),
		plugins.WithServiceRegistry(s.registry),
	}
	if s.aliasRepo != nil && s.aliasCache != nil {
		managerOpts = append(managerOpts, plugins.WithAliasSeeder(s.aliasRepo, s.aliasCache))
	}
	s.manager = plugins.NewManager(pluginsDir, managerOpts...)
	s.manager.RegisterHost(plugins.TypeBinary, instrumentedBinaryHost)

	// 9. Set ABAC plugin provider registry.
	s.cfg.PluginProv.PluginProvider().SetRegistry(s.manager)

	// 10. Load all discovered plugins (alias seeding happens inside LoadAll).
	// Strict by default — any plugin failure aborts startup so configuration
	// errors fail fast and visibly. Use plugins.WithGracefulDegradation() in
	// the manager options (intended for local dev only) to log and continue.
	if loadErr := s.manager.LoadAll(ctx); loadErr != nil {
		slog.Error("failed to load plugins", "error", loadErr)
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

	slog.Info("plugin subsystem started", "plugins_dir", pluginsDir)
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
		slog.Warn("error closing plugin manager", "error", err)
	}
	if s.worldConn != nil {
		if err := s.worldConn.Close(); err != nil {
			slog.Warn("error closing world in-process connection", "error", err)
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

// CommandRegistry returns the command Registry. Panics if called before Start().
func (s *PluginSubsystem) CommandRegistry() *command.Registry {
	if s.cmdRegistry == nil {
		panic("plugin/setup: CommandRegistry() called before Start()")
	}
	return s.cmdRegistry
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
