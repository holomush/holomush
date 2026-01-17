// Package control provides HTTP control socket for process management.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/holomush/holomush/internal/xdg"
)

// HealthResponse is returned by the /health endpoint.
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// StatusResponse is returned by the /status endpoint.
type StatusResponse struct {
	Running       bool   `json:"running"`
	PID           int    `json:"pid"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Component     string `json:"component,omitempty"`
}

// ShutdownResponse is returned by the /shutdown endpoint.
type ShutdownResponse struct {
	Message string `json:"message"`
}

// ShutdownFunc is called when shutdown is requested.
type ShutdownFunc func()

// Server runs HTTP over a Unix socket for process management.
type Server struct {
	component    string
	startTime    time.Time
	listener     net.Listener
	httpServer   *http.Server
	socketPath   string
	shutdownFunc ShutdownFunc
	running      atomic.Bool
}

// NewServer creates a new control socket server.
// component is the name of the process (e.g., "core" or "gateway").
func NewServer(component string, shutdownFunc ShutdownFunc) *Server {
	s := &Server{
		component:    component,
		startTime:    time.Now(),
		shutdownFunc: shutdownFunc,
	}
	s.running.Store(true)
	return s
}

// SocketPath returns the path to the Unix socket.
// Returns an error if the runtime directory cannot be determined.
func SocketPath(component string) (string, error) {
	runtimeDir, err := xdg.RuntimeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get runtime directory: %w", err)
	}
	return filepath.Join(runtimeDir, fmt.Sprintf("holomush-%s.sock", component)), nil
}

// Start begins listening on the Unix socket.
func (s *Server) Start() error {
	socketPath, err := SocketPath(s.component)
	if err != nil {
		return err
	}
	s.socketPath = socketPath

	// Ensure runtime directory exists
	runtimeDir := filepath.Dir(socketPath)
	if err := xdg.EnsureDir(runtimeDir); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	// Remove existing socket file if present
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	// Set socket permissions to owner-only
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("POST /shutdown", s.handleShutdown)

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("control socket server error",
				"component", s.component,
				"error", err,
			)
		}
	}()

	return nil
}

// Stop gracefully shuts down the control socket server.
func (s *Server) Stop(ctx context.Context) error {
	s.running.Store(false)

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown http server: %w", err)
		}
	}

	// Close listener if httpServer didn't handle it
	if s.listener != nil {
		if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Warn("failed to close control socket listener",
				"component", s.component,
				"error", err,
			)
		}
	}

	// Clean up socket file
	if s.socketPath != "" {
		if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove control socket file",
				"component", s.component,
				"path", s.socketPath,
				"error", err,
			)
		}
	}

	return nil
}

// handleHealth returns health status.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeJSON(w, http.StatusOK, resp); err != nil {
		slog.Error("failed to write health response",
			"component", s.component,
			"error", err,
		)
	}
}

// handleStatus returns running status.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	resp := StatusResponse{
		Running:       s.running.Load(),
		PID:           os.Getpid(),
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		Component:     s.component,
	}
	if err := writeJSON(w, http.StatusOK, resp); err != nil {
		slog.Error("failed to write status response",
			"component", s.component,
			"error", err,
		)
	}
}

// handleShutdown initiates graceful shutdown.
func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	resp := ShutdownResponse{
		Message: "shutdown initiated",
	}
	if err := writeJSON(w, http.StatusOK, resp); err != nil {
		slog.Error("failed to write shutdown response",
			"component", s.component,
			"error", err,
		)
	}

	// Trigger shutdown asynchronously
	if s.shutdownFunc != nil {
		go s.shutdownFunc()
	}
}

// writeJSON writes a JSON response with the given status code.
// Returns an error if JSON encoding fails.
//
//nolint:unparam // statusCode is always http.StatusOK currently, but API is designed for extensibility
func writeJSON(w http.ResponseWriter, statusCode int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("failed to encode JSON response: %w", err)
	}
	return nil
}
