// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// Compile-time checks.
var (
	_ auth.CharacterRepository = (*CharRepoAdapter)(nil)
	_ auth.LocationRepository  = (*LocRepoAdapter)(nil)
)

// CharRepoAdapter wraps a pgxpool.Pool to implement auth.CharacterRepository.
// It delegates Create to worldpostgres and adds auth-specific queries.
type CharRepoAdapter struct {
	pool     *pgxpool.Pool
	charRepo *worldpostgres.CharacterRepository
}

// NewCharRepoAdapter constructs a CharRepoAdapter using the provided PostgreSQL pool and character repository.
func NewCharRepoAdapter(pool *pgxpool.Pool, charRepo *worldpostgres.CharacterRepository) *CharRepoAdapter {
	return &CharRepoAdapter{pool: pool, charRepo: charRepo}
}

// Create persists a new character using the underlying world repository.
// The world repository now returns a *wmodel.MutationDelta; this adapter is a
// documented wave-1 compatibility bridge (05-14) that discards the delta so the
// auth.CharacterRepository interface (Create(...) error) stays unchanged. 05-15
// removes Create from the auth-side interfaces entirely.
func (a *CharRepoAdapter) Create(ctx context.Context, char *world.Character) error {
	if _, err := a.charRepo.Create(ctx, char); err != nil {
		return oops.Code("CHARACTER_CREATE_FAILED").Wrap(err)
	}
	return nil
}

// ExistsByName reports whether a character with the given name already exists (case-insensitive).
func (a *CharRepoAdapter) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := a.pool.QueryRow(
		ctx,
		"SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))",
		name,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", name).Wrap(err)
	}
	return exists, nil
}

// CountByPlayer returns the number of characters owned by the given player.
func (a *CharRepoAdapter) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	var count int
	err := a.pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM characters WHERE player_id = $1",
		playerID.String(),
	).Scan(&count)
	if err != nil {
		return 0, oops.Code("CHARACTER_COUNT_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return count, nil
}

// ListByPlayer returns all characters owned by the given player, ordered by name.
func (a *CharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	rows, err := a.pool.Query(
		ctx,
		`SELECT id, player_id, name, description, location_id, created_at
         FROM characters WHERE player_id = $1 ORDER BY name`,
		playerID.String(),
	)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	defer rows.Close()

	var chars []*world.Character
	for rows.Next() {
		var c world.Character
		var idStr, playerIDStr string
		var locIDStr *string
		var createdAt pgnanos.Time
		scanErr := rows.Scan(&idStr, &playerIDStr, &c.Name, &c.Description, &locIDStr, &createdAt)
		if scanErr != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(scanErr)
		}
		c.CreatedAt = createdAt.Time()
		var parseErr error
		c.ID, parseErr = ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_PARSE_FAILED").With("field", "id").With("value", idStr).Wrap(parseErr)
		}
		c.PlayerID, parseErr = ulid.Parse(playerIDStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_PARSE_FAILED").With("field", "player_id").With("value", playerIDStr).Wrap(parseErr)
		}
		if locIDStr != nil {
			lid, lidErr := ulid.Parse(*locIDStr)
			if lidErr != nil {
				return nil, oops.Code("CHARACTER_PARSE_FAILED").With("field", "location_id").With("value", *locIDStr).Wrap(lidErr)
			}
			c.LocationID = &lid
		}
		chars = append(chars, &c)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, oops.Code("CHARACTER_ITERATE_FAILED").Wrap(rowsErr)
	}
	return chars, nil
}

// ListAll returns every character ordered by name ascending (id + name only).
// Delegates to the underlying world repository.
func (a *CharRepoAdapter) ListAll(ctx context.Context) ([]*world.Character, error) {
	chars, err := a.charRepo.ListAll(ctx)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_ALL_FAILED").Wrap(err)
	}
	return chars, nil
}

// LocRepoAdapter implements auth.LocationRepository using a pointer to the starting
// location ID. The pointer is necessary because the ID may be resolved after bootstrap,
// which runs after this adapter is created.
type LocRepoAdapter struct {
	startLocationID *ulid.ULID
	locRepo         *worldpostgres.LocationRepository
}

// NewLocRepoAdapter creates a LocRepoAdapter from a starting location ID pointer
// location ID and a worldpostgres.LocationRepository used to fetch locations.
func NewLocRepoAdapter(startLocationID *ulid.ULID, locRepo *worldpostgres.LocationRepository) *LocRepoAdapter {
	return &LocRepoAdapter{startLocationID: startLocationID, locRepo: locRepo}
}

// GetStartingLocation returns the configured starting location for new characters.
func (a *LocRepoAdapter) GetStartingLocation(ctx context.Context) (*world.Location, error) {
	if a.startLocationID == nil || a.startLocationID.IsZero() {
		return nil, oops.Code("START_LOCATION_NOT_SET").Errorf("starting location ID not yet resolved")
	}
	loc, err := a.locRepo.Get(ctx, *a.startLocationID)
	if err != nil {
		return nil, oops.Code("START_LOCATION_FETCH_FAILED").
			With("location_id", a.startLocationID.String()).Wrap(err)
	}
	return loc, nil
}
