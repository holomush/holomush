// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
)

// Config holds configuration for the web HTTP server.
type Config struct {
	Addr        string
	Handler     *Handler
	WebDir      string
	CORSOrigins []string
}

// Server is the web HTTP server hosting ConnectRPC and static files.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	errCh      chan error
}

// NewServer creates a web server with ConnectRPC handler and static file server.
func NewServer(cfg Config) *Server {
	mux := http.NewServeMux()

	// Register ConnectRPC handler
	path, connectHandler := webv1connect.NewWebServiceHandler(cfg.Handler)
	mux.Handle(path, connectHandler)

	// Register static file server as fallback
	mux.Handle("/", FileServer(cfg.WebDir))

	// Wrap with CORS if origins configured
	var handler http.Handler = mux
	if len(cfg.CORSOrigins) > 0 {
		handler = CORSMiddleware(cfg.CORSOrigins, mux)
	}

	return &Server{
		httpServer: &http.Server{
			Addr:    cfg.Addr,
			Handler: handler,
		},
		errCh: make(chan error, 1),
	}
}

// Start begins listening and serving.
func (s *Server) Start() (<-chan error, error) {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return nil, err
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
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the actual address the server is listening on.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.httpServer.Addr
}
