// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestRefreshConnectionBumpsLastSeenAndListLapsedExcludesIt verifies that:
//   - AddConnection stamps last_seen_at = connected_at (so a stale connected_at
//     appears in ListLapsedConnections before refresh)
//   - RefreshConnection bumps last_seen_at to now, so the connection is no
//     longer returned by ListLapsedConnections with a 45s cutoff.
func TestRefreshConnectionBumpsLastSeenAndListLapsedExcludesIt(t *testing.T) {
	ctx := context.Background()
	store, pool := sessiontest.NewStoreWithPool(t)

	ps := sessiontest.NewPlayerSession()
	sessiontest.SeedPlayerSession(t, pool, ps)
	sess := sessiontest.NewActiveSession(ps) // status=active, future ExpiresAt
	require.NoError(t, store.Set(ctx, sess.ID, sess))

	connID := ulid.Make()
	require.NoError(t, store.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: sess.ID, ClientType: "terminal",
		ConnectedAt: time.Now().Add(-time.Hour), // stale connect time
	}))

	// AddConnection stamps last_seen_at = connected_at (stale here), so the
	// connection is initially lapsed relative to a 45s TTL.
	lapsed, err := store.ListLapsedConnections(ctx, time.Now().Add(-45*time.Second))
	require.NoError(t, err)
	require.Len(t, lapsed, 1, "stale-connect connection is lapsed before refresh")
	// Assert the projected fields so a column-order regression in the scan path is caught.
	assert.Equal(t, connID, lapsed[0].ID)
	assert.Equal(t, sess.ID, lapsed[0].SessionID)
	assert.Equal(t, "terminal", lapsed[0].ClientType)

	// Refresh bumps last_seen_at to now.
	require.NoError(t, store.RefreshConnection(ctx, connID))

	lapsed, err = store.ListLapsedConnections(ctx, time.Now().Add(-45*time.Second))
	require.NoError(t, err)
	assert.Empty(t, lapsed, "refreshed connection is no longer lapsed")
}

// TestRefreshConnectionReturnsNotFoundForMissingConnection verifies that
// RefreshConnection returns a CONNECTION_NOT_FOUND oops error when the
// connection ID does not exist in the store.
func TestRefreshConnectionReturnsNotFoundForMissingConnection(t *testing.T) {
	ctx := context.Background()
	store, _ := sessiontest.NewStoreWithPool(t)
	err := store.RefreshConnection(ctx, ulid.Make())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONNECTION_NOT_FOUND")
}
