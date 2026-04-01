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
	"golang.org/x/net/http2/h2c"

	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
)

// Config holds configuration for the web HTTP server.
type Config struct {
	Addr        string
	Handler     *Handler
	WebDir      string
	CORSOrigins []string
	Secure      bool // controls cookie Secure flag and SameSite policy
}

// Server is the web HTTP server hosting ConnectRPC and static files.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	errCh      chan error
}

// NewServer creates a web server with ConnectRPC handler and static file server.
func NewServer(cfg Config) (*Server, error) {
	mux := http.NewServeMux()

	// Register ConnectRPC handler
	path, connectHandler := webv1connect.NewWebServiceHandler(cfg.Handler)
	mux.Handle(path, connectHandler)

	// Register static file server as fallback
	mux.Handle("/", FileServer(cfg.WebDir))

	// Wrap with cookie middleware (translates signal headers ↔ Set-Cookie)
	handler := CookieMiddleware(cfg.Secure, mux)

	// Wrap with CORS if origins configured
	if len(cfg.CORSOrigins) > 0 {
		handler = CORSMiddleware(cfg.CORSOrigins, handler)
	}

	// Wrap with OpenTelemetry HTTP instrumentation
	handler = otelhttp.NewHandler(handler, "holomush-gateway")

	// HTTP/2 server with keepalive pings to detect dead connections.
	// Server-streaming handlers block indefinitely when a client silently
	// disconnects — PING/PONG detects dead peers in ~45s.
	// See: docs/specs/decisions/001-http2-required.md
	h2s := &http2.Server{
		ReadIdleTimeout:  30 * time.Second, // Send PING after 30s of silence
		PingTimeout:      15 * time.Second, // Close if no PONG within 15s
		WriteByteTimeout: 10 * time.Second, // Close if a write blocks >10s
	}

	// h2c wraps the handler to accept HTTP/2 cleartext (no TLS) connections.
	// Enables HTTP/2 pings for programmatic clients (gRPC, ConnectRPC Go).
	// Browsers don't support h2c — they fall through to HTTP/1.1.
	h2cHandler := h2c.NewHandler(handler, h2s)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h2cHandler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Also configure HTTP/2 for TLS connections (production mode).
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
