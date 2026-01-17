package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/control"
	"github.com/holomush/holomush/internal/core"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	corev1 "github.com/holomush/holomush/internal/proto/holomush/core/v1"
	"google.golang.org/grpc"
)

func TestCoreCommand_Flags(t *testing.T) {
	cmd := NewCoreCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Verify all expected flags are present
	expectedFlags := []string{
		"--grpc-addr",
		"--control-socket",
		"--data-dir",
		"--game-id",
		"--log-format",
	}

	for _, flag := range expectedFlags {
		if !strings.Contains(output, flag) {
			t.Errorf("Help missing %q flag", flag)
		}
	}
}

func TestCoreCommand_DefaultValues(t *testing.T) {
	cmd := NewCoreCmd()

	// Check default grpc-addr
	grpcAddr, err := cmd.Flags().GetString("grpc-addr")
	if err != nil {
		t.Fatalf("Failed to get grpc-addr flag: %v", err)
	}
	if grpcAddr != "localhost:9000" {
		t.Errorf("grpc-addr default = %q, want %q", grpcAddr, "localhost:9000")
	}

	// Check default log-format
	logFormat, err := cmd.Flags().GetString("log-format")
	if err != nil {
		t.Fatalf("Failed to get log-format flag: %v", err)
	}
	if logFormat != "json" {
		t.Errorf("log-format default = %q, want %q", logFormat, "json")
	}

	// Check other flags have empty defaults
	controlSocket, err := cmd.Flags().GetString("control-socket")
	if err != nil {
		t.Fatalf("Failed to get control-socket flag: %v", err)
	}
	if controlSocket != "" {
		t.Errorf("control-socket default = %q, want empty string", controlSocket)
	}

	dataDir, err := cmd.Flags().GetString("data-dir")
	if err != nil {
		t.Fatalf("Failed to get data-dir flag: %v", err)
	}
	if dataDir != "" {
		t.Errorf("data-dir default = %q, want empty string", dataDir)
	}

	gameID, err := cmd.Flags().GetString("game-id")
	if err != nil {
		t.Fatalf("Failed to get game-id flag: %v", err)
	}
	if gameID != "" {
		t.Errorf("game-id default = %q, want empty string", gameID)
	}
}

func TestCoreCommand_Properties(t *testing.T) {
	cmd := NewCoreCmd()

	if cmd.Use != "core" {
		t.Errorf("Use = %q, want %q", cmd.Use, "core")
	}

	if !strings.Contains(cmd.Short, "core") {
		t.Error("Short description should mention core")
	}

	if !strings.Contains(cmd.Long, "game engine") {
		t.Error("Long description should mention game engine")
	}
}

func TestCoreCommand_NoDatabaseURL(t *testing.T) {
	// Ensure DATABASE_URL is not set for this test
	t.Setenv("DATABASE_URL", "")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"core"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when DATABASE_URL is not set")
	}

	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("Error should mention DATABASE_URL, got: %v", err)
	}
}

func TestCoreCommand_InvalidDatabaseURL(t *testing.T) {
	// Set an invalid DATABASE_URL
	t.Setenv("DATABASE_URL", "invalid://not-a-real-db")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"core"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error with invalid DATABASE_URL")
	}

	if !strings.Contains(err.Error(), "connect") && !strings.Contains(err.Error(), "database") {
		t.Errorf("Error should mention connection/database issue, got: %v", err)
	}
}

func TestCoreCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantAddr string
		wantFmt  string
	}{
		{
			name:     "default values",
			args:     []string{"--help"},
			wantAddr: "localhost:9000",
			wantFmt:  "json",
		},
		{
			name:     "custom grpc addr",
			args:     []string{"--grpc-addr=0.0.0.0:8080", "--help"},
			wantAddr: "0.0.0.0:8080",
			wantFmt:  "json",
		},
		{
			name:     "text log format",
			args:     []string{"--log-format=text", "--help"},
			wantAddr: "localhost:9000",
			wantFmt:  "text",
		},
		{
			name:     "all custom flags",
			args:     []string{"--grpc-addr=127.0.0.1:7000", "--log-format=text", "--help"},
			wantAddr: "127.0.0.1:7000",
			wantFmt:  "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCoreCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs(tt.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			addr, _ := cmd.Flags().GetString("grpc-addr")
			if addr != tt.wantAddr {
				t.Errorf("grpc-addr = %q, want %q", addr, tt.wantAddr)
			}

			fmtVal, _ := cmd.Flags().GetString("log-format")
			if fmtVal != tt.wantFmt {
				t.Errorf("log-format = %q, want %q", fmtVal, tt.wantFmt)
			}
		})
	}
}

