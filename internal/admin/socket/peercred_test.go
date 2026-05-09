// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPeerCredFromContextReturnsFalseWhenAbsent verifies that PeerCredFromContext
// returns ok=false when no PeerCred is stored in the context.
func TestPeerCredFromContextReturnsFalseWhenAbsent(t *testing.T) {
	_, ok := PeerCredFromContext(context.Background())
	assert.False(t, ok)
}

// TestPeerCredFromContextReturnsTrueWhenPresent verifies that PeerCredFromContext
// returns the stored PeerCred when it is present.
func TestPeerCredFromContextReturnsTrueWhenPresent(t *testing.T) {
	want := PeerCred{UID: 1000, GID: 1000, PID: 42}
	ctx := context.WithValue(context.Background(), peerCredContextKey{}, want)
	got, ok := PeerCredFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, want, got)
}

// TestPeerCredMiddlewarePassesRequestDownstreamWhenNoUnixConn verifies that
// PeerCredMiddleware calls the next handler even when no *net.UnixConn is
// stored in the context (plain HTTP or non-unix transport).
func TestPeerCredMiddlewarePassesRequestDownstreamWhenNoUnixConn(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		_, ok := PeerCredFromContext(r.Context())
		assert.False(t, ok)
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", http.NoBody)
	PeerCredMiddleware(next).ServeHTTP(nil, req)
	assert.True(t, called)
}
