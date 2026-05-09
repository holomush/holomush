// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build darwin

package socket

import (
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadPeerCredReturnsNonZeroValuesOnDarwin verifies that readPeerCred
// returns non-zero UID, GID, and PID on Darwin (GetsockoptXucred + LOCAL_PEERPID).
func TestReadPeerCredReturnsNonZeroValuesOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("darwin only; current GOOS=%s", runtime.GOOS)
	}
	// macOS limits UNIX socket paths to 104 bytes; t.TempDir() paths can exceed
	// that with long test names. Use os.MkdirTemp with a short prefix instead.
	dir, err := os.MkdirTemp("", "pcd")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sockPath := dir + "/p.sock"
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer ln.Close()
	type acceptResult struct {
		conn *net.UnixConn
		err  error
	}
	connCh := make(chan acceptResult, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			connCh <- acceptResult{err: acceptErr}
			return
		}
		connCh <- acceptResult{conn: conn.(*net.UnixConn)}
	}()
	client, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer client.Close()
	var serverConn *net.UnixConn
	select {
	case res := <-connCh:
		require.NoError(t, res.err)
		serverConn = res.conn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server-side accept")
	}
	defer serverConn.Close()
	cred, err := readPeerCred(serverConn)
	require.NoError(t, err)
	assert.NotZero(t, cred.UID, "UID must be non-zero")
	assert.NotZero(t, cred.GID, "GID must be non-zero")
	assert.NotZero(t, cred.PID, "PID must be non-zero")
}
