package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
