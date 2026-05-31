// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/settings"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// memScopedStore is an in-memory PlayerSettingsStore / CharacterSettingsStore.
// Each principal keeps a single Scoped across For() calls so owner-partition
// writes round-trip within a test.
type memScopedStore struct{ byID map[string]settings.Scoped }

func newMemScopedStore() *memScopedStore {
	return &memScopedStore{byID: map[string]settings.Scoped{}}
}

func (m *memScopedStore) For(_ context.Context, id ulid.ULID) settings.Scoped {
	k := id.String()
	if m.byID[k] == nil {
		m.byID[k] = settings.NewScopedForTest(nil)
	}
	return m.byID[k]
}

func (m *memScopedStore) SetString(ctx context.Context, id ulid.ULID, key, value string) error {
	return m.For(ctx, id).Host().SetString(ctx, key, value)
}

var (
	_ settings.PlayerSettingsStore    = (*memScopedStore)(nil)
	_ settings.CharacterSettingsStore = (*memScopedStore)(nil)
)

func TestSettingsAdapterRoundTripsCharacterScopeBoundToPluginOwner(t *testing.T) {
	ctx := context.Background()
	store := newMemScopedStore()
	adapter := &settingsStoresOpsAdapter{
		player:    newMemScopedStore(),
		character: store,
		game:      settings.NewGameSettings(newMemSysInfo()),
	}
	charID := core.NewULID().String()

	require.NoError(t, adapter.SetSetting(ctx,
		pluginv1.SettingScope_SETTING_SCOPE_CHARACTER, "plug-A", charID,
		"content.cw_block", []string{"gore"}))

	values, found, err := adapter.GetSetting(ctx,
		pluginv1.SettingScope_SETTING_SCOPE_CHARACTER, "plug-A", charID, "content.cw_block")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"gore"}, values)
}

func TestSettingsAdapterIsolatesByOwnerPartition(t *testing.T) {
	ctx := context.Background()
	store := newMemScopedStore()
	adapter := &settingsStoresOpsAdapter{
		player:    newMemScopedStore(),
		character: store,
		game:      settings.NewGameSettings(newMemSysInfo()),
	}
	charID := core.NewULID().String()

	require.NoError(t, adapter.SetSetting(ctx,
		pluginv1.SettingScope_SETTING_SCOPE_CHARACTER, "plug-A", charID,
		"content.cw_block", []string{"gore"}))

	// A different plugin owner addresses a disjoint partition (INV-11).
	_, found, err := adapter.GetSetting(ctx,
		pluginv1.SettingScope_SETTING_SCOPE_CHARACTER, "plug-B", charID, "content.cw_block")
	require.NoError(t, err)
	assert.False(t, found, "owner partitions must be isolated by plugin name")
}

func TestSettingsAdapterGameScopeIgnoresPrincipal(t *testing.T) {
	ctx := context.Background()
	adapter := &settingsStoresOpsAdapter{
		player:    newMemScopedStore(),
		character: newMemScopedStore(),
		game:      settings.NewGameSettings(newMemSysInfo()),
	}

	require.NoError(t, adapter.SetSetting(ctx,
		pluginv1.SettingScope_SETTING_SCOPE_GAME, "plug-A", "",
		"content.global_cw", []string{"flashing"}))

	values, found, err := adapter.GetSetting(ctx,
		pluginv1.SettingScope_SETTING_SCOPE_GAME, "plug-A", "", "content.global_cw")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"flashing"}, values)
}

// memSysInfo is an in-memory settings.SystemInfoStore backing a game-scope store.
type memSysInfo struct{ data map[string]string }

func newMemSysInfo() *memSysInfo { return &memSysInfo{data: map[string]string{}} }

func (m *memSysInfo) GetSystemInfo(_ context.Context, key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", settings.ErrNotFound
	}
	return v, nil
}

func (m *memSysInfo) SetSystemInfo(_ context.Context, key, value string) error {
	m.data[key] = value
	return nil
}
