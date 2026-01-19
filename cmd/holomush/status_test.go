// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/holomush/holomush/internal/tls"
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
		"--core-addr",
		"--gateway-addr",
	}

	for _, flag := range expectedFlags {
		if !strings.Contains(output, flag) {
			t.Errorf("Help missing %q flag", flag)
		}
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
			Error:     "failed to connect",
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
			Error:     "failed to connect",
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

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int64
		expected string
	}{
		{"0 seconds", 0, "0s"},
		{"30 seconds", 30, "30s"},
		{"59 seconds", 59, "59s"},
		{"1 minute", 60, "1m 0s"},
		{"5 minutes 30 seconds", 330, "5m 30s"},
		{"1 hour", 3600, "1h 0m"},
		{"2 hours 30 minutes", 9000, "2h 30m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUptime(tt.seconds)
			if got != tt.expected {
				t.Errorf("formatUptime(%d) = %q, want %q", tt.seconds, got, tt.expected)
			}
		})
	}
}

// TestStatusConfig_Validate tests validation of statusConfig.
func TestStatusConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       statusConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config",
			cfg: statusConfig{
				coreAddr:    "127.0.0.1:9001",
				gatewayAddr: "127.0.0.1:9002",
			},
			wantError: false,
		},
		{
			name: "valid config with json output",
			cfg: statusConfig{
				jsonOutput:  true,
				coreAddr:    "127.0.0.1:9001",
				gatewayAddr: "127.0.0.1:9002",
			},
			wantError: false,
		},
		{
			name: "empty core-addr",
			cfg: statusConfig{
				coreAddr:    "",
				gatewayAddr: "127.0.0.1:9002",
			},
			wantError: true,
			errorMsg:  "core-addr is required",
		},
		{
			name: "empty gateway-addr",
			cfg: statusConfig{
				coreAddr:    "127.0.0.1:9001",
				gatewayAddr: "",
			},
			wantError: true,
			errorMsg:  "gateway-addr is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantError {
				if err == nil {
					t.Fatalf("Validate() expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errorMsg)
				}
			} else if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestQueryProcessStatusGRPC_NoCerts(t *testing.T) {
	// Without proper certs directory, should return an error
	t.Setenv("XDG_DATA_HOME", "/nonexistent")

	status := queryProcessStatusGRPC("core", "127.0.0.1:9001")

	if status.Running {
		t.Error("status.Running should be false when certs can't be loaded")
	}
	if status.Error == "" {
		t.Error("status.Error should contain error message")
	}
}

func TestQueryProcessStatusGRPC_InvalidCA(t *testing.T) {
	// Create temp dir with invalid CA certificate
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		t.Fatalf("failed to create certs dir: %v", err)
	}

	// Write invalid CA file
	caPath := certsDir + "/root-ca.crt"
	if err := os.WriteFile(caPath, []byte("not a valid cert"), 0o600); err != nil {
		t.Fatalf("failed to write CA: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	status := queryProcessStatusGRPC("core", "127.0.0.1:9001")

	if status.Running {
		t.Error("status.Running should be false when CA is invalid")
	}
	if status.Error == "" {
		t.Error("status.Error should contain error message")
	}
	if !strings.Contains(status.Error, "game_id") {
		t.Errorf("error should mention game_id extraction, got: %s", status.Error)
	}
}

func TestQueryProcessStatusGRPC_ValidCANoClientCert(t *testing.T) {
	// Create temp dir with valid CA but missing client cert
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"

	// Generate valid CA
	gameID := "test-status-game-id"
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("failed to generate CA: %v", err)
	}

	// Save only the CA certificate (no client cert)
	if err := tls.SaveCertificates(certsDir, ca, nil); err != nil {
		t.Fatalf("failed to save CA: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	status := queryProcessStatusGRPC("core", "127.0.0.1:9001")

	if status.Running {
		t.Error("status.Running should be false when client cert is missing")
	}
	if status.Error == "" {
		t.Error("status.Error should contain error message")
	}
	if !strings.Contains(status.Error, "TLS") {
		t.Errorf("error should mention TLS, got: %s", status.Error)
	}
}

func TestQueryProcessStatusGRPC_ConnectionFailure(t *testing.T) {
	// Create temp dir with valid certs but no server running
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"

	// Generate valid CA and server/client certs
	gameID := "test-conn-refused"
	ca, err := tls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("failed to generate CA: %v", err)
	}

	// Generate server cert (for the core server we'd be connecting to)
	serverCert, err := tls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("failed to generate server cert: %v", err)
	}

	// Generate client cert (for our status query client)
	clientCert, err := tls.GenerateClientCert(ca, "core")
	if err != nil {
		t.Fatalf("failed to generate client cert: %v", err)
	}

	// Save all certs
	if err := tls.SaveCertificates(certsDir, ca, serverCert); err != nil {
		t.Fatalf("failed to save certs: %v", err)
	}
	if err := tls.SaveClientCert(certsDir, clientCert); err != nil {
		t.Fatalf("failed to save client cert: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Use a port that's definitely not listening
	status := queryProcessStatusGRPC("core", "127.0.0.1:59999")

	if status.Running {
		t.Error("status.Running should be false when server is not running")
	}
	// Error could be about connection or about querying status
	if status.Error == "" {
		t.Error("status.Error should contain error message")
	}
}

func TestNewProcessStatus(t *testing.T) {
	tests := []struct {
		name      string
		component string
		running   bool
		pid       int
		uptime    int64
	}{
		{
			name:      "running process",
			component: "core",
			running:   true,
			pid:       12345,
			uptime:    3600,
		},
		{
			name:      "gateway process",
			component: "gateway",
			running:   true,
			pid:       67890,
			uptime:    7200,
		},
		{
			name:      "zero uptime",
			component: "core",
			running:   true,
			pid:       1,
			uptime:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := NewProcessStatus(tt.component, tt.running, tt.pid, tt.uptime)

			if status.Component != tt.component {
				t.Errorf("Component = %q, want %q", status.Component, tt.component)
			}
			if status.Running != tt.running {
				t.Errorf("Running = %v, want %v", status.Running, tt.running)
			}
			if status.PID != tt.pid {
				t.Errorf("PID = %d, want %d", status.PID, tt.pid)
			}
			if status.UptimeSeconds != tt.uptime {
				t.Errorf("UptimeSeconds = %d, want %d", status.UptimeSeconds, tt.uptime)
			}
			if status.Health != "healthy" {
				t.Errorf("Health = %q, want %q", status.Health, "healthy")
			}
			// Constructor must not set Error for running processes
			if status.Error != "" {
				t.Errorf("Error = %q, want empty string", status.Error)
			}
		})
	}
}

func TestNewProcessStatusError(t *testing.T) {
	tests := []struct {
		name      string
		component string
		err       error
	}{
		{
			name:      "connection error",
			component: "core",
			err:       errors.New("failed to connect: connection refused"),
		},
		{
			name:      "tls error",
			component: "gateway",
			err:       errors.New("failed to load TLS config: certificate not found"),
		},
		{
			name:      "wrapped error",
			component: "core",
			err:       errors.New("failed to get certs directory: no such file or directory"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := NewProcessStatusError(tt.component, tt.err)

			if status.Component != tt.component {
				t.Errorf("Component = %q, want %q", status.Component, tt.component)
			}
			// Constructor must set Running to false for error states
			if status.Running {
				t.Error("Running should be false for error status")
			}
			if status.Error != tt.err.Error() {
				t.Errorf("Error = %q, want %q", status.Error, tt.err.Error())
			}
			// Constructor must not set these fields for error states
			if status.Health != "" {
				t.Errorf("Health = %q, want empty string", status.Health)
			}
			if status.PID != 0 {
				t.Errorf("PID = %d, want 0", status.PID)
			}
			if status.UptimeSeconds != 0 {
				t.Errorf("UptimeSeconds = %d, want 0", status.UptimeSeconds)
			}
		})
	}
}

func TestProcessStatus_InvalidStatesPreventedByConstructors(t *testing.T) {
	// This test documents that constructors prevent invalid state combinations.
	// Direct struct construction can still create invalid states, but the
	// constructors enforce correct invariants.

	t.Run("NewProcessStatus never has Error set", func(t *testing.T) {
		status := NewProcessStatus("core", true, 123, 100)
		if status.Error != "" {
			t.Error("NewProcessStatus should never set Error field")
		}
	})

	t.Run("NewProcessStatusError always has Running=false", func(t *testing.T) {
		status := NewProcessStatusError("core", errors.New("test error"))
		if status.Running {
			t.Error("NewProcessStatusError should always set Running=false")
		}
	})

	t.Run("NewProcessStatusError never has Health set", func(t *testing.T) {
		status := NewProcessStatusError("core", errors.New("test error"))
		if status.Health != "" {
			t.Error("NewProcessStatusError should never set Health field")
		}
	})

	t.Run("NewProcessStatus always has Health=healthy", func(t *testing.T) {
		status := NewProcessStatus("core", true, 123, 100)
		if status.Health != "healthy" {
			t.Errorf("NewProcessStatus should always set Health='healthy', got %q", status.Health)
		}
	})
}

func TestRunStatus_TableOutput(t *testing.T) {
	// Mock the cert directory to return error quickly
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/path")

	cmd := newStatusCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	// Should not error - it should handle connection failures gracefully
	if err := cmd.Execute(); err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	output := buf.String()

	// Should contain table headers
	if !strings.Contains(output, "PROCESS") {
		t.Error("output should contain PROCESS header")
	}
	if !strings.Contains(output, "STATUS") {
		t.Error("output should contain STATUS header")
	}

	// Should contain component names
	if !strings.Contains(output, "core") {
		t.Error("output should mention core")
	}
	if !strings.Contains(output, "gateway") {
		t.Error("output should mention gateway")
	}
}

func TestRunStatus_JSONOutput(t *testing.T) {
	// Mock the cert directory to return error quickly
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/path")

	cmd := newStatusCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--json"})

	// Should not error - it should handle connection failures gracefully
	if err := cmd.Execute(); err != nil {
		t.Fatalf("runStatus() with --json error = %v", err)
	}

	output := buf.String()

	// Should be valid JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output should be valid JSON: %v, got: %s", err, output)
	}

	// Should contain core and gateway keys
	if _, ok := result["core"]; !ok {
		t.Error("JSON output should contain 'core' key")
	}
	if _, ok := result["gateway"]; !ok {
		t.Error("JSON output should contain 'gateway' key")
	}
}

func TestRunStatus_CustomAddresses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/path")

	cmd := newStatusCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--core-addr=127.0.0.1:19001", "--gateway-addr=127.0.0.1:19002"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("runStatus() with custom addresses error = %v", err)
	}

	// Should complete without error even with non-standard addresses
	output := buf.String()
	if !strings.Contains(output, "core") {
		t.Error("output should mention core")
	}
}

