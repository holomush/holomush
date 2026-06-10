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

// Compile-time interface checks: NewGameSettings must satisfy both GameSettings and Scoped.
var (
	_ settings.GameSettings = settings.NewGameSettings((*mockSystemInfoStore)(nil))
	_ settings.Scoped       = settings.NewGameSettings((*mockSystemInfoStore)(nil))
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

func TestGameSettingsBoolNReturnsFalseWhenNotFound(t *testing.T) {
	ctx := context.Background()
	gs := settings.NewGameSettings(newMockSystemInfoStore())

	_, ok := gs.BoolN(ctx, "core.maintenance_mode")
	assert.False(t, ok)
}

func TestGameSettingsDurationNReturnsFalseWhenNotFound(t *testing.T) {
	ctx := context.Background()
	gs := settings.NewGameSettings(newMockSystemInfoStore())

	_, ok := gs.DurationN(ctx, "core.session_timeout")
	assert.False(t, ok)
}

// --- SystemInfoAdapter tests ---

func TestSystemInfoAdapterMapsNotFoundToSettingsErrNotFound(t *testing.T) {
	ctx := context.Background()
	storeErr := errors.New("store: not found")
	adapter := &settings.SystemInfoAdapter{
		Store:       &foreignNotFoundStore{sentinel: storeErr},
		NotFoundErr: storeErr,
	}

	_, err := adapter.GetSystemInfo(ctx, "anything")
	require.Error(t, err)
	assert.ErrorIs(t, err, settings.ErrNotFound)
}

func TestSystemInfoAdapterPassesThroughNonNotFoundErrors(t *testing.T) {
	ctx := context.Background()
	storeErr := errors.New("store: not found")
	otherErr := errors.New("connection refused")
	adapter := &settings.SystemInfoAdapter{
		Store:       &foreignNotFoundStore{sentinel: otherErr},
		NotFoundErr: storeErr,
	}

	_, err := adapter.GetSystemInfo(ctx, "anything")
	require.Error(t, err)
	assert.NotErrorIs(t, err, settings.ErrNotFound)
}

func TestSystemInfoAdapterReturnsValueOnSuccess(t *testing.T) {
	ctx := context.Background()
	inner := newMockSystemInfoStore()
	inner.data["core.name"] = "TestMUSH"
	adapter := &settings.SystemInfoAdapter{
		Store:       inner,
		NotFoundErr: settings.ErrNotFound,
	}

	v, err := adapter.GetSystemInfo(ctx, "core.name")
	require.NoError(t, err)
	assert.Equal(t, "TestMUSH", v)
}

func TestSystemInfoAdapterSetSystemInfoDelegatesToStore(t *testing.T) {
	ctx := context.Background()
	inner := newMockSystemInfoStore()
	adapter := &settings.SystemInfoAdapter{
		Store:       inner,
		NotFoundErr: settings.ErrNotFound,
	}

	err := adapter.SetSystemInfo(ctx, "core.name", "TestMUSH")
	require.NoError(t, err)
	assert.Equal(t, "TestMUSH", inner.data["core.name"])
}

func TestSystemInfoAdapterSetSystemInfoReturnsStoreError(t *testing.T) {
	ctx := context.Background()
	inner := newMockSystemInfoStore()
	inner.err = errors.New("write failed")
	adapter := &settings.SystemInfoAdapter{
		Store:       inner,
		NotFoundErr: settings.ErrNotFound,
	}

	err := adapter.SetSystemInfo(ctx, "core.name", "TestMUSH")
	assert.Error(t, err)
}

// foreignNotFoundStore is a SystemInfoStore that returns its sentinel error
// for all reads, simulating a store with its own not-found error type.
type foreignNotFoundStore struct {
	sentinel error
}

func (f *foreignNotFoundStore) GetSystemInfo(context.Context, string) (string, error) {
	return "", f.sentinel
}

func (f *foreignNotFoundStore) SetSystemInfo(context.Context, string, string) error {
	return nil
}

func TestGameSettingsStringSliceNDecodesJSONArrayString(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["scenes.focus.tags"] = `["a","b"]`
	gs := settings.NewGameSettings(store)

	v, ok := gs.StringSliceN(ctx, "scenes.focus.tags")
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, v)
}

func TestGameSettingsStringSliceNReturnsFalseForNonArrayString(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	store.data["scenes.focus.tags"] = "not-an-array"
	gs := settings.NewGameSettings(store)

	v, ok := gs.StringSliceN(ctx, "scenes.focus.tags")
	assert.False(t, ok)
	assert.Nil(t, v)
}

