// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	cryptotls "crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/holomush/holomush/internal/control"
	"github.com/holomush/holomush/internal/core"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/observability"
	corev1 "github.com/holomush/holomush/internal/proto/holomush/core/v1"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/internal/xdg"
)

// coreConfig holds configuration for the core command.
type coreConfig struct {
	grpcAddr    string
	controlAddr string
	metricsAddr string
	dataDir     string
	gameID      string
	logFormat   string
}

// Validate checks that the configuration is valid.
func (cfg *coreConfig) Validate() error {
	if cfg.grpcAddr == "" {
		return fmt.Errorf("grpc-addr is required")
	}
	if cfg.controlAddr == "" {
		return fmt.Errorf("control-addr is required")
	}
	if cfg.logFormat != "json" && cfg.logFormat != "text" {
		return fmt.Errorf("log-format must be 'json' or 'text', got %q", cfg.logFormat)
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
			return runCoreWithDeps(cmd.Context(), cfg, cmd, nil)
		},
	}

	cmd.Flags().StringVar(&cfg.grpcAddr, "grpc-addr", defaultGRPCAddr, "gRPC listen address")
	cmd.Flags().StringVar(&cfg.controlAddr, "control-addr", defaultCoreControlAddr, "control gRPC listen address with mTLS")
	cmd.Flags().StringVar(&cfg.metricsAddr, "metrics-addr", defaultCoreMetricsAddr, "metrics/health HTTP address (empty = disabled)")
	cmd.Flags().StringVar(&cfg.dataDir, "data-dir", "", "data directory (default: XDG_DATA_HOME/holomush)")
	cmd.Flags().StringVar(&cfg.gameID, "game-id", "", "game ID (default: auto-generated from database)")
	cmd.Flags().StringVar(&cfg.logFormat, "log-format", defaultLogFormat, "log format (json or text)")

	return cmd
}

// runCoreWithDeps starts the core process with injectable dependencies.
// If deps is nil, default implementations are used.
func runCoreWithDeps(ctx context.Context, cfg *coreConfig, cmd *cobra.Command, deps *CoreDeps) error {
	if deps == nil {
		deps = &CoreDeps{}
	}

	// Set up default factories
	if deps.EventStoreFactory == nil {
		deps.EventStoreFactory = func(ctx context.Context, url string) (EventStore, error) {
			return store.NewPostgresEventStore(ctx, url)
		}
	}
	if deps.TLSCertEnsurer == nil {
		deps.TLSCertEnsurer = ensureTLSCerts
	}
	if deps.ControlTLSLoader == nil {
		deps.ControlTLSLoader = control.LoadControlServerTLS
	}
	if deps.ControlServerFactory == nil {
		deps.ControlServerFactory = func(component string, shutdownFunc control.ShutdownFunc) (ControlServer, error) {
			return control.NewGRPCServer(component, shutdownFunc)
		}
	}
	if deps.ObservabilityServerFactory == nil {
		deps.ObservabilityServerFactory = func(addr string, readinessChecker observability.ReadinessChecker) ObservabilityServer {
			return observability.NewServer(addr, readinessChecker)
		}
	}
	if deps.CertsDirGetter == nil {
		deps.CertsDirGetter = xdg.CertsDir
	}
	if deps.DatabaseURLGetter == nil {
		deps.DatabaseURLGetter = func() string {
			return os.Getenv("DATABASE_URL")
		}
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	if err := setupLogging(cfg.logFormat); err != nil {
		return fmt.Errorf("failed to set up logging: %w", err)
	}

	slog.Info("starting core process",
		"grpc_addr", cfg.grpcAddr,
		"log_format", cfg.logFormat,
	)

	databaseURL := deps.DatabaseURLGetter()
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}

	eventStore, err := deps.EventStoreFactory(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer eventStore.Close()

	slog.Info("connected to database")

	// Initialize or get game_id
	gameID := cfg.gameID
	if gameID == "" {
		gameID, err = eventStore.InitGameID(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize game ID: %w", err)
		}
	}

	slog.Info("game ID initialized", "game_id", gameID)

	certsDir, err := deps.CertsDirGetter()
	if err != nil {
		return fmt.Errorf("failed to get certs directory: %w", err)
	}

	// Generate or load TLS certificates
	tlsConfig, err := deps.TLSCertEnsurer(certsDir, gameID)
	if err != nil {
		return fmt.Errorf("failed to set up TLS: %w", err)
	}

	slog.Info("TLS certificates ready", "certs_dir", certsDir)

	// For testing, we need the event store to be *store.PostgresEventStore
	// to pass to core.NewEngine. In production, this is always the case.
	// In tests with mocks, we skip the gRPC server creation.
	var grpcServer *grpc.Server
	var listener net.Listener

	// Only create gRPC server if we have a real event store
	if realStore, ok := eventStore.(*store.PostgresEventStore); ok {
		sessions := core.NewSessionManager()
		broadcaster := core.NewBroadcaster()
		engine := core.NewEngine(realStore, sessions, broadcaster)

		// Create gRPC server
		creds := credentials.NewTLS(tlsConfig)
		grpcServer = grpc.NewServer(grpc.Creds(creds))

		// Create and register Core service
		coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster)
		corev1.RegisterCoreServer(grpcServer, coreServer)

		// Start gRPC listener
		listener, err = net.Listen("tcp", cfg.grpcAddr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", cfg.grpcAddr, err)
		}
		defer func() {
			if closeErr := listener.Close(); closeErr != nil {
				slog.Debug("error closing gRPC listener", "error", closeErr)
			}
		}()

		slog.Info("gRPC server listening", "addr", cfg.grpcAddr)
	}

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start control gRPC server (always enabled)
	controlTLSConfig, tlsErr := deps.ControlTLSLoader(certsDir, "core")
	if tlsErr != nil {
		return fmt.Errorf("failed to load control TLS config: %w", tlsErr)
	}

	controlGRPCServer, err := deps.ControlServerFactory("core", func() { cancel() })
	if err != nil {
		return fmt.Errorf("failed to create control gRPC server: %w", err)
	}
	controlErrChan, err := controlGRPCServer.Start(cfg.controlAddr, controlTLSConfig)
	if err != nil {
		return fmt.Errorf("failed to start control gRPC server: %w", err)
	}
	// Monitor control server errors in background - cancel context on error
	go monitorServerErrors(ctx, cancel, controlErrChan, "control-grpc")

	slog.Info("control gRPC server started", "addr", cfg.controlAddr)

	// Start observability server if configured
	var obsServer ObservabilityServer
	if cfg.metricsAddr != "" {
		// For core, we're ready once we reach this point (database is connected,
		// listener is bound, core components initialized)
		obsServer = deps.ObservabilityServerFactory(cfg.metricsAddr, func() bool { return true })
		obsErrChan, err := obsServer.Start()
		if err != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if stopErr := controlGRPCServer.Stop(shutdownCtx); stopErr != nil {
				slog.Warn("failed to stop control gRPC server during cleanup", "error", stopErr)
			}
			return fmt.Errorf("failed to start observability server: %w", err)
		}
		// Monitor observability server errors - cancel context on error
		go monitorServerErrors(ctx, cancel, obsErrChan, "observability")
		slog.Info("observability server started", "addr", obsServer.Addr())
	}

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Start gRPC server in goroutine (only if we have one)
	errChan := make(chan error, 1)
	if grpcServer != nil && listener != nil {
		go func() {
			if serveErr := grpcServer.Serve(listener); serveErr != nil {
				errChan <- serveErr
			}
		}()
	}

	cmd.Println("Core process started")
	slog.Info("core process ready",
		"game_id", gameID,
		"grpc_addr", cfg.grpcAddr,
	)

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		slog.Info("received shutdown signal", "signal", sig)
	case err := <-errChan:
		return fmt.Errorf("gRPC server error: %w", err)
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down")
	}

	// Graceful shutdown
	slog.Info("shutting down...")

	// Stop accepting new connections
	if grpcServer != nil {
		grpcServer.GracefulStop()
	}

	// Stop servers
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if obsServer != nil {
		if err := obsServer.Stop(shutdownCtx); err != nil {
			slog.Warn("error stopping observability server", "error", err)
		}
	}

	// Stop control gRPC server
	if err := controlGRPCServer.Stop(shutdownCtx); err != nil {
		slog.Warn("error stopping control gRPC server", "error", err)
	}

	slog.Info("shutdown complete")
	return nil
}

