// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/settings"
)

// CharacterPreferencesUpdater is the world-boundary WRITE port the character
// settings repository routes preference mutations through (round-4 C5 / D-05).
// Its production implementation is *world.Service.UpdateCharacterPreferences,
// which performs the version-guarded characters-table preferences write inside
// the sanctioned internal/world/postgres writer boundary and emits exactly one
// character_preferences_update envelope in the same transaction. A conflicting
// concurrent write surfaces the typed WORLD_CONCURRENT_EDIT (MODEL-03; D-02 — no
// auto-retry). Declared as a narrow port here so internal/store does NOT import
// internal/world.
type CharacterPreferencesUpdater interface {
	UpdateCharacterPreferences(ctx context.Context, characterID ulid.ULID, prefs []byte) error
}

// CharacterSettingsRepository persists per-character preference bags in the
// characters.preferences JSONB column. It is a narrow, whole-struct surface
// dedicated to the settings subsystem: it READS the preferences column directly,
// and it ROUTES WRITES through the world boundary (round-4 C5 / D-05) so the
// characters-table mutation is version-guarded and enveloped — it no longer
// issues a raw UPDATE. It satisfies settings.CharacterRepository.
type CharacterSettingsRepository struct {
	pool   *pgxpool.Pool
	writer CharacterPreferencesUpdater
}

// NewCharacterSettingsRepository builds a character settings repository. pool
// backs the direct preferences READ; writer is the world-boundary write port the
// preferences mutation routes through (round-4 C5 / D-05) — the raw store UPDATE
// is gone.
func NewCharacterSettingsRepository(pool *pgxpool.Pool, writer CharacterPreferencesUpdater) *CharacterSettingsRepository {
	return &CharacterSettingsRepository{pool: pool, writer: writer}
}

// GetPreferences loads the whole preferences bag for a character. A missing
// character row yields a zero CharacterPreferences and a nil error so the
// settings store treats an unprovisioned character as having empty settings
// (matching the read-never-errors contract callers rely on). A NULL/empty
// column likewise decodes to a zero bag.
func (r *CharacterSettingsRepository) GetPreferences(
	ctx context.Context, characterID ulid.ULID,
) (settings.CharacterPreferences, error) {
	const query = `SELECT preferences FROM characters WHERE id = $1`

	var raw []byte
	err := r.pool.QueryRow(ctx, query, characterID.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings.CharacterPreferences{}, nil
	}
	if err != nil {
		return settings.CharacterPreferences{}, oops.
			With("character_id", characterID.String()).
			Wrapf(err, "query character preferences")
	}
	if len(raw) == 0 {
		return settings.CharacterPreferences{}, nil
	}

	var prefs settings.CharacterPreferences
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return settings.CharacterPreferences{}, oops.
			With("character_id", characterID.String()).
			Wrapf(err, "decode character preferences")
	}
	return prefs, nil
}

// SetPreferences writes the whole preferences bag for a character, routing the
// mutation through the world boundary (round-4 C5 / D-05) instead of a raw
// characters-table write. The full struct is marshaled (so typed partitions
// round-trip alongside the plugin-partitioned Plugins bag — no field
// cherry-picking, no clobber) and handed to the world-boundary writer, which
// version-guards the write and emits one character_preferences_update envelope in
// the same transaction. A missing character surfaces CHARACTER_NOT_FOUND and a
// conflicting concurrent write surfaces WORLD_CONCURRENT_EDIT (MODEL-03), both
// propagated to the settings caller unchanged (D-02 — no auto-retry).
func (r *CharacterSettingsRepository) SetPreferences(
	ctx context.Context, characterID ulid.ULID, prefs settings.CharacterPreferences,
) error {
	data, err := json.Marshal(prefs)
	if err != nil {
		return oops.With("character_id", characterID.String()).Wrapf(err, "encode character preferences")
	}
	if err := r.writer.UpdateCharacterPreferences(ctx, characterID, data); err != nil {
		return oops.With("character_id", characterID.String()).Wrap(err)
	}
	return nil
}

// Compile-time interface check.
var _ settings.CharacterRepository = (*CharacterSettingsRepository)(nil)
