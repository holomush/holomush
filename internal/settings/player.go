// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// PlayerPrefsReader is the narrow interface for reading and writing player
// preferences as raw JSON. Satisfied by a thin adapter around
// auth.PlayerRepository.
type PlayerPrefsReader interface {
	GetPlayerPreferencesJSON(ctx context.Context, playerID ulid.ULID) (json.RawMessage, error)
	SetPlayerPreferenceKey(ctx context.Context, playerID ulid.ULID, key, value string) error
}

// playerSettingsStore implements PlayerSettingsStore.
type playerSettingsStore struct {
	reader PlayerPrefsReader
}

// NewPlayerSettingsStore creates a PlayerSettingsStore backed by a
// PlayerPrefsReader.
func NewPlayerSettingsStore(reader PlayerPrefsReader) PlayerSettingsStore {
	return &playerSettingsStore{reader: reader}
}

// For returns a read-only Settings view for a specific player. The
// returned Settings reads the player's preferences JSONB as a flat
// dot-keyed map (keys like "scenes.focus.replay_tail_default").
func (s *playerSettingsStore) For(ctx context.Context, playerID ulid.ULID) Settings {
	raw, err := s.reader.GetPlayerPreferencesJSON(ctx, playerID)
	if err != nil {
		slog.DebugContext(ctx, "player settings read failed",
			"player_id", playerID.String(), "error", err)
		return &emptySettings{}
	}
	if raw == nil {
		return &emptySettings{}
	}

	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		slog.DebugContext(ctx, "player settings JSON unmarshal failed",
			"player_id", playerID.String(), "error", err)
		return &emptySettings{}
	}
	return &jsonMapSettings{data: data}
}

// SetString writes a single preference key for a player.
func (s *playerSettingsStore) SetString(
	ctx context.Context, playerID ulid.ULID, key, value string,
) error {
	if err := ValidateNamespace(key); err != nil {
		return oops.With("key", key).Wrap(err)
	}
	return s.reader.SetPlayerPreferenceKey(ctx, playerID, key, value)
}

// jsonMapSettings implements Settings backed by a flat JSON map. Values may
// be JSON strings, numbers, or bools. String accessor returns the raw
// JSON-decoded string; int/bool/duration parse from the string
// representation.
type jsonMapSettings struct {
	data map[string]json.RawMessage
}

func (j *jsonMapSettings) StringN(_ context.Context, key string) (string, bool) {
	raw, ok := j.data[key]
	if !ok {
		return "", false
	}
	// Try to unmarshal as a JSON string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	// Fall back to raw string (for numeric/bool JSON values).
	return string(raw), true
}

func (j *jsonMapSettings) IntN(ctx context.Context, key string) (int, bool) {
	raw, ok := j.data[key]
	if !ok {
		return 0, false
	}
	// Try native JSON number first.
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return int(n), true
	}
	// Fall back to string parse.
	s, ok := j.StringN(ctx, key)
	if !ok {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

func (j *jsonMapSettings) BoolN(ctx context.Context, key string) (bool, bool) {
	raw, ok := j.data[key]
	if !ok {
		return false, false
	}
	// Try native JSON bool first.
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, true
	}
	// Fall back to string parse.
	s, ok := j.StringN(ctx, key)
	if !ok {
		return false, false
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, false
	}
	return v, true
}

func (j *jsonMapSettings) DurationN(ctx context.Context, key string) (time.Duration, bool) {
	s, ok := j.StringN(ctx, key)
	if !ok {
		return 0, false
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

// emptySettings always returns (zero, false) for all reads.
type emptySettings struct{}

func (e *emptySettings) StringN(context.Context, string) (string, bool)           { return "", false }
func (e *emptySettings) IntN(context.Context, string) (int, bool)                 { return 0, false }
func (e *emptySettings) BoolN(context.Context, string) (bool, bool)               { return false, false }
func (e *emptySettings) DurationN(context.Context, string) (time.Duration, bool)  { return 0, false }

// Compile-time interface checks.
var (
	_ Settings = (*jsonMapSettings)(nil)
	_ Settings = (*emptySettings)(nil)
)
