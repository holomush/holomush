// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

// TestGetConnectionFocus_ReturnsSceneFocusForFocusedConnection verifies the
// read path mirrors what SetConnectionFocus wrote: a connection focused on a
// scene reports that FocusKey back to the caller (the routing input handleEmit
// consults).
func TestGetConnectionFocus_ReturnsSceneFocusForFocusedConnection(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	connID := ulid.Make()
	fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(t, map[string]*session.Info{
		"sess-1": {
			CharacterID: ulid.Make(),
			LocationID:  ulid.Make(),
			Status:      session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		},
	})

	ctx := context.Background()
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: "sess-1", ClientType: "terminal",
	}))
	_, err := coord.SetConnectionFocus(ctx, connID, &fk, false)
	require.NoError(t, err)

	got, err := coord.GetConnectionFocus(ctx, connID)
	require.NoError(t, err)
	require.NotNil(t, got, "a scene-focused connection MUST report its FocusKey")
	assert.Equal(t, session.FocusKindScene, got.Kind)
	assert.Equal(t, sceneID, got.TargetID)
}

// TestGetConnectionFocus_ReturnsNilForGridFocusedConnection verifies a
// connection with no per-connection focus (grid focus) reports absent focus,
// not an error — handleEmit falls back to single-membership inference here.
func TestGetConnectionFocus_ReturnsNilForGridFocusedConnection(t *testing.T) {
	t.Parallel()

	connID := ulid.Make()
	coord, _ := newTestCoordinator(t, map[string]*session.Info{
		"sess-1": {
			CharacterID: ulid.Make(),
			LocationID:  ulid.Make(),
			Status:      session.StatusActive,
		},
	})

	ctx := context.Background()
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID: connID, SessionID: "sess-1", ClientType: "terminal",
	}))

	got, err := coord.GetConnectionFocus(ctx, connID)
	require.NoError(t, err)
	assert.Nil(t, got, "a grid-focused connection has no FocusKey")
}

// TestGetConnectionFocus_TreatsUnknownConnectionAsAbsentFocus pins the
// safety-critical not-found-vs-error distinction: a connection that does not
// exist (e.g. disconnected between dispatch and lookup) yields (nil, nil), NOT
// an error — so a stale connection id degrades posing to the membership
// fallback rather than failing the emit.
func TestGetConnectionFocus_TreatsUnknownConnectionAsAbsentFocus(t *testing.T) {
	t.Parallel()

	coord, _ := newTestCoordinator(t, map[string]*session.Info{})

	got, err := coord.GetConnectionFocus(context.Background(), ulid.Make())
	require.NoError(t, err, "unknown connection MUST be absent focus, never an error")
	assert.Nil(t, got)
}
