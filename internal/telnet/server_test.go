// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_AcceptsConnections(t *testing.T) {
	ctx := t.Context()

	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)
	srv := NewServer(":0", engine, sessions, broadcaster)
	go func() {
		_ = srv.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	addr := srv.Addr()
	require.NotEmpty(t, addr, "Server has no address")

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err, "Failed to connect")
	defer func() {
		_ = conn.Close() // Best effort cleanup in tests
	}()

	err = conn.SetReadDeadline(time.Now().Add(time.Second))
	require.NoError(t, err, "Failed to set read deadline")
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	require.NoError(t, err, "Failed to read welcome")
	assert.NotEmpty(t, line, "Expected welcome message")
}
