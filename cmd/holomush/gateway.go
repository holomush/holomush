// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/control"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/internal/xdg"
)

// gatewayConfig holds configuration for the gateway command.
type gatewayConfig struct {
	telnetAddr  string
	coreAddr    string
	controlAddr string
	metricsAddr string
	logFormat   string
}

// Validate checks that the configuration is valid.
func (cfg *gatewayConfig) Validate() error {
	if cfg.telnetAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("telnet-addr is required")
	}
	if cfg.coreAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("core-addr is required")
	}
	if cfg.controlAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("control-addr is required")
	}
	if cfg.logFormat != "json" && cfg.logFormat != "text" {
		return oops.Code("CONFIG_INVALID").Errorf("log-format must be 'json' or 'text', got %q", cfg.logFormat)
	}
	return nil
}

// Default values for gateway command flags.
const (
	defaultTelnetAddr         = ":4201"
	defaultCoreAddr           = "localhost:9000"
	defaultGatewayControlAddr = "127.0.0.1:9002"
	defaultGatewayMetricsAddr = "127.0.0.1:9101"
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
			return runGatewayWithDeps(cmd.Context(), cfg, cmd, nil)
		},
	}

	cmd.Flags().StringVar(&cfg.telnetAddr, "telnet-addr", defaultTelnetAddr, "telnet listen address")
	cmd.Flags().StringVar(&cfg.coreAddr, "core-addr", defaultCoreAddr, "core gRPC server address")
	cmd.Flags().StringVar(&cfg.controlAddr, "control-addr", defaultGatewayControlAddr, "control gRPC listen address with mTLS")
	cmd.Flags().StringVar(&cfg.metricsAddr, "metrics-addr", defaultGatewayMetricsAddr, "metrics/health HTTP address (empty = disabled)")
	cmd.Flags().StringVar(&cfg.logFormat, "log-format", defaultLogFormat, "log format (json or text)")

	return cmd
}

