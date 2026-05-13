// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build darwin

package socket

import (
	"net"

	"github.com/samber/oops"
	"golang.org/x/sys/unix"
)

// readPeerCred reads peer credentials from the UNIX domain socket on Darwin
// using two getsockopt calls:
//   - unix.GetsockoptXucred(SOL_LOCAL, LOCAL_PEERCRED) → uid, gid
//   - unix.GetsockoptInt(SOL_LOCAL, LOCAL_PEERPID)     → pid
func readPeerCred(conn *net.UnixConn) (PeerCred, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return PeerCred{}, oops.Code("PEERCRED_RAWCONN_FAILED").Wrap(err)
	}

	var cred PeerCred
	var ctrlErr error
	if err := rawConn.Control(func(fd uintptr) {
		xucred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED) //nolint:gosec // G115: fd is a valid file descriptor; uintptr→int is safe at syscall boundaries
		if err != nil {
			ctrlErr = oops.Code("PEERCRED_XUCRED_FAILED").Wrap(err)
			return
		}

		pid, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID) //nolint:gosec // G115: fd is a valid file descriptor; uintptr→int is safe at syscall boundaries
		if err != nil {
			ctrlErr = oops.Code("PEERCRED_PEERPID_FAILED").Wrap(err)
			return
		}

		gid := uint32(0)
		if xucred.Ngroups > 0 {
			gid = xucred.Groups[0]
		}
		cred = PeerCred{
			UID: xucred.Uid,
			GID: gid,
			PID: int32(pid), //nolint:gosec // pid_t on Darwin fits int32
		}
	}); err != nil {
		return PeerCred{}, oops.Code("PEERCRED_CONTROL_FAILED").Wrap(err)
	}
	if ctrlErr != nil {
		return PeerCred{}, ctrlErr
	}

	return cred, nil
}
