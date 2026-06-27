// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"fmt"
	"sort"
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
	pool := freshMigratedPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var dataType string
	err := pool.QueryRow(ctx, `
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'session_connections' AND column_name = 'focus_key'
	`).Scan(&dataType)
	require.NoError(t, err)
	require.Equal(t, "jsonb", dataType,
		"INV-SCENE-15 substrate column: session_connections.focus_key MUST be JSONB nullable")
}

func TestPostgresListConnectionsBySession(t *testing.T) {
	t.Parallel()
	pool := freshMigratedPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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
	pool := freshMigratedPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
// pins INV-SCENE-27 / D11: two concurrent UpdateSessionConnection calls on
// the SAME session for DIFFERENT connections MUST NOT deadlock. The
// canonical lock order is the sessions row FIRST (FOR UPDATE), then
// the session_connections row (FOR UPDATE). Without canonical order
// this test would hang (Postgres detects the deadlock and one txn
// errors out, but observable behavior is intermittent timeouts).
func TestPostgresUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock(t *testing.T) {
	t.Parallel()
	pool := freshMigratedPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

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
		require.NoError(t, <-errs, "INV-SCENE-27: lock-order discipline MUST prevent deadlock under concurrency")
	}
}

// TestPostgresRemoveConnectionAndCount exercises the atomic remove+count
// primitive that backs Disconnect's lifecycle decision (holomush-cizj):
// removal is scoped, the returned Total counts every client type, and Grid
// counts only grid-bearing connections (terminal + telnet), excluding
// comms_hub.
func TestPostgresRemoveConnectionAndCount(t *testing.T) {
	t.Parallel()
	pool := freshMigratedPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := store.NewPostgresSessionStore(pool)

	sessionID := "sess-pg-rcac"
	require.NoError(t, s.Set(ctx, sessionID, &session.Info{ID: sessionID, Status: session.StatusActive}))

	term := ulid.Make()
	tel := ulid.Make()
	comms := ulid.Make()
	for id, ct := range map[ulid.ULID]string{term: "terminal", tel: "telnet", comms: "comms_hub"} {
		require.NoError(t, s.AddConnection(ctx, &session.Connection{
			ID: id, SessionID: sessionID, ClientType: ct, Streams: []string{},
		}))
	}

	// Remove the comms_hub connection: 2 remain (terminal+telnet), both grid.
	counts, removed, err := s.RemoveConnectionAndCount(ctx, sessionID, comms)
	require.NoError(t, err)
	assert.True(t, removed, "a real removal reports removed=true")
	assert.Equal(t, 2, counts.Total)
	assert.Equal(t, 2, counts.Grid, "comms_hub is excluded from the grid count")

	// Remove the terminal connection: 1 remains (telnet), still grid.
	counts, removed, err = s.RemoveConnectionAndCount(ctx, sessionID, term)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.Equal(t, 1, counts.Total)
	assert.Equal(t, 1, counts.Grid)

	// Remove the last connection (telnet): 0 remain.
	counts, removed, err = s.RemoveConnectionAndCount(ctx, sessionID, tel)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.Equal(t, 0, counts.Total)
	assert.Equal(t, 0, counts.Grid)
}

// TestPostgresRemoveConnectionAndCountMissingConnectionIsNoOp pins the
// idempotent contract (holomush-cizj): removing a connection that is not
// present returns the current counts without error, so Disconnect retries do
// not error.
func TestPostgresRemoveConnectionAndCountMissingConnectionIsNoOp(t *testing.T) {
	t.Parallel()
	pool := freshMigratedPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := store.NewPostgresSessionStore(pool)

	sessionID := "sess-pg-rcac-noop"
	require.NoError(t, s.Set(ctx, sessionID, &session.Info{ID: sessionID, Status: session.StatusActive}))
	require.NoError(t, s.AddConnection(ctx, &session.Connection{
		ID: ulid.Make(), SessionID: sessionID, ClientType: "terminal", Streams: []string{},
	}))

	counts, removed, err := s.RemoveConnectionAndCount(ctx, sessionID, ulid.Make()) // never added
	require.NoError(t, err)
	assert.False(t, removed, "removing an absent connection reports removed=false")
	assert.Equal(t, 1, counts.Total, "no-op removal leaves the existing connection")
	assert.Equal(t, 1, counts.Grid)
}

// TestPostgresRemoveConnectionAndCountMissingSessionIsNoOp pins that a removal
// targeting an already-deleted session returns zero counts without error —
// Disconnect treats a gone session as idempotent success.
func TestPostgresRemoveConnectionAndCountMissingSessionIsNoOp(t *testing.T) {
	t.Parallel()
	pool := freshMigratedPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := store.NewPostgresSessionStore(pool)

	counts, removed, err := s.RemoveConnectionAndCount(ctx, "sess-does-not-exist", ulid.Make())
	require.NoError(t, err)
	assert.False(t, removed, "a gone session reports removed=false")
	assert.Equal(t, 0, counts.Total)
	assert.Equal(t, 0, counts.Grid)
}

// TestPostgresRemoveConnectionAndCountConcurrentLastRemoveExactlyOneObservesZero
// is the regression for holomush-cizj. Two connections on one session are
// removed concurrently; the sessions-row FOR UPDATE lock serializes the
// remove+count, so the two returned Totals are exactly {0, 1} — never {0, 0}
// (the double-cleanup the bug caused) nor {1, 1} (a skip). Without the lock,
// under READ COMMITTED the two deletes are mutually invisible and this
// assertion fails. The shared single-session lock cannot deadlock: both
// callers contend for the SAME row before deleting their own connection row.
func TestPostgresRemoveConnectionAndCountConcurrentLastRemoveExactlyOneObservesZero(t *testing.T) {
	t.Parallel()
	pool := freshMigratedPool(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s := store.NewPostgresSessionStore(pool)

	const iters = 30
	for i := 0; i < iters; i++ {
		sessionID := fmt.Sprintf("sess-pg-rcac-conc-%d", i)
		// Each session needs a distinct CharacterID: idx_sessions_active_character
		// enforces one active session per character, so reusing the zero ULID
		// across iterations would collide.
		require.NoError(t, s.Set(ctx, sessionID, &session.Info{
			ID: sessionID, Status: session.StatusActive, CharacterID: ulid.Make(),
		}))
		connA := ulid.Make()
		connB := ulid.Make()
		require.NoError(t, s.AddConnection(ctx, &session.Connection{
			ID: connA, SessionID: sessionID, ClientType: "terminal", Streams: []string{},
		}))
		require.NoError(t, s.AddConnection(ctx, &session.Connection{
			ID: connB, SessionID: sessionID, ClientType: "telnet", Streams: []string{},
		}))

		type res struct {
			total   int
			removed bool
			err     error
		}
		ch := make(chan res, 2)
		remove := func(id ulid.ULID) {
			c, removed, rErr := s.RemoveConnectionAndCount(ctx, sessionID, id)
			ch <- res{c.Total, removed, rErr}
		}
		go remove(connA)
		go remove(connB)

		totals := make([]int, 0, 2)
		for j := 0; j < 2; j++ {
			r := <-ch
			require.NoError(t, r.err)
			assert.True(t, r.removed, "each goroutine removes a distinct, present connection")
			totals = append(totals, r.total)
		}
		sort.Ints(totals)
		assert.Equal(t, []int{0, 1}, totals,
			"holomush-cizj: concurrent last-removes MUST serialize so exactly one observes Total==0")
	}
}
