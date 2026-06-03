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

// TestAutoFocus_HappyPath_TerminalOnly verifies the canonical fan-out case:
// a session with one terminal and one comms_hub connection, with the
// required FocusMembership. Only the terminal connection is focused (INV-SCENE-17);
// total_connection_count reflects all connections.
func TestAutoFocus_HappyPath_TerminalOnly(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	termConnID := ulid.Make()
	commsConnID := ulid.Make()

	coord, _ := newTestCoordinator(t, map[string]*session.Info{})

	ctx := context.Background()

	// Seed session with FocusMembership for sceneID.
	info := &session.Info{
		ID:          "sess-1",
		CharacterID: charID,
		Status:      session.StatusActive,
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
		},
	}
	require.NoError(t, coord.sessionStore.Set(ctx, "sess-1", info))

	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         termConnID,
		SessionID:  "sess-1",
		ClientType: "terminal",
	}))
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         commsConnID,
		SessionID:  "sess-1",
		ClientType: "comms_hub",
	}))

	resp, err := coord.AutoFocusOnJoin(ctx, charID, sceneID)
	require.NoError(t, err)

	assert.Equal(t, uint32(2), resp.TotalConnectionCount, "both conns must count toward total")
	assert.Len(t, resp.FocusedConnectionIDs, 1, "only terminal should be focused")
	assert.Equal(t, termConnID, resp.FocusedConnectionIDs[0])
	assert.Empty(t, resp.SkippedConnectionIDs)
	assert.Empty(t, resp.FailedConnectionIDs)

	// Verify terminal FocusKey was written.
	conn, err := coord.sessionStore.GetConnection(ctx, termConnID)
	require.NoError(t, err)
	require.NotNil(t, conn.FocusKey)
	assert.Equal(t, session.FocusKindScene, conn.FocusKey.Kind)
	assert.Equal(t, sceneID, conn.FocusKey.TargetID)

	// PresentingFocus must be updated (D9: terminal explicit focus).
	updatedInfo, err := coord.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, updatedInfo.PresentingFocus)
	assert.Equal(t, sceneID, updatedInfo.PresentingFocus.TargetID)

	// comms_hub FocusKey must remain nil (filtered out by INV-SCENE-17).
	commsConn, err := coord.sessionStore.GetConnection(ctx, commsConnID)
	require.NoError(t, err)
	assert.Nil(t, commsConn.FocusKey, "INV-SCENE-17: comms_hub must not be auto-focused")
}

// TestAutoFocus_FiltersByClientType pins INV-SCENE-17: AutoFocusOnJoin only fans
// out to terminal and telnet connections. A session with a telnet connection
// and a comms_hub connection should auto-focus only the telnet connection.
func TestAutoFocus_FiltersByClientType(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	telnetConnID := ulid.Make()
	commsConnID := ulid.Make()

	coord, _ := newTestCoordinator(t, map[string]*session.Info{})

	ctx := context.Background()

	info := &session.Info{
		ID:          "sess-1",
		CharacterID: charID,
		Status:      session.StatusActive,
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
		},
	}
	require.NoError(t, coord.sessionStore.Set(ctx, "sess-1", info))

	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         telnetConnID,
		SessionID:  "sess-1",
		ClientType: "telnet",
	}))
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         commsConnID,
		SessionID:  "sess-1",
		ClientType: "comms_hub",
	}))

	resp, err := coord.AutoFocusOnJoin(ctx, charID, sceneID)
	require.NoError(t, err)

	assert.Equal(t, uint32(2), resp.TotalConnectionCount)
	assert.Len(t, resp.FocusedConnectionIDs, 1)
	assert.Equal(t, telnetConnID, resp.FocusedConnectionIDs[0])
	assert.Empty(t, resp.SkippedConnectionIDs, "INV-SCENE-17: comms_hub is filtered, not skipped")
	assert.Empty(t, resp.FailedConnectionIDs)
}

