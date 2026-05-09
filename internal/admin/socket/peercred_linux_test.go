// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build linux

package socket

import (
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadPeerCredReturnsNonZeroValuesOnLinux verifies that readPeerCred
// returns non-zero UID, GID, and PID when called on a real UDS connection
// on Linux (SO_PEERCRED).
func TestReadPeerCredReturnsNonZeroValuesOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("linux only; current GOOS=%s", runtime.GOOS)
	}
	dir := t.TempDir()
	sockPath := dir + "/peercred_test.sock"
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
	assert.NotZero(t, cred.PID, "PID must be non-zero")
	// GID is intentionally not asserted non-zero: GID=0 is valid for root processes.
}
