// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

func TestIsAnyConnFocused_TrueWhenOneMatches(t *testing.T) {
	sceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID: charID,
				Status:      session.StatusActive,
			},
		},
	)

	ctx := context.Background()
	err := coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
		FocusKey:   &fk,
	})
	require.NoError(t, err)

	got, err := coord.IsAnyConnFocused(ctx, charID, sceneID)
	require.NoError(t, err)
	assert.True(t, got)
}

func TestIsAnyConnFocused_FalseWhenNoneMatch(t *testing.T) {
	sceneID := ulid.Make()
	otherSceneID := ulid.Make()
	charID := ulid.Make()
	connID := ulid.Make()
	otherFk := session.FocusKey{Kind: session.FocusKindScene, TargetID: otherSceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {
				CharacterID: charID,
				Status:      session.StatusActive,
			},
		},
	)

	ctx := context.Background()
	// Connection focused on a different scene, not sceneID.
	err := coord.sessionStore.AddConnection(ctx, &session.Connection{
		ID:         connID,
		SessionID:  "sess-1",
		ClientType: "terminal",
		FocusKey:   &otherFk,
	})
	require.NoError(t, err)

	got, err := coord.IsAnyConnFocused(ctx, charID, sceneID)
	require.NoError(t, err)
	assert.False(t, got)
}

func TestIsAnyConnFocused_FalseWhenSessionMissing(t *testing.T) {
	// No sessions in store — FindByCharacter returns SESSION_NOT_FOUND.
	// Spec §6.3: inactive characters should short-circuit to false, not error.
	coord, _ := newTestCoordinator(t, nil)

	charID := ulid.Make()
	sceneID := ulid.Make()

	got, err := coord.IsAnyConnFocused(context.Background(), charID, sceneID)
	require.NoError(t, err, "SESSION_NOT_FOUND must translate to (false, nil)")
	assert.False(t, got)
}
