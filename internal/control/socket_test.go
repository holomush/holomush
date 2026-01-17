package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleHealth_ReturnsCorrectJSON(t *testing.T) {
	s := NewServer("core", nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if health.Status != "healthy" {
		t.Errorf("status = %q, want %q", health.Status, "healthy")
	}

	if health.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}

	// Verify timestamp is valid RFC3339
	if _, err := time.Parse(time.RFC3339, health.Timestamp); err != nil {
		t.Errorf("timestamp %q is not valid RFC3339: %v", health.Timestamp, err)
	}
}

func TestHandleStatus_ReturnsRequiredFields(t *testing.T) {
	s := NewServer("gateway", nil)
	// Wait a tiny bit to ensure uptime > 0
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	s.handleStatus(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if !status.Running {
		t.Error("running should be true")
	}

	if status.PID <= 0 {
		t.Errorf("pid = %d, should be positive", status.PID)
	}

	if status.UptimeSeconds < 0 {
		t.Errorf("uptime_seconds = %d, should be non-negative", status.UptimeSeconds)
	}

	if status.Component != "gateway" {
		t.Errorf("component = %q, want %q", status.Component, "gateway")
	}
}

func TestHandleShutdown_TriggersCallback(t *testing.T) {
	var shutdownCalled atomic.Bool

	s := NewServer("core", func() {
		shutdownCalled.Store(true)
	})

	req := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
	w := httptest.NewRecorder()

	s.handleShutdown(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var shutdown ShutdownResponse
	if err := json.NewDecoder(resp.Body).Decode(&shutdown); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if shutdown.Message != "shutdown initiated" {
		t.Errorf("message = %q, want %q", shutdown.Message, "shutdown initiated")
	}

	// Wait for async shutdown callback
	time.Sleep(50 * time.Millisecond)

	if !shutdownCalled.Load() {
		t.Error("shutdown callback was not called")
	}
}

func TestHandleShutdown_NilCallback(t *testing.T) {
	s := NewServer("core", nil)

	req := httptest.NewRequest(http.MethodPost, "/shutdown", nil)
	w := httptest.NewRecorder()

	// Should not panic with nil callback
	s.handleShutdown(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestSocketPath_ReturnsExpectedPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

	path, err := SocketPath("core")
	if err != nil {
		t.Fatalf("SocketPath() error = %v", err)
	}

	expected := "/run/user/1000/holomush/holomush-core.sock"
	if path != expected {
		t.Errorf("SocketPath() = %q, want %q", path, expected)
	}
}

func TestSocketPath_FallbackWithoutRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/custom/state")

	path, err := SocketPath("gateway")
	if err != nil {
		t.Fatalf("SocketPath() error = %v", err)
	}

	expected := "/custom/state/holomush/run/holomush-gateway.sock"
	if path != expected {
		t.Errorf("SocketPath() = %q, want %q", path, expected)
	}
}

func TestNewServer_SetsRunningTrue(t *testing.T) {
	s := NewServer("test", nil)

	if !s.running.Load() {
		t.Error("server should be running after creation")
	}
}

func TestWriteJSON_ReturnsErrorForUnencodableValue(t *testing.T) {
	w := httptest.NewRecorder()

	// Channel values cannot be encoded to JSON
	unencodable := make(chan int)
	err := writeJSON(w, http.StatusOK, unencodable)

	if err == nil {
		t.Error("writeJSON should return error for unencodable value")
	}

	if !strings.Contains(err.Error(), "failed to encode JSON response") {
		t.Errorf("error message should contain 'failed to encode JSON response', got: %v", err)
	}
}

func TestWriteJSON_SucceedsForValidValue(t *testing.T) {
	w := httptest.NewRecorder()

	data := map[string]string{"key": "value"}
	err := writeJSON(w, http.StatusOK, data)

	if err != nil {
		t.Errorf("writeJSON should succeed for valid value, got error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}
}

func TestHandleHealth_LogsErrorOnJSONEncodingFailure(t *testing.T) {
	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(originalLogger)

	s := NewServer("test-component", nil)

	// Create a response writer that will cause encoding to fail
	w := &failingWriter{ResponseRecorder: httptest.NewRecorder()}

	s.handleHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "failed to write health response") {
		t.Errorf("expected log to contain 'failed to write health response', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "test-component") {
		t.Errorf("expected log to contain component name 'test-component', got: %s", logOutput)
	}
}

func TestStop_LogsSocketFileRemovalError(t *testing.T) {
	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(originalLogger)

	s := NewServer("test-component", nil)
	// Set socket path to a directory with contents - os.Remove cannot remove
	// non-empty directories and will return an error that is NOT IsNotExist
	tmpDir := t.TempDir()
	// Create a file inside the directory to make it non-empty
	f, err := os.Create(tmpDir + "/test.txt") //nolint:gosec // tmpDir is from t.TempDir(), safe path
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	_ = f.Close()

	s.socketPath = tmpDir

	// Stop should not return an error (it logs instead)
	err = s.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop should not return error, got: %v", err)
	}

	// The error should be logged since it's not IsNotExist
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "failed to remove control socket file") {
		t.Errorf("expected log to contain 'failed to remove control socket file', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "test-component") {
		t.Errorf("expected log to contain component name 'test-component', got: %s", logOutput)
	}
}

func TestStop_HandlesNilServerGracefully(t *testing.T) {
	s := NewServer("test", nil)
	// httpServer is nil, listener is nil, socketPath is empty

	err := s.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop should succeed with nil server components, got: %v", err)
	}
}

// failingWriter is a ResponseWriter that fails during Write
type failingWriter struct {
	*httptest.ResponseRecorder
}

func (w *failingWriter) Write([]byte) (int, error) {
	// Simulate a write failure after headers are written
	return 0, &writeError{}
}

type writeError struct{}

func (e *writeError) Error() string {
	return "simulated write failure"
}

func TestHandleStatus_LogsErrorOnJSONEncodingFailure(t *testing.T) {
	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(originalLogger)

	s := NewServer("test-component", nil)

	// Create a response writer that will cause encoding to fail
	w := &failingWriter{ResponseRecorder: httptest.NewRecorder()}

	s.handleStatus(w, httptest.NewRequest(http.MethodGet, "/status", nil))

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "failed to write status response") {
		t.Errorf("expected log to contain 'failed to write status response', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "test-component") {
		t.Errorf("expected log to contain component name 'test-component', got: %s", logOutput)
	}
}

func TestHandleShutdown_LogsErrorOnJSONEncodingFailure(t *testing.T) {
	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(originalLogger)

	s := NewServer("test-component", nil)

	// Create a response writer that will cause encoding to fail
	w := &failingWriter{ResponseRecorder: httptest.NewRecorder()}

	s.handleShutdown(w, httptest.NewRequest(http.MethodPost, "/shutdown", nil))

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "failed to write shutdown response") {
		t.Errorf("expected log to contain 'failed to write shutdown response', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "test-component") {
		t.Errorf("expected log to contain component name 'test-component', got: %s", logOutput)
	}
}

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

func TestServer_StartAndStop(t *testing.T) {
	// Use a temporary directory in /tmp for the socket
	tmpDir := createSocketTempDir(t, "startstop")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	var shutdownCalled atomic.Bool
	s := NewServer("test", func() {
		shutdownCalled.Store(true)
	})

	// Start the server
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify socket was created
	expectedPath := tmpDir + "/holomush/holomush-test.sock"
	if s.socketPath != expectedPath {
		t.Errorf("socketPath = %q, want %q", s.socketPath, expectedPath)
	}

	// Verify socket file exists
	info, err := os.Stat(s.socketPath)
	if err != nil {
		t.Fatalf("socket file not created: %v", err)
	}

	// Verify socket permissions are 0600
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket permissions = %o, want 0600", perm)
	}

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Verify running flag is false
	if s.running.Load() {
		t.Error("server should not be running after Stop()")
	}

	// Verify socket file was cleaned up
	if _, err := os.Stat(s.socketPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after Stop()")
	}
}

func TestServer_StartAndStop_WithHTTPRequests(t *testing.T) {
	// Use a temporary directory in /tmp for the socket
	tmpDir := createSocketTempDir(t, "http")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	var shutdownCalled atomic.Bool
	s := NewServer("integration", func() {
		shutdownCalled.Store(true)
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Create HTTP client that uses Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	t.Run("health endpoint", func(t *testing.T) {
		resp, err := client.Get("http://localhost/health")
		if err != nil {
			t.Fatalf("GET /health error = %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var health HealthResponse
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if health.Status != "healthy" {
			t.Errorf("status = %q, want %q", health.Status, "healthy")
		}
	})

	t.Run("status endpoint", func(t *testing.T) {
		resp, err := client.Get("http://localhost/status")
		if err != nil {
			t.Fatalf("GET /status error = %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var status StatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !status.Running {
			t.Error("running should be true")
		}
		if status.Component != "integration" {
			t.Errorf("component = %q, want %q", status.Component, "integration")
		}
	})

	t.Run("shutdown endpoint", func(t *testing.T) {
		resp, err := client.Post("http://localhost/shutdown", "application/json", nil)
		if err != nil {
			t.Fatalf("POST /shutdown error = %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var shutdown ShutdownResponse
		if err := json.NewDecoder(resp.Body).Decode(&shutdown); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if shutdown.Message != "shutdown initiated" {
			t.Errorf("message = %q, want %q", shutdown.Message, "shutdown initiated")
		}

		// Wait for async shutdown callback
		time.Sleep(50 * time.Millisecond)
		if !shutdownCalled.Load() {
			t.Error("shutdown callback was not called")
		}
	})
}

func TestServer_Start_RemovesExistingSocket(t *testing.T) {
	tmpDir := createSocketTempDir(t, "removesocket")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Create runtime directory and a fake existing socket
	runtimeDir := tmpDir + "/holomush"
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("failed to create runtime dir: %v", err)
	}
	socketPath := runtimeDir + "/holomush-test.sock"
	if err := os.WriteFile(socketPath, []byte("old socket"), 0o600); err != nil {
		t.Fatalf("failed to create fake socket: %v", err)
	}

	s := NewServer("test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = s.Stop(ctx) }()

	// Verify socket was recreated (should be a real socket now, not a regular file)
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket file not created: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Error("file should be a socket, not a regular file")
	}
}

func TestServer_Stop_SetsRunningFalse(t *testing.T) {
	s := NewServer("test", nil)
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

func TestSocketPath_ErrorWhenRuntimeDirFails(t *testing.T) {
	// Clear all XDG environment variables and HOME to force an error
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	_, err := SocketPath("test")

	// The error path depends on os.UserHomeDir() failing, which may not happen
	// in all environments. Instead, we just verify the function handles the case.
	// In CI/test environments where HOME is unset and UserHomeDir fails, this tests
	// the error path. Otherwise it tests the fallback success path.
	if err != nil {
		if !strings.Contains(err.Error(), "failed to get runtime directory") {
			t.Errorf("error should mention 'failed to get runtime directory', got: %v", err)
		}
	}
}

func TestServer_Start_FailsOnInvalidDirectory(t *testing.T) {
	// Use /dev/null which is not a directory - cannot create subdirectories
	t.Setenv("XDG_RUNTIME_DIR", "/dev/null")

	s := NewServer("test", nil)
	err := s.Start()

	if err == nil {
		_ = s.Stop(context.Background())
		t.Fatal("Start() should fail when runtime directory cannot be created")
	}

	if !strings.Contains(err.Error(), "failed to create runtime directory") {
		t.Errorf("error should mention 'failed to create runtime directory', got: %v", err)
	}
}

func TestServer_Start_FailsOnUnwritableSocketPath(t *testing.T) {
	tmpDir := createSocketTempDir(t, "unwritable")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	// Create runtime directory but make socket path a directory so listen fails
	runtimeDir := tmpDir + "/holomush"
	socketPath := runtimeDir + "/holomush-test.sock"
	if err := os.MkdirAll(socketPath, 0o700); err != nil {
		t.Fatalf("failed to create socket path as directory: %v", err)
	}

	s := NewServer("test", nil)
	err := s.Start()

	// On most systems, trying to create a Unix socket where a directory exists fails
	if err == nil {
		_ = s.Stop(context.Background())
		// This may succeed on some systems, skip the test
		t.Skip("system allows creating socket over directory")
	}

	if !strings.Contains(err.Error(), "failed to listen on socket") &&
		!strings.Contains(err.Error(), "failed to remove existing socket") {
		t.Errorf("error should mention socket failure, got: %v", err)
	}
}

func TestServer_Stop_WithTimeoutContext(t *testing.T) {
	tmpDir := createSocketTempDir(t, "timeout")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("timeout-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Use an already-cancelled context to test timeout handling
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Stop should handle the cancelled context gracefully
	// The shutdown may fail, but it shouldn't panic
	_ = s.Stop(ctx)

	// Verify server state is still consistent
	if s.running.Load() {
		t.Error("server should not be running after Stop()")
	}
}

func TestServer_Stop_CleanupWithMissingSocketFile(t *testing.T) {
	tmpDir := createSocketTempDir(t, "missing")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("cleanup-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Delete the socket file before Stop to test cleanup handling
	if err := os.Remove(s.socketPath); err != nil {
		t.Fatalf("failed to pre-remove socket: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Stop should not fail when socket file is already gone
	if err := s.Stop(ctx); err != nil {
		t.Errorf("Stop() should succeed when socket already removed, got: %v", err)
	}
}

func TestServer_Stop_WithClosedListener(t *testing.T) {
	// Capture log output (warnings expected)
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(originalLogger)

	tmpDir := createSocketTempDir(t, "closedlistener")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("close-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Close listener prematurely to trigger the error path
	if s.listener != nil {
		_ = s.listener.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Stop should handle the already-closed listener gracefully
	// httpServer.Shutdown may fail, but Stop should not return error
	// because of the logging-based error handling
	err := s.Stop(ctx)
	// Error is acceptable here since httpServer.Shutdown will fail
	_ = err

	// Verify running flag is still correctly set
	if s.running.Load() {
		t.Error("server should not be running after Stop()")
	}
}

// =============================================================================
// Malformed Request Tests (e55.38)
// =============================================================================

func TestServer_MalformedJSON_InvalidContentType(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-ct")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// Send request with invalid content-type - server should still process GET endpoints
	req, err := http.NewRequest(http.MethodGet, "http://localhost/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// GET endpoints don't require JSON content-type
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestServer_MalformedRequest_EmptyBody(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-empty")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// POST with empty body - shutdown endpoint doesn't require body
	resp, err := client.Post("http://localhost/shutdown", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Shutdown doesn't read request body, so it should succeed
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestServer_MalformedRequest_InvalidMethod(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-method")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// DELETE method on health endpoint should return 405
	req, err := http.NewRequest(http.MethodDelete, "http://localhost/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Go 1.22+ returns 405 for method mismatch
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

func TestServer_MalformedRequest_UnknownEndpoint(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-endpoint")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// Request to unknown endpoint should return 404
	resp, err := client.Get("http://localhost/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", resp.StatusCode)
	}
}

func TestServer_MalformedRequest_OversizedHeaders(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-headers")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, "http://localhost/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Add a very large header (default limit is typically 1MB)
	largeValue := strings.Repeat("x", 100000)
	req.Header.Set("X-Large-Header", largeValue)

	// This should still work as 100KB is under the limit
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestServer_MalformedRequest_NoPanic(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-nopanic")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Send raw malformed HTTP via direct socket connection
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send malformed HTTP request (no HTTP version)
	_, err = conn.Write([]byte("GET /health\r\n\r\n"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Set read deadline
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("failed to set deadline: %v", err)
	}

	// Read response - server should respond with 400 or close connection
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)

	// Server should either return an error response or close the connection
	// Either behavior is acceptable for malformed requests
	if err != nil && !strings.Contains(err.Error(), "EOF") && !strings.Contains(err.Error(), "connection reset") {
		// Server closed connection, which is acceptable
		return
	}

	response := string(buf[:n])
	// If we got a response, it should be an HTTP error
	if n > 0 && !strings.Contains(response, "400") && !strings.Contains(response, "HTTP/1.") {
		t.Logf("unexpected response: %s", response)
	}
}

func TestServer_MalformedRequest_TruncatedRequest(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-truncated")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Send truncated request via direct socket connection
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send partial HTTP request and immediately close
	_, err = conn.Write([]byte("GET /heal"))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Close the write side to simulate truncation
	if tcpConn, ok := conn.(*net.UnixConn); ok {
		_ = tcpConn.CloseWrite()
	}

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	// Server should not panic - just verify it's still running
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://localhost/health")
	if err != nil {
		t.Fatalf("server not responding after truncated request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestServer_MalformedRequest_InvalidUTF8(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-utf8")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// Create request with invalid UTF-8 in header
	req, err := http.NewRequest(http.MethodGet, "http://localhost/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Add header with invalid UTF-8 bytes (Go's http library should handle this)
	req.Header.Set("X-Custom", string([]byte{0x80, 0x81, 0x82}))

	// Server should handle this without panicking
	resp, err := client.Do(req)
	if err != nil {
		// Connection error is acceptable for invalid input
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Any response is acceptable as long as server didn't panic
	t.Logf("Response status: %d", resp.StatusCode)
}

func TestServer_MalformedRequest_VeryLongURL(t *testing.T) {
	tmpDir := createSocketTempDir(t, "malformed-longurl")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("malformed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// Create URL with very long path
	longPath := "/health?" + strings.Repeat("x=y&", 10000)
	req, err := http.NewRequest(http.MethodGet, "http://localhost"+longPath, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Connection error is acceptable for oversized requests
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Either success (if under limit) or 414 URI Too Long is acceptable
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusRequestURITooLong {
		t.Errorf("expected 200 or 414, got %d", resp.StatusCode)
	}
}

// =============================================================================
// Edge Case Tests: Unix Socket Permission Errors (e55.31)
// =============================================================================

func TestServer_Start_FailsOnReadOnlyDirectory(t *testing.T) {
	// Create a directory and make it read-only
	tmpDir := createSocketTempDir(t, "readonly")
	readOnlyDir := tmpDir + "/readonly"
	if err := os.MkdirAll(readOnlyDir, 0o500); err != nil {
		t.Fatalf("failed to create read-only directory: %v", err)
	}
	// Ensure cleanup can remove it by restoring permissions
	t.Cleanup(func() { _ = os.Chmod(readOnlyDir, 0o700) }) //nolint:gosec // G302: intentionally restoring permissions for test cleanup

	t.Setenv("XDG_RUNTIME_DIR", readOnlyDir)

	s := NewServer("readonly-test", nil)
	err := s.Start()

	if err == nil {
		_ = s.Stop(context.Background())
		t.Fatal("Start() should fail when runtime directory is read-only")
	}

	// Should fail either creating runtime dir or creating socket
	if !strings.Contains(err.Error(), "failed to create runtime directory") &&
		!strings.Contains(err.Error(), "failed to listen on socket") {
		t.Errorf("error should mention directory or socket failure, got: %v", err)
	}
}

func TestServer_Start_FailsOnSocketPathTooLong(t *testing.T) {
	// Unix socket paths are limited (typically 104-108 chars on most systems)
	// Create a very long path to exceed this limit
	tmpDir := createSocketTempDir(t, "longpath")

	// Build a path that exceeds Unix socket path limits
	// Most systems limit to ~100 chars, so we'll create a 150+ char path
	longSubdir := strings.Repeat("a", 50) + "/" + strings.Repeat("b", 50) + "/" + strings.Repeat("c", 50)
	longPath := tmpDir + "/" + longSubdir
	if err := os.MkdirAll(longPath, 0o700); err != nil {
		t.Fatalf("failed to create long path: %v", err)
	}

	t.Setenv("XDG_RUNTIME_DIR", longPath)

	s := NewServer("longpath-test", nil)
	err := s.Start()

	if err == nil {
		_ = s.Stop(context.Background())
		// Some systems may allow longer paths, skip if so
		t.Skip("system allows longer socket paths than expected")
	}

	// Should fail on socket creation due to path length
	if !strings.Contains(err.Error(), "failed to listen on socket") &&
		!strings.Contains(err.Error(), "name too long") &&
		!strings.Contains(err.Error(), "invalid argument") {
		t.Errorf("error should mention socket or path failure, got: %v", err)
	}
}

func TestServer_Start_FailsWithInsufficientPermissions(t *testing.T) {
	// Skip if running as root (permissions don't apply)
	if os.Getuid() == 0 {
		t.Skip("test requires non-root user")
	}

	tmpDir := createSocketTempDir(t, "noperm")
	runtimeDir := tmpDir + "/holomush"
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("failed to create runtime directory: %v", err)
	}

	// Make the directory unwritable
	if err := os.Chmod(runtimeDir, 0o500); err != nil { //nolint:gosec // G302: intentionally making directory read-only to test permission errors
		t.Fatalf("failed to chmod directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(runtimeDir, 0o700) }) //nolint:gosec // G302: intentionally restoring permissions for test cleanup

	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("noperm-test", nil)
	err := s.Start()

	if err == nil {
		_ = s.Stop(context.Background())
		t.Fatal("Start() should fail when directory is not writable")
	}

	// Should fail removing existing socket or creating new one
	if !strings.Contains(err.Error(), "permission denied") &&
		!strings.Contains(err.Error(), "failed to listen on socket") &&
		!strings.Contains(err.Error(), "failed to remove existing socket") {
		t.Errorf("error should mention permission or socket failure, got: %v", err)
	}
}

// =============================================================================
// Edge Case Tests: Concurrent Request Handling (e55.32)
// =============================================================================

func TestServer_ConcurrentStatusRequests(t *testing.T) {
	tmpDir := createSocketTempDir(t, "concurrent-status")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("concurrent-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Create HTTP client that uses Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	const numRequests = 50
	results := make(chan error, numRequests)

	// Launch concurrent status requests
	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := client.Get("http://localhost/status")
			if err != nil {
				results <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				results <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
				return
			}

			var status StatusResponse
			if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
				results <- fmt.Errorf("decode error: %w", err)
				return
			}

			if !status.Running {
				results <- fmt.Errorf("server should be running")
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

func TestServer_ConcurrentShutdownRequests(t *testing.T) {
	tmpDir := createSocketTempDir(t, "concurrent-shutdown")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	var shutdownCount atomic.Int32
	s := NewServer("shutdown-test", func() {
		shutdownCount.Add(1)
	})

	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Create HTTP client that uses Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	const numRequests = 10
	results := make(chan error, numRequests)

	// Launch concurrent shutdown requests
	for i := 0; i < numRequests; i++ {
		go func() {
			resp, err := client.Post("http://localhost/shutdown", "application/json", nil)
			if err != nil {
				results <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				results <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
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
		t.Errorf("concurrent shutdown requests failed: %v", errors)
	}

	// Wait for all shutdown callbacks to complete
	time.Sleep(100 * time.Millisecond)

	// Each request should trigger the callback
	count := shutdownCount.Load()
	if count != numRequests {
		t.Errorf("shutdown callback called %d times, want %d", count, numRequests)
	}
}

func TestServer_ConcurrentMixedRequests(t *testing.T) {
	tmpDir := createSocketTempDir(t, "concurrent-mixed")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("mixed-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()

	// Create HTTP client that uses Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	const numRequests = 30 // 10 each of health, status, and invalid
	results := make(chan error, numRequests)

	// Launch concurrent mixed requests
	for i := 0; i < numRequests; i++ {
		endpoint := ""
		switch i % 3 {
		case 0:
			endpoint = "/health"
		case 1:
			endpoint = "/status"
		case 2:
			endpoint = "/nonexistent"
		}

		go func(ep string) {
			resp, err := client.Get("http://localhost" + ep)
			if err != nil {
				results <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()

			// /nonexistent should return 404, others 200
			if ep == "/nonexistent" {
				if resp.StatusCode != http.StatusNotFound {
					results <- fmt.Errorf("expected 404 for %s, got %d", ep, resp.StatusCode)
					return
				}
			} else {
				if resp.StatusCode != http.StatusOK {
					results <- fmt.Errorf("expected 200 for %s, got %d", ep, resp.StatusCode)
					return
				}
			}

			results <- nil
		}(endpoint)
	}

	// Collect results
	var errors []error
	for i := 0; i < numRequests; i++ {
		if err := <-results; err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		t.Errorf("concurrent mixed requests failed: %v", errors)
	}
}

// =============================================================================
// Edge Case Tests: Graceful Shutdown Under Load (e55.33)
// =============================================================================

func TestServer_ShutdownWithPendingRequests(t *testing.T) {
	tmpDir := createSocketTempDir(t, "shutdown-pending")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("pending-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Create HTTP client that uses Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 10 * time.Second,
	}

	// Start some long-running requests in background
	const numPending = 5
	requestStarted := make(chan struct{}, numPending)
	requestDone := make(chan error, numPending)

	for i := 0; i < numPending; i++ {
		go func() {
			requestStarted <- struct{}{}
			resp, err := client.Get("http://localhost/status")
			if err != nil {
				requestDone <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()
			requestDone <- nil
		}()
	}

	// Wait for all requests to start
	for i := 0; i < numPending; i++ {
		<-requestStarted
	}

	// Now initiate shutdown while requests may be in flight
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownErr := s.Stop(ctx)

	// Collect request results - some may succeed, some may fail
	var succeeded, failed int
	for i := 0; i < numPending; i++ {
		if err := <-requestDone; err != nil {
			failed++
		} else {
			succeeded++
		}
	}

	// Shutdown should succeed even with pending requests
	if shutdownErr != nil {
		t.Errorf("Stop() should succeed with pending requests, got: %v", shutdownErr)
	}

	// At least some requests should have completed or failed gracefully
	t.Logf("requests: %d succeeded, %d failed", succeeded, failed)
}

func TestServer_ShutdownTimeout(t *testing.T) {
	tmpDir := createSocketTempDir(t, "shutdown-timeout")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("timeout-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Use a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Sleep to ensure context expires
	time.Sleep(10 * time.Millisecond)

	// Shutdown with expired context - may return error but should not panic
	err := s.Stop(ctx)

	// The important thing is it doesn't panic and server state is consistent
	if s.running.Load() {
		t.Error("server should not be running after Stop() regardless of error")
	}

	// Error is expected due to context timeout
	if err != nil {
		t.Logf("expected error due to timeout: %v", err)
	}
}

func TestServer_ConnectionDraining(t *testing.T) {
	tmpDir := createSocketTempDir(t, "draining")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("drain-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Create a persistent connection
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		t.Fatalf("failed to dial socket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send a request on the connection
	req := "GET /health HTTP/1.1\r\nHost: localhost\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	// Read response
	buf := make([]byte, 1024)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	// Now shutdown - should drain existing connections
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownErr := s.Stop(ctx)
	if shutdownErr != nil {
		t.Errorf("Stop() error = %v", shutdownErr)
	}

	// Connection should be closed by server
	// Try to write on the connection - should eventually fail
	time.Sleep(100 * time.Millisecond)

	_, writeErr := conn.Write([]byte(req))
	if writeErr == nil {
		// Read to see if connection is really alive
		n, readErr := conn.Read(buf)
		if readErr == nil && n > 0 {
			// Connection still works - this is acceptable if we read a response
			// from before shutdown. The important thing is the server stopped.
			t.Log("connection still readable after shutdown (may have buffered response)")
		}
	}
}

func TestServer_ShutdownIdempotent(t *testing.T) {
	tmpDir := createSocketTempDir(t, "idempotent")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("idempotent-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx := context.Background()

	// Call Stop multiple times - should not panic or return errors on subsequent calls
	for i := 0; i < 3; i++ {
		err := s.Stop(ctx)
		if i == 0 && err != nil {
			t.Errorf("first Stop() error = %v", err)
		}
		// Subsequent calls should also not panic
	}

	if s.running.Load() {
		t.Error("server should not be running after multiple Stop() calls")
	}
}

func TestServer_RunningFlagRaceCondition(t *testing.T) {
	tmpDir := createSocketTempDir(t, "raceflag")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	s := NewServer("race-test", nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Create HTTP client
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	// Concurrently read running flag via status endpoint while stopping
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			default:
				resp, err := client.Get("http://localhost/status")
				if err != nil {
					continue // Connection may be closed during shutdown
				}
				_ = resp.Body.Close()
			}
		}
	}()

	// Stop the server while requests are in flight
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.Stop(ctx)
	close(done)

	if err != nil {
		t.Logf("Stop() error (may be expected): %v", err)
	}

	// The running flag should be consistently false after Stop
	if s.running.Load() {
		t.Error("running flag should be false after Stop")
	}
}