func TestSetupLogging(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		wantError bool
	}{
		{
			name:      "json format",
			format:    "json",
			wantError: false,
		},
		{
			name:      "text format",
			format:    "text",
			wantError: false,
		},
		{
			name:      "invalid format",
			format:    "invalid",
			wantError: true,
		},
		{
			name:      "empty format",
			format:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := setupLogging(tt.format)
			if (err != nil) != tt.wantError {
				t.Errorf("setupLogging(%q) error = %v, wantError = %v", tt.format, err, tt.wantError)
			}
		})
	}
}

func TestEnsureTLSCerts(t *testing.T) {
	// Create a temp directory for certs
	tmpDir, err := os.MkdirTemp("", "holomush-test-certs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	gameID := "test-game-id"

	// First call should generate new certs
	config1, err := ensureTLSCerts(tmpDir, gameID)
	if err != nil {
		t.Fatalf("ensureTLSCerts() error = %v", err)
	}
	if config1 == nil {
		t.Fatal("ensureTLSCerts() returned nil config")
	}

	// Verify certificates were created
	expectedFiles := []string{
		"root-ca.crt",
		"root-ca.key",
		"core.crt",
		"core.key",
		"gateway.crt",
		"gateway.key",
	}
	for _, file := range expectedFiles {
		path := tmpDir + "/" + file
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not created", file)
		}
	}

	// Second call should load existing certs
	config2, err := ensureTLSCerts(tmpDir, gameID)
	if err != nil {
		t.Fatalf("ensureTLSCerts() second call error = %v", err)
	}
	if config2 == nil {
		t.Fatal("ensureTLSCerts() second call returned nil config")
	}
}

func TestCoreCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"core", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Verify help contains expected sections
	expectedPhrases := []string{
		"Start the core process",
		"game engine",
		"--grpc-addr",
		"--log-format",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(output, phrase) {
			t.Errorf("Help missing phrase %q", phrase)
		}
	}
}

// =============================================================================
// Integration Tests: Core Process Lifecycle (holomush-m96)
// =============================================================================

// createSocketTempDir creates a temp directory in /tmp directly (not TMPDIR)
// because Unix sockets may not work in sandboxed temp directories like /tmp/claude.
func createSocketTempDir(t *testing.T, name string) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("/tmp", "holomush-"+name+"-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	return tmpDir
}

// TestCoreIntegration_StartAndStopGracefully tests that the core components
// start and stop gracefully together. This tests the integration between
// gRPC server, control socket, and core components.
func TestCoreIntegration_StartAndStopGracefully(t *testing.T) {
	// Use a temporary directory for the control socket
	tmpDir := createSocketTempDir(t, "core-integration")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Create core components (without database)
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	eventStore := core.NewMemoryEventStore()
	engine := core.NewEngine(eventStore, sessions, broadcaster)

	// Create gRPC server (insecure for testing)
	grpcServer := holoGRPC.NewGRPCServerInsecure()
	coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster)
	corev1.RegisterCoreServer(grpcServer, coreServer)

	// Start gRPC listener on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	// Create context for shutdown coordination
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create control socket server
	var shutdownCalled atomic.Bool
	controlServer := control.NewServer("core", func() {
		shutdownCalled.Store(true)
		cancel()
	})
	if err := controlServer.Start(); err != nil {
		t.Fatalf("failed to start control socket: %v", err)
	}

	// Start gRPC server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if serveErr := grpcServer.Serve(listener); serveErr != nil {
			errChan <- serveErr
		}
	}()

	// Give servers time to start
	time.Sleep(50 * time.Millisecond)

	// Verify control socket is responding
	socketPath := tmpDir + "/holomush/holomush-core.sock"
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://localhost/health")
	if err != nil {
		t.Fatalf("failed to check health: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check failed: status = %d", resp.StatusCode)
	}

	// Initiate shutdown
	cancel()

	// Stop servers gracefully
	grpcServer.GracefulStop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := controlServer.Stop(shutdownCtx); err != nil {
		t.Errorf("control socket stop error: %v", err)
	}

	// Verify socket file was cleaned up
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after shutdown")
	}
}

