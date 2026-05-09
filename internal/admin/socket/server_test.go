// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfig(t *testing.T) Config {
	t.Helper()
	// Use os.MkdirTemp with a short prefix in /tmp to stay under the 104-byte
	// macOS UNIX socket path limit. t.TempDir() embeds the full test name and
	// frequently exceeds the limit.
	dir, err := os.MkdirTemp("", "hm-adm-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return Config{
		SocketPath: filepath.Join(dir, "a.sock"),
		LockPath:   filepath.Join(dir, "a.lock"),
		Version:    "test-v1.2.3",
	}
}

// TestServerStartsAndStopsCleanlyOnUDS verifies AC-C1, AC-C2, AC-C9, AC-C11.
func TestServerStartsAndStopsCleanlyOnUDS(t *testing.T) {
	cfg := newTestConfig(t)
	s := NewServer(cfg)

	errCh, err := s.Start()
	require.NoError(t, err)

	// AC-C11: listener is *net.UnixListener (UDS-only, never TCP)
	_, ok := s.listener.(*net.UnixListener)
	require.True(t, ok, "listener must be *net.UnixListener")

	// AC-C1: admin.sock and admin.lock exist
	_, err = os.Stat(cfg.SocketPath)
	require.NoError(t, err, "admin.sock must exist after Start")
	_, err = os.Stat(cfg.LockPath)
	require.NoError(t, err, "admin.lock must exist after Start")

	// AC-C2: socket has mode 0600
	info, err := os.Stat(cfg.SocketPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "admin.sock must have mode 0600")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, s.Stop(ctx))

	// AC-C9: admin.sock removed; admin.lock persists
	_, err = os.Stat(cfg.SocketPath)
	assert.True(t, os.IsNotExist(err), "admin.sock must be removed after Stop")
	_, err = os.Stat(cfg.LockPath)
	assert.NoError(t, err, "admin.lock must persist after Stop")

	select {
	case serverErr, open := <-errCh:
		if open {
			t.Errorf("unexpected server goroutine error: %v", serverErr)
		}
	default:
	}
}

// TestServerRejectsStartWhenLockHeld verifies AC-C7: a live flock holder
// causes ErrAdminSocketAlreadyHeld.
func TestServerRejectsStartWhenLockHeld(t *testing.T) {
	cfg := newTestConfig(t)

	s1 := NewServer(cfg)
	_, err := s1.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s1.Stop(context.Background()) })

	s2 := NewServer(cfg)
	_, err = s2.Start()
	require.ErrorIs(t, err, ErrAdminSocketAlreadyHeld)
}

// TestServerRecoversStaleLockAndSocket verifies AC-C8: when admin.lock
// exists but is not held (no live flock), stale files are cleaned and server starts.
func TestServerRecoversStaleLockAndSocket(t *testing.T) {
	cfg := newTestConfig(t)

	require.NoError(t, os.WriteFile(cfg.LockPath, []byte("stale"), 0o600))
	require.NoError(t, os.WriteFile(cfg.SocketPath, []byte{}, 0o600))

	s := NewServer(cfg)
	_, err := s.Start()
	require.NoError(t, err, "must start cleanly despite stale files")
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
}

// TestServerAcceptsUnixConnections verifies the server accepts connections
// over its UDS path.
func TestServerAcceptsUnixConnections(t *testing.T) {
	cfg := newTestConfig(t)
	s := NewServer(cfg)

	_, err := s.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	conn, err := net.DialTimeout("unix", cfg.SocketPath, 2*time.Second)
	require.NoError(t, err)
	conn.Close()
}
