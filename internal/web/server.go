// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/net/http2"

	"connectrpc.com/connect"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
)

// Config holds configuration for the web HTTP server.
type Config struct {
	Addr        string
	Handler     *Handler
	WebDir      string
	CORSOrigins []string
	Secure      bool // controls cookie Secure flag and SameSite policy
	// SentryDSN, when non-empty, enables the /api/sentry-relay endpoint
	// that browser SDK envelopes can POST to as an ad-blocker bypass. The
	// DSN value is used to validate inbound envelopes — the relay only
	// forwards traffic destined for the configured project.
	SentryDSN string
	// OTLPRelayEndpoint, when non-empty, enables the /api/otlp/v1/traces
	// endpoint that the browser OTel tracer (web/src/lib/telemetry.ts) POSTs
	// spans to as a same-origin, ad-blocker-safe ingest. The value is the
	// collector's OTLP/HTTP base URL (e.g. http://otel-collector:4318); the
	// relay forwards to <base>/v1/traces. Distinct from
	// OTEL_EXPORTER_OTLP_ENDPOINT, which is the gateway's own gRPC export
	// target (port 4317).
	OTLPRelayEndpoint string
}

// Server is the web HTTP server hosting ConnectRPC and static files.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	errCh      chan error
}

// maxRequestBytes caps the decoded size of an inbound ConnectRPC request
// message at 4 MiB. connect-go only applies its body LimitReader when
// readMaxBytes > 0; left unset, it reads the entire request body into memory,
// so a single unauthenticated POST with a large body can OOM the public
// gateway. The value matches the core gRPC inbound cap
// (internal/grpc.MaxRecvMsgSize) so both server surfaces bound request memory
// identically (GH-4785).
const maxRequestBytes = 4 * 1024 * 1024

// NewServer creates a web server with ConnectRPC handler and static file server.
func NewServer(cfg Config) (*Server, error) {
	if !cfg.Secure {
		// SECURITY: Warn loudly when cookies are emitted without the Secure
		// flag. This is only appropriate for local development over plain
		// HTTP; any production deployment MUST set Secure=true so session
		// cookies are never transmitted over unencrypted channels.
		slog.Warn(
			"web: session cookies will be issued WITHOUT Secure flag; "+
				"this is unsafe for production. Set Config.Secure=true when serving over TLS.",
			"secure", cfg.Secure,
			"same_site", "Lax",
		)
	}

	mux := http.NewServeMux()

	// Register ConnectRPC handler with gRPC→connect status code translation.
	// The interceptor converts oops-wrapped grpc *status.Status errors into
	// *connect.Error so browsers receive specific codes (PermissionDenied,
	// NotFound, Unauthenticated) instead of the default CodeUnknown / HTTP 500.
	path, connectHandler := webv1connect.NewWebServiceHandler(
		cfg.Handler,
		connect.WithReadMaxBytes(maxRequestBytes),
		connect.WithInterceptors(statusTranslationInterceptor()),
	)
	mux.Handle(path, connectHandler)

	// Register Sentry envelope relay if SENTRY_DSN is configured. The
	// relay accepts browser SDK envelopes at /api/sentry-relay and
	// forwards them to Sentry's ingest, bypassing ad-blockers that
	// block direct *.ingest.sentry.io requests. A failed parse of the
	// configured DSN is treated as a configuration error: the relay
	// stays unregistered (so an open endpoint never ships by accident),
	// and the failure is logged so operators notice.
	if cfg.SentryDSN != "" {
		relayHandler, relayErr := NewSentryRelayHandler(cfg.SentryDSN)
		if relayErr != nil {
			errutil.LogError(slog.Default(),
				"web: sentry relay not registered due to DSN parse error", relayErr)
		} else {
			mux.Handle("/api/sentry-relay", relayHandler)
			slog.Info("web: sentry relay registered at /api/sentry-relay")
		}
	}

	// Register the browser OTLP trace relay if a collector endpoint is
	// configured. The relay accepts same-origin OTLP/HTTP trace POSTs at
	// /api/otlp/v1/traces and forwards them to the collector, bypassing the
	// ad-blocker/CORS failures that block direct browser POSTs to an external
	// ingest origin. A failed parse of the configured endpoint leaves the
	// route unregistered (so a non-functional endpoint never ships) and is
	// logged so operators notice.
	if cfg.OTLPRelayEndpoint != "" {
		otlpHandler, otlpErr := NewOTLPRelayHandler(cfg.OTLPRelayEndpoint)
		if otlpErr != nil {
			errutil.LogError(slog.Default(),
				"web: OTLP trace relay not registered due to endpoint parse error", otlpErr)
		} else {
			mux.Handle("/api/otlp/v1/traces", otlpHandler)
			slog.Info("web: OTLP trace relay registered at /api/otlp/v1/traces")
		}
	}

	// Register static file server as fallback
	mux.Handle("/", FileServer(cfg.WebDir))

	// Wrap with cookie middleware (translates signal headers ↔ Set-Cookie)
	handler := CookieMiddleware(cfg.Secure, mux)

	// Wrap with CORS if origins configured
	if len(cfg.CORSOrigins) > 0 {
		handler = CORSMiddleware(cfg.CORSOrigins, handler)
	}

	// Wrap with security headers OUTSIDE cookie/CORS so every response —
	// including CORS preflight 204s and early errors — carries the headers.
	handler = SecurityHeadersMiddleware(cfg.Secure, handler)

	// Wrap with OpenTelemetry HTTP instrumentation
	handler = otelhttp.NewHandler(handler, "holomush-gateway")

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout bounds the total time to read a request (headers + body),
		// closing the slow-body window ReadHeaderTimeout alone leaves open. Under
		// HTTP/2 (x/net/http2) this is applied per-stream as a request-body-read
		// deadline that is disarmed once the body is consumed, so it does not
		// truncate long-lived server-streaming responses like StreamEvents
		// (GH-4785).
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	// Configure HTTP/2 with keepalive pings to detect dead connections.
	// These settings apply to TLS connections (production) where browsers
	// negotiate HTTP/2 via ALPN. In dev mode (no TLS), browsers use
	// HTTP/1.1 and the application-level heartbeat in StreamEvents
	// handles disconnect detection instead.
	// See: docs/specs/decisions/001-http2-required.md
	h2s := &http2.Server{
		ReadIdleTimeout:  30 * time.Second, // Send PING after 30s of silence
		PingTimeout:      15 * time.Second, // Close if no PONG within 15s
		WriteByteTimeout: 10 * time.Second, // Close if a write blocks >10s
	}
	if err := http2.ConfigureServer(httpServer, h2s); err != nil {
		return nil, fmt.Errorf("configure http2: %w", err)
	}

	return &Server{
		httpServer: httpServer,
		errCh:      make(chan error, 1),
	}, nil
}

// Start begins listening and serving.
func (s *Server) Start() (<-chan error, error) {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller wraps with oops context
	}
	s.listener = ln

	go func() {
		defer close(s.errCh)
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("web: HTTP server error", "error", err)
			s.errCh <- err
		}
	}()

	slog.Info("web HTTP server started", "addr", s.Addr())
	return s.errCh, nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx) //nolint:wrapcheck // caller logs and handles shutdown errors
}

// Addr returns the actual address the server is listening on.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.httpServer.Addr
}