// TestCoreIntegration_ControlSocketHealth tests that the control socket
// responds correctly to health checks during core operation.
func TestCoreIntegration_ControlSocketHealth(t *testing.T) {
	tmpDir := createSocketTempDir(t, "core-health")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Create control socket server
	controlServer := control.NewServer("core", nil)
	if err := controlServer.Start(); err != nil {
		t.Fatalf("failed to start control socket: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = controlServer.Stop(ctx)
	}()

	// Create HTTP client for control socket
	socketPath := tmpDir + "/holomush/holomush-core.sock"
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// Test /health endpoint
	resp, err := client.Get("http://localhost/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var health control.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	if health.Status != "healthy" {
		t.Errorf("health.Status = %q, want %q", health.Status, "healthy")
	}

	if health.Timestamp == "" {
		t.Error("health.Timestamp should not be empty")
	}

	// Verify timestamp is valid RFC3339
	if _, err := time.Parse(time.RFC3339, health.Timestamp); err != nil {
		t.Errorf("health.Timestamp %q is not valid RFC3339: %v", health.Timestamp, err)
	}
}

// TestCoreIntegration_GracefulShutdownOnContextCancellation tests that
// all components shut down gracefully when the context is cancelled.
func TestCoreIntegration_GracefulShutdownOnContextCancellation(t *testing.T) {
	tmpDir := createSocketTempDir(t, "core-shutdown")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Create core components
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	eventStore := core.NewMemoryEventStore()
	engine := core.NewEngine(eventStore, sessions, broadcaster)

	// Create gRPC server
	grpcServer := holoGRPC.NewGRPCServerInsecure()
	coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster)
	corev1.RegisterCoreServer(grpcServer, coreServer)

	// Start gRPC listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	// Create context for shutdown coordination
	ctx, cancel := context.WithCancel(context.Background())

	// Track shutdown completion
	var shutdownComplete atomic.Bool

	// Create control socket with shutdown callback
	controlServer := control.NewServer("core", func() {
		cancel()
	})
	if err := controlServer.Start(); err != nil {
		t.Fatalf("failed to start control socket: %v", err)
	}

	// Start gRPC server
	grpcDone := make(chan struct{})
	go func() {
		_ = grpcServer.Serve(listener)
		close(grpcDone)
	}()

	// Give servers time to start
	time.Sleep(50 * time.Millisecond)

	// Verify servers are running
	socketPath := tmpDir + "/holomush/holomush-core.sock"
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://localhost/status")
	if err != nil {
		t.Fatalf("status check failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status check: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Cancel context to trigger shutdown
	cancel()

	// Perform graceful shutdown in goroutine
	go func() {
		grpcServer.GracefulStop()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = controlServer.Stop(shutdownCtx)

		shutdownComplete.Store(true)
	}()

	// Wait for context to be done
	<-ctx.Done()

	// Wait for gRPC server to stop
	select {
	case <-grpcDone:
		// Good, gRPC server stopped
	case <-time.After(5 * time.Second):
		t.Error("gRPC server did not stop within timeout")
	}

	// Wait for complete shutdown
	time.Sleep(100 * time.Millisecond)

	if !shutdownComplete.Load() {
		t.Error("shutdown did not complete")
	}

	// Verify socket file was cleaned up
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after shutdown")
	}
}

// TestCoreIntegration_GRPCAndControlSocketTogether tests that both the
// gRPC server and control socket can run simultaneously and independently.
func TestCoreIntegration_GRPCAndControlSocketTogether(t *testing.T) {
	tmpDir := createSocketTempDir(t, "core-both")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Create core components
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	eventStore := core.NewMemoryEventStore()
	engine := core.NewEngine(eventStore, sessions, broadcaster)

	// Create gRPC server
	grpcServer := grpc.NewServer()
	coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster)
	corev1.RegisterCoreServer(grpcServer, coreServer)

	// Start gRPC listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	grpcAddr := listener.Addr().String()

	// Create control socket
	controlServer := control.NewServer("core", nil)
	if err := controlServer.Start(); err != nil {
		t.Fatalf("failed to start control socket: %v", err)
	}

	// Start gRPC server
	go func() {
		_ = grpcServer.Serve(listener)
	}()

	// Give servers time to start
	time.Sleep(50 * time.Millisecond)

	// Test control socket
	socketPath := tmpDir + "/holomush/holomush-core.sock"
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := httpClient.Get("http://localhost/health")
	if err != nil {
		t.Fatalf("control socket health check failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("control socket health: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Test gRPC server is listening
	conn, err := net.DialTimeout("tcp", grpcAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("gRPC server not reachable: %v", err)
	}
	_ = conn.Close()

	// Clean up
	grpcServer.GracefulStop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := controlServer.Stop(shutdownCtx); err != nil {
		t.Errorf("control socket stop error: %v", err)
	}
}
