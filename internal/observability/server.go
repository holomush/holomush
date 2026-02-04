// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package observability provides HTTP endpoints for metrics and health checks.
package observability

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/samber/oops"
)

// ReadinessChecker returns whether the service is ready to accept connections.
type ReadinessChecker func() bool

// commandOutputFailures is a package-level counter for command output write failures.
// This allows handlers to increment the metric without needing access to the Server instance.
var commandOutputFailures = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "holomush_command_output_failures_total",
		Help: "Total number of command output write failures by command",
	},
	[]string{"command"},
)

// RecordCommandOutputFailure increments the command output failure counter.
// Called by command handlers when output write fails.
func RecordCommandOutputFailure(command string) {
	commandOutputFailures.WithLabelValues(command).Inc()
}

// Metrics contains custom Prometheus metrics for HoloMUSH.
type Metrics struct {
	ConnectionsTotal *prometheus.CounterVec
	RequestsTotal    *prometheus.CounterVec
}

// NewMetrics creates and registers custom HoloMUSH metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		ConnectionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "holomush_connections_total",
				Help: "Total number of connections by type",
			},
			[]string{"type"},
		),
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "holomush_requests_total",
				Help: "Total number of requests by type and status",
			},
			[]string{"type", "status"},
		),
	}

	reg.MustRegister(m.ConnectionsTotal)
	reg.MustRegister(m.RequestsTotal)
	reg.MustRegister(commandOutputFailures)

	return m
}

// Server provides HTTP endpoints for observability (metrics and health probes).
type Server struct {
	addr       string
	listener   net.Listener
	httpServer *http.Server
	registry   *prometheus.Registry
	metrics    *Metrics
	isReady    ReadinessChecker
	running    atomic.Bool
}

// NewServer creates a new observability server.
// addr: listen address in "host:port" format (e.g., "127.0.0.1:9100", ":9100" for all interfaces).
func NewServer(addr string, readinessChecker ReadinessChecker) *Server {
	// Create a new registry to avoid polluting the global one
	registry := prometheus.NewRegistry()

	// Register standard Go metrics
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// Register custom metrics
	metrics := NewMetrics(registry)

	s := &Server{
		addr:     addr,
		registry: registry,
		metrics:  metrics,
		isReady:  readinessChecker,
	}

	return s
}

// Metrics returns the custom metrics for recording application events.
func (s *Server) Metrics() *Metrics {
	return s.metrics
}

// Start begins serving observability endpoints.
// It returns an error channel that will receive any errors from the HTTP server
// after it starts. The channel is closed when the server stops gracefully.
// Callers should monitor this channel to detect server failures.
func (s *Server) Start() (<-chan error, error) {
	if !s.running.CompareAndSwap(false, true) {
		return nil, oops.Errorf("observability server already running")
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.running.Store(false)
		return nil, oops.With("addr", s.addr).Wrap(err)
	}
	s.listener = listener

	mux := http.NewServeMux()

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))

	// Kubernetes-style health probes
	mux.HandleFunc("/healthz/liveness", s.handleLiveness)
	mux.HandleFunc("/healthz/readiness", s.handleReadiness)

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.httpServer = httpSrv

	// Create buffered error channel so the goroutine doesn't block
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)
		// Use local httpSrv to avoid race with subsequent Start() calls
		if serveErr := httpSrv.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			slog.Error("observability server error", "error", serveErr)
			errCh <- serveErr
		}
	}()

	slog.Info("observability server started", "addr", listener.Addr().String())
	return errCh, nil
}

// Stop gracefully shuts down the observability server.
func (s *Server) Stop(ctx context.Context) error {
	// Use CompareAndSwap to atomically transition from running to stopped.
	// This prevents a race where a concurrent Start() could succeed between
	// checking the running state and setting it to false.
	if !s.running.CompareAndSwap(true, false) {
		return nil
	}

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			// Restore running state on failure so the server can be stopped again
			s.running.Store(true)
			return oops.With("operation", "shutdown_observability_server").Wrap(err)
		}
	}

	slog.Info("observability server stopped")
	return nil
}

// Addr returns the address the server is listening on.
// Returns empty string if not running.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// handleLiveness returns 200 if the process is running.
// This is a simple check that the process is alive.
func (s *Server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	//nolint:errcheck // health check write error is acceptable, client may disconnect
	w.Write([]byte("ok\n"))
}

// handleReadiness returns 200 if the service is ready to accept connections,
// or 503 if not ready.
func (s *Server) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if s.isReady == nil || s.isReady() {
		w.WriteHeader(http.StatusOK)
		//nolint:errcheck // health check write error is acceptable, client may disconnect
		w.Write([]byte("ok\n"))
		return
	}

	w.WriteHeader(http.StatusServiceUnavailable)
	//nolint:errcheck // health check write error is acceptable, client may disconnect
	w.Write([]byte("not ready\n"))
}
