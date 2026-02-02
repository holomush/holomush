// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// AliasRepository provides alias CRUD operations.
type AliasRepository interface {
	// System aliases
	GetSystemAliases(ctx context.Context) (map[string]string, error)
	SetSystemAlias(ctx context.Context, alias, command, createdBy string) error
	DeleteSystemAlias(ctx context.Context, alias string) error

	// Player aliases
	GetPlayerAliases(ctx context.Context, playerID ulid.ULID) (map[string]string, error)
	SetPlayerAlias(ctx context.Context, playerID ulid.ULID, alias, command string) error
	DeletePlayerAlias(ctx context.Context, playerID ulid.ULID, alias string) error
}

// PostgresAliasRepository implements AliasRepository using PostgreSQL.
type PostgresAliasRepository struct {
	pool poolIface
}

// NewPostgresAliasRepository creates a new PostgreSQL alias repository.
func NewPostgresAliasRepository(pool poolIface) *PostgresAliasRepository {
	return &PostgresAliasRepository{pool: pool}
}

// GetSystemAliases retrieves all system-wide aliases.
func (r *PostgresAliasRepository) GetSystemAliases(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT alias, command FROM system_aliases`)
	if err != nil {
		return nil, oops.With("operation", "get system aliases").Wrap(err)
	}
	defer rows.Close()

	aliases := make(map[string]string)
	for rows.Next() {
		var alias, command string
		if err := rows.Scan(&alias, &command); err != nil {
			return nil, oops.With("operation", "scan system alias row").Wrap(err)
		}
		aliases[alias] = command
	}

	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate system aliases").Wrap(err)
	}

	return aliases, nil
}

// SetSystemAlias creates or updates a system-wide alias.
func (r *PostgresAliasRepository) SetSystemAlias(ctx context.Context, alias, command, createdBy string) error {
	// Handle empty createdBy as NULL
	var createdByArg any = createdBy
	if createdBy == "" {
		createdByArg = nil
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO system_aliases (alias, command, created_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (alias) DO UPDATE SET command = $2, created_by = $3`,
		alias, command, createdByArg)
	if err != nil {
		return oops.With("operation", "set system alias").With("alias", alias).Wrap(err)
	}
	return nil
}

// DeleteSystemAlias removes a system-wide alias.
func (r *PostgresAliasRepository) DeleteSystemAlias(ctx context.Context, alias string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM system_aliases WHERE alias = $1`, alias)
	if err != nil {
		return oops.With("operation", "delete system alias").With("alias", alias).Wrap(err)
	}
	return nil
}

// GetPlayerAliases retrieves all aliases for a specific player.
func (r *PostgresAliasRepository) GetPlayerAliases(ctx context.Context, playerID ulid.ULID) (map[string]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT alias, command FROM player_aliases WHERE player_id = $1`,
		playerID.String())
	if err != nil {
		return nil, oops.With("operation", "get player aliases").With("player_id", playerID.String()).Wrap(err)
	}
	defer rows.Close()

	aliases := make(map[string]string)
	for rows.Next() {
		var alias, command string
		if err := rows.Scan(&alias, &command); err != nil {
			return nil, oops.With("operation", "scan player alias row").With("player_id", playerID.String()).Wrap(err)
		}
		aliases[alias] = command
	}

	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate player aliases").With("player_id", playerID.String()).Wrap(err)
	}

	return aliases, nil
}

// SetPlayerAlias creates or updates a player-specific alias.
func (r *PostgresAliasRepository) SetPlayerAlias(ctx context.Context, playerID ulid.ULID, alias, command string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO player_aliases (player_id, alias, command)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (player_id, alias) DO UPDATE SET command = $3`,
		playerID.String(), alias, command)
	if err != nil {
		return oops.With("operation", "set player alias").
			With("player_id", playerID.String()).
			With("alias", alias).
			Wrap(err)
	}
	return nil
}

// DeletePlayerAlias removes a player-specific alias.
func (r *PostgresAliasRepository) DeletePlayerAlias(ctx context.Context, playerID ulid.ULID, alias string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM player_aliases WHERE player_id = $1 AND alias = $2`,
		playerID.String(), alias)
	if err != nil {
		return oops.With("operation", "delete player alias").
			With("player_id", playerID.String()).
			With("alias", alias).
			Wrap(err)
	}
	return nil
}
