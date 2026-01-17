package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/control"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/internal/xdg"
)

// gatewayConfig holds configuration for the gateway command.
type gatewayConfig struct {
	telnetAddr    string
	coreAddr      string
	controlSocket string
	logFormat     string
}

// Default values for gateway command flags.
const (
	defaultTelnetAddr = ":4201"
	defaultCoreAddr   = "localhost:9000"
)

// newGatewayCmd creates the gateway subcommand with all flags configured.
// This is separate from NewGatewayCmd to allow for testable flag configuration.
func newGatewayCmd() *cobra.Command {
	cfg := &gatewayConfig{}

	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start the gateway process (telnet/web servers)",
		Long: `Start the gateway process which handles incoming connections
from telnet and web clients, forwarding commands to the core process.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGateway(cmd.Context(), cfg, cmd)
		},
	}

	// Register flags
	cmd.Flags().StringVar(&cfg.telnetAddr, "telnet-addr", defaultTelnetAddr, "telnet listen address")
	cmd.Flags().StringVar(&cfg.coreAddr, "core-addr", defaultCoreAddr, "core gRPC server address")
	cmd.Flags().StringVar(&cfg.controlSocket, "control-socket", "", "control socket path (default: XDG_RUNTIME_DIR/holomush/holomush-gateway.sock)")
	cmd.Flags().StringVar(&cfg.logFormat, "log-format", defaultLogFormat, "log format (json or text)")

	return cmd
}

// runGateway starts the gateway process.
func runGateway(ctx context.Context, cfg *gatewayConfig, cmd *cobra.Command) error {
	// Set up logging
	if err := setupLogging(cfg.logFormat); err != nil {
		return fmt.Errorf("failed to set up logging: %w", err)
	}

	slog.Info("starting gateway process",
		"telnet_addr", cfg.telnetAddr,
		"core_addr", cfg.coreAddr,
		"log_format", cfg.logFormat,
	)

	// Get certs directory
	certsDir, err := xdg.CertsDir()
	if err != nil {
		return fmt.Errorf("failed to get certs directory: %w", err)
	}

	// Load TLS client certificates for mTLS connection to core
	// Note: We use "localhost" as the expected game ID for now since LoadClientTLS
	// needs a game ID for server name validation. In production, this would come
	// from configuration or discovery.
	tlsConfig, err := tls.LoadClientTLS(certsDir, "gateway", "localhost")
	if err != nil {
		return fmt.Errorf("failed to load TLS certificates: %w", err)
	}

	slog.Info("TLS certificates loaded", "certs_dir", certsDir)

	// Create gRPC client with mTLS
	grpcClient, err := holoGRPC.NewClient(ctx, holoGRPC.ClientConfig{
		Address:   cfg.coreAddr,
		TLSConfig: tlsConfig,
	})
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}
	defer func() {
		if closeErr := grpcClient.Close(); closeErr != nil {
			slog.Warn("error closing gRPC client", "error", closeErr)
		}
	}()

	slog.Info("gRPC client created", "core_addr", cfg.coreAddr)

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create control socket server
	controlServer := control.NewServer("gateway", func() { cancel() })
	if err := controlServer.Start(); err != nil {
		return fmt.Errorf("failed to start control socket: %w", err)
	}

	slog.Info("control socket started")

	// Start telnet listener
	// Note: The current telnet server requires direct core components, which aren't
	// available in the gateway process. For now, we start a basic listener that
	// demonstrates the gateway is running. A future task will implement the gRPC-based
	// telnet handler that uses the grpcClient to communicate with core.
	telnetListener, err := net.Listen("tcp", cfg.telnetAddr)
	if err != nil {
		// Stop control socket before returning
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = controlServer.Stop(shutdownCtx)
		return fmt.Errorf("failed to listen on %s: %w", cfg.telnetAddr, err)
	}

	slog.Info("telnet server listening", "addr", telnetListener.Addr())

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start accepting telnet connections in goroutine
	errChan := make(chan error, 1)
	go func() {
		for {
			conn, acceptErr := telnetListener.Accept()
			if acceptErr != nil {
				select {
				case <-ctx.Done():
					return
				default:
					slog.Error("telnet accept failed", "error", acceptErr)
					continue
				}
			}
			// For now, just close the connection with a message.
			// A future task will implement proper gRPC-based handling.
			go handleTelnetConnectionPlaceholder(conn, grpcClient)
		}
	}()

	cmd.Println("Gateway process started")
	slog.Info("gateway process ready",
		"telnet_addr", telnetListener.Addr().String(),
		"core_addr", cfg.coreAddr,
	)

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		slog.Info("received shutdown signal", "signal", sig)
	case err := <-errChan:
		return fmt.Errorf("gateway error: %w", err)
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down")
	}

	// Graceful shutdown
	slog.Info("shutting down...")

	// Close telnet listener
	if err := telnetListener.Close(); err != nil {
		slog.Warn("error closing telnet listener", "error", err)
	}

	// Stop control socket
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := controlServer.Stop(shutdownCtx); err != nil {
		slog.Warn("error stopping control socket", "error", err)
	}

	slog.Info("shutdown complete")
	return nil
}

// handleTelnetConnectionPlaceholder handles a telnet connection.
// This is a placeholder until the gRPC-based telnet handler is implemented.
func handleTelnetConnectionPlaceholder(conn net.Conn, _ *holoGRPC.Client) {
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Debug("error closing telnet connection", "error", err)
		}
	}()

	// Send a welcome message indicating the gateway is running but not fully implemented
	_, err := fmt.Fprintln(conn, "Welcome to HoloMUSH Gateway!")
	if err != nil {
		slog.Debug("failed to send welcome message", "error", err)
		return
	}
	_, err = fmt.Fprintln(conn, "Gateway is connected to core but telnet handler is pending implementation.")
	if err != nil {
		slog.Debug("failed to send status message", "error", err)
		return
	}
	_, _ = fmt.Fprintln(conn, "Disconnecting...")
}
