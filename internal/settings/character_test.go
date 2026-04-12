// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/settings"
)

func TestNullCharacterSettingsForReturnsAlwaysUnset(t *testing.T) {
	ctx := context.Background()
	store := settings.NewNullCharacterSettingsStore()
	cid := ulid.Make()

	s := store.For(ctx, cid)

	_, ok := s.StringN(ctx, "scenes.focus.mode")
	assert.False(t, ok)

	_, ok = s.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.False(t, ok)

	_, ok = s.BoolN(ctx, "auth.auto_login")
	assert.False(t, ok)

	_, ok = s.DurationN(ctx, "core.session_timeout")
	assert.False(t, ok)
}

func TestNullCharacterSettingsForReturnsNonNilSettings(t *testing.T) {
	ctx := context.Background()
	store := settings.NewNullCharacterSettingsStore()
	cid := ulid.Make()

	s := store.For(ctx, cid)
	assert.NotNil(t, s)
}

func TestNullCharacterSettingsSetStringReturnsError(t *testing.T) {
	ctx := context.Background()
	store := settings.NewNullCharacterSettingsStore()
	cid := ulid.Make()

	err := store.SetString(ctx, cid, "scenes.focus.mode", "bounded")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestNullCharacterSettingsStoreImplementsInterface(_ *testing.T) {
	var _ settings.CharacterSettingsStore = settings.NewNullCharacterSettingsStore() //nolint:staticcheck // intentional interface check
}
