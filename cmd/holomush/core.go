// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/prometheus/client_golang/prometheus"

	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/admin/policy"
	socket "github.com/holomush/holomush/internal/admin/socket"
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
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/natsconn"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/logging"
	"github.com/holomush/holomush/internal/plugin/cryptowiring"
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	"github.com/holomush/holomush/internal/session"
	sessionsetup "github.com/holomush/holomush/internal/session/setup"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telemetry"
	tlscerts "github.com/holomush/holomush/internal/tls"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	worldsetup "github.com/holomush/holomush/internal/world/setup"
	"github.com/holomush/holomush/internal/xdg"
	"github.com/holomush/holomush/pkg/errutil"
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
	SessionLeaseTTL       string        `koanf:"session_lease_ttl"`
	SessionBootGrace      string        `koanf:"session_boot_grace"`
	Setting               string        `koanf:"setting"`
	ResetSetting          bool          `koanf:"reset_setting"`
	AutoGenKEK            bool          `koanf:"auto_gen_kek"`
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
			// Validate BEFORE Defaults() — Validate is documented as
			// order-independent w.r.t. Defaults() ("" is accepted and
			// normalized to ModeEmbedded). Defaults() itself is deliberately
			// NOT applied here: runCoreWithDeps captures the RAW
			// event_bus.game_id value (rawBusGameID) before applying
			// Defaults(), so an explicit koanf value stays distinguishable
			// from the substituted "main" default (07-09 item 7, round 8
			// correction). Applying Defaults() here would make that
			// distinction impossible by the time runCoreWithDeps sees it.
			if err := eventBusConfig.Validate(); err != nil {
				return err
			}
			cryptoConfig := config.DefaultCryptoConfig()
			if err := config.Load(configFile, cmd, &cryptoConfig, "crypto"); err != nil {
				return err
			}
			logConfig := config.DefaultLoggingConfig()
			if err := config.Load(configFile, cmd, &logConfig, "logging"); err != nil {
				return err
			}
			applyLogSinkFlags(cmd, &logConfig)
			return runCoreWithDeps(cmd.Context(), cfg, gameConfig, authConfig, eventBusConfig, cryptoConfig, logConfig, cmd, nil)
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
	cmd.Flags().StringVar(&cfg.SessionLeaseTTL, "session-lease-ttl", "45s", "connection lease TTL")
	cmd.Flags().StringVar(&cfg.SessionBootGrace, "session-boot-grace", "60s", "lease-sweep suppression window after core start")
	cmd.Flags().StringVar(&cfg.Setting, "setting", "crossroads", "setting plugin to bootstrap on first boot")
	cmd.Flags().BoolVar(&cfg.ResetSetting, "reset-setting", false, "force re-bootstrap from setting plugin")
	cmd.Flags().BoolVar(&cfg.AutoGenKEK, "auto-gen-kek", false,
		"generate a KEK file if absent on first boot (passphrase still required)")
	cmd.Flags().DurationVar(&cfg.LuaTimeout, "plugin-lua-timeout", defaultPluginLuaTimeout, "per-invocation CPU deadline for Lua plugins")
	cmd.Flags().IntVar(&cfg.LuaRegistryMaxSize, "plugin-lua-registry-max", defaultPluginLuaRegistryMax, "max Lua registry size per plugin state")
	registerLogSinkFlags(cmd)

	return cmd
}

// registerLogSinkFlags registers the six per-sink logging flags shared by the
// core and gateway commands. The flags are bound into config.LoggingConfig via
// the "logging" config section in each command's RunE.
func registerLogSinkFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("log-stderr", true, "enable stderr log sink")
	cmd.Flags().String("log-stderr-level", "", "stderr log level override (default: global)")
	cmd.Flags().Bool("log-otel", true, "enable OTLP-collector log sink")
	cmd.Flags().String("log-otel-level", "", "collector log level override (default: global)")
	cmd.Flags().Bool("log-sentry", true, "enable Sentry log sink")
	cmd.Flags().String("log-sentry-level", "", "Sentry log level override (default: warn)")
}

// applyLogSinkFlags overlays explicitly-set --log-* flags onto lc, giving CLI
// flags precedence over the YAML `logging` section (spec §5). The flat flag
// names don't map to LoggingConfig's nested koanf keys via posflag, so bind
// them here explicitly.
func applyLogSinkFlags(cmd *cobra.Command, lc *config.LoggingConfig) {
	if cmd.Flags().Changed("log-stderr") {
		lc.Stderr.Enabled, _ = cmd.Flags().GetBool("log-stderr") //nolint:errcheck // flag type is known/registered
	}
	if cmd.Flags().Changed("log-stderr-level") {
		lc.Stderr.Level, _ = cmd.Flags().GetString("log-stderr-level") //nolint:errcheck // flag type is known/registered
	}
	if cmd.Flags().Changed("log-otel") {
		lc.OTel.Enabled, _ = cmd.Flags().GetBool("log-otel") //nolint:errcheck // flag type is known/registered
	}
	if cmd.Flags().Changed("log-otel-level") {
		lc.OTel.Level, _ = cmd.Flags().GetString("log-otel-level") //nolint:errcheck // flag type is known/registered
	}
	if cmd.Flags().Changed("log-sentry") {
		lc.Sentry.Enabled, _ = cmd.Flags().GetBool("log-sentry") //nolint:errcheck // flag type is known/registered
	}
	if cmd.Flags().Changed("log-sentry-level") {
		lc.Sentry.Level, _ = cmd.Flags().GetString("log-sentry-level") //nolint:errcheck // flag type is known/registered
	}
}

