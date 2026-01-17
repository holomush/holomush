// internal/xdg/xdg_test.go
package xdg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got := ConfigDir()
	want := "/custom/config/holomush"
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestConfigDir_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got := ConfigDir()
	want := "/home/testuser/.config/holomush"
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestDataDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got := DataDir()
	want := "/custom/data/holomush"
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

func TestDataDir_Default(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got := DataDir()
	want := "/home/testuser/.local/share/holomush"
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

func TestStateDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got := StateDir()
	want := "/custom/state/holomush"
	if got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestStateDir_Default(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	got := StateDir()
	want := "/home/testuser/.local/state/holomush"
	if got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestRuntimeDir_EnvVar(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	got := RuntimeDir()
	want := "/run/user/1000/holomush"
	if got != want {
		t.Errorf("RuntimeDir() = %q, want %q", got, want)
	}
}

func TestRuntimeDir_Fallback(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got := RuntimeDir()
	want := "/custom/state/holomush/run"
	if got != want {
		t.Errorf("RuntimeDir() = %q, want %q", got, want)
	}
}

func TestCertsDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got := CertsDir()
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
