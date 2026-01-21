// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommand_HasExpectedSubcommands(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())

	output := buf.String()
	subcommands := []string{"gateway", "core", "migrate", "status"}
	for _, sub := range subcommands {
		assert.Contains(t, output, sub, "Help missing %q command", sub)
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

			require.NoError(t, cmd.Execute())
			assert.Equal(t, tt.wantFlag, configFile)
		})
	}
}

func TestRootCommand_LongDescription(t *testing.T) {
	cmd := NewRootCmd()

	assert.Equal(t, "holomush", cmd.Use)
	assert.Contains(t, cmd.Long, "event sourcing", "Long description should mention event sourcing")
	assert.Contains(t, cmd.Long, "WebAssembly", "Long description should mention WebAssembly")
}

func TestRootCommand_VersionFlag(t *testing.T) {
	cmd := NewRootCmd()
	cmd.Version = "test-version"
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "test-version", "Version output missing version info: %s", output)
}

func TestRootCommand_NoArgs(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	// Root command with no args should show help (no error)
	require.NoError(t, cmd.Execute())
}

// Gateway command tests are now in gateway_test.go
// Core command tests are now in core_test.go

func TestMigrateCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"migrate", "--help"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "--config", "Migrate missing --config flag")
}

func TestMigrateCommand_Properties(t *testing.T) {
	cmd := NewMigrateCmd()

	assert.Equal(t, "migrate", cmd.Use)
	assert.Contains(t, cmd.Short, "migration", "Short description should mention migration")
	assert.Contains(t, cmd.Long, "PostgreSQL", "Long description should mention PostgreSQL")
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
	require.Error(t, err, "Expected error when DATABASE_URL is not set")
	assert.Contains(t, err.Error(), "DATABASE_URL")
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
	require.Error(t, err, "Expected error with invalid DATABASE_URL")

	// Error from pgx parsing - "cannot parse" the URL
	assert.Contains(t, err.Error(), "parse", "Error should mention parse issue, got: %v", err)
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
	require.Error(t, err, "Expected error for unknown command")
}

func TestInvalidFlag(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"--invalid-flag"})

	err := cmd.Execute()
	require.Error(t, err, "Expected error for invalid flag")
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
			assert.Equal(t, tt.expected, result)
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
	assert.Equal(t, 0, exitCode)
}

func TestRun_Error(t *testing.T) {
	// Save original args and restore after test
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test with invalid command which should fail
	os.Args = []string{"holomush", "nonexistent-command"}

	exitCode := run()
	assert.Equal(t, 1, exitCode)
}
