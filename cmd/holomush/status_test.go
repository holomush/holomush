package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
