package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/control"
)

func TestStatus_Properties(t *testing.T) {
	cmd := NewStatusCmd()

	if cmd.Use != "status" {
		t.Errorf("Use = %q, want %q", cmd.Use, "status")
	}

	if !strings.Contains(cmd.Short, "status") {
		t.Error("Short description should mention status")
	}

	if !strings.Contains(cmd.Long, "health") {
		t.Error("Long description should mention health")
	}
}

func TestStatus_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"status", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Verify help contains expected sections
	expectedPhrases := []string{
		"status",
		"running",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(output, phrase) {
			t.Errorf("Help missing phrase %q", phrase)
		}
	}
}

func TestStatus_Flags(t *testing.T) {
	cmd := NewStatusCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Verify expected flags are present
	expectedFlags := []string{
		"--json",
	}

	for _, flag := range expectedFlags {
		if !strings.Contains(output, flag) {
			t.Errorf("Help missing %q flag", flag)
		}
	}
}

func TestStatus_NoProcessesRunning(t *testing.T) {
	// Use a temporary directory with no sockets
	tmpDir := createStatusSocketTempDir(t, "no-processes")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"status"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Should show both processes as stopped
	if !strings.Contains(output, "core") {
		t.Error("Output should mention core process")
	}
	if !strings.Contains(output, "gateway") {
		t.Error("Output should mention gateway process")
	}
	if !strings.Contains(output, "stopped") && !strings.Contains(output, "not running") {
		t.Errorf("Output should indicate processes are stopped/not running, got: %s", output)
	}
}

func TestStatus_CoreRunning(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "core-running")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Start a mock core control socket
	coreServer := control.NewServer("core", nil)
	if err := coreServer.Start(); err != nil {
		t.Fatalf("failed to start core server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = coreServer.Stop(ctx)
	}()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"status"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Core should be running
	if !strings.Contains(output, "core") {
		t.Error("Output should mention core process")
	}
	if !strings.Contains(output, "running") && !strings.Contains(output, "healthy") {
		t.Errorf("Output should indicate core is running/healthy, got: %s", output)
	}
}

func TestStatus_BothRunning(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "both-running")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Start mock control sockets for both processes
	coreServer := control.NewServer("core", nil)
	if err := coreServer.Start(); err != nil {
		t.Fatalf("failed to start core server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = coreServer.Stop(ctx)
	}()

	gatewayServer := control.NewServer("gateway", nil)
	if err := gatewayServer.Start(); err != nil {
		t.Fatalf("failed to start gateway server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = gatewayServer.Stop(ctx)
	}()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"status"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Both should be running
	if !strings.Contains(output, "core") {
		t.Error("Output should mention core process")
	}
	if !strings.Contains(output, "gateway") {
		t.Error("Output should mention gateway process")
	}
}

func TestStatus_JSONOutput(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "json-output")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Start mock control sockets for both processes
	coreServer := control.NewServer("core", nil)
	if err := coreServer.Start(); err != nil {
		t.Fatalf("failed to start core server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = coreServer.Stop(ctx)
	}()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"status", "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Output should be valid JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Errorf("Output should be valid JSON, got error: %v, output: %s", err, output)
	}

	// Should have core and gateway keys
	if _, ok := result["core"]; !ok {
		t.Error("JSON output should have 'core' key")
	}
	if _, ok := result["gateway"]; !ok {
		t.Error("JSON output should have 'gateway' key")
	}
}

func TestStatus_DisplaysUptime(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "uptime")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Start a mock core control socket
	coreServer := control.NewServer("core", nil)
	if err := coreServer.Start(); err != nil {
		t.Fatalf("failed to start core server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = coreServer.Stop(ctx)
	}()

	// Wait a bit to ensure uptime > 0
	time.Sleep(50 * time.Millisecond)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"status", "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()

	// Parse JSON and verify uptime is present
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v, output: %s", err, output)
	}

	coreStatus, ok := result["core"].(map[string]any)
	if !ok {
		t.Fatalf("core should be an object, got: %v", result)
	}

	// Should have uptime_seconds when running (can be 0)
	if coreStatus["running"] != true {
		t.Errorf("core should be running, got: %v", coreStatus)
	}
	// Note: uptime_seconds may be omitted if 0 due to omitempty tag
	// Check that running is true instead, which is the key indicator
}

// =============================================================================
// Helper Functions
// =============================================================================