// TestAutoFocus_SkipsAlreadyExplicitlyFocusedConn pins INV-SCENE-24 (D8 skip-rule):
// a terminal connection that is already explicitly focused on a different
// scene (FocusKey != nil && *FocusKey != target) lands in skipped, not focused.
func TestAutoFocus_SkipsAlreadyExplicitlyFocusedConn(t *testing.T) {
	t.Parallel()

	sceneA := ulid.Make() // the scene the terminal is already focused on
	sceneB := ulid.Make() // the scene being joined (AutoFocusOnJoin target)
	charID := ulid.Make()
	connID := ulid.Make()

	coord, _ := newTestCoordinator(t, map[string]*session.Info{})

	ctx := context.Background()

	fkA := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneA}

	info := &session.Info{
		ID:          "sess-1",
		CharacterID: charID,
		Status:      session.StatusActive,
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: sceneA, JoinedAt: time.Now()},
			{Kind: session.FocusKindScene, TargetID: sceneB, JoinedAt: time.Now()},
		},
	}
	require.NoError(t, coord.sessionStore.Set(ctx, "sess-1", info))

	// Terminal already focused on scene A.
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
		FocusKey:   &fkA,
	}))

	// AutoFocus to scene B — terminal is already focused elsewhere.
	resp, err := coord.AutoFocusOnJoin(ctx, charID, sceneB)
	require.NoError(t, err)

	assert.Equal(t, uint32(1), resp.TotalConnectionCount)
	assert.Empty(t, resp.FocusedConnectionIDs, "INV-SCENE-24: already-focused conn must not be refocused")
	assert.Len(t, resp.SkippedConnectionIDs, 1, "INV-SCENE-24: already-focused conn must be in skipped")
	assert.Equal(t, connID, resp.SkippedConnectionIDs[0])
	assert.Empty(t, resp.FailedConnectionIDs)

	// FocusKey must remain pointing at scene A (unchanged).
	conn, err := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, err)
	require.NotNil(t, conn.FocusKey)
	assert.Equal(t, sceneA, conn.FocusKey.TargetID, "skip must not overwrite existing FocusKey")
}

// TestAutoFocus_FailsForMembershipAbsent verifies that when the session has
// no FocusMembership for the target scene, all terminal connections land in
// failed with reason "membership_absent" (INV-SCENE-14 FOCUS_WITHOUT_MEMBERSHIP).
func TestAutoFocus_FailsForMembershipAbsent(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()

	coord, _ := newTestCoordinator(t, map[string]*session.Info{})

	ctx := context.Background()

	// Session without FocusMembership for sceneID.
	info := &session.Info{
		ID:               "sess-1",
		CharacterID:      charID,
		Status:           session.StatusActive,
		FocusMemberships: nil,
	}
	require.NoError(t, coord.sessionStore.Set(ctx, "sess-1", info))

	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
	}))

	resp, err := coord.AutoFocusOnJoin(ctx, charID, sceneID)
	require.NoError(t, err, "membership_absent is per-conn failure, not a function-level error")

	assert.Equal(t, uint32(1), resp.TotalConnectionCount)
	assert.Empty(t, resp.FocusedConnectionIDs)
	assert.Empty(t, resp.SkippedConnectionIDs)
	require.Len(t, resp.FailedConnectionIDs, 1)
	assert.Equal(t, connID, resp.FailedConnectionIDs[0].ConnectionID)
	assert.Equal(t, "membership_absent", resp.FailedConnectionIDs[0].Reason)

	// FocusKey must not have been written.
	conn, err := coord.sessionStore.GetConnection(ctx, connID)
	require.NoError(t, err)
	assert.Nil(t, conn.FocusKey, "FocusKey must not be written on membership_absent failure")
}

// TestAutoFocus_TotalConnectionCount verifies that TotalConnectionCount in the
// response reflects ALL connections on the session (not just terminal ones).
// A session with 2 terminal + 1 comms_hub must return total == 3.
func TestAutoFocus_TotalConnectionCount(t *testing.T) {
	t.Parallel()

	sceneID := ulid.Make()
	charID := ulid.Make()

	coord, _ := newTestCoordinator(t, map[string]*session.Info{})

	ctx := context.Background()

	info := &session.Info{
		ID:          "sess-1",
		CharacterID: charID,
		Status:      session.StatusActive,
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
		},
	}
	require.NoError(t, coord.sessionStore.Set(ctx, "sess-1", info))

	for i := 0; i < 2; i++ {
		require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
			ID:         ulid.Make(),
			SessionID:  "sess-1",
			ClientType: "terminal",
		}))
	}
	require.NoError(t, coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         ulid.Make(),
		SessionID:  "sess-1",
		ClientType: "comms_hub",
	}))

	resp, err := coord.AutoFocusOnJoin(ctx, charID, sceneID)
	require.NoError(t, err)

	assert.Equal(t, uint32(3), resp.TotalConnectionCount, "total_connection_count must include comms_hub")
	assert.Len(t, resp.FocusedConnectionIDs, 2, "both terminal conns must be focused")
	assert.Empty(t, resp.SkippedConnectionIDs)
	assert.Empty(t, resp.FailedConnectionIDs)
}

// TestAutoFocus_SessionNotFound verifies that when the character has no active
// session, AutoFocusOnJoin returns a zero response (consistent with T16 pattern).
func TestAutoFocus_SessionNotFound(t *testing.T) {
	t.Parallel()

	charID := ulid.Make()
	sceneID := ulid.Make()

	coord, _ := newTestCoordinator(t, map[string]*session.Info{})

	resp, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err, "SESSION_NOT_FOUND must return empty response, not error")

	assert.Equal(t, uint32(0), resp.TotalConnectionCount)
	assert.Empty(t, resp.FocusedConnectionIDs)
	assert.Empty(t, resp.SkippedConnectionIDs)
	assert.Empty(t, resp.FailedConnectionIDs)
}
