// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// NewCharacterNameGate builds the character-name admission gate.
//
// # There is exactly ONE of these, and that is the point
//
// CharacterService is constructed at TWO independent composition roots
// (internal/bootstrap/setup and cmd/holomush) and GuestService at a third. A
// gate built separately at each is a gate that can drift: two caches polling
// two copies of the block list, one root wired and the other quietly not. Both
// roots call THIS function, so there is one construction and one set of
// dependencies.
//
// It fails closed on a nil block-list subsystem. A nil matcher means "no list
// configured", and silently accepting one at a production root is how the whole
// IDENT-07 mechanism becomes decorative: everything compiles, every unit test
// passes, and no name is ever checked against the operator's list.
//
// The matcher is read through blocklist.Subsystem.Matcher(), which hands back
// the LIVE cache rather than a snapshot, so a gate built once at boot sees every
// later reload.
func NewCharacterNameGate(pool *pgxpool.Pool, blockList *blocklist.Subsystem) (*charname.Gate, error) {
	if pool == nil {
		return nil, oops.Code("CHARACTER_NAME_GATE_MISCONFIGURED").
			Errorf("database pool is required to build the character-name gate")
	}
	if blockList == nil {
		return nil, oops.Code("CHARACTER_NAME_GATE_MISCONFIGURED").
			Errorf("character-name block list subsystem is required; refusing to build a gate with no block list")
	}
	return &charname.Gate{
		Skeletons: worldpostgres.NewSkeletonLookup(pool),
		BlockList: blockList.Matcher(),
	}, nil
}

// Compile-time checks.
var (
	_ auth.CharacterRepository = (*CharRepoAdapter)(nil)
	_ auth.LocationRepository  = (*LocRepoAdapter)(nil)
)

// CharRepoAdapter wraps a pgxpool.Pool to implement auth.CharacterRepository.
// It provides the auth-side READ queries only; character creation is owned
// exclusively by auth.CharacterGenesisService (05-15 removed Create from the
// auth-side interfaces — the compile-level fence).
type CharRepoAdapter struct {
	pool     *pgxpool.Pool
	charRepo *worldpostgres.CharacterRepository
}

// NewCharRepoAdapter constructs a CharRepoAdapter using the provided PostgreSQL pool and character repository.
func NewCharRepoAdapter(pool *pgxpool.Pool, charRepo *worldpostgres.CharacterRepository) *CharRepoAdapter {
	return &CharRepoAdapter{pool: pool, charRepo: charRepo}
}

// ExistsByNormalizedName reports whether any character holds the given §6.1.1
// uniqueness key, optionally excluding one character's own row.
//
// The predicate is TRANSITIONAL and its NULL branch is deliberate:
//
//	normalized_name = $1 OR (normalized_name IS NULL AND LOWER(name) = LOWER($1))
//
// Cutting straight to `normalized_name = $1` here would make every pre-existing
// row invisible to this check for a whole wave — the backfill is migration
// 000055 and the UNIQUE index 000056, both in plan 02-12, while the LOWER(name)
// safety net is removed here. In that window a duplicate would be caught by
// NOTHING, in a commit that deploys green and whose tests all pass because
// every fixture writes the column.
//
// REMOVE with migration 000056; see plan 02-12.
//
// excluding is the B-18 self-exclusion channel: 01-SPEC.md:702-706 settles that
// a rename whose uniqueness key matches the current one but whose display form
// differs is a REAL rename and does not collide with itself. Create passes nil;
// only a rename path passes an id.
//
// This check is a UX AFFORDANCE, not the uniqueness guarantee. §6.1.3 assigns
// that to the database constraint; this exists to produce a friendly error most
// of the time and it does not close the race.
func (a *CharRepoAdapter) ExistsByNormalizedName(ctx context.Context, key string, excluding *ulid.ULID) (bool, error) {
	var excludingArg *string
	if excluding != nil {
		s := excluding.String()
		excludingArg = &s
	}
	var exists bool
	err := a.pool.QueryRow(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM characters
			WHERE (normalized_name = $1
			       OR (normalized_name IS NULL AND LOWER(name) = LOWER($1)))
			  AND ($2::text IS NULL OR id::text <> $2)
		)`,
		key, excludingArg,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", key).Wrap(err)
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
