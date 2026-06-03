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
	"github.com/holomush/holomush/pkg/errutil"
)

// TestSetConnectionFocus_HappyPath verifies the canonical case: a terminal
// connection in a session whose FocusMemberships already include the target
// scene. Expected commits per D7 + D9:
//   - Connection.FocusKey = focusKey
//   - Info.PresentingFocus = focusKey (terminal + non-grid)
//   - OldFocusKey returned == prior conn.FocusKey (nil here for first focus)
func TestSetConnectionFocus_HappyPath(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	locID := ulid.Make()
	fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID: charID,
				LocationID:  locID,
				Status:      session.StatusActive,
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
	}))

	res, err := coord.SetConnectionFocus(ctx, connID, &fk, false)
	require.NoError(t, err)
	assert.Nil(t, res.OldFocusKey, "first focus → OldFocusKey must be nil")
	assert.Equal(t, "sess-1", res.SessionID)
	assert.Equal(t, locID, res.CharLocationID)

	conn, err := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, err)
	require.NotNil(t, conn.FocusKey, "Connection.FocusKey MUST be set")
	assert.Equal(t, fk.Kind, conn.FocusKey.Kind)
	assert.Equal(t, fk.TargetID, conn.FocusKey.TargetID)

	info, err := coord.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, info.PresentingFocus, "D9: terminal + non-grid MUST write PresentingFocus")
	assert.Equal(t, fk.Kind, info.PresentingFocus.Kind)
	assert.Equal(t, fk.TargetID, info.PresentingFocus.TargetID)
}

// TestSetConnectionFocus_HappyPath_ReturnsOldFocusKey verifies the
// OldFocusKey-capture closure: when switching focus from scene A → B, the
// returned old key matches A (the coordinator's driveFocusDeltas needs this to
// drive subscription_router stream deltas).
func TestSetConnectionFocus_HappyPath_ReturnsOldFocusKey(t *testing.T) {
	t.Parallel()

	sceneA := ulid.Make()
	sceneB := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	fkA := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneA}
	fkB := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneB}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID: charID,
				LocationID:  ulid.Make(),
				Status:      session.StatusActive,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: sceneA, JoinedAt: time.Now()},
					{Kind: session.FocusKindScene, TargetID: sceneB, JoinedAt: time.Now()},
				},
			},
		},
	)

	ctx := context.Background()
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
		FocusKey:   &fkA,
	}))

	res, err := coord.SetConnectionFocus(ctx, connID, &fkB, false)
	require.NoError(t, err)
	require.NotNil(t, res.OldFocusKey, "OldFocusKey MUST surface the prior focus for subscription_router delta")
	assert.Equal(t, sceneA, res.OldFocusKey.TargetID, "OldFocusKey MUST be the pre-mutation conn.FocusKey")
}

// TestSetConnectionFocus_RequiresMembership pins INV-SCENE-14: focus on a scene
// requires a matching FocusMembership in the session. Missing membership →
// FOCUS_WITHOUT_MEMBERSHIP and no state writes.
func TestSetConnectionFocus_RequiresMembership(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID:      charID,
				LocationID:       ulid.Make(),
				Status:           session.StatusActive,
				FocusMemberships: nil, // INV-SCENE-14 violation if SetConnectionFocus proceeds
			},
		},
	)

	ctx := context.Background()
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
	}))

	res, err := coord.SetConnectionFocus(ctx, connID, &fk, false)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "FOCUS_WITHOUT_MEMBERSHIP")
	assert.Nil(t, res.OldFocusKey, "on error, OldFocusKey MUST NOT leak partial state")

	// Verify no writes committed.
	conn, gerr := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, gerr)
	assert.Nil(t, conn.FocusKey, "FocusKey MUST remain nil on membership-validation failure")

	info, gerr := coord.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, gerr)
	assert.Nil(t, info.PresentingFocus, "PresentingFocus MUST NOT be written on membership-validation failure")
}

// TestSetConnectionFocus_CommsHubDoesNotWritePresentingFocus pins D9: only
// terminal/telnet client types update Info.PresentingFocus. A comms_hub
// SetConnectionFocus still writes Connection.FocusKey but leaves
// PresentingFocus untouched.
func TestSetConnectionFocus_CommsHubDoesNotWritePresentingFocus(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID: charID,
				LocationID:  ulid.Make(),
				Status:      session.StatusActive,
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
		ClientType: "comms_hub",
	}))

	_, err := coord.SetConnectionFocus(ctx, connID, &fk, false)
	require.NoError(t, err)

	conn, err := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, err)
	require.NotNil(t, conn.FocusKey, "Connection.FocusKey MUST be written regardless of client type")
	assert.Equal(t, sceneID, conn.FocusKey.TargetID)

	info, err := coord.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, err)
	assert.Nil(t, info.PresentingFocus, "D9: comms_hub MUST NOT write Info.PresentingFocus")
}

// TestSceneGrid_DoesNotClearPresentingFocus pins INV-SCENE-26: a scene-grid
// focus call (isSceneGrid=true) MUST NOT touch Info.PresentingFocus, even
// when focusKey is nil. The session's last explicit focus must survive a
// grid-pivot so reconnect lands on the explicit focus, not the grid.
func TestSceneGrid_DoesNotClearPresentingFocus(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	explicit := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID: charID,
				LocationID:  ulid.Make(),
				Status:      session.StatusActive,
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
	}))

	// Step 1: explicit terminal focus on scene → PresentingFocus is set.
	_, err := coord.SetConnectionFocus(ctx, connID, &explicit, false)
	require.NoError(t, err)
	info, err := coord.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, info.PresentingFocus, "precondition: explicit focus writes PresentingFocus")

	// Step 2: pivot to grid (focusKey=nil + isSceneGrid=true). Conn.FocusKey
	// clears; PresentingFocus MUST remain pointing at the explicit scene.
	res, err := coord.SetConnectionFocus(ctx, connID, nil, true)
	require.NoError(t, err)
	require.NotNil(t, res.OldFocusKey)
	assert.Equal(t, sceneID, res.OldFocusKey.TargetID, "OldFocusKey returned must be the explicit focus")

	conn, err := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, err)
	assert.Nil(t, conn.FocusKey, "scene-grid pivot clears Connection.FocusKey")

	info, err = coord.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, info.PresentingFocus, "INV-SCENE-26: scene-grid MUST NOT clear PresentingFocus")
	assert.Equal(t, sceneID, info.PresentingFocus.TargetID, "INV-SCENE-26: PresentingFocus must still point at the prior explicit focus")
}

// TestSetConnectionFocus_ConnectionNotFound verifies that a bogus connID
// surfaces as CONNECTION_NOT_FOUND.
func TestSetConnectionFocus_ConnectionNotFound(t *testing.T) {
	t.Parallel()

	charID := ulid.Make()
	bogus := ulid.Make()
	fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {CharacterID: charID, Status: session.StatusActive},
		},
	)

	res, err := coord.SetConnectionFocus(context.Background(), bogus, &fk, false)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONNECTION_NOT_FOUND")
	assert.Nil(t, res.OldFocusKey, "on lookup failure, OldFocusKey MUST be nil")
}
