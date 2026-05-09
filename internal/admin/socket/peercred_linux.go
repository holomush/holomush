// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build linux

package socket

import (
	"net"

	"github.com/samber/oops"
	"golang.org/x/sys/unix"
)

// readPeerCred reads SO_PEERCRED from the UNIX domain socket connection
// using a single getsockopt syscall (Linux-only).
func readPeerCred(conn *net.UnixConn) (PeerCred, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return PeerCred{}, oops.Code("PEERCRED_RAWCONN_FAILED").Wrap(err)
	}

	var ucred *unix.Ucred
	var ctrlErr error
	if err := rawConn.Control(func(fd uintptr) {
		ucred, ctrlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) //nolint:gosec // G115: fd is a valid file descriptor; uintptr→int is safe at syscall boundaries
	}); err != nil {
		return PeerCred{}, oops.Code("PEERCRED_CONTROL_FAILED").Wrap(err)
	}
	if ctrlErr != nil {
		return PeerCred{}, oops.Code("PEERCRED_GETSOCKOPT_FAILED").Wrap(ctrlErr)
	}

	return PeerCred{
		UID: ucred.Uid,
		GID: ucred.Gid,
		PID: ucred.Pid,
	}, nil
}
