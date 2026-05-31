// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

// Verifies: I-LIVE-3
// Verifies: I-LIVE-5
func TestRecomputeSessionLivenessDetachesOnZeroConnections(t *testing.T) {
	char := ulid.Make()
	loc := ulid.Make()
	sess := mkActiveAt("sess-1", char, loc)
	sess.TTLSeconds = 300 // explicit TTL so we can assert the exact window
	store := newTestSessionStore(t, map[string]*session.Info{"sess-1": sess})
	s := &CoreServer{sessionStore: store}

	before := time.Now()
	err := s.recomputeSessionLiveness(context.Background(), "sess-1")
	require.NoError(t, err)

	got, err := store.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, session.StatusDetached, got.Status)
	assert.False(t, got.GridPresent)
	require.NotNil(t, got.ExpiresAt, "detached session must have a non-nil ExpiresAt (reattach TTL)")

	// Assert the expiry window matches the session's TTLSeconds (300 s), not
	// the 1800 s default. Allow a 5-second wall-clock slack for slow CI.
	wantMin := before.Add(300 * time.Second)
	wantMax := before.Add(305 * time.Second)
	assert.True(t, !got.ExpiresAt.Before(wantMin) && !got.ExpiresAt.After(wantMax),
		"ExpiresAt %v should be within [%v, %v] (TTL=300 s ± 5 s slack)",
		got.ExpiresAt, wantMin, wantMax)
}

func TestRecomputeSessionLivenessUsesDefaultTTLWhenTTLSecondsIsZero(t *testing.T) {
	char := ulid.Make()
	loc := ulid.Make()
	sess := mkActiveAt("sess-default-ttl", char, loc)
	// TTLSeconds left at zero → helper must fall back to 1800 s.
	store := newTestSessionStore(t, map[string]*session.Info{"sess-default-ttl": sess})
	s := &CoreServer{sessionStore: store}

	before := time.Now()
	err := s.recomputeSessionLiveness(context.Background(), "sess-default-ttl")
	require.NoError(t, err)

	got, err := store.Get(context.Background(), "sess-default-ttl")
	require.NoError(t, err)
	assert.Equal(t, session.StatusDetached, got.Status)
	require.NotNil(t, got.ExpiresAt)

	// Default TTL is 1800 s; allow 5 s slack.
	wantMin := before.Add(1800 * time.Second)
	wantMax := before.Add(1805 * time.Second)
	assert.True(t, !got.ExpiresAt.Before(wantMin) && !got.ExpiresAt.After(wantMax),
		"ExpiresAt %v should be within [%v, %v] (default TTL=1800 s ± 5 s slack)",
		got.ExpiresAt, wantMin, wantMax)
}

func TestRecomputeSessionLivenessKeepsActiveWithConnections(t *testing.T) {
	char := ulid.Make()
	loc := ulid.Make()
	sess := mkActiveAt("sess-2", char, loc)
	store := newTestSessionStore(t, map[string]*session.Info{"sess-2": sess})
	s := &CoreServer{sessionStore: store}

	connID := ulid.Make()
	require.NoError(t, store.AddConnection(context.Background(), &session.Connection{
		ID: connID, SessionID: "sess-2", ClientType: "terminal",
	}))

	err := s.recomputeSessionLiveness(context.Background(), "sess-2")
	require.NoError(t, err)

	got, err := store.Get(context.Background(), "sess-2")
	require.NoError(t, err)
	assert.Equal(t, session.StatusActive, got.Status)
	assert.True(t, got.GridPresent)
}

// TestRecomputeSessionLivenessReactivatesDetachedSessionWithConnections
// covers the I2 finding: when a session row is StatusDetached but connections
// exist (e.g. the lease sweep or a future AddConnection caller runs after an
// interrupted recompute), the >0 branch MUST flip status back to active.
func TestRecomputeSessionLivenessReactivatesDetachedSessionWithConnections(t *testing.T) {
	char := ulid.Make()
	loc := ulid.Make()
	future := time.Now().Add(30 * time.Minute)
	sess := &session.Info{
		ID:          "sess-detached",
		Status:      session.StatusDetached,
		ExpiresAt:   &future,
		CharacterID: char,
		LocationID:  loc,
		PlayerID:    ownedPlayerID,
		GridPresent: false,
		TTLSeconds:  1800,
	}
	store := newTestSessionStore(t, map[string]*session.Info{"sess-detached": sess})
	s := &CoreServer{sessionStore: store}

	connID := ulid.Make()
	require.NoError(t, store.AddConnection(context.Background(), &session.Connection{
		ID: connID, SessionID: "sess-detached", ClientType: "terminal",
	}))

	err := s.recomputeSessionLiveness(context.Background(), "sess-detached")
	require.NoError(t, err)

	got, err := store.Get(context.Background(), "sess-detached")
	require.NoError(t, err)
	assert.Equal(t, session.StatusActive, got.Status,
		"detached session with live connections MUST be flipped back to active")
	assert.True(t, got.GridPresent,
		"terminal connection MUST set grid_present=true")
}
