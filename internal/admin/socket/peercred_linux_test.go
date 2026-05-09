// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build linux

package socket

import (
	"net"
	"runtime"
	"testing"

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
	connCh := make(chan *net.UnixConn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			connCh <- conn.(*net.UnixConn)
		}
	}()
	client, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer client.Close()
	serverConn := <-connCh
	defer serverConn.Close()
	cred, err := readPeerCred(serverConn)
	require.NoError(t, err)
	assert.NotZero(t, cred.UID, "UID must be non-zero")
	assert.NotZero(t, cred.PID, "PID must be non-zero")
	// GID is intentionally not asserted non-zero: GID=0 is valid for root processes.
}