// setupLogging configures the default slog logger.
func setupLogging(format string) error {
	var handler slog.Handler

	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	case "text":
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	default:
		return fmt.Errorf("invalid log format %q: must be 'json' or 'text'", format)
	}

	slog.SetDefault(slog.New(handler))
	return nil
}

// ensureTLSCerts generates or loads TLS certificates.
func ensureTLSCerts(certsDir, gameID string) (*cryptotls.Config, error) {
	// Check if certificate files exist
	certPath := certsDir + "/core.crt"
	keyPath := certsDir + "/core.key"
	caPath := certsDir + "/root-ca.crt"

	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)
	caExists := fileExists(caPath)

	// If any certificate files exist, try to load them
	// If loading fails (corruption, permission issues, etc.), return the error
	// rather than silently regenerating certificates
	if certExists || keyExists || caExists {
		existingConfig, err := tls.LoadServerTLS(certsDir, "core")
		if err != nil {
			return nil, fmt.Errorf("failed to load existing TLS certificates: %w", err)
		}
		return existingConfig, nil
	}

	// No certificate files exist, generate new ones
	slog.Info("generating TLS certificates", "certs_dir", certsDir)

	// Ensure directory exists
	if err := xdg.EnsureDir(certsDir); err != nil {
		return nil, fmt.Errorf("failed to create certs directory: %w", err)
	}

	// Generate CA
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA: %w", err)
	}

	// Generate server certificate
	serverCert, err := tls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		return nil, fmt.Errorf("failed to generate server certificate: %w", err)
	}

	// Save certificates
	if err := tls.SaveCertificates(certsDir, ca, serverCert); err != nil {
		return nil, fmt.Errorf("failed to save certificates: %w", err)
	}

	// Generate gateway client certificate
	gatewayCert, err := tls.GenerateClientCert(ca, "gateway")
	if err != nil {
		return nil, fmt.Errorf("failed to generate gateway certificate: %w", err)
	}

	if err := tls.SaveClientCert(certsDir, gatewayCert); err != nil {
		return nil, fmt.Errorf("failed to save gateway certificate: %w", err)
	}

	slog.Info("TLS certificates generated")

	// Load the newly generated certificates
	config, err := tls.LoadServerTLS(certsDir, "core")
	if err != nil {
		return nil, fmt.Errorf("failed to load generated certificates: %w", err)
	}
	return config, nil
}

// fileExists returns true if the file exists, false otherwise.
// Permission errors are treated as "file exists" to avoid silently
// overwriting files we can't read.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// monitorServerErrors monitors a server's error channel and cancels the context on error.
// This ensures that server failures trigger graceful shutdown of the entire process.
// It exits when either an error is received, the channel is closed, or the context is cancelled.
func monitorServerErrors(ctx context.Context, cancel context.CancelFunc, errCh <-chan error, serverName string) {
	select {
	case err, ok := <-errCh:
		if !ok {
			// Channel closed, server stopped gracefully
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
		// Context cancelled, exit monitoring
	}
}
