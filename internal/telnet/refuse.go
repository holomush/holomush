// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"log/slog"
	"net"
	"time"
)

// refusalMessage is written to clients that hit the connection cap.
// Terminated with CRLF because telnet clients expect line-ending pairs.
const refusalMessage = "Server at capacity. Try again later.\r\n"

// RefuseOverCapacity writes a refusal line to conn and closes it. Used by
// the accept loop when the concurrent-connection semaphore is full. Any
// write error is logged at debug and swallowed — the client has already
// given up or we cannot reach them further.
//
// writeTimeout bounds the refusal write; callers pass the same value used
// for live-connection writes.
//
// The function also increments ConnectionsRefusedTotal so capacity events
// show up in operator metrics.
func RefuseOverCapacity(conn net.Conn, writeTimeout time.Duration) {
	RecordConnectionRefused()

	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		slog.Debug("telnet: failed to set refusal write deadline", "error", err)
	}
	if _, err := conn.Write([]byte(refusalMessage)); err != nil {
		slog.Debug("telnet: failed to write refusal", "error", err)
	}
	if err := conn.Close(); err != nil {
		slog.Debug("telnet: failed to close refused connection", "error", err)
	}
}
