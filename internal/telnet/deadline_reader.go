// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"net"
	"time"

	"github.com/samber/oops"
)

// deadlineReader refreshes the underlying connection's read deadline
// before every Read call, so the idle timeout is measured from the last
// byte received rather than connection open. A Slowloris attacker that
// drip-feeds bytes still hits the deadline because the refresh only
// fires at read time — no bytes in flight, no refresh, deadline expires.
type deadlineReader struct {
	conn    net.Conn
	timeout time.Duration
}

func (r *deadlineReader) Read(p []byte) (int, error) {
	if err := r.conn.SetReadDeadline(time.Now().Add(r.timeout)); err != nil {
		return 0, oops.With("operation", "set read deadline").Wrap(err)
	}
	n, err := r.conn.Read(p)
	if err != nil {
		return n, oops.With("operation", "read from conn").Wrap(err)
	}
	return n, nil
}