// runCoreWithDeps starts and runs the core process using the provided configuration and injectable dependencies.
// It validates configuration, initializes logging and telemetry, ensures database migrations and TLS certificates,
// constructs and starts subsystems under an orchestrator, optionally starts observability, launches the control gRPC server,
// waits for readiness, handles OS signals and context cancellation, and performs a graceful shutdown.
// codecov:ignore — tested by integration and E2E tests
func runCoreWithDeps(ctx context.Context, cfg *coreConfig, gameConfig config.GameConfig, authConfig config.AuthConfig, eventBusConfig eventbus.Config, cryptoConfig config.CryptoConfig, logConfig config.LoggingConfig, cmd *cobra.Command, deps *CoreDeps) error {
	// Stamp the bootstrap start as early as possible — anything after this
	// (config validation, migrations, subsystem starts) shows up as part of
	// the "process.startup" span's duration when emitted at the ready point.
	bootStart := time.Now()

	if deps == nil {
		deps = &CoreDeps{}
	}

	// Apply defaults for injectable infrastructure dependencies.
	deps.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return oops.Code("CONFIG_INVALID").With("operation", "validate configuration").Wrap(err)
	}

	// --- 1. Logging (phase 1: stderr-only) + telemetry ---
	level, err := resolveLogLevel(cmd)
	if err != nil {
		return err
	}
	stderrLevel := logConfig.Stderr.EffectiveLevel(level)
	logging.SetDefaultWithBridge("holomush-core", version, cfg.LogFormat, logConfig.Stderr.Enabled, stderrLevel, nil, level)

	res, telErr := telemetry.Init(ctx, "holomush-core", version, logConfig, level)
	if telErr != nil {
		return oops.Code("TELEMETRY_INIT_FAILED").Wrap(telErr)
	}
	// Phase 2: re-seat the default logger with the OTel bridge when present.
	if res.LogHandler != nil {
		logging.SetDefaultWithBridge("holomush-core", version, cfg.LogFormat, logConfig.Stderr.Enabled, stderrLevel, res.LogHandler, res.LogBridgeLevel)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := res.Shutdown(shutdownCtx); shutdownErr != nil {
			slog.WarnContext(shutdownCtx, "telemetry shutdown error", "error", shutdownErr)
		}
	}()

	slog.InfoContext(
		ctx,
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

	// --- 3. Database subsystem ---
	// Construction only — no live resources. The DB subsystem's own Start
	// (invoked by orch.StartAll below) opens the pool and resolves the
	// gameID (InitGameID); every gameID consumer resolves through
	// gameIDProvider below instead of a hand-sequenced pre-start (07-09).
	dbSub := store.NewSubsystem(store.SubsystemConfig{
		DatabaseURL: databaseURL,
	})

	// gameIDProvider is THE single gameID resolution + override site
	// (07-09 item 7): every consumer in this process — TLS, world, plugin,
	// outbox relay, cluster, audit DLQ, the EventBus itself, and the
	// cryptoWiring block — resolves the game id through this closure at its
	// own Start, never eagerly here. cfg.GameID wins when explicitly set;
	// otherwise it defers to dbSub.GameID(), which panics before the
	// database subsystem's Start has run — that panic-before-Start guard is
	// the point: no gameID consumer may resolve it before Database starts.
	gameIDProvider := func() string {
		if cfg.GameID != "" {
			return cfg.GameID
		}
		return dbSub.GameID()
	}

	// --- 4. TLS certificates ---
	// TLSSubsystem resolves the gameID (via gameIDProvider) and generates
	// or loads the certificates inside its own Start, declaring
	// DependsOn(SubsystemDatabase) — the real edge that used to be a
	// hand-sequenced DB pre-start (core.go pre-07-09).
	certsDir, err := deps.CertsDirGetter()
	if err != nil {
		return oops.Code("CERTS_DIR_FAILED").With("operation", "get certs directory").Wrap(err)
	}

	tlsSub := tlscerts.NewTLSSubsystem(tlscerts.TLSSubsystemConfig{
		CertsDir:    certsDir,
		GameID:      gameIDProvider,
		CertEnsurer: deps.TLSCertEnsurer,
	})

	// Derive admin socket paths from XDG runtime dir. Non-fatal: if the
	// runtime dir is unavailable, the admin socket is disabled (break-glass
	// unavailable) but the server continues serving. AdminSocketSubsystem.Start
	// is a no-op when SocketPath is empty.
	var adminSocketPath, adminLockPath string
	if runtimeDir, rdErr := xdg.RuntimeDir(); rdErr != nil {
		slog.WarnContext(ctx, "admin socket disabled: cannot determine XDG runtime dir; break-glass unavailable",
			"error", rdErr)
	} else if ensureErr := xdg.EnsureDir(runtimeDir); ensureErr != nil {
		slog.WarnContext(ctx, "admin socket disabled: cannot create XDG runtime dir; break-glass unavailable",
			"path", runtimeDir, "error", ensureErr)
	} else {
		adminSocketPath = filepath.Join(runtimeDir, "admin.sock")
		adminLockPath = filepath.Join(runtimeDir, "admin.lock")
	}

	// --- 5. Parse session configuration ---
	sessionTTL, reaperInterval, leaseTTL, bootGrace, err := parseSessionConfig(cfg)
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
		slog.InfoContext(ctx, "observability server started", "addr", obsServer.Addr())
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if stopErr := obsServer.Stop(shutdownCtx); stopErr != nil {
				slog.WarnContext(shutdownCtx, "error stopping observability server", "error", stopErr)
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

	// Filter crypto.dual_control_required against the known op_kind registry.
	// Lax+warn: unknown op_kinds emit a structured warning and are excluded
	// from enforcement; the server continues to start. Per spec §9. This is
	// a pure config filter (no live reads) — it stays in runCore.
	validatedDualControl := validateDualControlRequired(cryptoConfig.DualControlRequired, slog.Default())

	// abacSub validates the RAW configured crypto.operator allow-list
	// against the players table INSIDE its own Start (07-09 item 6) — lax+
	// warn (INV-B5/INV-B7); unknown IDs and transient PG failures WARN but
	// MUST NOT gate startup. Routing this through the cryptoWiring builder
	// instead would make ABAC a wiring consumer and close a second
	// ABAC -> cryptoWiring -> ABAC cycle (THE RULE forces every consumer to
	// DependsOn SubsystemABAC) — cross-AI round 4, BLOCKER.
	abacSub := abacsetup.NewABACSubsystem(abacsetup.ABACSubsystemConfig{
		DB:              dbSub,
		Registry:        registry,
		CryptoOperators: cryptoConfig.Operators,
	})

	authSub := authsetup.NewAuthSubsystem(authsetup.AuthSubsystemConfig{
		DB:                   dbSub,
		MaxSessionsPerPlayer: authConfig.MaxPlayerSessionsPerPlayer,
	})

	worldSub := worldsetup.NewWorldSubsystem(worldsetup.WorldSubsystemConfig{
		DB:     dbSub,
		ABAC:   abacSub,
		GameID: gameIDProvider,
	})

	sessionSub := sessionsetup.NewSessionSubsystem(sessionsetup.SessionSubsystemConfig{
		DB: dbSub,
	})

	// Create stream registry early so it can be shared between the plugin
	// subsystem (hostfunc) and the gRPC subsystem (CoreServer + the
	// capability-scoped host services).
	streamRegistry := holoGRPC.NewSessionStreamRegistry()

	pluginSub := pluginsetup.NewPluginSubsystem(pluginsetup.PluginSubsystemConfig{
		DataDir:            cfg.DataDir,
		DatabaseConnStr:    databaseURL,
		CertsDir:           certsDir,
		GameID:             gameIDProvider,
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
	//
	// rawBusGameID captures the koanf event_bus.game_id value BEFORE
	// Defaults() substitutes "main" for an empty GameID — after Defaults an
	// explicit value is indistinguishable from the substituted default
	// (round 8 correction: event_bus.game_id IS a live boot-path koanf read
	// today; post-plan the global gameIDProvider supersedes it here, but an
	// explicitly-set value must never be silently discarded — it WARNs
	// instead). GameIDProvider is a wiring-only field (not koanf-mapped)
	// that wins over the koanf GameID/Defaults() substitution, resolved
	// once at the top of Subsystem.Start (07-09 item 7, round 7 BLOCKER 1).
	rawBusGameID := eventBusConfig.GameID
	eventBusConfig = eventBusConfig.Defaults()
	eventBusConfig.GameIDProvider = func() string {
		resolved := gameIDProvider()
		if rawBusGameID != "" && rawBusGameID != resolved {
			//nolint:sloglint // no-ctx closure (GameIDProvider is func() string, matching
			// eventbus.Config's field signature) — the core.go:1122-class bare-Warn carve-out
			// for "no ctx exists and one cannot reasonably be plumbed" (.claude/rules/logging.md).
			slog.Warn("event_bus.game_id config key is set but does not match the resolved game id; the global game_id (or database-derived id) wins — remove event_bus.game_id or align it with the global game_id",
				"event_bus_game_id", rawBusGameID,
				"resolved_game_id", resolved)
		}
		return resolved
	}
	eventBusSub := eventbus.NewSubsystem(eventBusConfig)

	// OutboxRelaySubsystem (MODEL-04, 05-07): the single leased relay that drains
	// world-change outbox rows to JetStream. Constructed with dbSub + eventBusSub,
	// DependsOn Database + EventBus, registered in productionSubsystems after
	// eventBusSub so start ordering respects the dependency.
	outboxRelaySub := worldsetup.NewOutboxRelaySubsystem(worldsetup.OutboxRelaySubsystemConfig{
		DB:       dbSub,
		EventBus: eventBusSub,
		GameID:   gameIDProvider,
	})

	// Phase 3c (holomush-ojw1.3): cluster.Registry runs in every deployment
	// from this PR onward; it provides cross-replica health/status surface
	// and (when DEK pipeline activates at Phase 3d) the failure-remediation
	// substrate for cache invalidation. ProductionPill is wired here;
	// dev/test wirings live in their respective entry points. ProductionPill
	// terminates the process with os.Exit(125), so production deployments
	// MUST run under a supervisor that interprets exit code 125 as
	// restart-eligible (systemd Restart=on-failure, k8s restartPolicy=Always,
	// docker restart=on-failure).
	// Register cluster metrics on the observability server's own registry (the
	// one /metrics serves) rather than prometheus.DefaultRegisterer, which the
	// endpoint does NOT serve — otherwise these metrics are silently unscraped
	// (holomush-cluster-smoke relies on cluster_member_skew_seconds appearing on
	// /metrics to prove two-member convergence). Falls back to the default
	// registry when metrics are disabled (obsServer == nil).
	metricsReg := prometheus.DefaultRegisterer
	if obsServer != nil {
		metricsReg = obsServer.Registerer()
	}
	// Register the audit collectors (projection_lag_seconds,
	// projection_plugin_owned_skipped_total, dlq_messages_total) on the SAME
	// served registry so the DLQ alerting story (D-11) actually reaches
	// /metrics. Without this the counters increment in-process but are never
	// scraped — the same "silently unscraped" defect the cluster_*/invalidation_*
	// metrics above avoid by routing to obsServer.Registerer().
	audit.RegisterMetrics(metricsReg)
	clusterPillMetrics := cluster.NewPillMetrics(metricsReg)
	clusterSkewMetrics := cluster.NewSkewMetrics(metricsReg)
	clusterSelfTimeoutMetrics := cluster.NewSelfTimeoutMetrics(metricsReg)
	clusterHeartbeatMetrics := cluster.NewHeartbeatMetrics(metricsReg)
	clusterDuplicateIDMetrics := cluster.NewDuplicateMemberIDMetrics(metricsReg)
	clusterSelfID := cluster.MemberID(idgen.New().String())
	clusterPill := cluster.NewProductionPill(clusterSelfID, slog.Default(), clusterPillMetrics)
	clusterSub, clusterErr := cluster.NewSubsystem(cluster.Config{
		ClusterIDProvider: gameIDProvider,
		HolomushVersion:   version, // package-private to main; ldflag-set via -X
	}, cluster.Deps{
		ConnProvider:      cluster.ConnProviderFunc(func() natsconn.Conn { return eventBusSub.Conn() }),
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
		return oops.Code("CLUSTER_SUBSYSTEM_INIT_FAILED").Wrap(clusterErr)
	}

	// AuditProjection drains the EVENTS stream into events_audit so every
	// published message lands in the forever-archive PostgreSQL table.
	// Depends on DB (target table), EventBus (JetStream source), and
	// Plugins (F5: per-plugin consumers need plugin manifests + gRPC
	// clients); the orchestrator enforces that ordering via DependsOn.
	// DLQ retention/subject come from the event_bus section (D-12). The
	// dead-letter subject nests under the CLUSTER-02 `internal.>` granted
	// prefix so it stays within the holomush-server account's permissions.
	auditSub := audit.NewSubsystem(eventBusSub, dbSub, audit.Config{
		DLQ: audit.DLQConfig{
			// SubjectProvider resolves the gameID through the SAME
			// gameIDProvider closure every other consumer uses, so the DLQ
			// subject matches the rest of the process (07-09 item 7). The
			// format string stays in package main, matching the replay
			// CLI's shape (cmd_audit.go's dlqConfigForGame) so the WR-06
			// same-subject contract is preserved by inspection. Resolved
			// once inside audit.Subsystem.Start, before config validation.
			SubjectProvider: func() string {
				return fmt.Sprintf("internal.%s.audit.dlq", gameIDProvider())
			},
			MaxAge:   eventBusConfig.DLQ.MaxAge,
			MaxBytes: eventBusConfig.DLQ.MaxBytes,
		},
		// events_audit retention (OPS-02 / D-02). Zero values defer to the
		// audit subsystem's DefaultRetainWindow (90d) / DefaultPurgeInterval
		// (24h) via Config.Defaults(); a negative value is rejected by
		// Config.Validate() at Start before the projection accepts traffic.
		RetainWindow:  eventBusConfig.Audit.RetainWindow,
		PurgeInterval: eventBusConfig.Audit.PurgeInterval,
	})

	// Phase 7 INV-CRYPTO-45: build the codec.KeySelector ONCE at boot. The
	// SAME pointer-identity instance is threaded into BOTH the
	// PluginConsumerManager (audit closure below at the
	// audit.NewPluginConsumerManager call) AND the history.Reader
	// (via grpcSubsystemConfig.KeySelector → newHistoryReader →
	// history.WithCodecSelector). Pointer-identity is asserted by
	// TestDispatcherAndHotTierShareSelector.
	pluginCodecKeySelector := cryptowiring.KeySelector()

	// coordHolderPtr is the late-bound indirection through which the
	// Manager's Invalidator closure and the Orchestrator's Phase5Coordinator
	// reach the invalidation.Coordinator, which the cryptoWiring builder
	// constructs+starts as a side effect (cryptowiring.go). grpcSubsystem
	// owns its lifecycle (07-09 item 4): grpcSub.Stop calls
	// coordHolderPtr.coord.Stop when non-nil.
	coordHolderPtr := &coordHolder{}

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
		// INV-CRYPTO-45: thread the boot-time pluginCodecKeySelector into the
		// PluginConsumerManager. The SAME instance is also passed to the
		// gRPC subsystem (grpcSubsystemConfig.KeySelector) for
		// history.NewReader's WithCodecSelector — pointer-identity is
		// asserted by TestDispatcherAndHotTierShareSelector.
		pcm := audit.NewPluginConsumerManager(js, audit.WithKeySelector(pluginCodecKeySelector))
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
		TLSProvider:    tlsSub.TLSConfig,
		CoordHolder:    coordHolderPtr,
		SessionTTL:     sessionTTL,
		ReaperInterval: reaperInterval,
		LeaseTTL:       leaseTTL,
		BootGrace:      bootGrace,
		MaxHistory:     cfg.SessionMaxHistory,
		GameConfig:     gameConfig,
		StreamRegistry: streamRegistry,
		VerbRegistry:   verbRegistry,
		// Phase 7 INV-CRYPTO-45: SAME selector instance the audit closure
		// passes into PluginConsumerManager. history.NewReader gets it
		// via newHistoryReader's WithCodecSelector branch.
		KeySelector: pluginCodecKeySelector,
	})

	// --- Crypto subsystems (T22 / holomush-jxo8.6.21; generalized holomush-jxo8.7.8) ---
	// The block that used to run here eagerly (auditChainRepo, admin-handler
	// construction, rekey/readstream wiring, the invalidation.Coordinator)
	// is now the memoized cryptoWiring builder (cryptowiring.go) — every
	// dbSub.Pool()/authSub.Hasher()/abacSub.Resolver()/eventBusSub.Conn()/
	// eventBusSub.Publisher() read it contains executes lazily, inside the
	// first wiring consumer's Start, never here on runCore's straight-line
	// path (D-09 / 07-09 item 9). THE RULE: every consumer below declares
	// DependsOn ⊇ {Database, Auth, ABAC, EventBus} — the first consumer to
	// resolve cryptoWiringFn is the one that builds it.
	cryptoCfg := cryptoConfig.Defaults()
	cryptoWiringFn := resolveCryptoWiring(cryptoWiringInputs{
		DB:                   dbSub,
		Auth:                 authSub,
		ABAC:                 abacSub,
		EventBus:             eventBusSub,
		Cluster:              clusterSub,
		GameID:               gameIDProvider,
		DataDir:              cfg.DataDir,
		AutoGenKEK:           cfg.AutoGenKEK,
		CryptoCfg:            cryptoCfg,
		ValidatedDualControl: validatedDualControl,
		VerbRegistry:         verbRegistry,
		MetricsRegisterer:    metricsReg,
		CoordHolder:          coordHolderPtr,
	})

	// Thread the memoized wiring into the gRPC subsystem — the one consumer
	// in package main, so it may hold func() (*cryptoWiring, error) directly
	// rather than a narrow consumer-owned provider type (round 5, BLOCKER 4).
	// Resolved inside grpcSubsystem.Prepare. Per cryptoWiringInputs.CoordHolder's
	// doc, grpcSubsystem is not necessarily the first caller of cryptoWiringFn —
	// whichever wiring consumer's Prepare runs earliest in topological order is —
	// but every caller's resolution happens during the Prepare sweep, so the
	// Coordinator's construction+Start (a side effect of this builder) is
	// always confined there, before any Activate.
	grpcSub.cfg.CryptoWiring = cryptoWiringFn

	// --- The block's three lifecycle-subsystem CONSTRUCTIONS stay on
	// runCore's straight-line path (round 7, BLOCKER 2 — a builder first
	// invoked by a consumer's Start cannot also create that consumer). Each
	// takes a provider-shaped config whose closure projects off the
	// memoized cryptoWiringFn; the constructors allocate nothing and read
	// no live value.

	// CryptoPolicySubsystem emits the current policy snapshot after AuditProjection.
	cryptoPolicySub := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
		EmitDepsProvider: func() (policy.EmitDeps, error) {
			w, wErr := cryptoWiringFn()
			if wErr != nil {
				return policy.EmitDeps{}, wErr
			}
			return w.policyEmitDeps, nil
		},
		PolicyNames: []string{"dual_control_required"},
	})

	// The sweep subsystem auto-aborts non-terminal checkpoints whose
	// last_heartbeat_at has exceeded TTL (INV-CRYPTO-105 / INV-CRYPTO-106,
	// spec §6.2). TTL/Interval are pure config (cryptoCfg); only the
	// storage (Repo/AuditEmitter) is provider-backed.
	rekeyCheckpointSweepSub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
		DepsProvider: func() (*dek.CheckpointRepo, dek.AuditEmitter, error) {
			w, wErr := cryptoWiringFn()
			if wErr != nil {
				return nil, nil, wErr
			}
			return w.checkpointRepo, w.checkpointAuditEmitter, nil
		},
		Logger:   slog.Default(),
		TTL:      cryptoCfg.RekeyCheckpointTTL,
		Interval: cryptoCfg.RekeyCheckpointSweepInterval,
	})

	// auditchain.VerifierSubsystem walks every registered hash chain at boot
	// time (RekeyChain + policy_set + operator_read, when readstream wiring
	// succeeds). NewVerifier(repo) is constructed inside Start once
	// RepoProvider resolves — no pool exists at construction time post-plan.
	cryptoChainVerifierSub := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
		RepoProvider: func() (chain.Repo, error) {
			w, wErr := cryptoWiringFn()
			if wErr != nil {
				return nil, wErr
			}
			return w.chainRepo, nil
		},
		HandlersProvider: func() []chain.Handler {
			// Safe error-free: the memoized build already succeeded — Start
			// resolves RepoProvider first, and its error aborts the boot
			// before HandlersProvider is ever called.
			w, wErr := cryptoWiringFn()
			if wErr != nil {
				return nil
			}
			return w.chainHandlers
		},
		Logger: slog.Default(),
	})

	// Lifted ahead of admin subsystem construction (holomush-jxo8.9) so the
	// admin socket's post-startup error monitor can cancel the parent ctx on
	// failure, matching obsServer/controlGRPCServer below. The same cancel is
	// reused for the control gRPC server's monitorServerErrors wiring.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	adminSub := socket.NewAdminSocketSubsystem(socket.AdminSocketSubsystemConfig{
		SocketPath: adminSocketPath,
		LockPath:   adminLockPath,
		Version:    version,
		// HandlersProvider resolves all five RPC handlers together, AFTER
		// the disabled-mode (SocketPath == "") early return — a disabled
		// admin socket never triggers the cryptoWiring build (07-09 item 9).
		HandlersProvider: func() (socket.Handlers, error) {
			w, wErr := cryptoWiringFn()
			if wErr != nil {
				return socket.Handlers{}, wErr
			}
			return w.adminHandlers, nil
		},
		Shutdown: func(err error) {
			errutil.LogError(slog.Default(), "admin socket failure — cancelling root context", err)
			cancel()
		},
	})

	// --- 8. Orchestrator: register + start ---
	// Zero subsystem Start calls exist outside orch.StartAll (D-09/D-12 Wave
	// A) — every dependency edge below (including TLS -> Database,
	// Cluster -> Database, gRPC -> TLS/Cluster/CryptoChainVerifier,
	// admin_socket -> CryptoChainVerifier) is expressed as a real DependsOn,
	// resolved by topoSort, never by construction-order or a hand-sequenced
	// pre-start.
	orch := lifecycle.NewOrchestrator()
	for _, sub := range productionSubsystems(productionSubsystemSet{
		Database:             dbSub,
		TLS:                  tlsSub,
		ABAC:                 abacSub,
		Auth:                 authSub,
		World:                worldSub,
		Sessions:             sessionSub,
		Plugins:              pluginSub,
		Bootstrap:            bootstrapSub,
		CryptoChainVerifier:  cryptoChainVerifierSub,
		EventBus:             eventBusSub,
		Cluster:              clusterSub,
		AuditProjection:      auditSub,
		CryptoPolicy:         cryptoPolicySub,
		GRPC:                 grpcSub,
		AdminSocket:          adminSub,
		RekeyCheckpointSweep: rekeyCheckpointSweepSub,
		OutboxRelay:          outboxRelaySub,
	}) {
		orch.Register(sub)
	}

	if orchErr := orch.StartAll(ctx); orchErr != nil {
		return orchErr
	}
	defer func() {
		// The timeout MUST be constructed inside this closure, not at the
		// defer site: Go evaluates deferred call arguments at registration
		// time, so a context.WithTimeout built here at the `defer` keyword
		// would start its 5s timer at boot and already be dead by the time
		// this closure actually runs at shutdown (LOW-7; same in-repo idiom
		// as the telemetry/observability shutdown closures above).
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		orch.StopAll(stopCtx)
	}()

	// --- 9. Readiness gate ---
	readinessCtx, readinessCancel := context.WithTimeout(ctx, 30*time.Second)
	defer readinessCancel()
	if readyErr := registry.WaitReady(readinessCtx); readyErr != nil {
		for id, status := range registry.Status() {
			if !status.Tier.IsReady() {
				slog.ErrorContext(
					ctx,
					"subsystem not ready",
					"subsystem", id.String(),
					"tier", status.Tier.String(),
				)
			}
		}
		// Differentiate parent-context cancellation (e.g., admin-socket Shutdown
		// callback fired per holomush-jxo8.9) from genuine readiness timeout so
		// the operator log surfaces the real cause.
		if errors.Is(ctx.Err(), context.Canceled) {
			return fmt.Errorf("startup aborted by parent context cancellation: %w", readyErr)
		}
		return fmt.Errorf("startup timeout: %w", readyErr)
	}
	startupComplete.Store(true)

	// --- 10. Control server ---
	// ctx/cancel were lifted above (holomush-jxo8.9) so the admin subsystem's
	// Shutdown wiring shares the same cancel function. Reuse them here.
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
	slog.InfoContext(ctx, "control gRPC server started", "addr", cfg.ControlAddr)

	// --- 11. Signal handling ---
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	telemetry.EmitStartupSpan(ctx, "holomush-core", version, bootStart)

	cmd.Println("Core process started")
	// Safe to resolve here: orch.StartAll has returned, so the database
	// subsystem (and therefore dbSub.GameID()) is guaranteed started.
	slog.InfoContext(
		ctx,
		"core process ready",
		"game_id", gameIDProvider(),
		"grpc_addr", cfg.GRPCAddr,
	)

	select {
	case sig := <-sigChan:
		slog.InfoContext(ctx, "received shutdown signal", "signal", sig)
	case <-ctx.Done():
		slog.InfoContext(ctx, "context cancelled, shutting down")
	}

	// --- 12. Graceful shutdown ---
	slog.InfoContext(ctx, "shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := controlGRPCServer.Stop(shutdownCtx); err != nil {
		slog.WarnContext(shutdownCtx, "error stopping control gRPC server", "error", err)
	}

	// Subsystem shutdown is handled by the deferred orchestrator stop
	// closure registered right after StartAll above.

	slog.InfoContext(ctx, "shutdown complete")
	return nil
}

