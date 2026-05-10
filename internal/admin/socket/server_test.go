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

// TestAcquireLockFailsWhenPathIsUnwritable verifies the ADMIN_LOCK_OPEN_FAILED
// path in acquireLock when the lock file directory does not exist.
func TestAcquireLockFailsWhenPathIsUnwritable(t *testing.T) {
	_, err := acquireLock("/nonexistent/dir/admin.lock")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAdminSocketAlreadyHeld)
}

// TestServerStartFailsWhenSocketPathDirectoryMissing verifies the
// ADMIN_SOCKET_LISTEN_FAILED path: lock acquired but net.Listen fails because
// the socket parent directory does not exist.
func TestServerStartFailsWhenSocketPathDirectoryMissing(t *testing.T) {
	dir, err := os.MkdirTemp("", "hm-adm-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	cfg := Config{
		LockPath:   filepath.Join(dir, "a.lock"),       // valid — lock succeeds
		SocketPath: filepath.Join(dir, "nx", "a.sock"), // parent dir missing — Listen fails
	}
	s := NewServer(cfg)
	_, err = s.Start()
	require.Error(t, err)
}

// TestServerStopIsIdempotentWhenCalledTwice verifies that a second Stop call
// on an already-stopped server (s.httpServer == nil) is a no-op.
func TestServerStopIsIdempotentWhenCalledTwice(t *testing.T) {
	cfg := newTestConfig(t)
	s := NewServer(cfg)
	_, err := s.Start()
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, s.Stop(ctx))
	// Second Stop — httpServer is nil; must not panic or error.
	require.NoError(t, s.Stop(ctx))
}

// TestServerServesStatusRPCOverUDS verifies AC-C3/AC-C4 and exercises the
// PeerCredMiddleware + StoreUnixConn path by making a real HTTP request over
// the UDS.
func TestServerServesStatusRPCOverUDS(t *testing.T) {
	cfg := newTestConfig(t)
	s := NewServer(cfg)
	_, err := s.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// Dial the UDS socket and send a minimal HTTP/1.1 POST to the Status endpoint.
	conn, err := net.DialTimeout("unix", cfg.SocketPath, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	const req = "POST /holomush.admin.v1.AdminService/Status HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: 2\r\n" +
		"\r\n" +
		"{}"
	_, err = conn.Write([]byte(req))
	require.NoError(t, err)

	buf := make([]byte, 512)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	n, _ := conn.Read(buf)
	response := string(buf[:n])
	assert.Contains(t, response, "200", "Status endpoint must return HTTP 200")
}
