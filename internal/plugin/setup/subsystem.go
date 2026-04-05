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

	"github.com/samber/oops"
	"go.opentelemetry.io/otel"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/lifecycle"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/session"
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

// SessionAccess combines the session interfaces required by both the hostfunc
// bridge (session.Access) and the service proxy (plugins.SessionAccess).
// The session subsystem's PostgresSessionStore satisfies this interface.
type SessionAccess interface {
	session.Access
	Delete(ctx context.Context, id string, reason string) error
}

// SessionProvider provides session access for host functions and the service
// proxy. The returned value must satisfy both session.Access and
// plugins.SessionAccess.
type SessionProvider interface {
	SessionStore() SessionAccess
}

// EventStoreProvider provides the event store for the service proxy.
type EventStoreProvider interface {
	EventStore() core.EventStore
}

// AdminDepsProvider provides the dependencies needed for admin command
// registration. This decouples the plugin subsystem from direct auth imports.
type AdminDepsProvider interface {
	AdminDeps() handlers.AdminDeps
}

// PluginSubsystemConfig configures the plugin subsystem.
type PluginSubsystemConfig struct {
	DataDir    string
	ABAC       EngineProvider
	PolicyInst PolicyInstallerProvider
	PluginProv PluginProviderSetter
	World      WorldServiceProvider
	Sessions   SessionProvider
	Events     EventStoreProvider
	AdminDeps  AdminDepsProvider
}

// PluginSubsystem manages the plugin Manager, Lua host, core plugin
// registration, and the command registry.
type PluginSubsystem struct {
	cfg         PluginSubsystemConfig
	manager     *plugins.Manager
	cmdRegistry *command.Registry
	proxy       *plugins.ServiceProxyImpl
	registry    *plugins.ServiceRegistry
	worldConn   *plugins.InProcessConn
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
	if regErr := s.registry.Register(plugins.RegisteredService{
		Name:       "holomush.world.v1.WorldService",
		Conn:       worldConn,
		PluginType: plugins.TypeServerInternal(),
	}); regErr != nil {
		_ = worldConn.Close()
		return oops.Code("WORLD_SERVICE_REGISTER_FAILED").Wrap(regErr)
	}

	// Create ServiceProxy for the Lua host function bridge.
	proxy, proxyErr := plugins.NewServiceProxy(plugins.ServiceProxyConfig{
		World:    s.cfg.World.Service(),
		Sessions: sessionStore,
		Events:   s.cfg.Events.EventStore(),
	})
	if proxyErr != nil {
		return oops.Code("SERVICE_PROXY_FAILED").Wrap(proxyErr)
	}
	s.proxy = proxy

	// 5. (core plugins have all been migrated to Lua — no in-process host needed)

	// 6. Wrap hosts with OTel instrumentation.
	instrumentedLuaHost, luaMWErr := plugins.NewHostMiddleware(
		luaHost, otel.GetTracerProvider(), otel.GetMeterProvider(),
	)
	if luaMWErr != nil {
		return oops.Code("LUA_HOST_MW_FAILED").Wrap(luaMWErr)
	}

	// Create binary plugin host (subprocess plugins via hashicorp/go-plugin).
	binaryHost := goplugin.NewHost()
	instrumentedBinaryHost, binaryMWErr := plugins.NewHostMiddleware(
		binaryHost, otel.GetTracerProvider(), otel.GetMeterProvider(),
	)
	if binaryMWErr != nil {
		return oops.Code("BINARY_HOST_MW_FAILED").Wrap(binaryMWErr)
	}

	// 7. Create Manager, register hosts.
	s.manager = plugins.NewManager(pluginsDir,
		plugins.WithLuaHost(instrumentedLuaHost),
		plugins.WithPolicyInstaller(s.cfg.PolicyInst.PolicyInstaller()),
		plugins.WithServiceRegistry(s.registry),
	)
	s.manager.RegisterHost(plugins.TypeBinary, instrumentedBinaryHost)

	// 8. Set ABAC plugin provider registry.
	s.cfg.PluginProv.PluginProvider().SetRegistry(s.manager)

	// 9. Load all discovered plugins.
	if loadErr := s.manager.LoadAll(ctx); loadErr != nil {
		slog.Error("failed to load plugins", "error", loadErr)
	}

	// 10. Create command registry, register built-in + admin handlers.
	s.cmdRegistry = command.NewRegistry()
	handlers.RegisterAll(s.cmdRegistry)
	handlers.RegisterAdmin(s.cmdRegistry, s.cfg.AdminDeps.AdminDeps())

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

// ServiceProxy returns the ServiceProxyImpl for late-binding configuration.
// Panics if called before Start().
func (s *PluginSubsystem) ServiceProxy() *plugins.ServiceProxyImpl {
	if s.proxy == nil {
		panic("plugin/setup: ServiceProxy() called before Start()")
	}
	return s.proxy
}

// ServiceRegistry returns the ServiceRegistry. Panics if called before Start().
func (s *PluginSubsystem) ServiceRegistry() *plugins.ServiceRegistry {
	if s.registry == nil {
		panic("plugin/setup: ServiceRegistry() called before Start()")
	}
	return s.registry
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
