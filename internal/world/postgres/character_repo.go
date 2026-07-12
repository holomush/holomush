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

	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
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
		SELECT id, player_id, name, description, location_id, created_at, version
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
// Uses querierFromCtx so callers may compose this within a transaction; the
// struct's Version is refreshed to the DB-assigned initial version (1) so a
// reused struct does not later carry a stale version and spuriously conflict
// (finding 12).
func (r *CharacterRepository) Create(ctx context.Context, char *world.Character) (*wmodel.MutationDelta, error) {
	var newVersion int
	err := querierFromCtx(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO characters (id, player_id, name, description, location_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING version
	`, char.ID.String(), char.PlayerID.String(), char.Name, char.Description,
		ulidToStringPtr(char.LocationID), pgnanos.From(char.CreatedAt)).Scan(&newVersion)
	if err != nil {
		return nil, oops.Code("CHARACTER_CREATE_FAILED").With("id", char.ID.String()).Wrap(err)
	}
	char.Version = newVersion
	return primaryDeltaVersioned(wmodel.AggregateCharacter, char.ID, false, 0, newVersion), nil
}

// Update modifies an existing character with a version-predicated CAS (MODEL-03).
// Callers must validate the character before calling this method.
//
// When char.Version > 0 the UPDATE's WHERE clause matches both id and version, so
// a stale writer affects zero rows; a locked follow-up read on the same connection
// then classifies the zero-row result into WORLD_CONCURRENT_EDIT (the row exists
// with a different version) or CHARACTER_NOT_FOUND (the row is absent). When
// char.Version == 0 the write is unversioned (id-only) for callers that have not
// yet threaded a read version. On success char.Version is refreshed to the
// committed value (finding 12).
func (r *CharacterRepository) Update(ctx context.Context, char *world.Character) (*wmodel.MutationDelta, error) {
	query := `
		UPDATE characters SET name = $2, description = $3, location_id = $4, version = version + 1
		WHERE id = $1`
	args := []any{char.ID.String(), char.Name, char.Description, ulidToStringPtr(char.LocationID)}
	if char.Version > 0 {
		query += ` AND version = $5`
		args = append(args, char.Version)
	}
	query += ` RETURNING version`

	var delta *wmodel.MutationDelta
	txErr := withTx(ctx, r.pool, func(txCtx context.Context) error {
		tx := txFromContext(txCtx)
		var newVersion int
		err := tx.QueryRow(txCtx, query, args...).Scan(&newVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return classifyCASZeroRow(txCtx, tx,
				`SELECT version FROM characters WHERE id = $1 FOR UPDATE`,
				char.ID,
				oops.Code("CHARACTER_NOT_FOUND").With("id", char.ID.String()).Wrap(world.ErrNotFound))
		}
		if err != nil {
			return oops.Code("CHARACTER_UPDATE_FAILED").With("id", char.ID.String()).Wrap(err)
		}
		char.Version = newVersion
		delta = primaryDeltaVersioned(wmodel.AggregateCharacter, char.ID, false, newVersion-1, newVersion)
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return delta, nil
}

// Delete removes a character by ID with a version-predicated CAS (MODEL-03).
//
// Inside the same transaction the method locks the character row with
// SELECT version ... FOR UPDATE (existence check + version read), enforces the
// optimistic-concurrency expectation, then deletes. The classifier is TWO outcomes
// only (round-5 Codex MEDIUM): an absent row → CHARACTER_NOT_FOUND (a concurrent
// delete that already committed is correctly observed as not-found); an existing
// row whose version differs from a non-zero expectedVersion → WORLD_CONCURRENT_EDIT.
// expectedVersion == 0 is an unversioned delete (existence-checked only).
func (r *CharacterRepository) Delete(ctx context.Context, id ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error) {
	var delta *wmodel.MutationDelta
	txErr := withTx(ctx, r.pool, func(txCtx context.Context) error {
		tx := txFromContext(txCtx)

		var currentVersion int
		err := tx.QueryRow(txCtx, `SELECT version FROM characters WHERE id = $1 FOR UPDATE`, id.String()).Scan(&currentVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("CHARACTER_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
		}
		if err != nil {
			return oops.With("operation", "lock character for delete").With("id", id.String()).Wrap(err)
		}
		if expectedVersion > 0 && currentVersion != expectedVersion {
			return oops.Code(world.CodeConcurrentEdit).
				With("id", id.String()).
				With("expected_version", expectedVersion).
				With("current_version", currentVersion).
				Wrap(world.ErrConcurrentEdit)
		}

		if _, err := tx.Exec(txCtx, `DELETE FROM characters WHERE id = $1`, id.String()); err != nil {
			return oops.Code("CHARACTER_DELETE_FAILED").With("id", id.String()).Wrap(err)
		}

		delta = primaryDeltaVersioned(wmodel.AggregateCharacter, id, true, currentVersion, 0)
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return delta, nil
}

// GetByLocation retrieves characters at a location with pagination.
func (r *CharacterRepository) GetByLocation(ctx context.Context, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = world.DefaultLimit
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, player_id, name, description, location_id, created_at, version
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

// ListByPlayer returns every character owned by the given player, ordered by name,
// with each Character.Version populated from the row's version column (round-6
// R6-1). This is the canonical in-boundary version-bearing list the 05-16 guest
// reaper lists through, so its CAS Delete(ctx, id, char.Version) matches the
// stored version rather than conflicting on a zero. A READ, so r.pool.Query is
// correct — the SQL fence only fences mutations.
func (r *CharacterRepository) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, player_id, name, description, location_id, created_at, version
		FROM characters WHERE player_id = $1 ORDER BY name
	`, playerID.String())
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	defer rows.Close()

	return scanCharacters(rows)
}

// UpdateLocation moves a character to a new location with a version-predicated CAS
// (MODEL-03). When expectedVersion > 0 the UPDATE matches id + version, so a stale
// move affects zero rows and the locked follow-up read classifies it into
// WORLD_CONCURRENT_EDIT (existing row, moved version) or CHARACTER_NOT_FOUND
// (absent). expectedVersion == 0 is an unversioned move. The expected version is
// threaded from the calling command in service.go (05-11 MoveCharacter rollout).
func (r *CharacterRepository) UpdateLocation(ctx context.Context, characterID ulid.ULID, locationID *ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error) {
	query := `UPDATE characters SET location_id = $2, version = version + 1 WHERE id = $1`
	args := []any{characterID.String(), ulidToStringPtr(locationID)}
	if expectedVersion > 0 {
		query += ` AND version = $3`
		args = append(args, expectedVersion)
	}
	query += ` RETURNING version`

	var delta *wmodel.MutationDelta
	txErr := withTx(ctx, r.pool, func(txCtx context.Context) error {
		tx := txFromContext(txCtx)
		var newVersion int
		err := tx.QueryRow(txCtx, query, args...).Scan(&newVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return classifyCASZeroRow(txCtx, tx,
				`SELECT version FROM characters WHERE id = $1 FOR UPDATE`,
				characterID,
				oops.Code("CHARACTER_NOT_FOUND").With("character_id", characterID.String()).Wrap(world.ErrNotFound))
		}
		if err != nil {
			return oops.Code("CHARACTER_MOVE_FAILED").With("character_id", characterID.String()).Wrap(err)
		}
		delta = primaryDeltaVersioned(wmodel.AggregateCharacter, characterID, false, newVersion-1, newVersion)
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return delta, nil
}

// IsOwnedByPlayer checks if a character is owned by a specific player.
// Returns false (not an error) if the character does not exist.
func (r *CharacterRepository) IsOwnedByPlayer(ctx context.Context, characterID, playerID ulid.ULID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM characters WHERE id = $1 AND player_id = $2)
	`, characterID.String(), playerID.String()).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_OWNERSHIP_CHECK_FAILED").
			With("character_id", characterID.String()).
			With("player_id", playerID.String()).
			Wrap(err)
	}
	return exists, nil
}

// characterScanFields holds intermediate scan values for character parsing.
type characterScanFields struct {
	idStr         string
	playerIDStr   string
	locationIDStr *string
	createdAt     pgnanos.Time
}

// scanCharacterRow scans a single character from a row.
func scanCharacterRow(row pgx.Row) (*world.Character, error) {
	var char world.Character
	var f characterScanFields

	err := row.Scan(
		&f.idStr, &f.playerIDStr, &char.Name, &char.Description,
		&f.locationIDStr, &f.createdAt, &char.Version,
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
	char.CreatedAt = f.createdAt.Time()
	return nil
}

func scanCharacters(rows pgx.Rows) ([]*world.Character, error) {
	characters := make([]*world.Character, 0)
	for rows.Next() {
		var char world.Character
		var f characterScanFields

		if err := rows.Scan(
			&f.idStr, &f.playerIDStr, &char.Name, &char.Description,
			&f.locationIDStr, &f.createdAt, &char.Version,
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

// GetNamesByIDs returns a map[id]name for the given character IDs.
// Missing IDs are absent from the result (not an error).
func (r *CharacterRepository) GetNamesByIDs(ctx context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error) {
	if len(ids) == 0 {
		return map[ulid.ULID]string{}, nil
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	rows, err := r.pool.Query(ctx, `SELECT id, name FROM characters WHERE id = ANY($1)`, strs)
	if err != nil {
		return nil, oops.With("operation", "get_names_by_ids").
			With("count", len(ids)).Wrap(err)
	}
	defer rows.Close()

	out := make(map[ulid.ULID]string, len(ids))
	for rows.Next() {
		var idStr, name string
		if err := rows.Scan(&idStr, &name); err != nil {
			return nil, oops.With("operation", "scan_get_names").Wrap(err)
		}
		id, perr := ulid.Parse(idStr)
		if perr != nil {
			return nil, oops.With("operation", "parse_ulid").With("id_str", idStr).Wrap(perr)
		}
		out[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "rows_err_get_names").Wrap(err)
	}
	return out, nil
}

// ListAll returns every character ordered by name ascending (id + name only —
// directory surface; other columns are left zero). Fetch-all: no LIMIT/OFFSET.
func (r *CharacterRepository) ListAll(ctx context.Context) ([]*world.Character, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name FROM characters ORDER BY name ASC`)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_ALL_FAILED").Wrap(err)
	}
	defer rows.Close()

	out := make([]*world.Character, 0)
	for rows.Next() {
		var idStr, name string
		if err := rows.Scan(&idStr, &name); err != nil {
			return nil, oops.Code("CHARACTER_LIST_ALL_SCAN_FAILED").Wrap(err)
		}
		id, err := ulid.Parse(idStr)
		if err != nil {
			return nil, oops.Code("CHARACTER_PARSE_FAILED").With("field", "id").With("value", idStr).Wrap(err)
		}
		out = append(out, &world.Character{ID: id, Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("CHARACTER_ITERATE_FAILED").Wrap(err)
	}
	return out, nil
}

// Compile-time interface check.
var _ world.CharacterRepository = (*CharacterRepository)(nil)
