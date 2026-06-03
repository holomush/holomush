// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

// TestRestoreConnectionFocus_RestoresFromPresentingFocus verifies the happy
// path: PresentingFocus is set, the matching FocusMembership exists, and the
// connection's FocusKey is restored to a copy of PresentingFocus.
func TestRestoreConnectionFocus_RestoresFromPresentingFocus(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID:     charID,
				Status:          session.StatusActive,
				PresentingFocus: &fk,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
				},
			},
		},
	)

	ctx := context.Background()
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
		// FocusKey starts nil (default after AddConnection).
	}))

	require.NoError(t, coord.RestoreConnectionFocus(ctx, "sess-1", connID))

	conn, err := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, err)
	require.NotNil(t, conn.FocusKey, "RestoreConnectionFocus MUST set FocusKey when PresentingFocus + membership match")
	assert.Equal(t, fk.Kind, conn.FocusKey.Kind)
	assert.Equal(t, fk.TargetID, conn.FocusKey.TargetID)

	// Must be a copy: mutating the connection's FocusKey must NOT mutate
	// the session's PresentingFocus.
	conn.FocusKey.TargetID = ulid.Make()
	info, err := coord.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, info.PresentingFocus)
	assert.Equal(t, sceneID, info.PresentingFocus.TargetID, "PresentingFocus must be a separate copy from conn.FocusKey")
}

// TestRestoreConnectionFocus_NoOpWhenPresentingFocusNil verifies that with
// no PresentingFocus set, the connection's FocusKey stays nil (grid default).
func TestRestoreConnectionFocus_NoOpWhenPresentingFocusNil(t *testing.T) {
	t.Parallel()

	charID := ulid.Make()
	connID := ulid.Make()

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID:     charID,
				Status:          session.StatusActive,
				PresentingFocus: nil, // explicit grid default
			},
		},
	)

	ctx := context.Background()
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
	}))

	require.NoError(t, coord.RestoreConnectionFocus(ctx, "sess-1", connID))

	conn, err := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, err)
	assert.Nil(t, conn.FocusKey, "FocusKey must remain nil when PresentingFocus is nil (grid default)")
}

// TestReconnect_FallsBackToGridWhenMembershipRevoked pins INV-SCENE-18: when
// PresentingFocus is set but the matching FocusMembership was removed while
// disconnected, restoration falls back to grid (FocusKey stays nil) rather
// than restoring stale state.
func TestReconnect_FallsBackToGridWhenMembershipRevoked(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	stalePF := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID:     charID,
				Status:          session.StatusActive,
				PresentingFocus: &stalePF,
				// Membership for sceneID has been removed (revoked while disconnected).
				FocusMemberships: nil,
			},
		},
	)

	ctx := context.Background()
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
	}))

	require.NoError(t, coord.RestoreConnectionFocus(ctx, "sess-1", connID))

	conn, err := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, err)
	assert.Nil(t, conn.FocusKey, "INV-SCENE-18: FocusKey must stay nil when membership is revoked (grid fallback)")
}

// TestReconnect_VsConcurrentLeave_Serializes pins INV-SCENE-25: concurrent
// RestoreConnectionFocus and LeaveFocus serialize via the SessionConnectionMutator
// / FocusMutator path under the store-side lock. Both orderings are valid:
//   - leave-first: restoration's mutator sees no membership → grid fallback.
//   - restoration-first: restoration commits FocusKey, then leave clears
//     PresentingFocus + membership (FocusKey on the connection is unaffected
//     by LeaveFocus by design — LeaveFocus mutates Info.FocusMemberships,
//     not Connection.FocusKey).
//
// The test asserts no corruption and consistent post-state for each outcome.
func TestReconnect_VsConcurrentLeave_Serializes(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()

	const iterations = 32
	for i := 0; i < iterations; i++ {
		connID := ulid.Make()
		pf := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

		coord, _ := newTestCoordinator(
			t,
			map[string]*session.Info{
				"sess-1": {
					CharacterID:     charID,
					Status:          session.StatusActive,
					PresentingFocus: &pf,
					FocusMemberships: []session.FocusMembership{
						{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
					},
				},
			},
			NewNullPolicy(session.FocusKindScene),
		)

		ctx := context.Background()
		require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
			ID:         connID,
			SessionID:  "sess-1",
			ClientType: "terminal",
		}))

		var wg sync.WaitGroup
		wg.Add(2)
		var restoreErr, leaveErr error
		go func() {
			defer wg.Done()
			restoreErr = coord.RestoreConnectionFocus(ctx, "sess-1", connID)
		}()
		go func() {
			defer wg.Done()
			leaveErr = coord.LeaveFocus(ctx, "sess-1", session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID})
		}()
		wg.Wait()

		require.NoError(t, restoreErr, "RestoreConnectionFocus must not error under race")
		require.NoError(t, leaveErr, "LeaveFocus must not error under race")

		info, err := coord.sessionStore.Get(ctx, "sess-1")
		require.NoError(t, err)
		conn, err := coord.sessionStore.GetConnection(ctx, connID)
		require.NoError(t, err)

		// Post-state consistency:
		// - LeaveFocus always commits: FocusMemberships empty, PresentingFocus nil.
		assert.Empty(t, info.FocusMemberships, "iter %d: LeaveFocus must clear FocusMemberships", i)
		assert.Nil(t, info.PresentingFocus, "iter %d: LeaveFocus must clear PresentingFocus (it pointed at the removed membership)", i)

		// - conn.FocusKey: either nil (leave-first → restore saw no membership)
		//   or {scene, sceneID} (restore-first → leave doesn't touch conn.FocusKey).
		if conn.FocusKey != nil {
			assert.Equal(t, session.FocusKindScene, conn.FocusKey.Kind, "iter %d: if conn.FocusKey set, must be the original scene focus (no corruption)", i)
			assert.Equal(t, sceneID, conn.FocusKey.TargetID, "iter %d: if conn.FocusKey set, target must match original sceneID", i)
		}
	}
}
