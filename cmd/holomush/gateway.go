// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/control"
	holoGRPC "github.com/holomush/holomush/internal/grpcclient"
	"github.com/holomush/holomush/internal/logging"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/internal/telemetry"
	"github.com/holomush/holomush/internal/telnet"
	tlscerts "github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/internal/web"
	"github.com/holomush/holomush/internal/xdg"
)

// gatewayConfig holds configuration for the gateway command.
type gatewayConfig struct {
	TelnetAddr           string        `koanf:"telnet_addr"`
	CoreAddr             string        `koanf:"core_addr"`
	ControlAddr          string        `koanf:"control_addr"`
	MetricsAddr          string        `koanf:"metrics_addr"`
	LogFormat            string        `koanf:"log_format"`
	WebAddr              string        `koanf:"web_addr"`
	WebDir               string        `koanf:"web_dir"`
	CORSOrigins          []string      `koanf:"cors_origins"`
	SecureCookies        bool          `koanf:"secure_cookies"`
	TelnetMaxConns       int           `koanf:"telnet_max_conns"`
	TelnetIdleTimeout    time.Duration `koanf:"telnet_idle_timeout"`
	TelnetWriteTimeout   time.Duration `koanf:"telnet_write_timeout"`
	TelnetPreAuthTimeout time.Duration `koanf:"telnet_pre_auth_timeout"`
}

// Validate checks that the configuration is valid.
func (cfg *gatewayConfig) Validate() error {
	if cfg.TelnetAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("telnet-addr is required")
	}
	if cfg.CoreAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("core-addr is required")
	}
	if cfg.ControlAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("control-addr is required")
	}
	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		return oops.Code("CONFIG_INVALID").Errorf("log-format must be 'json' or 'text', got %q", cfg.LogFormat)
	}
	if cfg.TelnetMaxConns <= 0 {
		return oops.Code("CONFIG_INVALID").Errorf("telnet-max-conns must be positive, got %d", cfg.TelnetMaxConns)
	}
	if cfg.TelnetIdleTimeout <= 0 {
		return oops.Code("CONFIG_INVALID").Errorf("telnet-idle-timeout must be positive, got %s", cfg.TelnetIdleTimeout)
	}
	if cfg.TelnetWriteTimeout <= 0 {
		return oops.Code("CONFIG_INVALID").Errorf("telnet-write-timeout must be positive, got %s", cfg.TelnetWriteTimeout)
	}
	if cfg.TelnetPreAuthTimeout <= 0 {
		return oops.Code("CONFIG_INVALID").Errorf("telnet-pre-auth-timeout must be positive, got %s", cfg.TelnetPreAuthTimeout)
	}
	return nil
}

// Default values for gateway command flags.
const (
	defaultTelnetAddr           = ":4201"
	defaultCoreAddr             = "localhost:9000"
	defaultGatewayControlAddr   = "127.0.0.1:9002"
	defaultGatewayMetricsAddr   = "127.0.0.1:9101"
	defaultWebAddr              = ":8080"
	defaultTelnetMaxConns       = 1000
	defaultTelnetIdleTimeout    = 5 * time.Minute
	defaultTelnetWriteTimeout   = 30 * time.Second
	defaultTelnetPreAuthTimeout = 2 * time.Minute
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
			if err := config.Load(configFile, cmd, cfg, "gateway"); err != nil {
				return err
			}
			logConfig := config.DefaultLoggingConfig()
			if err := config.Load(configFile, cmd, &logConfig, "logging"); err != nil {
				return err
			}
			applyLogSinkFlags(cmd, &logConfig)
			return runGatewayWithDeps(cmd.Context(), cfg, logConfig, cmd, nil)
		},
	}

	cmd.Flags().StringVar(&cfg.TelnetAddr, "telnet-addr", defaultTelnetAddr, "telnet listen address")
	cmd.Flags().StringVar(&cfg.CoreAddr, "core-addr", defaultCoreAddr, "core gRPC server address")
	cmd.Flags().StringVar(&cfg.ControlAddr, "control-addr", defaultGatewayControlAddr, "control gRPC listen address with mTLS")
	cmd.Flags().StringVar(&cfg.MetricsAddr, "metrics-addr", defaultGatewayMetricsAddr, "metrics/health HTTP address (empty = disabled)")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", defaultLogFormat, "log format (json or text)")
	cmd.Flags().StringVar(&cfg.WebAddr, "web-addr", defaultWebAddr, "web HTTP listen address")
	cmd.Flags().StringVar(&cfg.WebDir, "web-dir", "", "override embedded static files with directory path")
	cmd.Flags().StringSliceVar(&cfg.CORSOrigins, "cors-origins", nil, "allowed CORS origins (e.g., http://localhost:5173)")
	cmd.Flags().BoolVar(&cfg.SecureCookies, "secure-cookies", false, "set the Secure flag + SameSite=Strict on session cookies (MUST be true for any TLS-served deployment; default false for local plain-HTTP dev)")
	cmd.Flags().IntVar(&cfg.TelnetMaxConns, "telnet-max-conns", defaultTelnetMaxConns, "max concurrent telnet connections")
	cmd.Flags().DurationVar(&cfg.TelnetIdleTimeout, "telnet-idle-timeout", defaultTelnetIdleTimeout, "per-connection idle read timeout")
	cmd.Flags().DurationVar(&cfg.TelnetWriteTimeout, "telnet-write-timeout", defaultTelnetWriteTimeout, "per-send write deadline")
	cmd.Flags().DurationVar(&cfg.TelnetPreAuthTimeout, "telnet-pre-auth-timeout", defaultTelnetPreAuthTimeout, "disconnect unauthenticated clients after this duration")
	registerLogSinkFlags(cmd)

	return cmd
}