func TestStatusConfig_Defaults(t *testing.T) {
	cmd := newStatusCmd()

	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		t.Fatalf("failed to get json flag: %v", err)
	}
	if jsonOutput != false {
		t.Errorf("json default should be false, got %v", jsonOutput)
	}

	coreAddr, err := cmd.Flags().GetString("core-addr")
	if err != nil {
		t.Fatalf("failed to get core-addr flag: %v", err)
	}
	if coreAddr != defaultCoreControlAddr {
		t.Errorf("core-addr default = %q, want %q", coreAddr, defaultCoreControlAddr)
	}

	gatewayAddr, err := cmd.Flags().GetString("gateway-addr")
	if err != nil {
		t.Fatalf("failed to get gateway-addr flag: %v", err)
	}
	if gatewayAddr != defaultGatewayControlAddr {
		t.Errorf("gateway-addr default = %q, want %q", gatewayAddr, defaultGatewayControlAddr)
	}
}

func TestByteWriter(t *testing.T) {
	var buf byteWriter

	n, err := buf.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write() returned %d, want 5", n)
	}
	if string(buf) != "hello" {
		t.Errorf("buf = %q, want %q", string(buf), "hello")
	}

	// Write more
	n, err = buf.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if n != 6 {
		t.Errorf("second Write() returned %d, want 6", n)
	}
	if string(buf) != "hello world" {
		t.Errorf("buf = %q, want %q", string(buf), "hello world")
	}
}

