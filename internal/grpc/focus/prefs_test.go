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
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

// stubPlayerPrefs is a test PlayerPreferencesReader.
type stubPlayerPrefs struct {
	tail *int
}

func (s *stubPlayerPrefs) SceneFocusReplayTail(_ context.Context, _ ulid.ULID) *int {
	return s.tail
}

func TestBuildPolicyContextUsesSubstrateDefaultWhenNoSettings(t *testing.T) {
	coord, _ := newTestCoordinator(t, map[string]*session.Info{
		"sess-1": {CharacterID: ulid.Make(), PlayerID: ulid.Make(), Status: session.StatusActive},
	})
	info, err := coord.sessionStore.Get(context.Background(), "sess-1")
	require.NoError(t, err)
	pctx := coord.buildPolicyContext(context.Background(), info, session.FocusKey{
		Kind: session.FocusKindScene, TargetID: ulid.Make(),
	})
	assert.Equal(t, 3, pctx.SceneFocusReplayTail)
}

func TestBuildPolicyContextUsesPlayerPreferenceWhenSet(t *testing.T) {
	tail := 7
	store := sessiontest.NewStore(t)
	ctx := context.Background()
	playerID := ulid.Make()
	require.NoError(t, store.Set(ctx, "sess-1", &session.Info{
		ID: "sess-1", CharacterID: ulid.Make(), PlayerID: playerID, Status: session.StatusActive,
	}))
	coord, err := NewCoordinator(
		WithSessionStore(store),
		WithPlayerPreferences(&stubPlayerPrefs{tail: &tail}),
	)
	require.NoError(t, err)
	dc := coord.(*defaultCoordinator)
	info, infoErr := dc.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, infoErr)
	pctx := dc.buildPolicyContext(ctx, info, session.FocusKey{
		Kind: session.FocusKindScene, TargetID: ulid.Make(),
	})
	assert.Equal(t, 7, pctx.SceneFocusReplayTail)
}

func TestBuildPolicyContextClampsPlayerPreference(t *testing.T) {
	tail := 50
	store := sessiontest.NewStore(t)
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "sess-1", &session.Info{
		ID: "sess-1", CharacterID: ulid.Make(), PlayerID: ulid.Make(), Status: session.StatusActive,
	}))
	coord, err := NewCoordinator(
		WithSessionStore(store),
		WithPlayerPreferences(&stubPlayerPrefs{tail: &tail}),
	)
	require.NoError(t, err)
	dc := coord.(*defaultCoordinator)
	info, infoErr := dc.sessionStore.Get(ctx, "sess-1")
	require.NoError(t, infoErr)
	pctx := dc.buildPolicyContext(ctx, info, session.FocusKey{
		Kind: session.FocusKindScene, TargetID: ulid.Make(),
	})
	assert.Equal(t, 10, pctx.SceneFocusReplayTail) // clamped
}
