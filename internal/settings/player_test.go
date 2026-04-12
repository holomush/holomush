// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/settings"
)

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

func TestPlayerSettingsForReturnsNilWhenNoPreferences(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	store := settings.NewPlayerSettingsStore(reader)
	pid := ulid.Make()

	s := store.For(ctx, pid)
	_, ok := s.StringN(ctx, "scenes.focus.replay_tail_default")
	assert.False(t, ok)
}

func TestPlayerSettingsForReadsNestedDotKey(t *testing.T) {
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

func TestPlayerSettingsIntNParsesStoredString(t *testing.T) {
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

func TestPlayerSettingsIntNHandlesNumericJSON(t *testing.T) {
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

func TestPlayerSettingsBoolNParsesStoredValue(t *testing.T) {
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

func TestPlayerSettingsBoolNHandsNativeBoolJSON(t *testing.T) {
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

func TestPlayerSettingsForReturnsFalseOnReadError(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	reader.err = errors.New("db down")
	store := settings.NewPlayerSettingsStore(reader)
	pid := ulid.Make()

	s := store.For(ctx, pid)
	_, ok := s.StringN(ctx, "scenes.focus.mode")
	assert.False(t, ok)
}

func TestPlayerSettingsSetStringWritesKey(t *testing.T) {
	ctx := context.Background()
	reader := newMockPlayerPrefsReader()
	store := settings.NewPlayerSettingsStore(reader)
	pid := ulid.Make()

	err := store.SetString(ctx, pid, "scenes.focus.replay_tail_default", "8")
	assert.NoError(t, err)
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

func TestPlayerSettingsStoreImplementsInterface(_ *testing.T) {
	var _ settings.PlayerSettingsStore = settings.NewPlayerSettingsStore(newMockPlayerPrefsReader()) //nolint:staticcheck // intentional interface check
}
