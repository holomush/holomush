// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// mockRefuseConn records SetWriteDeadline, Write, and Close for assertions.
type mockRefuseConn struct {
	net.Conn
	written   []byte
	deadlines []time.Time
	closed    bool
	writeErr  error
	closeErr  error
}

func (m *mockRefuseConn) SetWriteDeadline(t time.Time) error {
	m.deadlines = append(m.deadlines, t)
	return nil
}

func (m *mockRefuseConn) Write(p []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.written = append(m.written, p...)
	return len(p), nil
}

func (m *mockRefuseConn) Close() error {
	m.closed = true
	return m.closeErr
}

func TestRefuseOverCapacityWritesMessageAndCloses(t *testing.T) {
	before := testutil.ToFloat64(ConnectionsRefusedTotal)

	mc := &mockRefuseConn{}
	RefuseOverCapacity(mc, 30*time.Second)

	assert.True(t, mc.closed, "connection must be closed")
	assert.NotEmpty(t, mc.deadlines, "a write deadline must be set")
	assert.Contains(t, strings.ToLower(string(mc.written)), "capacity",
		"refusal message must indicate capacity problem")
	assert.Contains(t, string(mc.written), "\r\n",
		"refusal message must end with CRLF for telnet clients")
	assert.Equal(t, before+1, testutil.ToFloat64(ConnectionsRefusedTotal),
		"refusal must increment the counter")
}

func TestRefuseOverCapacityClosesEvenIfWriteFails(t *testing.T) {
	mc := &mockRefuseConn{writeErr: net.ErrClosed}
	RefuseOverCapacity(mc, 30*time.Second)
	assert.True(t, mc.closed)
}
