// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	cryptotls "crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/prometheus/client_golang/prometheus"

	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/admin/approval"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/internal/admin/policy"
	socket "github.com/holomush/holomush/internal/admin/socket"
	totpaudit "github.com/holomush/holomush/internal/admin/totp_audit"
	authsetup "github.com/holomush/holomush/internal/auth/setup"
	"github.com/holomush/holomush/internal/bootstrap"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/logging"
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	"github.com/holomush/holomush/internal/session"
	sessionsetup "github.com/holomush/holomush/internal/session/setup"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telemetry"
	tlscerts "github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/internal/totp"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	worldsetup "github.com/holomush/holomush/internal/world/setup"
	"github.com/holomush/holomush/internal/xdg"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// coreConfig holds configuration for the core command.
type coreConfig struct {
	GRPCAddr              string        `koanf:"grpc_addr"`
	ControlAddr           string        `koanf:"control_addr"`
	MetricsAddr           string        `koanf:"metrics_addr"`
	DataDir               string        `koanf:"data_dir"`
	GameID                string        `koanf:"game_id"`
	LogFormat             string        `koanf:"log_format"`
	SkipSeedMigrations    bool          `koanf:"skip_seed_migrations"`
	SessionTTL            string        `koanf:"session_ttl"`
	SessionMaxHistory     int           `koanf:"session_max_history"`
	SessionReaperInterval string        `koanf:"session_reaper_interval"`
	Setting               string        `koanf:"setting"`
	ResetSetting          bool          `koanf:"reset_setting"`
	LuaTimeout            time.Duration `koanf:"lua_timeout"`
	LuaRegistryMaxSize    int           `koanf:"lua_registry_max_size"`
}

// Validate checks that the configuration is valid.
func (cfg *coreConfig) Validate() error {
	if cfg.GRPCAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("grpc-addr is required")
	}
	if cfg.ControlAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("control-addr is required")
	}
	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		return oops.Code("CONFIG_INVALID").Errorf("log-format must be 'json' or 'text', got %q", cfg.LogFormat)
	}
	if cfg.LuaTimeout <= 0 {
		return oops.Code("CONFIG_INVALID").Errorf("plugin-lua-timeout must be positive, got %s", cfg.LuaTimeout)
	}
	if cfg.LuaRegistryMaxSize <= 0 {
		return oops.Code("CONFIG_INVALID").Errorf("plugin-lua-registry-max must be positive, got %d", cfg.LuaRegistryMaxSize)
	}
	return nil
}

// Default values for core command flags.
const (
	defaultGRPCAddr             = "localhost:9000"
	defaultCoreControlAddr      = "127.0.0.1:9001"
	defaultCoreMetricsAddr      = "127.0.0.1:9100"
	defaultLogFormat            = "json"
	defaultPluginLuaTimeout     = 1 * time.Second
	defaultPluginLuaRegistryMax = 65536
)

