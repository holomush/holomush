// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/settings"
)

// Compile-time interface check: NewNullCharacterSettingsStore must satisfy CharacterSettingsStore.
var _ settings.CharacterSettingsStore = settings.NewNullCharacterSettingsStore()

// errCharacterRepo is a CharacterRepository whose load always fails, used to
// exercise the fail-closed degrade path.
type errCharacterRepo struct{ err error }

func (r errCharacterRepo) GetPreferences(context.Context, ulid.ULID) (settings.CharacterPreferences, error) {
	return settings.CharacterPreferences{}, r.err
}

func (r errCharacterRepo) SetPreferences(context.Context, ulid.ULID, settings.CharacterPreferences) error {
	return nil
}

// TestRepoCharacterSettingsWriteFailsClosedOnLoadError proves that when the repo
// load fails, reads still degrade to empty (the Settings reads-never-error
// contract) but a WRITE surfaces the load error rather than silently dropping
// into an in-memory map that will never be persisted.
func TestRepoCharacterSettingsWriteFailsClosedOnLoadError(t *testing.T) {
	ctx := context.Background()
	st := settings.NewRepoCharacterSettingsStore(errCharacterRepo{err: errors.New("load boom")})

	// Read degrades to empty (never-error contract preserved).
	_, ok := st.For(ctx, ulid.Make()).Plugin("core-scenes").StringSliceN(ctx, "k")
	assert.False(t, ok)

	// Write must surface the failure, not silently succeed.
	err := st.For(ctx, ulid.Make()).Plugin("core-scenes").SetStringSlice(ctx, "k", []string{"v"})
	assert.Error(t, err, "a write after a load failure must fail closed, not silently drop")
}

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

	// Exercises emptySettings.StringSliceN (unexported; reached via the
	// null character store).
	v, ok := s.StringSliceN(ctx, "scenes.focus.tags")
	assert.False(t, ok)
	assert.Nil(t, v)
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
