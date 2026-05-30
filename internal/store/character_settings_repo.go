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

// CharacterSettingsRepository persists per-character preference bags in the
// characters.preferences JSONB column. It is a narrow, whole-struct
// read-modify-write surface dedicated to the settings subsystem: it reads and
// writes only the preferences column, never the rest of the character row, so
// it does not couple the settings store to the world.Character model. It
// satisfies settings.CharacterRepository.
type CharacterSettingsRepository struct {
	pool *pgxpool.Pool
}

// NewCharacterSettingsRepository builds a character settings repository over
// pool.
func NewCharacterSettingsRepository(pool *pgxpool.Pool) *CharacterSettingsRepository {
	return &CharacterSettingsRepository{pool: pool}
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

// SetPreferences writes the whole preferences bag for a character into the
// preferences JSONB column, overwriting the prior value. The full struct is
// marshaled so typed partitions round-trip alongside the owner-partitioned
// Plugins bag (no field cherry-picking, no clobber).
func (r *CharacterSettingsRepository) SetPreferences(
	ctx context.Context, characterID ulid.ULID, prefs settings.CharacterPreferences,
) error {
	data, err := json.Marshal(prefs)
	if err != nil {
		return oops.With("character_id", characterID.String()).Wrapf(err, "encode character preferences")
	}

	const query = `UPDATE characters SET preferences = $1 WHERE id = $2`

	result, err := r.pool.Exec(ctx, query, data, characterID.String())
	if err != nil {
		return oops.With("character_id", characterID.String()).Wrapf(err, "persist character preferences")
	}
	if result.RowsAffected() == 0 {
		return oops.Code("CHARACTER_NOT_FOUND").
			With("character_id", characterID.String()).
			Errorf("character not found")
	}
	return nil
}

// Compile-time interface check.
var _ settings.CharacterRepository = (*CharacterSettingsRepository)(nil)
