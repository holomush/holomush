// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// CharacterRepository implements world.CharacterRepository using PostgreSQL.
type CharacterRepository struct {
	pool *pgxpool.Pool
}

// NewCharacterRepository creates a new PostgreSQL character repository.
func NewCharacterRepository(pool *pgxpool.Pool) *CharacterRepository {
	return &CharacterRepository{pool: pool}
}

// Get retrieves a character by ID.
func (r *CharacterRepository) Get(ctx context.Context, id ulid.ULID) (*world.Character, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, player_id, name, description, location_id, created_at
		FROM characters WHERE id = $1
	`, id.String())
	char, err := scanCharacterRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("CHARACTER_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("CHARACTER_GET_FAILED").With("id", id.String()).Wrap(err)
	}
	return char, nil
}

// Create persists a new character.
// Callers must validate the character before calling this method.
func (r *CharacterRepository) Create(ctx context.Context, char *world.Character) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, description, location_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, char.ID.String(), char.PlayerID.String(), char.Name, char.Description,
		ulidToStringPtr(char.LocationID), char.CreatedAt)
	if err != nil {
		return oops.Code("CHARACTER_CREATE_FAILED").With("id", char.ID.String()).Wrap(err)
	}
	return nil
}

// Update modifies an existing character.
// Callers must validate the character before calling this method.
func (r *CharacterRepository) Update(ctx context.Context, char *world.Character) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE characters SET name = $2, description = $3, location_id = $4
		WHERE id = $1
	`, char.ID.String(), char.Name, char.Description, ulidToStringPtr(char.LocationID))
	if err != nil {
		return oops.Code("CHARACTER_UPDATE_FAILED").With("id", char.ID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("CHARACTER_NOT_FOUND").With("id", char.ID.String()).Wrap(world.ErrNotFound)
	}
	return nil
}

// Delete removes a character by ID.
func (r *CharacterRepository) Delete(ctx context.Context, id ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, id.String())
	if err != nil {
		return oops.Code("CHARACTER_DELETE_FAILED").With("id", id.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("CHARACTER_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	return nil
}

// GetByLocation retrieves characters at a location with pagination.
func (r *CharacterRepository) GetByLocation(ctx context.Context, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = world.DefaultLimit
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, player_id, name, description, location_id, created_at
		FROM characters WHERE location_id = $1
		ORDER BY name
		LIMIT $2 OFFSET $3
	`, locationID.String(), limit, opts.Offset)
	if err != nil {
		return nil, oops.Code("CHARACTER_QUERY_FAILED").With("location_id", locationID.String()).Wrap(err)
	}
	defer rows.Close()

	return scanCharacters(rows)
}

// UpdateLocation moves a character to a new location.
func (r *CharacterRepository) UpdateLocation(ctx context.Context, characterID ulid.ULID, locationID *ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE characters SET location_id = $2 WHERE id = $1
	`, characterID.String(), ulidToStringPtr(locationID))
	if err != nil {
		return oops.Code("CHARACTER_MOVE_FAILED").With("character_id", characterID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("CHARACTER_NOT_FOUND").With("character_id", characterID.String()).Wrap(world.ErrNotFound)
	}
	return nil
}

// characterScanFields holds intermediate scan values for character parsing.
type characterScanFields struct {
	idStr         string
	playerIDStr   string
	locationIDStr *string
}

// scanCharacterRow scans a single character from a row.
func scanCharacterRow(row pgx.Row) (*world.Character, error) {
	var char world.Character
	var f characterScanFields

	err := row.Scan(
		&f.idStr, &f.playerIDStr, &char.Name, &char.Description,
		&f.locationIDStr, &char.CreatedAt,
	)
	if err != nil {
		return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(err)
	}

	if err := parseCharacterFromFields(&f, &char); err != nil {
		return nil, err
	}

	return &char, nil
}

// parseCharacterFromFields converts scan fields to character fields.
func parseCharacterFromFields(f *characterScanFields, char *world.Character) error {
	var err error
	char.ID, err = ulid.Parse(f.idStr)
	if err != nil {
		return oops.Code("CHARACTER_PARSE_FAILED").With("field", "id").With("value", f.idStr).Wrap(err)
	}
	char.PlayerID, err = ulid.Parse(f.playerIDStr)
	if err != nil {
		return oops.Code("CHARACTER_PARSE_FAILED").With("field", "player_id").With("value", f.playerIDStr).Wrap(err)
	}
	char.LocationID, err = parseOptionalULID(f.locationIDStr, "location_id")
	if err != nil {
		return err
	}
	return nil
}

func scanCharacters(rows pgx.Rows) ([]*world.Character, error) {
	characters := make([]*world.Character, 0)
	for rows.Next() {
		var char world.Character
		var f characterScanFields

		if err := rows.Scan(
			&f.idStr, &f.playerIDStr, &char.Name, &char.Description,
			&f.locationIDStr, &char.CreatedAt,
		); err != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(err)
		}

		if err := parseCharacterFromFields(&f, &char); err != nil {
			return nil, err
		}

		characters = append(characters, &char)
	}

	if err := rows.Err(); err != nil {
		return nil, oops.Code("CHARACTER_ITERATE_FAILED").Wrap(err)
	}

	return characters, nil
}

// Compile-time interface check.
var _ world.CharacterRepository = (*CharacterRepository)(nil)
