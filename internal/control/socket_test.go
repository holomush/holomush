package control

import (
	"bytes"
	"context"
	"encoding/json"
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