// runGatewayWithDeps starts the gateway process with injectable dependencies.
// If deps is nil, default implementations are used.
func runGatewayWithDeps(ctx context.Context, cfg *gatewayConfig, cmd *cobra.Command, deps *GatewayDeps) error {
	if deps == nil {
		deps = &GatewayDeps{}
	}

	// Set up default factories
	if deps.CertsDirGetter == nil {
		deps.CertsDirGetter = xdg.CertsDir
	}
	if deps.GameIDExtractor == nil {
		deps.GameIDExtractor = control.ExtractGameIDFromCA
	}
	if deps.ClientTLSLoader == nil {
		deps.ClientTLSLoader = tls.LoadClientTLS
	}
	if deps.GRPCClientFactory == nil {
		deps.GRPCClientFactory = func(ctx context.Context, cfg holoGRPC.ClientConfig) (GRPCClient, error) {
			return holoGRPC.NewClient(ctx, cfg)
		}
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
	if deps.ListenerFactory == nil {
		deps.ListenerFactory = net.Listen
	}

	if err := cfg.Validate(); err != nil {
		return oops.Code("CONFIG_INVALID").With("operation", "validate configuration").Wrap(err)
	}

	if err := setupLogging(cfg.logFormat); err != nil {
		return oops.Code("LOGGING_SETUP_FAILED").With("operation", "set up logging").Wrap(err)
	}

	slog.Info("starting gateway process",
		"telnet_addr", cfg.telnetAddr,
		"core_addr", cfg.coreAddr,
		"log_format", cfg.logFormat,
	)

	certsDir, err := deps.CertsDirGetter()
	if err != nil {
		return oops.Code("CERTS_DIR_FAILED").With("operation", "get certs directory").Wrap(err)
	}

	// Extract game_id from CA certificate for proper ServerName verification
	gameID, err := deps.GameIDExtractor(certsDir)
	if err != nil {
		return oops.Code("GAME_ID_EXTRACT_FAILED").With("operation", "extract game_id from CA").With("certs_dir", certsDir).Wrap(err)
	}

	// Load TLS client certificates for mTLS connection to core
	tlsConfig, err := deps.ClientTLSLoader(certsDir, "gateway", gameID)
	if err != nil {
		return oops.Code("TLS_LOAD_FAILED").With("operation", "load TLS certificates").With("component", "gateway").Wrap(err)
	}

	slog.Info("TLS certificates loaded", "certs_dir", certsDir)

	// Create gRPC client with mTLS
	grpcClient, err := deps.GRPCClientFactory(ctx, holoGRPC.ClientConfig{
		Address:   cfg.coreAddr,
		TLSConfig: tlsConfig,
	})
	if err != nil {
		return oops.Code("GRPC_CLIENT_CREATE_FAILED").With("operation", "create gRPC client").With("core_addr", cfg.coreAddr).Wrap(err)
	}
	defer func() {
		if closeErr := grpcClient.Close(); closeErr != nil {
			slog.Warn("error closing gRPC client", "error", closeErr)
		}
	}()

	slog.Info("gRPC client created", "core_addr", cfg.coreAddr)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start control gRPC server (always enabled)
	controlTLSConfig, tlsErr := deps.ControlTLSLoader(certsDir, "gateway")
	if tlsErr != nil {
		return oops.Code("CONTROL_TLS_FAILED").With("operation", "load control TLS config").With("component", "gateway").Wrap(tlsErr)
	}

	controlGRPCServer, err := deps.ControlServerFactory("gateway", func() { cancel() })
	if err != nil {
		return oops.Code("CONTROL_SERVER_CREATE_FAILED").With("operation", "create control gRPC server").Wrap(err)
	}
	controlErrChan, err := controlGRPCServer.Start(cfg.controlAddr, controlTLSConfig)
	if err != nil {
		return oops.Code("CONTROL_SERVER_START_FAILED").With("operation", "start control gRPC server").With("addr", cfg.controlAddr).Wrap(err)
	}
	// Monitor control server errors in background - triggers shutdown on error
	go monitorServerErrors(ctx, cancel, controlErrChan, "control-grpc")

	slog.Info("control gRPC server started", "addr", cfg.controlAddr)

	// TODO(grpc-telnet): Replace placeholder telnet handler with gRPC-based implementation.
	// The current telnet server requires direct core components, which aren't available
	// in the gateway process. For now, we start a basic listener that demonstrates the
	// gateway is running.
	telnetListener, err := deps.ListenerFactory("tcp", cfg.telnetAddr)
	if err != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if stopErr := controlGRPCServer.Stop(shutdownCtx); stopErr != nil {
			slog.Warn("failed to stop control gRPC server during cleanup", "error", stopErr)
		}
		return oops.Code("LISTEN_FAILED").With("operation", "listen").With("addr", cfg.telnetAddr).Wrap(err)
	}

	slog.Info("telnet server listening", "addr", telnetListener.Addr())

	// Start observability server if configured
	var obsServer ObservabilityServer
	if cfg.metricsAddr != "" {
		// For gateway, we're ready once telnet listener is up
		obsServer = deps.ObservabilityServerFactory(cfg.metricsAddr, func() bool { return true })
		// Register command package metrics with the observability server
		obsServer.MustRegister(command.CommandExecutions, command.CommandDuration, command.AliasExpansions)
		obsErrChan, err := obsServer.Start()
		if err != nil {
			if closeErr := telnetListener.Close(); closeErr != nil {
				slog.Warn("failed to close telnet listener during cleanup", "error", closeErr)
			}
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if stopErr := controlGRPCServer.Stop(shutdownCtx); stopErr != nil {
				slog.Warn("failed to stop control gRPC server during cleanup", "error", stopErr)
			}
			return oops.Code("OBSERVABILITY_START_FAILED").With("operation", "start observability server").With("addr", cfg.metricsAddr).Wrap(err)
		}
		// Monitor observability server errors in background - triggers shutdown on error
		go monitorServerErrors(ctx, cancel, obsErrChan, "observability")
		slog.Info("observability server started", "addr", obsServer.Addr())
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Start accepting telnet connections in goroutine with backoff on errors
	go runTelnetAcceptLoop(ctx, telnetListener, cancel)

	cmd.Println("Gateway process started")
	slog.Info("gateway process ready",
		"telnet_addr", telnetListener.Addr().String(),
		"core_addr", cfg.coreAddr,
	)

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		slog.Info("received shutdown signal", "signal", sig)
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down")
	}

	// Graceful shutdown
	slog.Info("shutting down...")

	// Close telnet listener
	if err := telnetListener.Close(); err != nil {
		slog.Warn("error closing telnet listener", "error", err)
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

// handleTelnetConnection handles a telnet connection.
// This is a placeholder until the gRPC-based telnet handler is implemented.
func handleTelnetConnection(conn net.Conn) {
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
	// Error intentionally ignored: connection closes immediately after, so logging
	// a write failure here would be noise (client may have already disconnected).
	//nolint:errcheck // write error ignored on connection close
	fmt.Fprintln(conn, "Disconnecting...")
}

// acceptBackoff manages exponential backoff for the accept loop.
// It starts at 100ms and doubles on each failure, capped at 30 seconds.
// Resets to initial value on successful accept.
type acceptBackoff struct {
	current time.Duration
	initial time.Duration
	max     time.Duration
}

// newAcceptBackoff creates a new backoff with 100ms initial and 30s max.
func newAcceptBackoff() *acceptBackoff {
	return &acceptBackoff{
		current: 0, // No delay until first failure
		initial: 100 * time.Millisecond,
		max:     30 * time.Second,
	}
}

// failure records a failure and increases the backoff.
func (b *acceptBackoff) failure() {
	if b.current == 0 {
		b.current = b.initial
	} else {
		b.current *= 2
		if b.current > b.max {
			b.current = b.max
		}
	}
}

// success resets the backoff to initial state.
func (b *acceptBackoff) success() {
	b.current = 0
}

// wait returns the current backoff duration to wait.
func (b *acceptBackoff) wait() time.Duration {
	return b.current
}

// runTelnetAcceptLoop accepts telnet connections with exponential backoff on errors.
// The cancel function is called on panic to trigger graceful shutdown.
func runTelnetAcceptLoop(ctx context.Context, listener net.Listener, cancel func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in telnet accept loop, triggering shutdown",
				"panic", r,
			)
			cancel()
		}
	}()

	backoff := newAcceptBackoff()

	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			select {
			case <-ctx.Done():
				return
			default:
				backoff.failure()
				waitDuration := backoff.wait()
				slog.Error("telnet accept failed, backing off",
					"error", acceptErr,
					"backoff", waitDuration,
				)
				// Wait with context so we can exit promptly on shutdown
				select {
				case <-ctx.Done():
					return
				case <-time.After(waitDuration):
				}
				continue
			}
		}
		backoff.success()
		go handleTelnetConnection(conn)
	}
}
