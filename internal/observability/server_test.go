package observability

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServer_Metrics(t *testing.T) {
	// Create server with always-ready checker
	server := NewServer("127.0.0.1:0", func() bool { return true })

	if _, err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	addr := server.Addr()
	if addr == "" {
		t.Fatal("server address is empty")
	}

	// Make request to /metrics
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	// Check for Prometheus format indicators
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "# HELP") {
		t.Error("expected Prometheus format with HELP comments")
	}
	if !strings.Contains(bodyStr, "# TYPE") {
		t.Error("expected Prometheus format with TYPE comments")
	}

	// Check for standard Go metrics
	if !strings.Contains(bodyStr, "go_") {
		t.Error("expected go_* metrics")
	}

	// Check for process metrics
	if !strings.Contains(bodyStr, "process_") {
		t.Error("expected process_* metrics")
	}

	// Increment custom metrics so they appear in output
	metrics := server.Metrics()
	metrics.ConnectionsTotal.WithLabelValues("test").Inc()
	metrics.RequestsTotal.WithLabelValues("test", "ok").Inc()

	// Make another request to see the custom metrics
	resp2, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET /metrics (second request): %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	bodyStr2 := string(body2)

	// Check for custom metrics (they appear after being used)
	if !strings.Contains(bodyStr2, "holomush_connections_total") {
		t.Error("expected holomush_connections_total metric")
	}
	if !strings.Contains(bodyStr2, "holomush_requests_total") {
		t.Error("expected holomush_requests_total metric")
	}
}

func TestServer_LivenessReturns200(t *testing.T) {
	server := NewServer("127.0.0.1:0", nil)

	if _, err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	resp, err := http.Get("http://" + server.Addr() + "/healthz/liveness")
	if err != nil {
		t.Fatalf("failed to GET /healthz/liveness: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) != "ok" {
		t.Errorf("expected body 'ok', got %q", string(body))
	}
}

func TestServer_ReadinessWhenReady(t *testing.T) {
	// Create server with always-ready checker
	server := NewServer("127.0.0.1:0", func() bool { return true })

	if _, err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	resp, err := http.Get("http://" + server.Addr() + "/healthz/readiness")
	if err != nil {
		t.Fatalf("failed to GET /healthz/readiness: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) != "ok" {
		t.Errorf("expected body 'ok', got %q", string(body))
	}
}

func TestServer_ReadinessWhenNotReady(t *testing.T) {
	// Create server with never-ready checker
	server := NewServer("127.0.0.1:0", func() bool { return false })

	if _, err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	resp, err := http.Get("http://" + server.Addr() + "/healthz/readiness")
	if err != nil {
		t.Fatalf("failed to GET /healthz/readiness: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) != "not ready" {
		t.Errorf("expected body 'not ready', got %q", string(body))
	}
}

func TestServer_ReadinessWithNilChecker(t *testing.T) {
	// Create server with nil readiness checker (should default to ready)
	server := NewServer("127.0.0.1:0", nil)

	if _, err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	resp, err := http.Get("http://" + server.Addr() + "/healthz/readiness")
	if err != nil {
		t.Fatalf("failed to GET /healthz/readiness: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 with nil checker, got %d", resp.StatusCode)
	}
}

func TestServer_DoubleStartFails(t *testing.T) {
	server := NewServer("127.0.0.1:0", nil)

	if _, err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	// Second start should fail
	if _, err := server.Start(); err == nil {
		t.Error("expected error on double start, got nil")
	}
}

func TestServer_StopIdempotent(t *testing.T) {
	server := NewServer("127.0.0.1:0", nil)

	// Stop without start should not error
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Errorf("stop without start should not error: %v", err)
	}
}

func TestServer_ErrorChannelReportsServeErrors(t *testing.T) {
	// This test proves the bug fix: when the server encounters an error after Start() returns,
	// the caller can now detect it via the error channel.

	server := NewServer("127.0.0.1:0", nil)

	errCh, err := server.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Get the listener address before we close it
	addr := server.Addr()
	if addr == "" {
		t.Fatal("server address is empty")
	}

	// Force close the underlying listener to trigger an error in Serve()
	// This simulates a real-world scenario where the listener fails unexpectedly
	if server.listener != nil {
		_ = server.listener.Close()
	}

	// The error channel should receive the error from Serve()
	select {
	case serveErr := <-errCh:
		// We expect an error because we closed the listener
		if serveErr == nil {
			t.Error("expected an error from the error channel after closing listener")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for error on error channel - bug: server errors are not propagated")
	}

	// Clean up
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Stop(ctx)
}

func TestServer_ErrorChannelClosesOnNormalShutdown(t *testing.T) {
	// Verify the error channel closes gracefully on normal shutdown (no error sent)
	server := NewServer("127.0.0.1:0", nil)

	errCh, err := server.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Normal shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}

	// The error channel should be closed (receive nil or closed)
	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			t.Errorf("unexpected error on normal shutdown: %v", err)
		}
		// ok=false (closed) or ok=true with err=nil are both acceptable
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for error channel to close")
	}
}

func TestServer_MetricsIncrement(t *testing.T) {
	server := NewServer("127.0.0.1:0", func() bool { return true })

	if _, err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	// Increment custom metrics
	metrics := server.Metrics()
	metrics.ConnectionsTotal.WithLabelValues("telnet").Inc()
	metrics.ConnectionsTotal.WithLabelValues("telnet").Inc()
	metrics.RequestsTotal.WithLabelValues("command", "success").Inc()

	// Check metrics endpoint includes our incremented values
	resp, err := http.Get("http://" + server.Addr() + "/metrics")
	if err != nil {
		t.Fatalf("failed to GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)

	// Check telnet connections were counted
	if !strings.Contains(bodyStr, `holomush_connections_total{type="telnet"} 2`) {
		t.Error("expected telnet connections counter to be 2")
	}

	// Check request was counted
	if !strings.Contains(bodyStr, `holomush_requests_total{status="success",type="command"} 1`) {
		t.Error("expected command request counter to be 1")
	}
}
