// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package xdg provides XDG Base Directory paths for HoloMUSH.
package xdg

import (
	"fmt"
	"os"
	"path/filepath"
)

const appName = "holomush"

// homeDir returns the user's home directory, preferring $HOME but falling
// back to os.UserHomeDir() for robustness in containers and edge cases.
func homeDir() (string, error) {
	if home := os.Getenv("HOME"); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return home, nil
}

// ConfigDir returns the XDG config directory for holomush.
// Checks XDG_CONFIG_HOME first, falls back to ~/.config.
// Returns an error if the home directory cannot be determined.
func ConfigDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := homeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appName), nil
}

// DataDir returns the XDG data directory for holomush.
// Checks XDG_DATA_HOME first, falls back to ~/.local/share.
// Returns an error if the home directory cannot be determined.
func DataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := homeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, appName), nil
}

// StateDir returns the XDG state directory for holomush.
// Checks XDG_STATE_HOME first, falls back to ~/.local/state.
// Returns an error if the home directory cannot be determined.
func StateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := homeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, appName), nil
}

// RuntimeDir returns the XDG runtime directory for holomush.
// Checks XDG_RUNTIME_DIR first, falls back to StateDir()/run.
// Returns an error if the home directory cannot be determined.
func RuntimeDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		stateDir, err := StateDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(stateDir, "run"), nil
	}
	return filepath.Join(base, appName), nil
}

// CertsDir returns the TLS certificates directory.
// Returns an error if the home directory cannot be determined.
func CertsDir() (string, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "certs"), nil
}

// EnsureDir creates a directory and all parent directories if they don't exist.
// Directories are created with 0700 permissions.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}
	return nil
}
