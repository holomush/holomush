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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tlscerts "github.com/holomush/holomush/internal/tls"
)

func TestStatus_Properties(t *testing.T) {
	cmd := NewStatusCmd()

	assert.Equal(t, "status", cmd.Use)
	assert.Contains(t, cmd.Short, "status", "Short description should mention status")
	assert.Contains(t, cmd.Long, "health", "Long description should mention health")
}

func TestStatus_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"status", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	require.NoError(t, cmd.Execute())

	output := buf.String()

	// Verify help contains expected sections
	expectedPhrases := []string{
		"status",
		"running",
	}

	for _, phrase := range expectedPhrases {
		assert.Contains(t, output, phrase, "Help missing phrase %q", phrase)
	}
}

func TestStatus_Flags(t *testing.T) {
	cmd := NewStatusCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())

	output := buf.String()

	// Verify expected flags are present
	expectedFlags := []string{
		"--json",
		"--core-addr",
		"--gateway-addr",
	}

	for _, flag := range expectedFlags {
		assert.Contains(t, output, flag, "Help missing %q flag", flag)
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
	assert.Contains(t, output, "core", "table should contain 'core'")
	assert.Contains(t, output, "gateway", "table should contain 'gateway'")

	// Should indicate running/stopped
	assert.True(t, strings.Contains(output, "running") || strings.Contains(output, "healthy"),
		"table should indicate running/healthy status")
	assert.True(t, strings.Contains(output, "stopped") || strings.Contains(output, "not running"),
		"table should indicate stopped/not running status")
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
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &result), "output should be valid JSON")

	// Verify core status
	coreStatus, ok := result["core"].(map[string]any)
	require.True(t, ok, "core should be an object")
	assert.Equal(t, true, coreStatus["running"], "core.running should be true")
	assert.Equal(t, "healthy", coreStatus["health"])

	// Verify gateway status
	gatewayStatus, ok := result["gateway"].(map[string]any)
	require.True(t, ok, "gateway should be an object")
	assert.Equal(t, false, gatewayStatus["running"], "gateway.running should be false")
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
			assert.Equal(t, tt.expected, got)
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
				require.Error(t, err, "Validate() expected error")
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestQueryProcessStatusGRPC_NoCerts(t *testing.T) {
	// Without proper certs directory, should return an error
	t.Setenv("XDG_DATA_HOME", "/nonexistent")

	status := queryProcessStatusGRPC("core", "127.0.0.1:9001")

	assert.False(t, status.Running, "status.Running should be false when certs can't be loaded")
	assert.NotEmpty(t, status.Error, "status.Error should contain error message")
}

func TestQueryProcessStatusGRPC_InvalidCA(t *testing.T) {
	// Create temp dir with invalid CA certificate
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"
	require.NoError(t, os.MkdirAll(certsDir, 0o700), "failed to create certs dir")

	// Write invalid CA file
	caPath := certsDir + "/root-ca.crt"
	require.NoError(t, os.WriteFile(caPath, []byte("not a valid cert"), 0o600), "failed to write CA")

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	status := queryProcessStatusGRPC("core", "127.0.0.1:9001")

	assert.False(t, status.Running, "status.Running should be false when CA is invalid")
	assert.NotEmpty(t, status.Error, "status.Error should contain error message")
	// Error comes from extracting game_id which fails when CA PEM can't be decoded
	assert.Contains(t, status.Error, "decode", "error should mention decode failure, got: %s", status.Error)
}

func TestQueryProcessStatusGRPC_ValidCANoClientCert(t *testing.T) {
	// Create temp dir with valid CA but missing client cert
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"

	// Generate valid CA
	gameID := "test-status-game-id"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	// Save only the CA certificate (no client cert)
	require.NoError(t, tlscerts.SaveCertificates(certsDir, ca, nil), "failed to save CA")

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	status := queryProcessStatusGRPC("core", "127.0.0.1:9001")

	assert.False(t, status.Running, "status.Running should be false when client cert is missing")
	assert.NotEmpty(t, status.Error, "status.Error should contain error message")
	// Error mentions file not found (cert file missing)
	assert.Contains(t, status.Error, "no such file or directory", "error should mention file not found, got: %s", status.Error)
}

