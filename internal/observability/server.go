// Package observability provides HTTP endpoints for metrics and health checks.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ReadinessChecker returns whether the service is ready to accept connections.
type ReadinessChecker func() bool

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
func (s *Server) Start() error {
	if !s.running.CompareAndSwap(false, true) {
		return fmt.Errorf("observability server already running")
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.running.Store(false)
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
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

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if serveErr := s.httpServer.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			slog.Error("observability server error", "error", serveErr)
		}
	}()

	slog.Info("observability server started", "addr", listener.Addr().String())
	return nil
}

// Stop gracefully shuts down the observability server.
func (s *Server) Stop(ctx context.Context) error {
	if !s.running.Load() {
		return nil
	}

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown observability server: %w", err)
		}
	}

	s.running.Store(false)
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
	_, _ = w.Write([]byte("ok\n"))
}

// handleReadiness returns 200 if the service is ready to accept connections,
// or 503 if not ready.
func (s *Server) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if s.isReady == nil || s.isReady() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
		return
	}

	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("not ready\n"))
}
