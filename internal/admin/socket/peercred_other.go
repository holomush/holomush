// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !linux && !darwin

package socket

import (
	"errors"
	"log/slog"
	"net"
	"sync"
)

// errPeerCredUnsupported is returned by readPeerCred on platforms that
// support neither SO_PEERCRED (Linux) nor GetsockoptXucred (Darwin).
var errPeerCredUnsupported = errors.New("SO_PEERCRED not supported on this platform")

var warnOnce sync.Once

// readPeerCred is a no-op on platforms that support neither SO_PEERCRED
// (Linux) nor GetsockoptXucred/LOCAL_PEERPID (Darwin). A one-time warning
// is logged at first call.
func readPeerCred(_ *net.UnixConn) (PeerCred, error) {
	warnOnce.Do(func() {
		slog.Warn("admin socket: peer credentials not available on this platform; audit enrichment disabled")
	})
	return PeerCred{}, errPeerCredUnsupported
}
