// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRootCommand_HasExpectedSubcommands(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	subcommands := []string{"gateway", "core", "migrate", "status"}
	for _, sub := range subcommands {
		if !strings.Contains(output, sub) {
			t.Errorf("Help missing %q command", sub)
		}
	}
}

func TestRootCommand_ConfigFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantFlag string
	}{
		{
			name:     "short config flag",
			args:     []string{"--config", "/path/to/config.yaml", "--help"},
			wantFlag: "/path/to/config.yaml",
		},
		{
			name:     "config flag with equals",
			args:     []string{"--config=/etc/holomush.yaml", "--help"},
			wantFlag: "/etc/holomush.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global
			configFile = ""

			cmd := NewRootCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs(tt.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			if configFile != tt.wantFlag {
				t.Errorf("configFile = %q, want %q", configFile, tt.wantFlag)
			}
		})
	}
}

func TestRootCommand_LongDescription(t *testing.T) {
	cmd := NewRootCmd()

	if cmd.Use != "holomush" {
		t.Errorf("Use = %q, want %q", cmd.Use, "holomush")
	}

	if !strings.Contains(cmd.Long, "event sourcing") {
		t.Error("Long description should mention event sourcing")
	}

	if !strings.Contains(cmd.Long, "WebAssembly") {
		t.Error("Long description should mention WebAssembly")
	}
}

func TestRootCommand_VersionFlag(t *testing.T) {
	cmd := NewRootCmd()
	cmd.Version = "test-version"
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "test-version") {
		t.Errorf("Version output missing version info: %s", output)
	}
}

func TestRootCommand_NoArgs(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	// Root command with no args should show help (no error)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

// Gateway command tests are now in gateway_test.go
// Core command tests are now in core_test.go

func TestMigrateCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"migrate", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "--config") {
		t.Error("Migrate missing --config flag")
	}
}

func TestMigrateCommand_Properties(t *testing.T) {
	cmd := NewMigrateCmd()

	if cmd.Use != "migrate" {
		t.Errorf("Use = %q, want %q", cmd.Use, "migrate")
	}

	if !strings.Contains(cmd.Short, "migration") {
		t.Error("Short description should mention migration")
	}

	if !strings.Contains(cmd.Long, "PostgreSQL") {
		t.Error("Long description should mention PostgreSQL")
	}
}

func TestMigrateCommand_NoDatabaseURL(t *testing.T) {
	// Ensure DATABASE_URL is not set for this test
	t.Setenv("DATABASE_URL", "")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"migrate"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when DATABASE_URL is not set")
	}

	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("Error should mention DATABASE_URL, got: %v", err)
	}
}

func TestMigrateCommand_InvalidDatabaseURL(t *testing.T) {
	// Set an invalid DATABASE_URL
	t.Setenv("DATABASE_URL", "invalid://not-a-real-db")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"migrate"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error with invalid DATABASE_URL")
	}

	if !strings.Contains(err.Error(), "connect") && !strings.Contains(err.Error(), "database") {
		t.Errorf("Error should mention connection/database issue, got: %v", err)
	}
}

// Status command tests are now in status_test.go

func TestUnknownCommand(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error for unknown command")
	}
}

func TestInvalidFlag(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"--invalid-flag"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error for invalid flag")
	}
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		commit   string
		date     string
		expected string
	}{
		{
			name:     "default values",
			version:  "dev",
			commit:   "unknown",
			date:     "unknown",
			expected: "dev (commit: unknown, built: unknown)",
		},
		{
			name:     "release version",
			version:  "1.0.0",
			commit:   "abc123",
			date:     "2024-01-15",
			expected: "1.0.0 (commit: abc123, built: 2024-01-15)",
		},
		{
			name:     "empty values",
			version:  "",
			commit:   "",
			date:     "",
			expected: " (commit: , built: )",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatVersion(tt.version, tt.commit, tt.date)
			if result != tt.expected {
				t.Errorf("formatVersion() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRun_Success(t *testing.T) {
	// Save original args and restore after test
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test with --help flag which should succeed
	os.Args = []string{"holomush", "--help"}

	exitCode := run()
	if exitCode != 0 {
		t.Errorf("run() = %d, want 0", exitCode)
	}
}

func TestRun_Error(t *testing.T) {
	// Save original args and restore after test
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test with invalid command which should fail
	os.Args = []string{"holomush", "nonexistent-command"}

	exitCode := run()
	if exitCode != 1 {
		t.Errorf("run() = %d, want 1", exitCode)
	}
}
