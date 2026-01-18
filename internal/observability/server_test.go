package observability

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
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

func TestServer_ConcurrentStopCalls(t *testing.T) {
	// This test verifies that Stop() uses CompareAndSwap for atomic state transition.
	// Multiple concurrent Stop() calls should be safe and idempotent.
	// Only one Stop() should actually perform the shutdown; others should return nil.
	server := NewServer("127.0.0.1:0", nil)

	errCh, err := server.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Drain error channel in background
	go func() {
		for range errCh { //nolint:revive // intentional empty block to drain channel
		}
	}()

	// Launch multiple concurrent Stop calls
	const numStoppers = 10
	var wg sync.WaitGroup
	wg.Add(numStoppers)

	for i := 0; i < numStoppers; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// All Stop calls should complete without error
			if err := server.Stop(ctx); err != nil {
				t.Errorf("Stop should not error: %v", err)
			}
		}()
	}

	wg.Wait()

	// After all stops, server should not be running - Start() should succeed
	errCh2, err := server.Start()
	if err != nil {
		t.Fatalf("Start after Stop should succeed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	// Drain error channel
	go func() {
		for range errCh2 { //nolint:revive // intentional empty block to drain channel
		}
	}()

	if server.Addr() == "" {
		t.Error("server should be running after Start")
	}
}

func TestServer_StopContextTimeout(t *testing.T) {
	// This test verifies that when Stop() times out due to active connections,
	// the server returns an error and restores the running state so it can be retried.
	server := NewServer("127.0.0.1:0", nil)

	errCh, err := server.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Drain error channel in background
	go func() {
		for range errCh { //nolint:revive // intentional empty block to drain channel
		}
	}()

	addr := server.Addr()
	if addr == "" {
		t.Fatal("server should be running")
	}

	// Create a connection that will hold open during shutdown.
	// We use a slow handler response to keep the connection active.
	// Open a connection and make it hang by not completing the request.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send a partial HTTP request (connection stays open waiting for more data)
	_, err = conn.Write([]byte("GET /healthz/liveness HTTP/1.1\r\n"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Use a very short timeout that will expire before the connection drains
	shortCtx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Small delay to ensure the timeout context expires
	time.Sleep(5 * time.Millisecond)

	// Stop with expired context should fail because connection is still active
	err = server.Stop(shortCtx)
	if err == nil {
		t.Error("expected error when stopping with expired context and active connection")
	}

	// The running state should be restored (server still running)
	// Close the hanging connection first
	_ = conn.Close()

	// Now Stop with valid context should succeed
	validCtx, validCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer validCancel()

	if err := server.Stop(validCtx); err != nil {
		t.Fatalf("Stop with valid context should succeed: %v", err)
	}

	// Server should now be stopped - Start should work
	errCh2, err := server.Start()
	if err != nil {
		t.Fatalf("Start after successful Stop should work: %v", err)
	}

	// Clean up
	go func() {
		for range errCh2 { //nolint:revive // intentional empty block to drain channel
		}
	}()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	_ = server.Stop(cleanupCtx)
}

func TestServer_StopContextTimeoutRestoresState(t *testing.T) {
	// This test specifically verifies that when Stop() fails due to context timeout,
	// the running state is restored to true, allowing Stop() to be retried.
	// This tests the state restoration logic at line 149-150 in server.go.
	server := NewServer("127.0.0.1:0", nil)

	errCh, err := server.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Drain error channel in background
	go func() {
		for range errCh { //nolint:revive // intentional empty block to drain channel
		}
	}()

	addr := server.Addr()
	if addr == "" {
		t.Fatal("server should be running")
	}

	// Create a connection that will block in a handler during shutdown.
	// We make a partial request that keeps the connection in read state.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Send partial HTTP request to keep connection active
	_, err = conn.Write([]byte("GET /healthz/liveness HTTP/1.1\r\n"))
	if err != nil {
		_ = conn.Close()
		t.Fatalf("failed to write: %v", err)
	}

	// Use a very short timeout - this should fail
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond) // Ensure context expires
	shortCancel()

	// First Stop should fail due to timeout
	err = server.Stop(shortCtx)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected error when stopping with expired context and active connection")
	}

	// Key assertion: after failed Stop(), we should be able to call Stop() again.
	// If running state wasn't restored, this second Stop() would return nil immediately
	// without actually shutting down.

	// Close the connection so shutdown can succeed
	_ = conn.Close()

	// Second Stop() with valid context should succeed and actually shut down
	validCtx, validCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer validCancel()

	if err := server.Stop(validCtx); err != nil {
		t.Fatalf("second Stop with valid context should succeed: %v", err)
	}

	// Verify shutdown actually happened by confirming Start() works
	errCh2, err := server.Start()
	if err != nil {
		t.Fatalf("Start after successful Stop should work: %v", err)
	}

	// Clean up
	go func() {
		for range errCh2 { //nolint:revive // intentional empty block to drain channel
		}
	}()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	_ = server.Stop(cleanupCtx)
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
