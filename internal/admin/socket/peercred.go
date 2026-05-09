// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package socket provides the admin UNIX domain socket substrate:
// a ConnectRPC server bound to admin.sock (never TCP), SO_PEERCRED
// middleware for audit enrichment, and flock-based double-start detection.
package socket

import (
	"context"
	"net"
	"net/http"
)

// PeerCred holds the peer process identity captured from a UNIX domain socket
// connection. It is populated for audit enrichment only — not a defense factor.
type PeerCred struct {
	UID uint32
	GID uint32
	PID int32
}

// peerCredContextKey is the unexported context key for PeerCred storage.
type peerCredContextKey struct{}

// unixConnContextKey is the context key for storing the *net.UnixConn
// injected by the http.Server ConnContext field.
type unixConnContextKey struct{}

// PeerCredFromContext extracts the PeerCred stored by PeerCredMiddleware.
// ok is false when no peer cred is available (non-linux/darwin platforms,
// or request arrived before ConnContext populated the connection).
func PeerCredFromContext(ctx context.Context) (PeerCred, bool) {
	v, ok := ctx.Value(peerCredContextKey{}).(PeerCred)
	return v, ok
}

// StoreUnixConn is called from http.Server.ConnContext to attach the
// *net.UnixConn to the connection-level context before any request handler runs.
func StoreUnixConn(ctx context.Context, c net.Conn) context.Context {
	if uc, ok := c.(*net.UnixConn); ok {
		return context.WithValue(ctx, unixConnContextKey{}, uc)
	}
	return ctx
}

// WithPeerCred returns ctx with the given PeerCred attached, using the
// same context key the PeerCredMiddleware uses. Exported for tests that
// need to construct a context outside the middleware path; production
// code paths (handlers reached via the UDS server) get PeerCred via
// the middleware automatically.
func WithPeerCred(ctx context.Context, cred PeerCred) context.Context {
	return context.WithValue(ctx, peerCredContextKey{}, cred)
}

// PeerCredMiddleware reads SO_PEERCRED (or platform equivalent) from the
// underlying *net.UnixConn stored in the request context and attaches the
// result as a PeerCred. If the platform does not support peer credentials,
// the request passes through unmodified (ok=false in PeerCredFromContext).
func PeerCredMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if uc, ok := r.Context().Value(unixConnContextKey{}).(*net.UnixConn); ok {
			if cred, err := readPeerCred(uc); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), peerCredContextKey{}, cred))
			}
		}
		next.ServeHTTP(w, r)
	})
}
