// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/settings"
)

// Compile-time interface check: *RepoPlayerSettingsStore must satisfy settings.PlayerSettingsStore.
var _ settings.PlayerSettingsStore = settings.NewPlayerSettingsStore(newMockPlayerPrefsReader())

// TestRepoPlayerSettingsStoreSetStringIsUnsupported proves the store-level
// SetString on a repo-backed player store fails with an explicit error instead
// of panicking on a nil reader. Host-key writes are unsupported on the
// repo-backed store; plugin owner-partition writes go through For().Plugin().
func TestRepoPlayerSettingsStoreSetStringIsUnsupported(t *testing.T) {
	ctx := context.Background()
	// The repo is never dereferenced by SetString — the nil-reader guard
	// returns first — so a nil repo is sufficient to exercise the guard.
	store := settings.NewRepoPlayerSettingsStore(nil)
	assert.NotPanics(t, func() {
		err := store.SetString(ctx, ulid.Make(), "scenes.focus.mode", "bounded")
		assert.Error(t, err)
	})
}

// mockPlayerPrefsReader implements settings.PlayerPrefsReader for testing.
type mockPlayerPrefsReader struct {
	prefs map[ulid.ULID]json.RawMessage
	err   error
}

func newMockPlayerPrefsReader() *mockPlayerPrefsReader {
	return &mockPlayerPrefsReader{prefs: make(map[ulid.ULID]json.RawMessage)}
}