// NewCoreCmd creates the core subcommand.
func NewCoreCmd() *cobra.Command {
	cfg := &coreConfig{}

	cmd := &cobra.Command{
		Use:   "core",
		Short: "Start the core process (engine, plugins)",
		Long: `Start the core process which runs the game engine,
manages plugins, and handles game state.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := config.Load(configFile, cmd, cfg, "core"); err != nil {
				return err
			}
			var gameConfig config.GameConfig
			if err := config.Load(configFile, cmd, &gameConfig, "game"); err != nil {
				return err
			}
			authConfig := config.DefaultAuthConfig()
			if err := config.Load(configFile, cmd, &authConfig, "auth"); err != nil {
				return err
			}
			eventBusConfig := eventbus.Config{}
			if err := config.Load(configFile, cmd, &eventBusConfig, "event_bus"); err != nil {
				return err
			}
			cryptoConfig := config.DefaultCryptoConfig()
			if err := config.Load(configFile, cmd, &cryptoConfig, "crypto"); err != nil {
				return err
			}
			return runCoreWithDeps(cmd.Context(), cfg, gameConfig, authConfig, eventBusConfig, cryptoConfig, cmd, nil)
		},
	}

	cmd.Flags().StringVar(&cfg.GRPCAddr, "grpc-addr", defaultGRPCAddr, "gRPC listen address")
	cmd.Flags().StringVar(&cfg.ControlAddr, "control-addr", defaultCoreControlAddr, "control gRPC listen address with mTLS")
	cmd.Flags().StringVar(&cfg.MetricsAddr, "metrics-addr", defaultCoreMetricsAddr, "metrics/health HTTP address (empty = disabled)")
	cmd.Flags().StringVar(&cfg.DataDir, "data-dir", "", "data directory (default: XDG_DATA_HOME/holomush)")
	cmd.Flags().StringVar(&cfg.GameID, "game-id", "", "game ID (default: auto-generated from database)")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", defaultLogFormat, "log format (json or text)")
	cmd.Flags().BoolVar(&cfg.SkipSeedMigrations, "skip-seed-migrations", false, "disable automatic seed policy version upgrades during bootstrap")
	cmd.Flags().StringVar(&cfg.SessionTTL, "session-ttl", "30m", "default session TTL after disconnect")
	cmd.Flags().IntVar(&cfg.SessionMaxHistory, "session-max-history", 500, "max command history entries per session")
	cmd.Flags().StringVar(&cfg.SessionReaperInterval, "session-reaper-interval", "30s", "session reaper check interval")
	cmd.Flags().StringVar(&cfg.Setting, "setting", "crossroads", "setting plugin to bootstrap on first boot")
	cmd.Flags().BoolVar(&cfg.ResetSetting, "reset-setting", false, "force re-bootstrap from setting plugin")
	cmd.Flags().DurationVar(&cfg.LuaTimeout, "plugin-lua-timeout", defaultPluginLuaTimeout, "per-invocation CPU deadline for Lua plugins")
	cmd.Flags().IntVar(&cfg.LuaRegistryMaxSize, "plugin-lua-registry-max", defaultPluginLuaRegistryMax, "max Lua registry size per plugin state")

	return cmd
}

// runCoreWithDeps starts and runs the core process using the provided configuration and injectable dependencies.
// It validates configuration, initializes logging and telemetry, ensures database migrations and TLS certificates,
// constructs and starts subsystems under an orchestrator, optionally starts observability, launches the control gRPC server,
// waits for readiness, handles OS signals and context cancellation, and performs a graceful shutdown.
// codecov:ignore — tested by integration and E2E tests
func runCoreWithDeps(ctx context.Context, cfg *coreConfig, gameConfig config.GameConfig, authConfig config.AuthConfig, eventBusConfig eventbus.Config, cryptoConfig config.CryptoConfig, cmd *cobra.Command, deps *CoreDeps) error {
	if deps == nil {
		deps = &CoreDeps{}
	}

	// Apply defaults for injectable infrastructure dependencies.
	deps.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return oops.Code("CONFIG_INVALID").With("operation", "validate configuration").Wrap(err)
	}

	// --- 1. Logging + telemetry ---
	level, err := resolveLogLevel(cmd)
	if err != nil {
		return err
	}
	logging.SetDefault("holomush-core", version, cfg.LogFormat, level)

	telemetryShutdown, telErr := telemetry.Init(ctx, "holomush-core", version)
	if telErr != nil {
		return oops.Code("TELEMETRY_INIT_FAILED").Wrap(telErr)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := telemetryShutdown(shutdownCtx); shutdownErr != nil {
			slog.Warn("telemetry shutdown error", "error", shutdownErr)
		}
	}()

	slog.Info(
		"starting core process",
		"grpc_addr", cfg.GRPCAddr,
		"log_format", cfg.LogFormat,
	)

	// --- 2. Database URL + schema migration ---
	databaseURL := deps.DatabaseURLGetter()
	if databaseURL == "" {
		return oops.Code("CONFIG_INVALID").Errorf("DATABASE_URL environment variable is required")
	}

	migrationBoot := bootstrap.NewMigrationBootstrapper(databaseURL, deps.MigratorFactory, deps.AutoMigrateGetter())
	if migErr := migrationBoot.Bootstrap(ctx, nil, ""); migErr != nil {
		return migErr
	}

	// --- 3. Database subsystem (started early for gameID) ---
	// The DB subsystem must start before TLS cert generation because the gameID
	// (from InitGameID) is embedded in the CA certificate on first boot.
	dbSub := store.NewSubsystem(store.SubsystemConfig{
		DatabaseURL: databaseURL,
	})
	if startErr := dbSub.Start(ctx); startErr != nil {
		return startErr
	}
	// DB shutdown is handled by orch.StopAll (step 8) — no explicit defer needed here.

	// --- 3b. Bootstrap orphan check (w9ml T19) ---
	// Defense-in-depth: refuse to start if any plugin-kind event in events_audit
	// lacks an actor_id. Migration 000018 makes orphans impossible from a clean
	// install; this guards against manual restore from an old backup.
	if orphanErr := runBootstrapOrphanCheck(ctx, dbSub.Pool()); orphanErr != nil {
		return orphanErr
	}

	gameID := cfg.GameID
	if gameID == "" {
		gameID = dbSub.GameID()
	}
	slog.Info("game ID initialized", "game_id", gameID)

	// --- 4. TLS certificates ---
	certsDir, err := deps.CertsDirGetter()
	if err != nil {
		return oops.Code("CERTS_DIR_FAILED").With("operation", "get certs directory").Wrap(err)
	}

	tlsConfig, err := deps.TLSCertEnsurer(certsDir, gameID)
	if err != nil {
		return oops.Code("TLS_SETUP_FAILED").With("operation", "set up TLS").With("certs_dir", certsDir).Wrap(err)
	}
	slog.Info("TLS certificates ready", "certs_dir", certsDir)

	// Derive admin socket paths from XDG runtime dir. Non-fatal: if the
	// runtime dir is unavailable, the admin socket is disabled (break-glass
	// unavailable) but the server continues serving. AdminSocketSubsystem.Start
	// is a no-op when SocketPath is empty.
	var adminSocketPath, adminLockPath string
	if runtimeDir, rdErr := xdg.RuntimeDir(); rdErr != nil {
		slog.Warn("admin socket disabled: cannot determine XDG runtime dir; break-glass unavailable",
			"error", rdErr)
	} else if ensureErr := xdg.EnsureDir(runtimeDir); ensureErr != nil {
		slog.Warn("admin socket disabled: cannot create XDG runtime dir; break-glass unavailable",
			"path", runtimeDir, "error", ensureErr)
	} else {
		adminSocketPath = filepath.Join(runtimeDir, "admin.sock")
		adminLockPath = filepath.Join(runtimeDir, "admin.lock")
	}

	// --- 5. Parse session configuration ---
	sessionTTL, reaperInterval, err := parseSessionConfig(cfg)
	if err != nil {
		return err
	}

	// --- 6. ReadinessRegistry + observability ---
	registry := lifecycle.NewReadinessRegistry()
	startupComplete := &atomic.Bool{}
	obsReadiness := func() bool {
		return startupComplete.Load() && registry.AllReady()
	}

	var obsServer ObservabilityServer
	if cfg.MetricsAddr != "" {
		obsServer = deps.ObservabilityServerFactory(cfg.MetricsAddr, obsReadiness)
		obsServer.MustRegister(command.CommandExecutions, command.CommandDuration, command.AliasExpansions)
		obsErrChan, obsErr := obsServer.Start()
		if obsErr != nil {
			return oops.Code("OBSERVABILITY_START_FAILED").With("addr", cfg.MetricsAddr).Wrap(obsErr)
		}
		slog.Info("observability server started", "addr", obsServer.Addr())
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if stopErr := obsServer.Stop(shutdownCtx); stopErr != nil {
				slog.Warn("error stopping observability server", "error", stopErr)
			}
		}()
		// Monitor in background — will be cancelled when context ends.
		obsCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go monitorServerErrors(obsCtx, cancel, obsErrChan, "observability")
	}

	// --- 6b. Verb registry ---
	verbRegistry, err := core.BootstrapVerbRegistry(version)
	if err != nil {
		return oops.Code("VERB_REGISTRY_BOOTSTRAP_FAILED").Wrap(err)
	}

	// --- 7. Subsystem construction (config only, no live resources) ---

	// Cross-check the configured crypto.operator allow-list against the
	// players table. Lax+warn: unknown IDs and transient PG failures
	// emit a structured warning but MUST NOT gate startup (Phase 5
	// sub-epic B INV-B5 / INV-B7). The returned slice is the source of
	// truth wired into the PlayerAttributeProvider via the ABAC stack.
	// validateCryptoOperators always returns nil error in Phase 5 sub-epic B
	// (lax+warn). The signature reserves the slot for future fail-closed
	// modes (sub-epic D), but today there is no error path to handle.
	cryptoOperators, _ := validateCryptoOperators(ctx, dbSub.Pool(), cryptoConfig.Operators, slog.Default()) //nolint:errcheck // Phase 5 sub-epic B is lax+warn; sub-epic D will rewire to handle errors here.

	// Filter crypto.dual_control_required against the known op_kind registry.
	// Lax+warn: unknown op_kinds emit a structured warning and are excluded
	// from enforcement; the server continues to start. Per spec §9.
	validatedDualControl := validateDualControlRequired(cryptoConfig.DualControlRequired, slog.Default())

	abacSub := abacsetup.NewABACSubsystem(abacsetup.ABACSubsystemConfig{
		DB:              dbSub,
		Registry:        registry,
		CryptoOperators: cryptoOperators,
	})

	authSub := authsetup.NewAuthSubsystem(authsetup.AuthSubsystemConfig{
		DB:                   dbSub,
		MaxSessionsPerPlayer: authConfig.MaxPlayerSessionsPerPlayer,
	})

	worldSub := worldsetup.NewWorldSubsystem(worldsetup.WorldSubsystemConfig{
		DB:   dbSub,
		ABAC: abacSub,
	})

	sessionSub := sessionsetup.NewSessionSubsystem(sessionsetup.SessionSubsystemConfig{
		DB: dbSub,
	})

	// Create stream registry early so it can be shared between the plugin
	// subsystem (hostfunc) and the gRPC subsystem (CoreServer + PluginHostService).
	streamRegistry := holoGRPC.NewSessionStreamRegistry()

	pluginSub := pluginsetup.NewPluginSubsystem(pluginsetup.PluginSubsystemConfig{
		DataDir:            cfg.DataDir,
		DatabaseConnStr:    databaseURL,
		CertsDir:           certsDir,
		GameID:             gameID,
		TrustAllowlist:     gameConfig.PluginTrustAllowlist,
		ABAC:               abacSub,
		PolicyInst:         abacSub,
		PluginProv:         abacSub,
		World:              worldSub,
		Sessions:           &sessionBridge{sub: sessionSub},
		AdminDeps:          &adminDepsBridge{auth: authSub, db: dbSub},
		Registry:           registry,
		StreamRegistry:     streamRegistry,
		LuaTimeout:         cfg.LuaTimeout,
		LuaRegistryMaxSize: cfg.LuaRegistryMaxSize,
		VerbRegistry:       verbRegistry,
	})

	bootstrapSub := bootstrapsetup.NewBootstrapSubsystem(bootstrapsetup.BootstrapSubsystemConfig{
		DB:                 dbSub,
		ABAC:               abacSub,
		World:              worldSub,
		WorldTx:            worldSub,
		Plugins:            pluginSub,
		PlayerRepos:        authSub,
		Hashers:            authSub,
		Setting:            cfg.Setting,
		ResetSetting:       cfg.ResetSetting,
		SkipSeedMigrations: cfg.SkipSeedMigrations,
		GuestStartLocation: gameConfig.GuestStartLocation,
	})

	// EventBus (Phase A): embedded NATS JetStream runs alongside other
	// subsystems but has no consumers, publishers, or subscribers attached
	// yet. Phase B (F1+) will wire the plugin emit path and gRPC Subscribe
	// handler through the bus. Until then this is pure infrastructure.
	eventBusSub := eventbus.NewSubsystem(eventBusConfig)

	// EventBus is pre-started here (mirroring the early-start of dbSub
	// above) because cluster.NewSubsystem requires a non-nil *nats.Conn
	// at construction time, and eventBusSub.Conn() returns nil prior to
	// Start. Start is idempotent, so the orchestrator's StartAll below
	// will short-circuit when it reaches eventBusSub. EventBus shutdown
	// is handled by orch.StopAll (step 8) — no explicit defer needed.
	if startErr := eventBusSub.Start(ctx); startErr != nil {
		return oops.Code("EVENTBUS_START_FAILED").
			With("operation", "start_event_bus").
			Wrap(startErr)
	}
	// Ownership: cleanup eventBusSub on early-return paths until the
	// orchestrator takes over via orch.StopAll below. The flag flips to
	// true after orch.StartAll succeeds.
	eventBusOwnedByOrchestrator := false
	defer func() {
		if !eventBusOwnedByOrchestrator {
			// Bound the cleanup so a wedged Stop can't hang startup-
			// failure handling forever. 5s matches typical NATS
			// graceful-shutdown budgets; primary error has already
			// been returned, so logging is the only signal we have.
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			if stopErr := eventBusSub.Stop(stopCtx); stopErr != nil {
				slog.Warn("event bus cleanup failed on early-return path",
					"err", stopErr.Error())
			}
		}
	}()

	// Phase 3c (holomush-ojw1.3): cluster.Registry runs in every deployment
	// from this PR onward; it provides cross-replica health/status surface
	// and (when DEK pipeline activates at Phase 3d) the failure-remediation
	// substrate for cache invalidation. ProductionPill is wired here;
	// dev/test wirings live in their respective entry points. ProductionPill
	// terminates the process with os.Exit(125), so production deployments
	// MUST run under a supervisor that interprets exit code 125 as
	// restart-eligible (systemd Restart=on-failure, k8s restartPolicy=Always,
	// docker restart=on-failure).
	clusterPillMetrics := cluster.NewPillMetrics(prometheus.DefaultRegisterer)
	clusterSkewMetrics := cluster.NewSkewMetrics(prometheus.DefaultRegisterer)
	clusterSelfTimeoutMetrics := cluster.NewSelfTimeoutMetrics(prometheus.DefaultRegisterer)
	clusterHeartbeatMetrics := cluster.NewHeartbeatMetrics(prometheus.DefaultRegisterer)
	clusterDuplicateIDMetrics := cluster.NewDuplicateMemberIDMetrics(prometheus.DefaultRegisterer)
	clusterSelfID := cluster.MemberID(idgen.New().String())
	clusterPill := cluster.NewProductionPill(clusterSelfID, slog.Default(), clusterPillMetrics)
	clusterSub, clusterErr := cluster.NewSubsystem(cluster.Config{
		ClusterID:       gameID,
		HolomushVersion: version, // package-private to main; ldflag-set via -X
	}, cluster.Deps{
		Conn:              eventBusSub.Conn(),
		Logger:            slog.Default(),
		PillMetrics:       clusterPillMetrics,
		SkewMetrics:       clusterSkewMetrics,
		SelfTimeout:       clusterSelfTimeoutMetrics,
		HeartbeatMetrics:  clusterHeartbeatMetrics,
		DuplicateMemberID: clusterDuplicateIDMetrics,
		Pill:              clusterPill,
		SelfIDForTest:     clusterSelfID,
	})
	if clusterErr != nil {
		return oops.Code("CLUSTER_SUBSYSTEM_INIT_FAILED").
			With("cluster_id", gameID).
			Wrap(clusterErr)
	}

	// AuditProjection drains the EVENTS stream into events_audit so every
	// published message lands in the forever-archive PostgreSQL table.
	// Depends on DB (target table), EventBus (JetStream source), and
	// Plugins (F5: per-plugin consumers need plugin manifests + gRPC
	// clients); the orchestrator enforces that ordering via DependsOn.
	auditSub := audit.NewSubsystem(eventBusSub, dbSub, audit.Config{})

	// F5: wire per-plugin audit plumbing. Both the OwnerMap (drives host-
	// projection ack-and-skip) and the per-plugin consumer manager (drives
	// dispatch to the plugin's PluginAuditService.AuditEvent RPC) are
	// built from the same plugin-manifest snapshot inside a single lazy
	// provider. Evaluated once at audit.Subsystem.Start, after the plugin
	// subsystem has loaded manifests per DependsOn.
	auditSub.SetLateInitProvider(func() (*audit.OwnerMap, *audit.PluginConsumerManager) {
		mgr := pluginSub.Manager()
		if mgr == nil {
			return nil, nil
		}
		decls := mgr.AuditSubjects()
		if len(decls) == 0 {
			return nil, nil
		}

		js := eventBusSub.JS()
		if js == nil {
			// With no JetStream we cannot register plugin consumers at
			// all. Returning a nil OwnerMap means the host projection
			// stays the sole audit sink for every subject, which is
			// the correct degraded behavior: we must never mark a
			// subject as plugin-owned when no consumer is running.
			slog.Warn("plugin audit consumer manager: JetStream not available; host projection will handle all audit subjects")
			return nil, nil
		}
		pcm := audit.NewPluginConsumerManager(js)
		byPlugin := make(map[string][]string)
		for _, d := range decls {
			byPlugin[d.PluginName] = append(byPlugin[d.PluginName], d.Subject)
		}
		// Only add a SubjectOwner entry for subjects whose plugin consumer
		// was successfully registered. Without this, the host projection
		// would ack-and-skip those subjects while no plugin consumer was
		// running to archive them, dropping events from every audit sink.
		owners := make([]audit.SubjectOwner, 0, len(decls))
		for pluginName, subjects := range byPlugin {
			client, ok := mgr.PluginAuditClient(pluginName)
			if !ok {
				slog.Warn("plugin declared audit subjects but no PluginAuditService client is registered; host projection will archive these subjects",
					"plugin", pluginName,
					"subjects", subjects)
				continue
			}
			if addErr := pcm.Add(context.Background(), audit.PluginConsumerConfig{
				PluginName: pluginName,
				Subjects:   subjects,
				Client:     pluginAuditClientAdapter{client: client},
			}); addErr != nil {
				slog.Error("plugin audit consumer registration failed; host projection will archive these subjects",
					"plugin", pluginName,
					"subjects", subjects,
					"error", addErr)
				continue
			}
			for _, subject := range subjects {
				owners = append(owners, audit.SubjectOwner{
					PluginName: pluginName,
					Pattern:    subject,
				})
			}
		}
		if len(owners) == 0 {
			// Every plugin consumer registration failed (or no plugins
			// declared subjects); leave the OwnerMap nil so the host
			// projection archives everything.
			return nil, pcm
		}
		ownerMap, omErr := audit.NewOwnerMap(owners)
		if omErr != nil {
			slog.Error("failed to build audit OwnerMap from successfully registered plugin consumers; defaulting to host-everything",
				"error", omErr)
			return nil, pcm
		}
		return ownerMap, pcm
	})

	grpcSub := newGRPCSubsystem(grpcSubsystemConfig{
		DB:             dbSub,
		ABAC:           abacSub,
		Auth:           authSub,
		World:          worldSub,
		Plugins:        pluginSub,
		Sessions:       sessionSub,
		Bootstrap:      bootstrapSub,
		EventBus:       eventBusSub,
		GRPCAddr:       cfg.GRPCAddr,
		TLSConfig:      tlsConfig,
		SessionTTL:     sessionTTL,
		ReaperInterval: reaperInterval,
		MaxHistory:     cfg.SessionMaxHistory,
		GameConfig:     gameConfig,
		StreamRegistry: streamRegistry,
		VerbRegistry:   verbRegistry,
	})

	// --- Crypto subsystems (T22 / holomush-jxo8.6.21) ---
	// CryptoChainVerifierSubsystem runs VerifyChain before EventBus starts.
	cryptoChainVerifierSub := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{
		Pool:        dbSub.Pool(),
		GameID:      gameID,
		PolicyNames: []string{"dual_control_required"},
	})

	// Mint a server-start ULID to stamp the CryptoPolicySubsystem's emits so
	// a given run's events can be correlated in events_audit.
	serverStartULID := core.NewULID().String()

	// Derive server identity: prefer hostname, fall back to "holomush".
	serverIdentity := "holomush"
	if hostname, hostErr := os.Hostname(); hostErr == nil && hostname != "" {
		serverIdentity = "holomush@" + hostname
	}

	effectiveConfig := policy.CryptoEffectiveConfig{
		DualControlRequired: validatedDualControl,
	}

	// Wrap the bare EventBus publisher with RenderingPublisher so the
	// host-emit audit publishers stamp the App-Rendering NATS header
	// required by audit/projection.go::persist (headerRendering check).
	// Without this wrapping the projection rejects every host-emit audit
	// event with AUDIT_MISSING_HEADER and they never reach events_audit.
	// (holomush-jxo8.6.26 / INV-D14, INV-D17.)
	auditPublisher := eventbus.NewRenderingPublisher(eventBusSub.Publisher(), verbRegistry)

	// CryptoPolicySubsystem emits the current policy snapshot after AuditProjection.
	cryptoPolicySub := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
		EmitDeps: policy.EmitDeps{
			GameID:          gameID,
			ServerStartULID: serverStartULID,
			ServerIdentity:  serverIdentity,
			Pool:            dbSub.Pool(),
			Publisher:       auditPublisher,
			Clock:           totp.NewRealClock(),
			Config:          effectiveConfig,
		},
		PolicyNames: []string{"dual_control_required"},
	})

	// --- Admin handler construction (T22 / holomush-jxo8.6.21) ---
	// Build the in-memory session store for Authenticate → Approve / ResetTOTP flow.
	// totp.NewRealClock() satisfies adminauth.Clock (both require Now() time.Time).
	adminSessionStore := adminauth.NewSessionStore(totp.NewRealClock(), 10*time.Minute)

	// Construct the TOTP service for use in the admin handlers. KEK provider is
	// read from the same env-var path used by the admin-totp CLI (HOLOMUSH_KEK_FILE
	// + HOLOMUSH_KEK_PASSPHRASE). On first boot without KEK env vars, Start will
	// fail-open: the admin handlers will return errors on TOTP operations but the
	// server continues to start (handler construction itself is always successful).
	kekProvider, kekErr := buildKEKProviderFromConfig(ctx, dbSub.Pool())
	if kekErr != nil {
		slog.Warn("admin handlers: KEK provider unavailable — TOTP-gated admin RPCs will fail at runtime",
			"error", kekErr)
		kekProvider = nil
	}

	var adminTOTPSvc totp.Service
	if kekProvider != nil {
		totpRepo := totp.NewRepository(dbSub.Pool())
		builtTOTP, totpErr := totp.NewService(
			totp.Config{GameID: gameID},
			totpRepo,
			kekProvider,
			totp.NewRealClock(),
			authSub.Hasher(),
		)
		if totpErr != nil {
			slog.Warn("admin handlers: TOTP service construction failed — admin TOTP RPCs will be unavailable at runtime",
				"error", totpErr)
		} else {
			adminTOTPSvc = builtTOTP
		}
	}

	// AuditingService wraps the totp.Service to emit crypto.totp_* events.
	// When adminTOTPSvc is nil (KEK unavailable), totpAuditSvc is also nil and
	// the admin handlers will return errors on any TOTP operation.
	var totpAuditSvc *totpaudit.AuditingService
	if adminTOTPSvc != nil {
		builtAudit, auditErr := totpaudit.NewAuditingService(
			adminTOTPSvc,
			auditPublisher,
			gameID,
			totp.NewRealClock(),
			slog.Default(),
		)
		if auditErr != nil {
			slog.Warn("admin handlers: TOTP audit service construction failed", "error", auditErr)
		} else {
			totpAuditSvc = builtAudit
		}
	}

	// Build the in-game credentials provider that walks the 6-step auth sequence.
	adminRoleStore := store.NewPostgresRoleStore(dbSub.Pool())

	var authenticateHandler socket.AuthenticateHandler
	var approveHandler socket.ApproveHandler
	var resetTOTPHandler socket.ResetTOTPHandler

	if totpAuditSvc != nil {
		ingameProvider, provErr := adminauth.NewInGameCredentialsProvider(
			authSub.AuthService(),
			totpAuditSvc,
			abacSub.Resolver(),
			adminRoleStore,
		)
		if provErr != nil {
			slog.Warn("admin handlers: InGameCredentialsProvider construction failed — Authenticate will be unavailable",
				"error", provErr)
		} else {
			approvalRepo := approval.NewPostgresRepo(dbSub.Pool(), nil)
			authenticateHandler = adminauth.NewAuthenticateHandler(ingameProvider, adminSessionStore)
			approveHandler = approval.NewApproveHandler(adminSessionStore, approvalRepo, abacSub.Resolver(), adminRoleStore)
			resetTOTPHandler = adminauth.NewResetTOTPHandler(adminSessionStore, abacSub.Resolver(), adminRoleStore, totpAuditSvc)
		}
	} else {
		slog.Warn("admin handlers: TOTP audit service unavailable — all three admin RPCs (Authenticate/Approve/ResetTOTP) will return errors")
	}

	adminSub := socket.NewAdminSocketSubsystem(socket.AdminSocketSubsystemConfig{
		SocketPath:          adminSocketPath,
		LockPath:            adminLockPath,
		Version:             version,
		AuthenticateHandler: authenticateHandler,
		ApproveHandler:      approveHandler,
		ResetTOTPHandler:    resetTOTPHandler,
	})

	// --- 8. Orchestrator: register + start ---
	// The database subsystem was pre-started (step 3) because the gameID must be
	// available before TLS cert generation. Its Start() is idempotent, so the
	// orchestrator will skip reconnection. It remains registered so other subsystems
	// resolve their SubsystemDatabase dependency and so StopAll shuts it down last.
	orch := lifecycle.NewOrchestrator()
	for _, sub := range productionSubsystems(
		dbSub, abacSub, authSub, worldSub,
		sessionSub, pluginSub, bootstrapSub,
		cryptoChainVerifierSub,
		eventBusSub, clusterSub, auditSub,
		cryptoPolicySub,
		grpcSub,
		adminSub,
	) {
		orch.Register(sub)
	}

	if orchErr := orch.StartAll(ctx); orchErr != nil {
		return orchErr
	}
	eventBusOwnedByOrchestrator = true
	defer orch.StopAll(context.Background())

	// --- 9. Readiness gate ---
	readinessCtx, readinessCancel := context.WithTimeout(ctx, 30*time.Second)
	defer readinessCancel()
	if readyErr := registry.WaitReady(readinessCtx); readyErr != nil {
		for id, status := range registry.Status() {
			if !status.Tier.IsReady() {
				slog.Error(
					"subsystem not ready",
					"subsystem", id.String(),
					"tier", status.Tier.String(),
				)
			}
		}
		return fmt.Errorf("startup timeout: %w", readyErr)
	}
	startupComplete.Store(true)

	// --- 10. Control server ---
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	controlTLSConfig, tlsErr := deps.ControlTLSLoader(certsDir, "core")
	if tlsErr != nil {
		return oops.Code("CONTROL_TLS_FAILED").With("operation", "load control TLS config").With("component", "core").Wrap(tlsErr)
	}

	controlGRPCServer, err := deps.ControlServerFactory("core", func() { cancel() })
	if err != nil {
		return oops.Code("CONTROL_SERVER_CREATE_FAILED").With("operation", "create control gRPC server").Wrap(err)
	}
	controlErrChan, err := controlGRPCServer.Start(cfg.ControlAddr, controlTLSConfig)
	if err != nil {
		return oops.Code("CONTROL_SERVER_START_FAILED").With("addr", cfg.ControlAddr).Wrap(err)
	}
	go monitorServerErrors(ctx, cancel, controlErrChan, "control-grpc")
	slog.Info("control gRPC server started", "addr", cfg.ControlAddr)

	// --- 11. Signal handling ---
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	cmd.Println("Core process started")
	slog.Info(
		"core process ready",
		"game_id", gameID,
		"grpc_addr", cfg.GRPCAddr,
	)

	select {
	case sig := <-sigChan:
		slog.Info("received shutdown signal", "signal", sig)
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down")
	}

	// --- 12. Graceful shutdown ---
	slog.Info("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := controlGRPCServer.Stop(shutdownCtx); err != nil {
		slog.Warn("error stopping control gRPC server", "error", err)
	}

	// Subsystem shutdown handled by deferred orch.StopAll above.

	slog.Info("shutdown complete")
	return nil
}

// parseSessionConfig parses and validates session TTL and reaper interval from cfg,
// applying defaults when values are empty. Returns an error if parsing fails or
// either duration is not positive.
func parseSessionConfig(cfg *coreConfig) (sessionTTL, reaperInterval time.Duration, err error) {
	if cfg.SessionTTL == "" {
		cfg.SessionTTL = "30m"
	}
	sessionTTL, err = time.ParseDuration(cfg.SessionTTL)
	if err != nil {
		return 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_ttl").Wrap(err)
	}

	if cfg.SessionReaperInterval == "" {
		cfg.SessionReaperInterval = "30s"
	}
	reaperInterval, err = time.ParseDuration(cfg.SessionReaperInterval)
	if err != nil {
		return 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_reaper_interval").Wrap(err)
	}

	if sessionTTL <= 0 {
		return 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_ttl").Errorf("session TTL must be positive")
	}
	if reaperInterval <= 0 {
		return 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_reaper_interval").Errorf("reaper interval must be positive")
	}

	if cfg.SessionMaxHistory <= 0 {
		cfg.SessionMaxHistory = 500
	}

	return sessionTTL, reaperInterval, nil
}

// --- Subsystem bridge adapters ---
//
// These thin adapters bridge subsystem accessor methods to the provider
// interfaces expected by other subsystems. They exist because Go interfaces
// require exact method signatures, and subsystem accessors return concrete
// types rather than interfaces. The adapters are constructed before Start()
// is called; the underlying subsystem methods are only invoked after the
// orchestrator starts the dependency.

// sessionBridge adapts SessionSubsystem to pluginsetup.SessionProvider.
type sessionBridge struct {
	sub *sessionsetup.SessionSubsystem
}

// pluginAuditClientAdapter bridges the proto-generated
// pluginv1.PluginAuditServiceClient to the narrow audit.PluginAuditClient
// interface. Keeps the audit package free of generated-proto dependency
// for the dispatch path (AuditEvent only), while the manager-side
// resolver still returns the full proto client.
type pluginAuditClientAdapter struct {
	client pluginv1.PluginAuditServiceClient
}

// AuditEvent forwards the RPC. gRPC CallOption is empty — the host owns
// dispatch context (timeout, metadata) at the consumer layer. Errors are
// wrapped downstream by the plugin consumer's dispatch path
// (AUDIT_PLUGIN_DISPATCH_FAILED), so returning the raw RPC error here is
// correct.
func (a pluginAuditClientAdapter) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	resp, err := a.client.AuditEvent(ctx, req)
	if err != nil {
		return nil, oops.Code("AUDIT_PLUGIN_RPC_FAILED").Wrap(err)
	}
	return resp, nil
}

func (b *sessionBridge) SessionStore() session.Access {
	return b.sub.Store()
}

// adminDepsBridge adapts auth subsystem + database subsystem to pluginsetup.AdminDepsProvider.
type adminDepsBridge struct {
	auth *authsetup.AuthSubsystem
	db   *store.DatabaseSubsystem
}

func (b *adminDepsBridge) AdminDeps() handlers.AdminDeps {
	pool := b.db.Pool()
	return handlers.AdminDeps{
		PlayerRepo:     b.auth.PlayerRepo(),
		Hasher:         b.auth.Hasher(),
		PlayerSessions: b.auth.PlayerSessionStore(),
		ResetRepo:      b.auth.ResetRepo(),
		CharLister:     bootstrapsetup.NewCharRepoAdapter(pool, worldpostgres.NewCharacterRepository(pool)),
	}
}

// ensureTLSCerts ensures server and CA/client TLS certificates exist for the core
// component and returns a loaded server TLS configuration.
//
// If any of the expected files (`core.crt`, `core.key`, `root-ca.crt`) are already
// present in certsDir, the existing server TLS configuration is loaded and returned.
// Otherwise the function creates certsDir, generates a CA and server certificate for
// `core`, generates a gateway client certificate, saves all artifacts, and then loads
// and returns the resulting server TLS configuration. Returns a coded error if any
// step (directory creation, certificate generation, saving, or loading) fails.
func ensureTLSCerts(certsDir, gameID string) (*cryptotls.Config, error) {
	certPath := certsDir + "/core.crt"
	keyPath := certsDir + "/core.key"
	caPath := certsDir + "/root-ca.crt"

	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)
	caExists := fileExists(caPath)

	if certExists || keyExists || caExists {
		existingConfig, err := tlscerts.LoadServerTLS(certsDir, "core")
		if err != nil {
			return nil, oops.Code("TLS_LOAD_FAILED").With("operation", "load existing TLS certificates").With("certs_dir", certsDir).Wrap(err)
		}
		return existingConfig, nil
	}

	slog.Info("generating TLS certificates", "certs_dir", certsDir)

	if err := xdg.EnsureDir(certsDir); err != nil {
		return nil, oops.Code("CERTS_DIR_CREATE_FAILED").With("operation", "create certs directory").With("certs_dir", certsDir).Wrap(err)
	}

	ca, err := tlscerts.GenerateCA(gameID)
	if err != nil {
		return nil, oops.Code("CA_GENERATE_FAILED").With("operation", "generate CA").With("game_id", gameID).Wrap(err)
	}

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		return nil, oops.Code("SERVER_CERT_GENERATE_FAILED").With("operation", "generate server certificate").With("component", "core").Wrap(err)
	}

	err = tlscerts.SaveCertificates(certsDir, ca, serverCert)
	if err != nil {
		return nil, oops.Code("CERTS_SAVE_FAILED").With("operation", "save certificates").With("certs_dir", certsDir).Wrap(err)
	}

	gatewayCert, err := tlscerts.GenerateClientCert(ca, "gateway")
	if err != nil {
		return nil, oops.Code("CLIENT_CERT_GENERATE_FAILED").With("operation", "generate gateway certificate").With("component", "gateway").Wrap(err)
	}

	err = tlscerts.SaveClientCert(certsDir, gatewayCert)
	if err != nil {
		return nil, oops.Code("CLIENT_CERT_SAVE_FAILED").With("operation", "save gateway certificate").With("component", "gateway").Wrap(err)
	}

	slog.Info("TLS certificates generated")

	tlsConfig, err := tlscerts.LoadServerTLS(certsDir, "core")
	if err != nil {
		return nil, oops.Code("TLS_LOAD_FAILED").With("operation", "load generated certificates").With("certs_dir", certsDir).Wrap(err)
	}
	return tlsConfig, nil
}

// fileExists reports whether the file at path exists or should be treated as
// existing. Permission errors are treated as "exists" to avoid silently
// overwriting files we can't read.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// monitorServerErrors watches errCh and cancels the provided context when a non-nil error is received.
// It logs the error with the given serverName before calling cancel. The function returns if errCh is closed
// or if ctx is done.
func monitorServerErrors(ctx context.Context, cancel context.CancelFunc, errCh <-chan error, serverName string) {
	select {
	case err, ok := <-errCh:
		if !ok {
			return
		}
		if err != nil {
			slog.Error(
				"server error, triggering shutdown",
				"server", serverName,
				"error", err,
			)
			cancel()
		}
	case <-ctx.Done():
	}
}

// runAutoMigration runs database migrations using the provided factory.
// Used by auto-migration unit/integration tests; production code uses MigrationBootstrapper.
//
//nolint:unparam // databaseURL varies in integration tests (connStr from testcontainers)
func runAutoMigration(databaseURL string, factory func(string) (bootstrap.AutoMigrator, error)) error {
	slog.Info("running auto-migration")

	migrator, err := factory(databaseURL)
	if err != nil {
		return oops.Code("MIGRATION_INIT_FAILED").With("operation", "create migrator").Wrap(err)
	}
	defer func() {
		if closeErr := migrator.Close(); closeErr != nil {
			slog.Warn("error closing migrator", "error", closeErr, "note", "connection may leak")
		}
	}()

	if err := migrator.Up(); err != nil {
		return oops.Code("AUTO_MIGRATION_FAILED").With("operation", "run migrations").Wrap(err)
	}

	slog.Info("auto-migration complete")
	return nil
}

// parseAutoMigrate reads the HOLOMUSH_DB_AUTO_MIGRATE environment variable
// and defaults to enabling auto-migration.
func parseAutoMigrate() bool {
	val := strings.TrimSpace(os.Getenv("HOLOMUSH_DB_AUTO_MIGRATE"))
	if val == "" {
		return true
	}

	if strings.EqualFold(val, "false") || val == "0" {
		return false
	}

	if strings.EqualFold(val, "true") || val == "1" {
		return true
	}

	slog.Warn("unrecognized HOLOMUSH_DB_AUTO_MIGRATE value, defaulting to true",
		"value", val,
		"valid_values", "true, false, 1, 0")
	return true
}

// productionSubsystems returns the ordered list of subsystems registered
// with the orchestrator. Extracted as a helper so regression tests can
// assert on the production set without spinning up the full server.
func productionSubsystems(
	dbSub, abacSub, authSub, worldSub,
	sessionSub, pluginSub, bootstrapSub,
	cryptoChainVerifierSub,
	eventBusSub, clusterSub, auditSub,
	cryptoPolicySub,
	grpcSub,
	adminSub lifecycle.Subsystem,
) []lifecycle.Subsystem {
	return []lifecycle.Subsystem{
		dbSub, abacSub, authSub, worldSub,
		sessionSub, pluginSub, bootstrapSub,
		cryptoChainVerifierSub, // runs before EventBus (chain integrity check)
		eventBusSub, clusterSub, auditSub,
		cryptoPolicySub, // runs after AuditProjection (policy snapshot emit)
		grpcSub, adminSub,
	}
}
