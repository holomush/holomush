// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// authCharRepoAdapter wraps a pgxpool.Pool to implement auth.CharacterRepository.
// It delegates Create to worldpostgres and adds auth-specific queries.
type authCharRepoAdapter struct {
	pool     *pgxpool.Pool
	charRepo *worldpostgres.CharacterRepository
}

func (a *authCharRepoAdapter) Create(ctx context.Context, char *world.Character) error {
	return a.charRepo.Create(ctx, char)
}

func (a *authCharRepoAdapter) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := a.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))",
		name,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", name).Wrap(err)
	}
	return exists, nil
}

func (a *authCharRepoAdapter) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	var count int
	err := a.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM characters WHERE player_id = $1",
		playerID.String(),
	).Scan(&count)
	if err != nil {
		return 0, oops.Code("CHARACTER_COUNT_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return count, nil
}

func (a *authCharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	rows, err := a.pool.Query(ctx,
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
		scanErr := rows.Scan(&idStr, &playerIDStr, &c.Name, &c.Description, &locIDStr, &c.CreatedAt)
		if scanErr != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(scanErr)
		}
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

// authLocRepoAdapter implements auth.LocationRepository by returning a fixed starting location.
type authLocRepoAdapter struct {
	startLocationID ulid.ULID
	locRepo         *worldpostgres.LocationRepository
}

func (a *authLocRepoAdapter) GetStartingLocation(ctx context.Context) (*world.Location, error) {
	return a.locRepo.Get(ctx, a.startLocationID)
}
