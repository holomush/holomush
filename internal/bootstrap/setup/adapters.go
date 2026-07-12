// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
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
//
// It delegates to the world repository's canonical version-scanning ListByPlayer
// (round-6 R6-1/R6-3) so every returned Character.Version carries the STORED
// version, not 0 — keeping the world reads inside the world boundary and ensuring
// a caller that lists here and then issues a guarded write/delete (e.g. the 05-16
// guest reaper's CAS Delete) has the correct expected version. A version-blind
// list feeding a version-predicated delete would permanently conflict.
func (a *CharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	chars, err := a.charRepo.ListByPlayer(ctx, playerID)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return chars, nil
}

// ListAll returns every character ordered by name ascending (id + name only).
// Delegates to the underlying world repository.
//
// This is a DIRECTORY read (id + name only, no version) for pickers/listings; it
// is intentionally version-blind and MUST NOT back a guarded delete/CAS. Any path
// that lists characters for a subsequent version-predicated write MUST use
// ListByPlayer (which scans version), not ListAll (round-6 R6-3).
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
