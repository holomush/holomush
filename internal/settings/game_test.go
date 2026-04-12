// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/settings"
)

// mockSystemInfoStore implements settings.SystemInfoStore for testing.
type mockSystemInfoStore struct {
	data map[string]string
	err  error
}

func newMockSystemInfoStore() *mockSystemInfoStore {
	return &mockSystemInfoStore{data: make(map[string]string)}
}

func (m *mockSystemInfoStore) GetSystemInfo(_ context.Context, key string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	v, ok := m.data[key]
	if !ok {
		return "", settings.ErrNotFound
	}
	return v, nil
}

func (m *mockSystemInfoStore) SetSystemInfo(_ context.Context, key, value string) error {
	if m.err != nil {
		return m.err
	}
	m.data[key] = value
	return nil
}

func TestGameSettingsStringNReturnsStoredValue(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["scenes.focus.mode"] = "bounded"
	gs := settings.NewGameSettings(store)

	v, ok := gs.StringN(ctx, "scenes.focus.mode")
	assert.True(t, ok)
	assert.Equal(t, "bounded", v)
}

func TestGameSettingsStringNReturnsFalseWhenNotFound(t *testing.T) {
	ctx := context.Background()
	gs := settings.NewGameSettings(newMockSystemInfoStore())

	_, ok := gs.StringN(ctx, "scenes.focus.mode")
	assert.False(t, ok)
}

func TestGameSettingsStringNReturnsFalseOnError(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.err = errors.New("connection failed")
	gs := settings.NewGameSettings(store)

	_, ok := gs.StringN(ctx, "scenes.focus.mode")
	assert.False(t, ok)
}

func TestGameSettingsIntNParsesStoredValue(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["scenes.focus.replay_tail_default"] = "3"
	gs := settings.NewGameSettings(store)

	v, ok := gs.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 3, v)
}

func TestGameSettingsIntNReturnsFalseForNonNumeric(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["scenes.focus.replay_tail_default"] = "not-a-number"
	gs := settings.NewGameSettings(store)

	_, ok := gs.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.False(t, ok)
}

func TestGameSettingsIntNReturnsFalseWhenNotFound(t *testing.T) {
	ctx := context.Background()
	gs := settings.NewGameSettings(newMockSystemInfoStore())

	_, ok := gs.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.False(t, ok)
}

func TestGameSettingsBoolNParsesTrue(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["core.maintenance_mode"] = "true"
	gs := settings.NewGameSettings(store)

	v, ok := gs.BoolN(ctx, "core.maintenance_mode")
	assert.True(t, ok)
	assert.True(t, v)
}

func TestGameSettingsBoolNParsesFalse(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["core.maintenance_mode"] = "false"
	gs := settings.NewGameSettings(store)

	v, ok := gs.BoolN(ctx, "core.maintenance_mode")
	assert.True(t, ok)
	assert.False(t, v)
}

func TestGameSettingsBoolNReturnsFalseForNonBool(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["core.maintenance_mode"] = "maybe"
	gs := settings.NewGameSettings(store)

	_, ok := gs.BoolN(ctx, "core.maintenance_mode")
	assert.False(t, ok)
}

func TestGameSettingsDurationNParsesStoredValue(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["core.session_timeout"] = "30s"
	gs := settings.NewGameSettings(store)

	v, ok := gs.DurationN(ctx, "core.session_timeout")
	assert.True(t, ok)
	assert.Equal(t, 30*time.Second, v)
}

func TestGameSettingsDurationNReturnsFalseForInvalidDuration(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["core.session_timeout"] = "not-a-duration"
	gs := settings.NewGameSettings(store)

	_, ok := gs.DurationN(ctx, "core.session_timeout")
	assert.False(t, ok)
}

func TestGameSettingsSetStringStoresValue(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	gs := settings.NewGameSettings(store)

	err := gs.SetString(ctx, "scenes.focus.replay_tail_default", "5")
	require.NoError(t, err)
	assert.Equal(t, "5", store.data["scenes.focus.replay_tail_default"])
}

func TestGameSettingsSetStringRejectsInvalidNamespace(t *testing.T) {
	ctx := context.Background()
	gs := settings.NewGameSettings(newMockSystemInfoStore())

	err := gs.SetString(ctx, "bogus.key", "val")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown namespace")
}

func TestGameSettingsSetStringReturnsStoreError(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.err = errors.New("write failed")
	gs := settings.NewGameSettings(store)

	err := gs.SetString(ctx, "scenes.focus.replay_tail_default", "5")
	assert.Error(t, err)
}

func TestGameSettingsImplementsGameSettingsInterface(t *testing.T) {
	var _ settings.GameSettings = settings.NewGameSettings(newMockSystemInfoStore())
}
