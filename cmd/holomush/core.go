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
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	abacsetup "github.com/holomush/holomush/internal/access/setup"
	authsetup "github.com/holomush/holomush/internal/auth/setup"
	"github.com/holomush/holomush/internal/bootstrap"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/logging"
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	"github.com/holomush/holomush/internal/session"
	sessionsetup "github.com/holomush/holomush/internal/session/setup"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telemetry"
	tlscerts "github.com/holomush/holomush/internal/tls"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	worldsetup "github.com/holomush/holomush/internal/world/setup"
	"github.com/holomush/holomush/internal/xdg"
)

// coreConfig holds configuration for the core command.
type coreConfig struct {
	GRPCAddr              string `koanf:"grpc_addr"`
	ControlAddr           string `koanf:"control_addr"`
	MetricsAddr           string `koanf:"metrics_addr"`
	DataDir               string `koanf:"data_dir"`
	GameID                string `koanf:"game_id"`
	LogFormat             string `koanf:"log_format"`
	SkipSeedMigrations    bool   `koanf:"skip_seed_migrations"`
	SessionTTL            string `koanf:"session_ttl"`
	SessionMaxHistory     int    `koanf:"session_max_history"`
	SessionReaperInterval string `koanf:"session_reaper_interval"`
	Setting               string `koanf:"setting"`
	ResetSetting          bool   `koanf:"reset_setting"`
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
	return nil
}

// Default values for core command flags.
const (
	defaultGRPCAddr        = "localhost:9000"
	defaultCoreControlAddr = "127.0.0.1:9001"
	defaultCoreMetricsAddr = "127.0.0.1:9100"
	defaultLogFormat       = "json"
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
			return runCoreWithDeps(cmd.Context(), cfg, gameConfig, cmd, nil)
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

	return cmd
}

// runCoreWithDeps starts and runs the core process using the provided configuration and injectable dependencies.
// It validates configuration, initializes logging and telemetry, ensures database migrations and TLS certificates,
// constructs and starts subsystems under an orchestrator, optionally starts observability, launches the control gRPC server,
// waits for readiness, handles OS signals and context cancellation, and performs a graceful shutdown.
// codecov:ignore — tested by integration and E2E tests
func runCoreWithDeps(ctx context.Context, cfg *coreConfig, gameConfig config.GameConfig, cmd *cobra.Command, deps *CoreDeps) error {
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

	slog.Info("starting core process",
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

	// --- 7. Subsystem construction (config only, no live resources) ---
	abacSub := abacsetup.NewABACSubsystem(abacsetup.ABACSubsystemConfig{
		DB:       dbSub,
		Registry: registry,
	})

	authSub := authsetup.NewAuthSubsystem(authsetup.AuthSubsystemConfig{
		DB: dbSub,
	})

	worldSub := worldsetup.NewWorldSubsystem(worldsetup.WorldSubsystemConfig{
		DB:   dbSub,
		ABAC: abacSub,
	})

	sessionSub := sessionsetup.NewSessionSubsystem(sessionsetup.SessionSubsystemConfig{
		DB: dbSub,
	})

	pluginSub := pluginsetup.NewPluginSubsystem(pluginsetup.PluginSubsystemConfig{
		DataDir:         cfg.DataDir,
		DatabaseConnStr: databaseURL,
		ABAC:            abacSub,
		PolicyInst:      abacSub,
		PluginProv:      abacSub,
		World:           worldSub,
		Sessions:        &sessionBridge{sub: sessionSub},
		AdminDeps:       &adminDepsBridge{auth: authSub, db: dbSub},
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

	grpcSub := newGRPCSubsystem(grpcSubsystemConfig{
		DB:             dbSub,
		ABAC:           abacSub,
		Auth:           authSub,
		World:          worldSub,
		Plugins:        pluginSub,
		Sessions:       sessionSub,
		Bootstrap:      bootstrapSub,
		GRPCAddr:       cfg.GRPCAddr,
		TLSConfig:      tlsConfig,
		SessionTTL:     sessionTTL,
		ReaperInterval: reaperInterval,
		MaxHistory:     cfg.SessionMaxHistory,
		GameConfig:     gameConfig,
	})

	// --- 8. Orchestrator: register + start ---
	// The database subsystem was pre-started (step 3) because the gameID must be
	// available before TLS cert generation. Its Start() is idempotent, so the
	// orchestrator will skip reconnection. It remains registered so other subsystems
	// resolve their SubsystemDatabase dependency and so StopAll shuts it down last.
	orch := lifecycle.NewOrchestrator()
	for _, sub := range []lifecycle.Subsystem{
		dbSub, abacSub, authSub, worldSub,
		sessionSub, pluginSub, bootstrapSub, grpcSub,
	} {
		orch.Register(sub)
	}

	if orchErr := orch.StartAll(ctx); orchErr != nil {
		return orchErr
	}
	defer orch.StopAll(context.Background())

	// --- 9. Readiness gate ---
	readinessCtx, readinessCancel := context.WithTimeout(ctx, 30*time.Second)
	defer readinessCancel()
	if readyErr := registry.WaitReady(readinessCtx); readyErr != nil {
		for id, status := range registry.Status() {
			if !status.Tier.IsReady() {
				slog.Error("subsystem not ready",
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
	slog.Info("core process ready",
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
			slog.Error("server error, triggering shutdown",
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