func TestGameSettingsStringSliceNReturnsFalseWhenNotFound(t *testing.T) {
	ctx := context.Background()
	gs := settings.NewGameSettings(newMockSystemInfoStore())

	v, ok := gs.StringSliceN(ctx, "scenes.focus.tags")
	assert.False(t, ok)
	assert.Nil(t, v)
}

// --- owner-partition (Scoped) tests ---

func TestGameSettingsHostSetStringSliceStoresJSONArrayString(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	gs := settings.NewGameSettings(store)

	err := gs.Host().SetStringSlice(ctx, "scenes.focus.tags", []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, `["a","b"]`, store.data["scenes.focus.tags"])

	v, ok := gs.StringSliceN(ctx, "scenes.focus.tags")
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, v)
}

func TestGameSettingsHostSetStringSliceRejectsInvalidNamespace(t *testing.T) {
	ctx := context.Background()
	gs := settings.NewGameSettings(newMockSystemInfoStore())

	err := gs.Host().SetStringSlice(ctx, "content.cw_taxonomy", []string{"a"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown namespace")
}

func TestGameSettingsOwnerStringSliceRoundTripsUnderPrefix(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	gs := settings.NewGameSettings(store)

	err := gs.Plugin("core-scenes").SetStringSlice(ctx, "content.cw_taxonomy", []string{"violence", "gore"})
	require.NoError(t, err)

	// Stored under the owner prefix, JSON-array-encoded.
	assert.Equal(t, `["violence","gore"]`, store.data["plugin/core-scenes/content.cw_taxonomy"])

	// Readable back via the same owner.
	v, ok := gs.Plugin("core-scenes").StringSliceN(ctx, "content.cw_taxonomy")
	assert.True(t, ok)
	assert.Equal(t, []string{"violence", "gore"}, v)
}

func TestGameSettingsOwnerSetStringRoundTripsUnderPrefix(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	gs := settings.NewGameSettings(store)

	err := gs.Plugin("core-scenes").SetString(ctx, "content.mode", "strict")
	require.NoError(t, err)
	assert.Equal(t, "strict", store.data["plugin/core-scenes/content.mode"])

	v, ok := gs.Plugin("core-scenes").StringN(ctx, "content.mode")
	assert.True(t, ok)
	assert.Equal(t, "strict", v)
}

func TestGameSettingsOwnerBypassesNamespaceValidation(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	gs := settings.NewGameSettings(store)

	// "content" is NOT a registered host namespace; an owner write must still
	// succeed because the plugin owns its keyspace.
	err := gs.Plugin("core-scenes").SetString(ctx, "content.cw_taxonomy", "x")
	require.NoError(t, err)
}

func TestGameSettingsOwnerPartitionIsInvisibleToHostAndOtherOwners(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	gs := settings.NewGameSettings(store)

	require.NoError(t,
		gs.Plugin("a").SetStringSlice(ctx, "content.cw_taxonomy", []string{"x"}))

	// Plugin("b") cannot see plugin a's key.
	_, ok := gs.Plugin("b").StringSliceN(ctx, "content.cw_taxonomy")
	assert.False(t, ok, "owner b must not see owner a's partition")

	// Host (bare reads) cannot see the owner-prefixed key either.
	_, ok = gs.StringSliceN(ctx, "content.cw_taxonomy")
	assert.False(t, ok, "host bare read must not see owner a's partition")

	// Host() Writable view also cannot see it.
	_, ok = gs.Host().StringSliceN(ctx, "content.cw_taxonomy")
	assert.False(t, ok, "host writable view must not see owner a's partition")
}

func TestGameSettingsHostWritablePersistsNamespaceValidatedKey(t *testing.T) {
	ctx := context.Background()
	store := newMockSystemInfoStore()
	gs := settings.NewGameSettings(store)

	err := gs.Host().SetString(ctx, "scenes.focus.mode", "bounded")
	require.NoError(t, err)
	assert.Equal(t, "bounded", store.data["scenes.focus.mode"])

	// Readable via bare reads too (same host keyspace).
	v, ok := gs.StringN(ctx, "scenes.focus.mode")
	assert.True(t, ok)
	assert.Equal(t, "bounded", v)
}

func TestGameSettingsHostWritableRejectsInvalidNamespace(t *testing.T) {
	ctx := context.Background()
	gs := settings.NewGameSettings(newMockSystemInfoStore())

	err := gs.Host().SetString(ctx, "content.cw_taxonomy", "x")
	assert.Error(t, err)

	err = gs.Host().SetString(ctx, "plugin.something", "x")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}