func (m *mockPlayerPrefsReader) GetPlayerPreferencesJSON(
	_ context.Context, playerID ulid.ULID,
) (json.RawMessage, error) {
	if m.err != nil {
		return nil, m.err
	}
	v, ok := m.prefs[playerID]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (m *mockPlayerPrefsReader) SetPlayerPreferenceKey(
	_ context.Context, playerID ulid.ULID, key, value string,
) error {
	if m.err != nil {
		return m.err
	}
	// Read existing prefs, update key, write back.
	raw := m.prefs[playerID]
	var data map[string]any
	if raw != nil {
		_ = json.Unmarshal(raw, &data)
	}
	if data == nil {
		data = make(map[string]any)
	}
	data[key] = value
	b, _ := json.Marshal(data)
	m.prefs[playerID] = b
	return nil
}

func TestPlayerSettingsStringNReturnsFalseWhenNoPreferencesExist(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	store := settings.NewPlayerSettingsStore(reader)
	pid := ulid.Make()

	s := store.For(ctx, pid)
	_, ok := s.StringN(ctx, "scenes.focus.replay_tail_default")
	assert.False(t, ok)
}

func TestPlayerSettingsStringNReturnsDotKeyedValue(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	// Store nested JSON matching dot-path "scenes.focus_replay_tail"
	reader.prefs[pid] = json.RawMessage(`{"scenes.focus.replay_tail_default":"7"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.StringN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, "7", v)
}

func TestPlayerSettingsIntNParsesStringValueAsInteger(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"scenes.focus.replay_tail_default":"5"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 5, v)
}

func TestPlayerSettingsIntNReturnsNativeJSONNumber(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"scenes.focus.replay_tail_default":5}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 5, v)
}

func TestPlayerSettingsBoolNParsesStringTrueValue(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"auth.auto_login":"true"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.BoolN(ctx, "auth.auto_login")
	assert.True(t, ok)
	assert.True(t, v)
}

func TestPlayerSettingsBoolNReturnsNativeJSONBool(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"auth.auto_login":true}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.BoolN(ctx, "auth.auto_login")
	assert.True(t, ok)
	assert.True(t, v)
}

func TestPlayerSettingsStringNReturnsFalseOnDatabaseReadError(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	reader.err = errors.New("db down")
	store := settings.NewPlayerSettingsStore(reader)
	pid := ulid.Make()

	s := store.For(ctx, pid)
	_, ok := s.StringN(ctx, "scenes.focus.mode")
	assert.False(t, ok)
}

func TestPlayerSettingsSetStringPersistsValidKey(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	store := settings.NewPlayerSettingsStore(reader)
	pid := ulid.Make()

	err := store.SetString(ctx, pid, "scenes.focus.replay_tail_default", "8")
	assert.NoError(t, err)

	// Read the value back and assert it was persisted.
	s := store.For(ctx, pid)
	val, ok := s.StringN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok, "value should be readable after SetString")
	assert.Equal(t, "8", val)
}

func TestPlayerSettingsSetStringRejectsInvalidNamespace(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	store := settings.NewPlayerSettingsStore(reader)
	pid := ulid.Make()

	err := store.SetString(ctx, pid, "bogus.key", "val")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown namespace")
}

func TestPlayerSettingsStringNReturnsFalseOnInvalidJSON(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`not valid json`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.StringN(ctx, "scenes.focus.mode")
	assert.False(t, ok)
}

func TestPlayerSettingsSetStringReturnsErrorOnWriteFailure(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	store := settings.NewPlayerSettingsStore(reader)
	pid := ulid.Make()

	// First set succeeds (no error configured).
	err := store.SetString(ctx, pid, "scenes.focus.mode", "bounded")
	assert.NoError(t, err)

	// Now configure a write error.
	reader.err = errors.New("disk full")
	err = store.SetString(ctx, pid, "scenes.focus.mode", "bounded")
	assert.Error(t, err)
}

func TestPlayerSettingsStringNReturnsRawJSONWhenValueIsNotString(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	// Store a raw numeric value — json.Unmarshal into string will fail,
	// triggering the fallback to string(raw).
	reader.prefs[pid] = json.RawMessage(`{"core.count":42}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.StringN(ctx, "core.count")
	assert.True(t, ok)
	assert.Equal(t, "42", v)
}

func TestPlayerSettingsIntNReturnsFalseForMissingKey(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"other.key":"hello"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.IntN(ctx, "scenes.focus.count")
	assert.False(t, ok)
}

func TestPlayerSettingsIntNReturnsFalseForUnparseableString(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"scenes.focus.count":"not-a-number"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.IntN(ctx, "scenes.focus.count")
	assert.False(t, ok)
}

func TestPlayerSettingsBoolNReturnsFalseForMissingKey(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"other.key":"hello"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.BoolN(ctx, "auth.auto_login")
	assert.False(t, ok)
}

func TestPlayerSettingsBoolNReturnsFalseForUnparseableString(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"auth.auto_login":"maybe"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.BoolN(ctx, "auth.auto_login")
	assert.False(t, ok)
}

func TestPlayerSettingsDurationNParsesValidDurationString(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"core.session_timeout":"30s"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.DurationN(ctx, "core.session_timeout")
	assert.True(t, ok)
	assert.Equal(t, 30*time.Second, v)
}

func TestPlayerSettingsDurationNReturnsFalseForMissingKey(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"other.key":"hello"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.DurationN(ctx, "core.session_timeout")
	assert.False(t, ok)
}

func TestPlayerSettingsDurationNReturnsFalseForInvalidDuration(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"core.session_timeout":"not-a-duration"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.DurationN(ctx, "core.session_timeout")
	assert.False(t, ok)
}

func TestPlayerSettingsStringNReturnsFalseForUnknownNamespace(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"bogus.key":"value"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.StringN(ctx, "bogus.key")
	assert.False(t, ok)
}

func TestPlayerSettingsIntNReturnsFalseForFractionalJSONNumber(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"scenes.focus.count":3.7}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	_, ok := s.IntN(ctx, "scenes.focus.count")
	assert.False(t, ok)
}

func TestPlayerSettingsStringSliceNReturnsNativeJSONArray(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"scenes.focus.tags":["a","b"]}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.StringSliceN(ctx, "scenes.focus.tags")
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, v)
}

func TestPlayerSettingsStringSliceNReturnsFalseForScalarValue(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"scenes.focus.tags":"hello"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.StringSliceN(ctx, "scenes.focus.tags")
	assert.False(t, ok)
	assert.Nil(t, v)
}

func TestPlayerSettingsStringSliceNReturnsFalseForMissingKey(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"other.key":"hello"}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.StringSliceN(ctx, "scenes.focus.tags")
	assert.False(t, ok)
	assert.Nil(t, v)
}

func TestPlayerSettingsStringSliceNReturnsFalseForUnknownNamespace(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	pid := ulid.Make()
	reader.prefs[pid] = json.RawMessage(`{"bogus.key":["a","b"]}`)
	store := settings.NewPlayerSettingsStore(reader)

	s := store.For(ctx, pid)
	v, ok := s.StringSliceN(ctx, "bogus.key")
	assert.False(t, ok)
	assert.Nil(t, v)
}
