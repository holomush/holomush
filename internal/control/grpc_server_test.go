package control

import (
	"context"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	controlv1 "github.com/holomush/holomush/internal/proto/holomush/control/v1"
)

func TestGRPCServer_NewGRPCServer(t *testing.T) {
	s := NewGRPCServer("core", nil)

	if !s.running.Load() {
		t.Error("server should be running after creation")
	}

	if s.component != "core" {
		t.Errorf("component = %q, want %q", s.component, "core")
	}
}

func TestGRPCServer_Status_ReturnsCorrectInfo(t *testing.T) {
	s := NewGRPCServer("test-component", nil)
	// Wait a bit to ensure uptime > 0
	time.Sleep(10 * time.Millisecond)

	resp, err := s.Status(context.Background(), &controlv1.StatusRequest{})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if !resp.Running {
		t.Error("running should be true")
	}

	//nolint:gosec // G115: PID values never exceed int32 range
	expectedPID := int32(os.Getpid())
	if resp.Pid != expectedPID {
		t.Errorf("pid = %d, want %d", resp.Pid, expectedPID)
	}

	if resp.UptimeSeconds < 0 {
		t.Errorf("uptime_seconds = %d, should be non-negative", resp.UptimeSeconds)
	}

	if resp.Component != "test-component" {
		t.Errorf("component = %q, want %q", resp.Component, "test-component")
	}
}

func TestGRPCServer_Shutdown_TriggersCallback(t *testing.T) {
	var shutdownCalled atomic.Bool

	s := NewGRPCServer("core", func() {
		shutdownCalled.Store(true)
	})

	resp, err := s.Shutdown(context.Background(), &controlv1.ShutdownRequest{Graceful: true})
	if err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if resp.Message != "shutdown initiated" {
		t.Errorf("message = %q, want %q", resp.Message, "shutdown initiated")
	}

	// Wait for async shutdown callback
	time.Sleep(50 * time.Millisecond)

	if !shutdownCalled.Load() {
		t.Error("shutdown callback was not called")
	}
}

func TestGRPCServer_Shutdown_NilCallback(t *testing.T) {
	s := NewGRPCServer("core", nil)

	// Should not panic with nil callback
	resp, err := s.Shutdown(context.Background(), &controlv1.ShutdownRequest{Graceful: true})
	if err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if resp.Message != "shutdown initiated" {
		t.Errorf("message = %q, want %q", resp.Message, "shutdown initiated")
	}
}

func TestGRPCServer_Stop_SetsRunningFalse(t *testing.T) {
	s := NewGRPCServer("test", nil)

	if !s.running.Load() {
		t.Error("server should be running after creation")
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	if s.running.Load() {
		t.Error("server should not be running after Stop()")
	}
}

// TestGRPCServer_IntegrationWithInsecure tests the gRPC server with an insecure
// connection (no TLS) for easier testing without certificate setup.
func TestGRPCServer_IntegrationWithInsecure(t *testing.T) {
	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	var shutdownCalled atomic.Bool
	s := NewGRPCServer("integration-test", func() {
		shutdownCalled.Store(true)
	})

	// Start server without TLS for testing
	serverListener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s.listener = serverListener
	s.grpcServer = grpc.NewServer()
	controlv1.RegisterControlServer(s.grpcServer, s)

	go func() {
		_ = s.grpcServer.Serve(serverListener)
	}()

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Create client
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlClient(conn)

	t.Run("Status", func(t *testing.T) {
		resp, err := client.Status(context.Background(), &controlv1.StatusRequest{})
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}

		if !resp.Running {
			t.Error("running should be true")
		}

		if resp.Component != "integration-test" {
			t.Errorf("component = %q, want %q", resp.Component, "integration-test")
		}

		if resp.Pid <= 0 {
			t.Errorf("pid = %d, should be positive", resp.Pid)
		}

		if resp.UptimeSeconds < 0 {
			t.Errorf("uptime_seconds = %d, should be non-negative", resp.UptimeSeconds)
		}
	})

	t.Run("Shutdown", func(t *testing.T) {
		resp, err := client.Shutdown(context.Background(), &controlv1.ShutdownRequest{Graceful: true})
		if err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}

		if resp.Message != "shutdown initiated" {
			t.Errorf("message = %q, want %q", resp.Message, "shutdown initiated")
		}

		// Wait for async shutdown callback
		time.Sleep(50 * time.Millisecond)

		if !shutdownCalled.Load() {
			t.Error("shutdown callback was not called")
		}
	})
}

func TestGRPCServer_Stop_HandlesNilServer(t *testing.T) {
	s := NewGRPCServer("test", nil)
	// grpcServer and listener are nil

	err := s.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop should succeed with nil server components, got: %v", err)
	}
}

func TestGRPCServer_ConcurrentStatusRequests(t *testing.T) {
	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	s := NewGRPCServer("concurrent-test", nil)

	// Start server without TLS for testing
	serverListener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s.listener = serverListener
	s.grpcServer = grpc.NewServer()
	controlv1.RegisterControlServer(s.grpcServer, s)

	go func() {
		_ = s.grpcServer.Serve(serverListener)
	}()

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Create client
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlClient(conn)

	const numRequests = 50
	results := make(chan error, numRequests)

	// Launch concurrent status requests
	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := client.Status(context.Background(), &controlv1.StatusRequest{})
			if err != nil {
				results <- err
				return
			}

			if !resp.Running {
				results <- err
				return
			}

			results <- nil
		}()
	}

	// Collect results
	var errors []error
	for i := 0; i < numRequests; i++ {
		if err := <-results; err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		t.Errorf("concurrent requests failed: %v", errors)
	}
}

func TestLoadControlServerTLS_FailsWithMissingCerts(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadControlServerTLS(tmpDir, "core")
	if err == nil {
		t.Fatal("LoadControlServerTLS should fail with missing certificates")
	}
}