func TestQueryProcessStatusGRPC_ConnectionFailure(t *testing.T) {
	// Create temp dir with valid certs but no server running
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/holomush/certs"

	// Generate valid CA and server/client certs
	gameID := "test-conn-refused"
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "failed to generate CA")

	// Generate server cert (for the core server we'd be connecting to)
	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "failed to generate server cert")

	// Generate client cert (for our status query client)
	clientCert, err := tlscerts.GenerateClientCert(ca, "core")
	require.NoError(t, err, "failed to generate client cert")

	// Save all certs
	require.NoError(t, tlscerts.SaveCertificates(certsDir, ca, serverCert), "failed to save certs")
	require.NoError(t, tlscerts.SaveClientCert(certsDir, clientCert), "failed to save client cert")

	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Use a port that's definitely not listening
	status := queryProcessStatusGRPC("core", "127.0.0.1:59999")

	assert.False(t, status.Running, "status.Running should be false when server is not running")
	// Error could be about connection or about querying status
	assert.NotEmpty(t, status.Error, "status.Error should contain error message")
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

			assert.Equal(t, tt.component, status.Component)
			assert.Equal(t, tt.running, status.Running)
			assert.Equal(t, tt.pid, status.PID)
			assert.Equal(t, tt.uptime, status.UptimeSeconds)
			assert.Equal(t, "healthy", status.Health)
			// Constructor must not set Error for running processes
			assert.Empty(t, status.Error)
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

			assert.Equal(t, tt.component, status.Component)
			// Constructor must set Running to false for error states
			assert.False(t, status.Running, "Running should be false for error status")
			assert.Equal(t, tt.err.Error(), status.Error)
			// Constructor must not set these fields for error states
			assert.Empty(t, status.Health)
			assert.Equal(t, 0, status.PID)
			assert.Equal(t, int64(0), status.UptimeSeconds)
		})
	}
}

func TestProcessStatus_InvalidStatesPreventedByConstructors(t *testing.T) {
	// This test documents that constructors prevent invalid state combinations.
	// Direct struct construction can still create invalid states, but the
	// constructors enforce correct invariants.

	t.Run("NewProcessStatus never has Error set", func(t *testing.T) {
		status := NewProcessStatus("core", true, 123, 100)
		assert.Empty(t, status.Error, "NewProcessStatus should never set Error field")
	})

	t.Run("NewProcessStatusError always has Running=false", func(t *testing.T) {
		status := NewProcessStatusError("core", errors.New("test error"))
		assert.False(t, status.Running, "NewProcessStatusError should always set Running=false")
	})

	t.Run("NewProcessStatusError never has Health set", func(t *testing.T) {
		status := NewProcessStatusError("core", errors.New("test error"))
		assert.Empty(t, status.Health, "NewProcessStatusError should never set Health field")
	})

	t.Run("NewProcessStatus always has Health=healthy", func(t *testing.T) {
		status := NewProcessStatus("core", true, 123, 100)
		assert.Equal(t, "healthy", status.Health)
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
	require.NoError(t, cmd.Execute())

	output := buf.String()

	// Should contain table headers
	assert.Contains(t, output, "PROCESS", "output should contain PROCESS header")
	assert.Contains(t, output, "STATUS", "output should contain STATUS header")

	// Should contain component names
	assert.Contains(t, output, "core", "output should mention core")
	assert.Contains(t, output, "gateway", "output should mention gateway")
}

func TestRunStatus_JSONOutput(t *testing.T) {
	// Mock the cert directory to return error quickly
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/path")

	cmd := newStatusCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--json"})

	// Should not error - it should handle connection failures gracefully
	require.NoError(t, cmd.Execute())

	output := buf.String()

	// Should be valid JSON
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &result), "output should be valid JSON, got: %s", output)

	// Should contain core and gateway keys
	_, ok := result["core"]
	assert.True(t, ok, "JSON output should contain 'core' key")
	_, ok = result["gateway"]
	assert.True(t, ok, "JSON output should contain 'gateway' key")
}

func TestRunStatus_CustomAddresses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/path")

	cmd := newStatusCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--core-addr=127.0.0.1:19001", "--gateway-addr=127.0.0.1:19002"})

	require.NoError(t, cmd.Execute())

	// Should complete without error even with non-standard addresses
	output := buf.String()
	assert.Contains(t, output, "core", "output should mention core")
}

func TestStatusConfig_Defaults(t *testing.T) {
	cmd := newStatusCmd()

	jsonOutput, err := cmd.Flags().GetBool("json")
	require.NoError(t, err, "failed to get json flag")
	assert.False(t, jsonOutput, "json default should be false")

	coreAddr, err := cmd.Flags().GetString("core-addr")
	require.NoError(t, err, "failed to get core-addr flag")
	assert.Equal(t, defaultCoreControlAddr, coreAddr)

	gatewayAddr, err := cmd.Flags().GetString("gateway-addr")
	require.NoError(t, err, "failed to get gateway-addr flag")
	assert.Equal(t, defaultGatewayControlAddr, gatewayAddr)
}

func TestByteWriter(t *testing.T) {
	var buf byteWriter

	n, err := buf.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf))

	// Write more
	n, err = buf.Write([]byte(" world"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, "hello world", string(buf))
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

	assert.Contains(t, output, "running", "output should contain 'running'")
	assert.Contains(t, output, "healthy", "output should contain 'healthy'")
	assert.Contains(t, output, "1234", "output should contain core PID")
	assert.Contains(t, output, "5678", "output should contain gateway PID")
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

	assert.Contains(t, output, "stopped", "output should contain 'stopped'")
	assert.Contains(t, output, "not running", "output should contain 'not running'")
}