// runGatewayWithDeps starts the gateway process with injectable dependencies.
// If deps is nil, default implementations are used.
func runGatewayWithDeps(ctx context.Context, cfg *gatewayConfig, logConfig config.LoggingConfig, cmd *cobra.Command, deps *GatewayDeps) error {
	// Stamp the bootstrap start for the process.startup span emitted at
	// the ready point below.
	bootStart := time.Now()

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
		deps.ClientTLSLoader = tlscerts.LoadClientTLS
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

	// --- Logging (phase 1: stderr-only) + telemetry ---
	level, err := resolveLogLevel(cmd)
	if err != nil {
		return err
	}
	stderrLevel := logConfig.Stderr.EffectiveLevel(level)
	logging.SetDefaultWithBridge("holomush-gateway", version, cfg.LogFormat, logConfig.Stderr.Enabled, stderrLevel, nil, level)

	res, telErr := telemetry.Init(ctx, "holomush-gateway", version, logConfig, level)
	if telErr != nil {
		return oops.Code("TELEMETRY_INIT_FAILED").Wrap(telErr)
	}
	// Phase 2: re-seat the default logger with the OTel bridge when present.
	if res.LogHandler != nil {
		logging.SetDefaultWithBridge("holomush-gateway", version, cfg.LogFormat, logConfig.Stderr.Enabled, stderrLevel, res.LogHandler, res.LogBridgeLevel)
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
		"starting gateway process",
		"telnet_addr", cfg.TelnetAddr,
		"core_addr", cfg.CoreAddr,
		"log_format", cfg.LogFormat,
	)

	certsDir, err := deps.CertsDirGetter()
	if err != nil {
		return oops.Code("CERTS_DIR_FAILED").With("operation", "get certs directory").Wrap(err)
	}

	// Poll for TLS certificates — allows gateway to start before core generates certs.
	certPollTimeout := deps.CertPollTimeout
	if certPollTimeout <= 0 {
		certPollTimeout = defaultCertPollTimeout
	}
	tlsCfg, err := waitForTLSCerts(ctx, deps, certsDir, "gateway", certPollTimeout)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "TLS certificates loaded", "certs_dir", certsDir)

	// Create gRPC client with mTLS
	grpcClient, err := deps.GRPCClientFactory(ctx, holoGRPC.ClientConfig{
		Address:   cfg.CoreAddr,
		TLSConfig: tlsCfg.clientTLS,
	})
	if err != nil {
		return oops.Code("GRPC_CLIENT_CREATE_FAILED").With("operation", "create gRPC client").With("core_addr", cfg.CoreAddr).Wrap(err)
	}
	defer func() {
		if closeErr := grpcClient.Close(); closeErr != nil {
			slog.WarnContext(ctx, "error closing gRPC client", "error", closeErr)
		}
	}()

	slog.InfoContext(ctx, "gRPC client created", "core_addr", cfg.CoreAddr)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	controlGRPCServer, err := deps.ControlServerFactory("gateway", func() { cancel() })
	if err != nil {
		return oops.Code("CONTROL_SERVER_CREATE_FAILED").With("operation", "create control gRPC server").Wrap(err)
	}
	controlErrChan, err := controlGRPCServer.Start(cfg.ControlAddr, tlsCfg.controlTLS)
	if err != nil {
		return oops.Code("CONTROL_SERVER_START_FAILED").With("operation", "start control gRPC server").With("addr", cfg.ControlAddr).Wrap(err)
	}
	// Monitor control server errors in background - triggers shutdown on error
	go monitorServerErrors(ctx, cancel, controlErrChan, "control-grpc")

	slog.InfoContext(ctx, "control gRPC server started", "addr", cfg.ControlAddr)

	// TODO(grpc-telnet): Replace placeholder telnet handler with gRPC-based implementation.
	// The current telnet server requires direct core components, which aren't available
	// in the gateway process. For now, we start a basic listener that demonstrates the
	// gateway is running.
	telnetListener, err := deps.ListenerFactory("tcp", cfg.TelnetAddr)
	if err != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if stopErr := controlGRPCServer.Stop(shutdownCtx); stopErr != nil {
			slog.WarnContext(shutdownCtx, "failed to stop control gRPC server during cleanup", "error", stopErr)
		}
		return oops.Code("LISTEN_FAILED").With("operation", "listen").With("addr", cfg.TelnetAddr).Wrap(err)
	}

	slog.InfoContext(ctx, "telnet server listening", "addr", telnetListener.Addr())

	// Start observability server if configured
	var obsServer ObservabilityServer
	if cfg.MetricsAddr != "" {
		// For gateway, we're ready once telnet listener is up
		obsServer = deps.ObservabilityServerFactory(cfg.MetricsAddr, func() bool { return true })
		// Register telnet metrics with the observability server.
		// Command-package metrics are registered in the core process only —
		// the gateway forwards commands via gRPC and never executes them.
		obsServer.MustRegister(
			telnet.ConnectionsActive, telnet.ConnectionsRefusedTotal,
			telnet.PreAuthTimeoutsTotal, telnet.IdleTimeoutsTotal,
		)
		var obsErrChan <-chan error
		obsErrChan, err = obsServer.Start()
		if err != nil {
			if closeErr := telnetListener.Close(); closeErr != nil {
				slog.WarnContext(ctx, "failed to close telnet listener during cleanup", "error", closeErr)
			}
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if stopErr := controlGRPCServer.Stop(shutdownCtx); stopErr != nil {
				slog.WarnContext(shutdownCtx, "failed to stop control gRPC server during cleanup", "error", stopErr)
			}
			return oops.Code("OBSERVABILITY_START_FAILED").With("operation", "start observability server").With("addr", cfg.MetricsAddr).Wrap(err)
		}
		// Monitor observability server errors in background - triggers shutdown on error
		go monitorServerErrors(ctx, cancel, obsErrChan, "observability")
		slog.InfoContext(ctx, "observability server started", "addr", obsServer.Addr())
	}

	// Start web HTTP server. The gateway is a protocol-translation layer
	// (Phase 1.6): it reads rendering metadata from EventFrame.Rendering on
	// the wire instead of holding a local VerbRegistry. The core process is
	// the sole owner of rendering enrichment via core/v1/RenderingMetadata.
	webHandler := web.NewHandler(grpcClient, web.WithContentClient(grpcClient), web.WithSceneAccessClient(grpcClient))
	webServer, err := web.NewServer(web.Config{
		Addr:        cfg.WebAddr,
		Handler:     webHandler,
		WebDir:      cfg.WebDir,
		CORSOrigins: cfg.CORSOrigins,
		Secure:      cfg.SecureCookies,
		// Forward the configured DSN so the web server can register the
		// /api/sentry-relay tunnel endpoint. Empty = no relay (and no
		// open-proxy risk).
		SentryDSN: os.Getenv("SENTRY_DSN"),
		// Collector's OTLP/HTTP base URL for the same-origin browser trace
		// relay at /api/otlp/v1/traces. Empty = relay disabled. This is the
		// HTTP receiver (port 4318), distinct from OTEL_EXPORTER_OTLP_ENDPOINT
		// (the gateway's own gRPC export target, port 4317).
		OTLPRelayEndpoint: os.Getenv("OTLP_RELAY_ENDPOINT"),
	})
	if err != nil {
		return oops.With("operation", "create web server").Wrap(err)
	}
	webErrChan, err := webServer.Start()
	if err != nil {
		// Clean up already-started servers
		if obsServer != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if stopErr := obsServer.Stop(shutdownCtx); stopErr != nil {
				slog.WarnContext(shutdownCtx, "failed to stop observability server during cleanup", "error", stopErr)
			}
		}
		if closeErr := telnetListener.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to close telnet listener during cleanup", "error", closeErr)
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if stopErr := controlGRPCServer.Stop(shutdownCtx); stopErr != nil {
			slog.WarnContext(shutdownCtx, "failed to stop control gRPC server during cleanup", "error", stopErr)
		}
		return oops.Code("WEB_SERVER_START_FAILED").With("operation", "start web HTTP server").With("addr", cfg.WebAddr).Wrap(err)
	}
	go monitorServerErrors(ctx, cancel, webErrChan, "web-http")
	// Note: Start() already logs "web HTTP server started" — no duplicate log here.

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Start accepting telnet connections in goroutine with backoff on errors.
	// The slots channel bounds concurrent handler goroutines; a full channel
	// causes new accepts to be refused immediately via RefuseOverCapacity.
	slots := make(chan struct{}, cfg.TelnetMaxConns)
	limits := telnet.Limits{
		IdleReadTimeout: cfg.TelnetIdleTimeout,
		WriteTimeout:    cfg.TelnetWriteTimeout,
		PreAuthTimeout:  cfg.TelnetPreAuthTimeout,
	}
	go runTelnetAcceptLoop(ctx, telnetListener, grpcClient, cancel, slots, limits)

	telemetry.EmitStartupSpan(ctx, "holomush-gateway", version, bootStart)

	cmd.Println("Gateway process started")
	slog.InfoContext(
		ctx,
		"gateway process ready",
		"telnet_addr", telnetListener.Addr().String(),
		"core_addr", cfg.CoreAddr,
		"web_addr", webServer.Addr(),
	)

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		slog.InfoContext(ctx, "received shutdown signal", "signal", sig)
	case <-ctx.Done():
		slog.InfoContext(ctx, "context cancelled, shutting down")
	}

	// Graceful shutdown
	slog.InfoContext(ctx, "shutting down...")

	// Close telnet listener
	if err := telnetListener.Close(); err != nil {
		slog.WarnContext(ctx, "error closing telnet listener", "error", err)
	}

	// Stop servers
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// Stop web HTTP server
	if err := webServer.Stop(shutdownCtx); err != nil {
		slog.WarnContext(shutdownCtx, "error stopping web HTTP server", "error", err)
	}

	if obsServer != nil {
		if err := obsServer.Stop(shutdownCtx); err != nil {
			slog.WarnContext(shutdownCtx, "error stopping observability server", "error", err)
		}
	}

	// Stop control gRPC server
	if err := controlGRPCServer.Stop(shutdownCtx); err != nil {
		slog.WarnContext(shutdownCtx, "error stopping control gRPC server", "error", err)
	}

	slog.InfoContext(ctx, "shutdown complete")
	return nil
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

