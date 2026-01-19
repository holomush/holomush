// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// internal/xdg/xdg_test.go
package xdg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}
	want := "/custom/config/holomush"
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestConfigDir_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}
	want := "/home/testuser/.config/holomush"
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestDataDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	want := "/custom/data/holomush"
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

func TestDataDir_Default(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	want := "/home/testuser/.local/share/holomush"
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

func TestStateDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir() error = %v", err)
	}
	want := "/custom/state/holomush"
	if got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestStateDir_Default(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir() error = %v", err)
	}
	want := "/home/testuser/.local/state/holomush"
	if got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestRuntimeDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	got, err := RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir() error = %v", err)
	}
	want := "/run/user/1000/holomush"
	if got != want {
		t.Errorf("RuntimeDir() = %q, want %q", got, want)
	}
}

func TestRuntimeDir_Fallback(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got, err := RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir() error = %v", err)
	}
	want := "/custom/state/holomush/run"
	if got != want {
		t.Errorf("RuntimeDir() = %q, want %q", got, want)
	}
}

func TestCertsDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := CertsDir()
	if err != nil {
		t.Fatalf("CertsDir() error = %v", err)
	}
	want := "/custom/config/holomush/certs"
	if got != want {
		t.Errorf("CertsDir() = %q, want %q", got, want)
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "nested", "dir")

	err := EnsureDir(testPath)
	if err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}

	info, err := os.Stat(testPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !info.IsDir() {
		t.Error("Expected directory, got file")
	}
}

func TestEnsureDir_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "secure", "dir")

	err := EnsureDir(testPath)
	if err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}

	info, err := os.Stat(testPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	// Check permissions are 0700
	perm := info.Mode().Perm()
	if perm != 0o700 {
		t.Errorf("EnsureDir() permissions = %o, want %o", perm, 0o700)
	}
}

func TestEnsureDir_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "idempotent")

	// Create twice - should not error
	if err := EnsureDir(testPath); err != nil {
		t.Fatalf("First EnsureDir() error = %v", err)
	}
	if err := EnsureDir(testPath); err != nil {
		t.Fatalf("Second EnsureDir() error = %v", err)
	}
}

func TestEnsureDir_Error(t *testing.T) {
	// Try to create a directory inside a file (should fail)
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "afile")

	// Create a file
	if err := os.WriteFile(filePath, []byte("content"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Try to create a directory inside that file
	invalidPath := filepath.Join(filePath, "subdir")
	err := EnsureDir(invalidPath)
	if err == nil {
		t.Error("EnsureDir() expected error, got nil")
	}
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
		if got != "" {
			t.Errorf("homeDir() returned non-empty string %q with error", got)
		}
		return
	}

	// If no error, we should have a valid path
	if got == "" {
		t.Error("homeDir() returned empty string")
	}
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
