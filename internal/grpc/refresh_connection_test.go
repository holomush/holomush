// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestRefreshConnectionReturnsInvalidArgumentOnMissingSessionID verifies that
// an empty session_id returns INVALID_ARGUMENT without touching the store.
func TestRefreshConnectionReturnsInvalidArgumentOnMissingSessionID(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId:          "",
		ConnectionId:       ulid.Make().String(),
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

// TestRefreshConnectionReturnsInvalidArgumentOnMissingConnectionID verifies
// that an empty connection_id returns INVALID_ARGUMENT.
func TestRefreshConnectionReturnsInvalidArgumentOnMissingConnectionID(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId:          "sess-1",
		ConnectionId:       "",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

// TestRefreshConnectionCollapsesOwnershipFailureToSessionNotFound verifies
// that ALL ownership validation failures collapse to SESSION_NOT_FOUND
// (enumeration-safe, I-SEC-1). Here the session does not exist.
// Verifies: I-SEC-1
func TestRefreshConnectionCollapsesOwnershipFailureToSessionNotFound(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId:          "missing",
		ConnectionId:       ulid.Make().String(),
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	// Top-level code — no chain walk needed (handler returns the code directly).
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

// TestRefreshConnectionReturnsInvalidArgumentOnMalformedConnectionID verifies
// that a non-ULID connection_id (after passing ownership validation) returns
// INVALID_ARGUMENT.
func TestRefreshConnectionReturnsInvalidArgumentOnMalformedConnectionID(t *testing.T) {
	char := ulid.Make()
	loc := ulid.Make()
	sess := mkActiveAt("sess-1", char, loc)
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	_, err := s.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId:          "sess-1",
		ConnectionId:       "not-a-ulid",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

// TestRefreshConnectionReturnsConnectionNotFoundWhenConnectionMissing verifies
// that a valid ULID connection_id that does not exist in the store propagates
// CONNECTION_NOT_FOUND from the store (un-wrapped, top-level code assertion).
func TestRefreshConnectionReturnsConnectionNotFoundWhenConnectionMissing(t *testing.T) {
	char := ulid.Make()
	loc := ulid.Make()
	sess := mkActiveAt("sess-1", char, loc)
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	_, err := s.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId:          "sess-1",
		ConnectionId:       ulid.Make().String(),
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	// Use top-level code assertion per .claude/rules/grpc-errors.md — the store
	// returns the oops code un-wrapped, and the handler passes it through un-wrapped.
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "CONNECTION_NOT_FOUND", o.Code())
}

// TestRefreshConnectionRefreshesOwnedConnection verifies the success path: an
// owned session with an existing connection refreshes without error and returns
// a non-nil response.
func TestRefreshConnectionRefreshesOwnedConnection(t *testing.T) {
	char := ulid.Make()
	loc := ulid.Make()
	sess := mkActiveAt("sess-1", char, loc)
	store := newTestSessionStore(t, map[string]*session.Info{"sess-1": sess})

	connID := ulid.Make()
	require.NoError(t, store.AddConnection(context.Background(), &session.Connection{
		ID: connID, SessionID: "sess-1", ClientType: "terminal",
	}))

	s := &CoreServer{
		sessionStore:      store,
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	resp, err := s.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId:          "sess-1",
		ConnectionId:       connID.String(),
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}
