// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
)

func TestSessionConnectionsHasFocusKeyColumn(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 46))

	var dataType string
	err := pool.QueryRow(ctx, `
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'session_connections' AND column_name = 'focus_key'
	`).Scan(&dataType)
	require.NoError(t, err)
	require.Equal(t, "jsonb", dataType,
		"INV-P5-2 substrate column: session_connections.focus_key MUST be JSONB nullable")
}

func TestPostgresListConnectionsBySession(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 46))

	s := store.NewPostgresSessionStore(pool)

	sessionID := "sess-pg-list"
	require.NoError(t, s.Set(ctx, sessionID, &session.Info{ID: sessionID, Status: session.StatusActive}))

	for _, ct := range []string{"terminal", "comms_hub"} {
		require.NoError(t, s.AddConnection(ctx, &session.Connection{
			ID: ulid.Make(), SessionID: sessionID, ClientType: ct, Streams: []string{},
		}))
	}

	conns, err := s.ListConnectionsBySession(ctx, sessionID)
	require.NoError(t, err)
	assert.Len(t, conns, 2)
}

// TestPostgresUpdateSessionConnection_HappyPath exercises the
// GetConnection + UpdateSessionConnection round-trip: mutator updates
// both Connection.FocusKey and Info.PresentingFocus inside a single
// transaction; both writes commit and read back atomically.
func TestPostgresUpdateSessionConnection_HappyPath(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 46))

	s := store.NewPostgresSessionStore(pool)

	sessionID := "sess-pg-uc-happy"
	require.NoError(t, s.Set(ctx, sessionID, &session.Info{
		ID: sessionID, Status: session.StatusActive,
	}))

	connID := ulid.Make()
	require.NoError(t, s.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: sessionID, ClientType: "terminal", Streams: []string{},
	}))

	target := &session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()}
	m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
		conn.FocusKey = target
		info.PresentingFocus = target
		return info, conn, nil
	})
	require.NoError(t, s.UpdateSessionConnection(ctx, sessionID, connID, m))

	// Verify Postgres round-trip via GetConnection.
	conn, err := s.GetConnection(ctx, connID)
	require.NoError(t, err)
	require.NotNil(t, conn.FocusKey)
	assert.Equal(t, target.TargetID, conn.FocusKey.TargetID)
	assert.Equal(t, target.Kind, conn.FocusKey.Kind)

	// Verify the sessions row received the presenting_focus write.
	info, err := s.Get(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, info.PresentingFocus)
	assert.Equal(t, target.TargetID, info.PresentingFocus.TargetID)
}

// TestPostgresUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock
// pins INV-P5-14 / D11: two concurrent UpdateSessionConnection calls on
// the SAME session for DIFFERENT connections MUST NOT deadlock. The
// canonical lock order is the sessions row FIRST (FOR UPDATE), then
// the session_connections row (FOR UPDATE). Without canonical order
// this test would hang (Postgres detects the deadlock and one txn
// errors out, but observable behavior is intermittent timeouts).
func TestPostgresUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 46))

	s := store.NewPostgresSessionStore(pool)

	sessionID := "sess-pg-deadlock"
	require.NoError(t, s.Set(ctx, sessionID, &session.Info{
		ID: sessionID, Status: session.StatusActive,
	}))

	connA := ulid.Make()
	connB := ulid.Make()
	require.NoError(t, s.AddConnection(ctx, &session.Connection{
		ID: connA, SessionID: sessionID, ClientType: "terminal", Streams: []string{},
	}))
	require.NoError(t, s.AddConnection(ctx, &session.Connection{
		ID: connB, SessionID: sessionID, ClientType: "telnet", Streams: []string{},
	}))

	// Spawn N concurrent UpdateSessionConnection pairs racing on
	// (connA, connB). With canonical order both take the sessions
	// row FIRST then their respective session_connections row;
	// no deadlock possible. Without canonical order this would hang.
	const iters = 50
	targetA := &session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()}
	targetB := &session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()}

	errs := make(chan error, iters*2)
	for i := 0; i < iters; i++ {
		go func() {
			m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
				conn.FocusKey = targetA
				return info, conn, nil
			})
			errs <- s.UpdateSessionConnection(ctx, sessionID, connA, m)
		}()
		go func() {
			m := session.NewSessionConnectionMutator(func(info session.Info, conn session.Connection) (session.Info, session.Connection, error) {
				conn.FocusKey = targetB
				return info, conn, nil
			})
			errs <- s.UpdateSessionConnection(ctx, sessionID, connB, m)
		}()
	}

	for i := 0; i < iters*2; i++ {
		require.NoError(t, <-errs, "INV-P5-14: lock-order discipline MUST prevent deadlock under concurrency")
	}
}
