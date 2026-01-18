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
			return runCore(cmd.Context(), cfg, cmd)
		},
	}

	// Register flags
	cmd.Flags().StringVar(&cfg.grpcAddr, "grpc-addr", defaultGRPCAddr, "gRPC listen address")
	cmd.Flags().StringVar(&cfg.controlAddr, "control-addr", defaultCoreControlAddr, "control gRPC listen address with mTLS")
	cmd.Flags().StringVar(&cfg.metricsAddr, "metrics-addr", defaultCoreMetricsAddr, "metrics/health HTTP address (empty = disabled)")
	cmd.Flags().StringVar(&cfg.dataDir, "data-dir", "", "data directory (default: XDG_DATA_HOME/holomush)")
	cmd.Flags().StringVar(&cfg.gameID, "game-id", "", "game ID (default: auto-generated from database)")
	cmd.Flags().StringVar(&cfg.logFormat, "log-format", defaultLogFormat, "log format (json or text)")

	return cmd
}

// runCore starts the core process.
func runCore(ctx context.Context, cfg *coreConfig, cmd *cobra.Command) error {
	// Set up logging
	if err := setupLogging(cfg.logFormat); err != nil {
		return fmt.Errorf("failed to set up logging: %w", err)
	}

	slog.Info("starting core process",
		"grpc_addr", cfg.grpcAddr,
		"log_format", cfg.logFormat,
	)

	// Get database URL
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}

	// Connect to database
	eventStore, err := store.NewPostgresEventStore(ctx, databaseURL)
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

	// Get certs directory
	certsDir, err := xdg.CertsDir()
	if err != nil {
		return fmt.Errorf("failed to get certs directory: %w", err)
	}

	// Generate or load TLS certificates
	tlsConfig, err := ensureTLSCerts(certsDir, gameID)
	if err != nil {
		return fmt.Errorf("failed to set up TLS: %w", err)
	}

	slog.Info("TLS certificates ready", "certs_dir", certsDir)

	// Create core components
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(eventStore, sessions, broadcaster)

	// Create gRPC server
	creds := credentials.NewTLS(tlsConfig)
	grpcServer := grpc.NewServer(grpc.Creds(creds))

	// Create and register Core service
	coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster)
	corev1.RegisterCoreServer(grpcServer, coreServer)

	// Start gRPC listener
	listener, err := net.Listen("tcp", cfg.grpcAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", cfg.grpcAddr, err)
	}

	slog.Info("gRPC server listening", "addr", cfg.grpcAddr)

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start control gRPC server (always enabled)
	controlTLSConfig, tlsErr := control.LoadControlServerTLS(certsDir, "core")
	if tlsErr != nil {
		return fmt.Errorf("failed to load control TLS config: %w", tlsErr)
	}

	controlGRPCServer := control.NewGRPCServer("core", func() { cancel() })
	if err := controlGRPCServer.Start(cfg.controlAddr, controlTLSConfig); err != nil {
		return fmt.Errorf("failed to start control gRPC server: %w", err)
	}

	slog.Info("control gRPC server started", "addr", cfg.controlAddr)

	// Start observability server if configured
	var obsServer *observability.Server
	if cfg.metricsAddr != "" {
		// For core, we're always ready once we reach this point
		// (gRPC server is listening, database is connected)
		obsServer = observability.NewServer(cfg.metricsAddr, func() bool { return true })
		if err := obsServer.Start(); err != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = controlGRPCServer.Stop(shutdownCtx)
			return fmt.Errorf("failed to start observability server: %w", err)
		}
		slog.Info("observability server started", "addr", obsServer.Addr())
	}

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start gRPC server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if serveErr := grpcServer.Serve(listener); serveErr != nil {
			errChan <- serveErr
		}
	}()

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
	grpcServer.GracefulStop()

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
	// Try to load existing certificates
	existingConfig, err := tls.LoadServerTLS(certsDir, "core")
	if err == nil {
		return existingConfig, nil
	}

	// Certificates don't exist, generate new ones
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