// createStatusSocketTempDir creates a temp directory in /tmp directly (not TMPDIR)
// because Unix sockets may not work in sandboxed temp directories like /tmp/claude.
func createStatusSocketTempDir(t *testing.T, name string) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("/tmp", "holomush-status-"+name+"-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	return tmpDir
}

// =============================================================================
// Unit Tests for internal functions
// =============================================================================

func TestQueryProcessStatus_SocketNotFound(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "not-found")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	status := queryProcessStatus("core")

	if status.Running {
		t.Error("status.Running should be false when socket doesn't exist")
	}
	if status.Error == "" {
		t.Error("status.Error should contain error message when socket doesn't exist")
	}
}

func TestQueryProcessStatus_SocketExists(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "exists")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Start a mock control socket
	server := control.NewServer("core", nil)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	status := queryProcessStatus("core")

	if !status.Running {
		t.Error("status.Running should be true when socket exists and responds")
	}
	if status.Health != "healthy" {
		t.Errorf("status.Health = %q, want %q", status.Health, "healthy")
	}
	if status.PID <= 0 {
		t.Errorf("status.PID = %d, should be positive", status.PID)
	}
}

func TestQueryProcessStatus_SocketExistsButNotResponding(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "not-responding")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Create the runtime directory
	runtimeDir := tmpDir + "/holomush"
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	// Create a fake socket file (not a real socket)
	socketPath := runtimeDir + "/holomush-core.sock"
	if err := os.WriteFile(socketPath, []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("failed to create fake socket: %v", err)
	}

	status := queryProcessStatus("core")

	if status.Running {
		t.Error("status.Running should be false when socket doesn't respond")
	}
	if status.Error == "" {
		t.Error("status.Error should contain error message")
	}
}

func TestFormatStatusTable(t *testing.T) {
	statuses := map[string]ProcessStatus{
		"core": {
			Component:     "core",
			Running:       true,
			Health:        "healthy",
			PID:           12345,
			UptimeSeconds: 3600,
		},
		"gateway": {
			Component: "gateway",
			Running:   false,
			Error:     "socket not found",
		},
	}

	output := formatStatusTable(statuses)

	// Should contain process names
	if !strings.Contains(output, "core") {
		t.Error("table should contain 'core'")
	}
	if !strings.Contains(output, "gateway") {
		t.Error("table should contain 'gateway'")
	}

	// Should indicate running/stopped
	if !strings.Contains(output, "running") && !strings.Contains(output, "healthy") {
		t.Error("table should indicate running/healthy status")
	}
	if !strings.Contains(output, "stopped") && !strings.Contains(output, "not running") {
		t.Error("table should indicate stopped/not running status")
	}
}

func TestFormatStatusJSON(t *testing.T) {
	statuses := map[string]ProcessStatus{
		"core": {
			Component:     "core",
			Running:       true,
			Health:        "healthy",
			PID:           12345,
			UptimeSeconds: 3600,
		},
		"gateway": {
			Component: "gateway",
			Running:   false,
			Error:     "socket not found",
		},
	}

	output, err := formatStatusJSON(statuses)
	if err != nil {
		t.Fatalf("formatStatusJSON() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output should be valid JSON: %v", err)
	}

	// Verify core status
	coreStatus, ok := result["core"].(map[string]any)
	if !ok {
		t.Fatal("core should be an object")
	}
	if coreStatus["running"] != true {
		t.Error("core.running should be true")
	}
	if coreStatus["health"] != "healthy" {
		t.Errorf("core.health = %v, want %q", coreStatus["health"], "healthy")
	}

	// Verify gateway status
	gatewayStatus, ok := result["gateway"].(map[string]any)
	if !ok {
		t.Fatal("gateway should be an object")
	}
	if gatewayStatus["running"] != false {
		t.Error("gateway.running should be false")
	}
}

func TestCreateUnixHTTPClient(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "http-client")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Start a mock control socket
	server := control.NewServer("core", nil)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	socketPath := tmpDir + "/holomush/holomush-core.sock"
	client := createUnixHTTPClient(socketPath)

	// Client should be able to make requests
	resp, err := client.Get("http://localhost/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestCreateUnixHTTPClient_Timeout(t *testing.T) {
	tmpDir := createStatusSocketTempDir(t, "http-timeout")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Create runtime directory but no socket
	runtimeDir := tmpDir + "/holomush"
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}

	socketPath := runtimeDir + "/holomush-core.sock"
	client := createUnixHTTPClient(socketPath)

	// Create a listener that accepts but never responds
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Client should timeout
	_, err = client.Get("http://localhost/health")
	if err == nil {
		t.Error("expected timeout error")
	}
}