// parseSessionConfig parses and validates session TTL, reaper interval, lease TTL,
// and boot grace from cfg, applying defaults when values are empty. Returns an error
// if parsing fails or any duration is not positive.
func parseSessionConfig(cfg *coreConfig) (sessionTTL, reaperInterval, leaseTTL, bootGrace time.Duration, err error) {
	if cfg.SessionTTL == "" {
		cfg.SessionTTL = "30m"
	}
	sessionTTL, err = time.ParseDuration(cfg.SessionTTL)
	if err != nil {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_ttl").Wrap(err)
	}

	if cfg.SessionReaperInterval == "" {
		cfg.SessionReaperInterval = "30s"
	}
	reaperInterval, err = time.ParseDuration(cfg.SessionReaperInterval)
	if err != nil {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_reaper_interval").Wrap(err)
	}

	if cfg.SessionLeaseTTL == "" {
		cfg.SessionLeaseTTL = "45s"
	}
	leaseTTL, err = time.ParseDuration(cfg.SessionLeaseTTL)
	if err != nil {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_lease_ttl").Wrap(err)
	}

	if cfg.SessionBootGrace == "" {
		cfg.SessionBootGrace = "60s"
	}
	bootGrace, err = time.ParseDuration(cfg.SessionBootGrace)
	if err != nil {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_boot_grace").Wrap(err)
	}

	if sessionTTL <= 0 {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_ttl").Errorf("session TTL must be positive")
	}
	if reaperInterval <= 0 {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_reaper_interval").Errorf("reaper interval must be positive")
	}
	if leaseTTL <= 0 {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_lease_ttl").Errorf("lease TTL must be positive")
	}
	if bootGrace <= 0 {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_boot_grace").Errorf("boot grace must be positive")
	}

	// A live gateway connection only refreshes its lease once per
	// session.DefaultLeaseRefreshInterval (15s). A LeaseTTL/BootGrace below 2× that
	// cadence lets the reaper sweep a healthy connection between refreshes (lease)
	// or before a surviving gateway re-asserts after restart (grace, I-LIVE-4), so
	// reject anything under the floor (holomush-rsoe6.22). Defaults (45s/60s) clear it.
	minLeaseGrace := 2 * session.DefaultLeaseRefreshInterval
	if leaseTTL < minLeaseGrace {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_lease_ttl").
			Errorf("lease TTL must be at least %s (2× the %s gateway refresh cadence) so a healthy connection is not reaped between refreshes", minLeaseGrace, session.DefaultLeaseRefreshInterval)
	}
	if bootGrace < minLeaseGrace {
		return 0, 0, 0, 0, oops.Code("CONFIG_INVALID").With("field", "session_boot_grace").
			Errorf("boot grace must be at least %s (2× the %s gateway refresh cadence) so a surviving gateway can re-assert its leases before the post-restart sweep", minLeaseGrace, session.DefaultLeaseRefreshInterval)
	}

	if cfg.SessionMaxHistory <= 0 {
		cfg.SessionMaxHistory = 500
	}

	return sessionTTL, reaperInterval, leaseTTL, bootGrace, nil
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
			slog.ErrorContext(
				ctx,
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

// productionSubsystemSet names every subsystem registered with the
// orchestrator. Replaces the former 16-position positional parameter list
// (LOW-8 / D-14, 07-09 Task 3): a mis-ordered field is now a compile error
// naming the wrong field, not a silently-accepted positional swap that only
// topoSort's real DependsOn graph happened to defuse.
type productionSubsystemSet struct {
	Database             lifecycle.Subsystem
	TLS                  lifecycle.Subsystem
	ABAC                 lifecycle.Subsystem
	Auth                 lifecycle.Subsystem
	World                lifecycle.Subsystem
	Sessions             lifecycle.Subsystem
	Plugins              lifecycle.Subsystem
	Bootstrap            lifecycle.Subsystem
	CryptoChainVerifier  lifecycle.Subsystem
	EventBus             lifecycle.Subsystem
	Cluster              lifecycle.Subsystem
	AuditProjection      lifecycle.Subsystem
	CryptoPolicy         lifecycle.Subsystem
	GRPC                 lifecycle.Subsystem
	AdminSocket          lifecycle.Subsystem
	RekeyCheckpointSweep lifecycle.Subsystem
	OutboxRelay          lifecycle.Subsystem
}

// productionSubsystems returns the ordered list of subsystems registered
// with the orchestrator. Extracted as a helper so regression tests can
// assert on the production set without spinning up the full server. The
// SLICE order is cosmetic — topoSort (internal/lifecycle/orchestrator.go)
// derives the real start order from each subsystem's DependsOn(); this is
// documentation, not enforcement.
func productionSubsystems(s productionSubsystemSet) []lifecycle.Subsystem {
	return []lifecycle.Subsystem{
		s.Database,
		// TLS declares DependsOn(Database) — the gameID edge that used to be
		// a hand-sequenced DB pre-start (07-09 Task 1).
		s.TLS,
		s.ABAC, s.Auth, s.World,
		s.Sessions, s.Plugins, s.Bootstrap,
		// The verifier's real start order is EventBus first, then
		// CryptoChainVerifier — its handler set is built from
		// eventBusSub.Publisher() (see readStreamW below), so the bus must be
		// up first. This SLICE position is cosmetic per the doc comment
		// above; the asserted order lives in
		// cmd/holomush/core_topo_order_test.go (07-10 Task 3's pin), not here.
		s.CryptoChainVerifier,
		s.EventBus, s.Cluster, s.AuditProjection,
		// CryptoPolicy's real edge is AuditProjection (policy.CryptoPolicySubsystemConfig.DependsOn
		// includes SubsystemAuditProjection) — see core_topo_order_test.go for the asserted order.
		s.CryptoPolicy,
		s.GRPC, s.AdminSocket,
		// Sweep auto-aborts non-terminal rekey checkpoints whose heartbeat
		// has exceeded TTL. Runs after CryptoChainVerifier (chain integrity
		// confirmed), EventBus (audit events route to JetStream), and
		// AuditProjection (emitted events land in events_audit). Per
		// spec §6.2 / Task 28 DependsOn declaration. Sub-epic E T37
		// (holomush-jxo8.7.34).
		s.RekeyCheckpointSweep,
		// OutboxRelaySubsystem (MODEL-04, 05-07): DependsOn Database + EventBus;
		// registered after eventBusSub so the relay starts once the bus is up.
		s.OutboxRelay,
	}
}

// rekeyAuditPublisherAdapter adapts eventbus.Publisher to dek.AuditPublisher,
// the narrow seam consumed by dek.RekeyAuditEmitter. The dek package cannot
// import eventbus (cycle: eventbus → dek), so the bridge lives in the
// wiring layer (cmd/holomush). Sub-epic E T37 (holomush-jxo8.7.34).
//
// PublishAudit mints an event ULID, builds the eventbus.Event from the
// caller-supplied subject + type + payload, and delegates to the underlying
// Publisher (which is RenderingPublisher in production — stamping the
// App-Rendering header required by audit/projection.go::persist). Mirrors
// the pattern used by internal/admin/policy/emitter.go for the
// crypto.policy_set chain.
type rekeyAuditPublisherAdapter struct {
	publisher eventbus.Publisher
	clock     interface{ Now() time.Time }
}

func (a *rekeyAuditPublisherAdapter) PublishAudit(
	ctx context.Context,
	subject, evType string,
	payload []byte,
) (ulid.ULID, error) {
	subj, err := eventbus.NewSubject(subject)
	if err != nil {
		return ulid.ULID{}, oops.Code("DEK_REKEY_AUDIT_INVALID_SUBJECT").
			With("subject", subject).Wrap(err)
	}
	etyp, err := eventbus.NewType(evType)
	if err != nil {
		return ulid.ULID{}, oops.Code("DEK_REKEY_AUDIT_INVALID_TYPE").
			With("type", evType).Wrap(err)
	}
	ev := eventbus.NewEvent(subj, etyp, eventbus.Actor{Kind: eventbus.ActorKindSystem}, payload)
	ev.Timestamp = a.clock.Now() // honour the injected clock rather than time.Now() inside NewEvent
	if pubErr := a.publisher.Publish(ctx, ev); pubErr != nil {
		return ulid.ULID{}, oops.Code("DEK_REKEY_AUDIT_PUBLISH_FAILED").
			With("subject", subject).Wrap(pubErr)
	}
	return ev.ID, nil
}
