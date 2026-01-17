package control

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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
