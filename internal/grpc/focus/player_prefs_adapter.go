// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/auth"
)

// PlayerRepoReader is the subset of auth.PlayerRepository needed by
// PlayerPrefsAdapter.
type PlayerRepoReader interface {
	GetByID(ctx context.Context, id ulid.ULID) (*auth.Player, error)
}

// PlayerPrefsAdapter bridges auth.PlayerRepository to PlayerPreferencesReader.
type PlayerPrefsAdapter struct {
	repo PlayerRepoReader
}

// NewPlayerPrefsAdapter creates a PlayerPrefsAdapter.
func NewPlayerPrefsAdapter(repo PlayerRepoReader) *PlayerPrefsAdapter {
	return &PlayerPrefsAdapter{repo: repo}
}

// SceneFocusReplayTail returns the player's configured value, or nil if
// unset or on lookup error (degrades to "unset").
func (a *PlayerPrefsAdapter) SceneFocusReplayTail(ctx context.Context, playerID ulid.ULID) *int {
	player, err := a.repo.GetByID(ctx, playerID)
	if err != nil {
		return nil
	}
	return player.Preferences.Scenes.FocusReplayTail
}
