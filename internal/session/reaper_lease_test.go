// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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
)

func TestReaperSweepsLapsedConnectionAndDetachesSession(t *testing.T) {
	ctx := context.Background()
	store, pool := sessiontest.NewStoreWithPool(t)
	ps := sessiontest.NewPlayerSession()
	sessiontest.SeedPlayerSession(t, pool, ps)
	sess := sessiontest.NewActiveSession(ps)
	require.NoError(t, store.Set(ctx, sess.ID, sess))
	connID := ulid.Make()
	require.NoError(t, store.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: sess.ID, ClientType: "terminal",
		ConnectedAt: time.Now().Add(-time.Hour), // lapsed
	}))

	now := time.Now()
	var detached []string
	r := session.NewReaper(store, session.ReaperConfig{
		Interval:          50 * time.Millisecond,
		LeaseTTL:          45 * time.Second,
		BootGrace:         0, // disable boot-grace for this test
		Now:               func() time.Time { return now },
		OnSessionDetached: func(info *session.Info) { detached = append(detached, info.ID) },
	})
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	r.Run(rctx)

	count, err := store.CountConnections(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "lapsed connection was swept")
	assert.Contains(t, detached, sess.ID, "session detached after last connection swept")

	got, err := store.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, session.StatusDetached, got.Status)
	require.NotNil(t, got.ExpiresAt)
	assert.True(t, got.ExpiresAt.After(now), "detached session keeps a future reattach window (TTL guard)")
}

func TestReaperSuppressesLeaseSweepDuringBootGrace(t *testing.T) {
	ctx := context.Background()
	store, pool := sessiontest.NewStoreWithPool(t)
	ps := sessiontest.NewPlayerSession()
	sessiontest.SeedPlayerSession(t, pool, ps)
	sess := sessiontest.NewActiveSession(ps)
	require.NoError(t, store.Set(ctx, sess.ID, sess))
	connID := ulid.Make()
	require.NoError(t, store.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: sess.ID, ClientType: "terminal",
		ConnectedAt: time.Now().Add(-time.Hour),
	}))

	start := time.Now()
	r := session.NewReaper(store, session.ReaperConfig{
		Interval:  50 * time.Millisecond,
		LeaseTTL:  45 * time.Second,
		BootGrace: time.Hour,                                          // still inside grace window during the test
		Now:       func() time.Time { return start.Add(time.Minute) }, // < BootGrace
	})
	rctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	r.Run(rctx)

	count, err := store.CountConnections(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "boot-grace suppresses the lease sweep (I-LIVE-4)")
}
