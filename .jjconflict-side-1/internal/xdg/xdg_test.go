// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/xdg/xdg_test.go
package xdg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := ConfigDir()
	require.NoError(t, err)
	assert.Equal(t, "/custom/config/holomush", got)
}

func TestConfigDir_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got, err := ConfigDir()
	require.NoError(t, err)
	assert.Equal(t, "/home/testuser/.config/holomush", got)
}

func TestDataDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got, err := DataDir()
	require.NoError(t, err)
	assert.Equal(t, "/custom/data/holomush", got)
}

func TestDataDir_Default(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got, err := DataDir()
	require.NoError(t, err)
	assert.Equal(t, "/home/testuser/.local/share/holomush", got)
}

func TestStateDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got, err := StateDir()
	require.NoError(t, err)
	assert.Equal(t, "/custom/state/holomush", got)
}

func TestStateDir_Default(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got, err := StateDir()
	require.NoError(t, err)
	assert.Equal(t, "/home/testuser/.local/state/holomush", got)
}

func TestRuntimeDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	got, err := RuntimeDir()
	require.NoError(t, err)
	assert.Equal(t, "/run/user/1000/holomush", got)
}

func TestRuntimeDir_Fallback(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got, err := RuntimeDir()
	require.NoError(t, err)
	assert.Equal(t, "/custom/state/holomush/run", got)
}

func TestCertsDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := CertsDir()
	require.NoError(t, err)
	assert.Equal(t, "/custom/config/holomush/certs", got)
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "nested", "dir")

	err := EnsureDir(testPath)
	require.NoError(t, err)

	info, err := os.Stat(testPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "Expected directory, got file")
}

func TestEnsureDir_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "secure", "dir")

	err := EnsureDir(testPath)
	require.NoError(t, err)

	info, err := os.Stat(testPath)
	require.NoError(t, err)

	// Check permissions are 0700
	perm := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0o700), perm, "EnsureDir() permissions mismatch")
}

func TestEnsureDir_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "idempotent")

	// Create twice - should not error
	err := EnsureDir(testPath)
	require.NoError(t, err, "First EnsureDir() failed")
	err = EnsureDir(testPath)
	require.NoError(t, err, "Second EnsureDir() failed")
}

func TestEnsureDir_Error(t *testing.T) {
	// Try to create a directory inside a file (should fail)
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "afile")

	// Create a file
	err := os.WriteFile(filePath, []byte("content"), 0o600)
	require.NoError(t, err)

	// Try to create a directory inside that file
	invalidPath := filepath.Join(filePath, "subdir")
	err = EnsureDir(invalidPath)
	assert.Error(t, err, "EnsureDir() expected error")
}

func TestHomeDir_Fallback(t *testing.T) {
	// Unset HOME to force os.UserHomeDir() fallback
	t.Setenv("HOME", "")

	// Call homeDir - it should fall back to os.UserHomeDir()
	// On some systems (macOS), os.UserHomeDir() also needs HOME,
	// so it may return an error. We're testing both paths.
	got, err := homeDir()
	if err != nil {
		// This is expected on systems where os.UserHomeDir() requires HOME
		// Verify the error message is properly wrapped
		assert.Empty(t, got, "homeDir() returned non-empty string with error")
		return
	}

	// If no error, we should have a valid path
	assert.NotEmpty(t, got, "homeDir() returned empty string")
}

func TestConfigDir_HomeDirError(t *testing.T) {
	// Clear both HOME and XDG_CONFIG_HOME, then break os.UserHomeDir
	// by setting HOME to empty on systems that require it
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	// On most test systems, os.UserHomeDir will still work
	// So we just verify the function doesn't panic
	_, _ = ConfigDir()
}

func TestDataDir_HomeDirError(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	// Verify the function doesn't panic
	_, _ = DataDir()
}

func TestStateDir_HomeDirError(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	// Verify the function doesn't panic
	_, _ = StateDir()
}

func TestRuntimeDir_StateDirError(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	// Verify the function doesn't panic
	_, _ = RuntimeDir()
}

func TestCertsDir_ConfigDirError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	// Verify the function doesn't panic
	_, _ = CertsDir()
}