func TestFormatStatusTable_AllRunning(t *testing.T) {
	statuses := map[string]ProcessStatus{
		"core": {
			Component:     "core",
			Running:       true,
			Health:        "healthy",
			PID:           1234,
			UptimeSeconds: 60,
		},
		"gateway": {
			Component:     "gateway",
			Running:       true,
			Health:        "healthy",
			PID:           5678,
			UptimeSeconds: 3700,
		},
	}

	output := formatStatusTable(statuses)

	if !strings.Contains(output, "running") {
		t.Error("output should contain 'running'")
	}
	if !strings.Contains(output, "healthy") {
		t.Error("output should contain 'healthy'")
	}
	if !strings.Contains(output, "1234") {
		t.Error("output should contain core PID")
	}
	if !strings.Contains(output, "5678") {
		t.Error("output should contain gateway PID")
	}
}

func TestFormatStatusTable_AllStopped(t *testing.T) {
	statuses := map[string]ProcessStatus{
		"core": {
			Component: "core",
			Running:   false,
		},
		"gateway": {
			Component: "gateway",
			Running:   false,
		},
	}

	output := formatStatusTable(statuses)

	if !strings.Contains(output, "stopped") {
		t.Error("output should contain 'stopped'")
	}
	if !strings.Contains(output, "not running") {
		t.Error("output should contain 'not running'")
	}
}
