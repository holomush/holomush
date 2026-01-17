// Package xdg provides XDG Base Directory paths for HoloMUSH.
package xdg

import (
	"fmt"
	"os"
	"path/filepath"
)

const appName = "holomush"

// ConfigDir returns the XDG config directory for holomush.
// Checks XDG_CONFIG_HOME first, falls back to ~/.config.
func ConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, appName)
}

// DataDir returns the XDG data directory for holomush.
// Checks XDG_DATA_HOME first, falls back to ~/.local/share.
func DataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	return filepath.Join(base, appName)
}

// StateDir returns the XDG state directory for holomush.
// Checks XDG_STATE_HOME first, falls back to ~/.local/state.
func StateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, appName)
}

// RuntimeDir returns the XDG runtime directory for holomush.
// Checks XDG_RUNTIME_DIR first, falls back to StateDir()/run.
func RuntimeDir() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		return filepath.Join(StateDir(), "run")
	}
	return filepath.Join(base, appName)
}

// CertsDir returns the TLS certificates directory.
func CertsDir() string {
	return filepath.Join(ConfigDir(), "certs")
}

// EnsureDir creates a directory and all parent directories if they don't exist.
// Directories are created with 0700 permissions.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}
	return nil
}
