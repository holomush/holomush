// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/auth"
)

type mockPlayerRepo struct {
	player *auth.Player
	err    error
}

func (m *mockPlayerRepo) GetByID(_ context.Context, _ ulid.ULID) (*auth.Player, error) {
	return m.player, m.err
}

func TestPlayerPrefsAdapterReturnsNilWhenNotSet(t *testing.T) {
	adapter := NewPlayerPrefsAdapter(&mockPlayerRepo{
		player: &auth.Player{Preferences: auth.PlayerPreferences{}},
	})
	result := adapter.SceneFocusReplayTail(context.Background(), ulid.Make())
	assert.Nil(t, result)
}

func TestPlayerPrefsAdapterReturnsValueWhenSet(t *testing.T) {
	tail := 7
	adapter := NewPlayerPrefsAdapter(&mockPlayerRepo{
		player: &auth.Player{
			Preferences: auth.PlayerPreferences{
				Scenes: auth.ScenePlayerPreferences{FocusReplayTail: &tail},
			},
		},
	})
	result := adapter.SceneFocusReplayTail(context.Background(), ulid.Make())
	assert.NotNil(t, result)
	assert.Equal(t, 7, *result)
}

func TestPlayerPrefsAdapterReturnsNilOnError(t *testing.T) {
	adapter := NewPlayerPrefsAdapter(&mockPlayerRepo{err: assert.AnError})
	result := adapter.SceneFocusReplayTail(context.Background(), ulid.Make())
	assert.Nil(t, result)
}
