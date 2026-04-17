// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDeadlineConn is a minimal net.Conn stand-in that records every
// SetReadDeadline call. Only Read + SetReadDeadline are exercised.
type mockDeadlineConn struct {
	net.Conn       // embed for the methods we don't care about (they'll panic if touched)
	readErr        error
	readBuf        []byte
	reads          int
	deadlines      []time.Time
	setDeadlineErr error
}

func (m *mockDeadlineConn) SetReadDeadline(t time.Time) error {
	m.deadlines = append(m.deadlines, t)
	return m.setDeadlineErr
}

func (m *mockDeadlineConn) Read(p []byte) (int, error) {
	m.reads++
	if m.readErr != nil {
		return 0, m.readErr
	}
	n := copy(p, m.readBuf)
	return n, nil
}

func TestDeadlineReaderSetsDeadlineBeforeEveryRead(t *testing.T) {
	mc := &mockDeadlineConn{readBuf: []byte("ab")}
	r := &deadlineReader{conn: mc, timeout: 100 * time.Millisecond}

	buf := make([]byte, 1)

	start := time.Now()
	_, err := r.Read(buf)
	require.NoError(t, err)
	_, err = r.Read(buf)
	require.NoError(t, err)

	assert.Equal(t, 2, mc.reads, "one underlying Read per wrapper Read")
	require.Len(t, mc.deadlines, 2, "one deadline per Read")

	for i, d := range mc.deadlines {
		delta := d.Sub(start)
		assert.GreaterOrEqual(t, delta, 100*time.Millisecond,
			"deadline #%d is in the future by at least timeout", i)
		assert.Less(t, delta, 1*time.Second,
			"deadline #%d is not absurdly far in the future", i)
	}
}

func TestDeadlineReaderPropagatesReadError(t *testing.T) {
	sentinel := errors.New("boom")
	mc := &mockDeadlineConn{readErr: sentinel}
	r := &deadlineReader{conn: mc, timeout: 100 * time.Millisecond}

	_, err := r.Read(make([]byte, 1))
	assert.ErrorIs(t, err, sentinel)
}

func TestDeadlineReaderReturnsDeadlineErrorWithoutCallingRead(t *testing.T) {
	sentinel := errors.New("deadline-boom")
	mc := &mockDeadlineConn{
		readBuf:        []byte("never-read"),
		setDeadlineErr: sentinel,
	}
	r := &deadlineReader{conn: mc, timeout: 100 * time.Millisecond}

	n, err := r.Read(make([]byte, 8))

	assert.ErrorIs(t, err, sentinel, "wrapper must surface SetReadDeadline error")
	assert.Zero(t, n, "no bytes read when deadline-set fails")
	assert.Zero(t, mc.reads, "underlying Read must not be called when deadline-set fails")
}