// acceptLoopHooks bundles test-only observability seams. Production callers
// pass no options; tests use withOnSlotReleased to receive a deterministic
// signal when a handler goroutine exits and frees its semaphore slot.
//
// Without this seam, tests must poll the `telnet.ConnectionsActive` Prometheus
// gauge with `require.Eventually`, which is timing-sensitive under CI
// contention (handler.Handle's deadline-driven exit + goroutine scheduling +
// gauge propagation can together exceed a 2s ceiling on a busy runner — see
// bead holomush-rfzb).
type acceptLoopHooks struct {
	onSlotReleased func()
}

type acceptLoopOption func(*acceptLoopHooks)

// withOnSlotReleased sets a callback fired after the handler goroutine
// drains its slot and decrements the active-connections gauge. Test-only.
func withOnSlotReleased(cb func()) acceptLoopOption {
	return func(h *acceptLoopHooks) { h.onSlotReleased = cb }
}

// runTelnetAcceptLoop accepts telnet connections with exponential backoff on errors.
// slots bounds the number of concurrent handler goroutines; a full slots channel
// triggers immediate refusal via RefuseOverCapacity. The cancel function is called
// on panic to trigger graceful shutdown.
func runTelnetAcceptLoop(
	ctx context.Context,
	listener net.Listener,
	client GRPCClient,
	cancel func(),
	slots chan struct{},
	limits telnet.Limits,
	opts ...acceptLoopOption,
) {
	var hooks acceptLoopHooks
	for _, opt := range opts {
		opt(&hooks)
	}

	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(
				ctx,
				"panic in telnet accept loop, triggering shutdown",
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
				slog.ErrorContext(
					ctx,
					"telnet accept failed, backing off",
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

		select {
		case slots <- struct{}{}:
			telnet.IncConnectionsActive()
			handler := telnet.NewGatewayHandler(conn, client, limits)
			go func() {
				defer func() {
					<-slots
					telnet.DecConnectionsActive()
					if hooks.onSlotReleased != nil {
						hooks.onSlotReleased()
					}
				}()
				handler.Handle(ctx)
			}()
		default:
			telnet.RefuseOverCapacity(conn, limits.WriteTimeout)
		}
	}
}
