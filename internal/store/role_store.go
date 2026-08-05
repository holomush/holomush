// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// RoleStore manages character role assignments in the character_roles table.
type RoleStore interface {
	GetRoles(ctx context.Context, characterID string) ([]string, error)
	AddRole(ctx context.Context, characterID, role string) error
	RemoveRole(ctx context.Context, characterID, role string) error
	// PlayerHasRole returns true iff at least one character belonging to
	// playerID has the given role assigned. Used by sub-epic D's
	// OperatorAuthProvider to gate operator authentication.
	PlayerHasRole(ctx context.Context, playerID, role string) (bool, error)
}

// PostgresRoleStore implements RoleStore using PostgreSQL.
type PostgresRoleStore struct {
	pool poolIface
}

// NewPostgresRoleStore creates a new PostgreSQL role store.
func NewPostgresRoleStore(pool poolIface) *PostgresRoleStore {
	return &PostgresRoleStore{pool: pool}
}

// GetRoles returns all roles assigned to a character.
func (s *PostgresRoleStore) GetRoles(ctx context.Context, characterID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT role FROM character_roles WHERE character_id = $1 ORDER BY role`, characterID)
	if err != nil {
		return nil, oops.With("character_id", characterID).Wrap(err)
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, oops.With("character_id", characterID).Wrap(err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("character_id", characterID).Wrap(err)
	}
	return roles, nil
}

// AddRole assigns a role to a character. Idempotent — does nothing if already assigned.
func (s *PostgresRoleStore) AddRole(ctx context.Context, characterID, role string) error {
	_, err := s.pool.Exec(
		ctx,
		`INSERT INTO character_roles (character_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		characterID, role,
	)
	if err != nil {
		return oops.With("character_id", characterID).With("role", role).Wrap(err)
	}
	return nil
}

// RemoveRole removes a role from a character. No error if the role wasn't assigned.
func (s *PostgresRoleStore) RemoveRole(ctx context.Context, characterID, role string) error {
	_, err := s.pool.Exec(
		ctx,
		`DELETE FROM character_roles WHERE character_id = $1 AND role = $2`,
		characterID, role,
	)
	if err != nil {
		return oops.With("character_id", characterID).With("role", role).Wrap(err)
	}
	return nil
}

// PlayerRoles returns the deduplicated union of roles held by any character of
// playerID, sorted, or an EMPTY slice when the player holds no roles at all —
// absence of roles is not an error. A malformed (non-ULID) player id IS an
// error: silently returning an empty slice would let a caller read "bad input"
// as "no roles", and every role-gated policy would then default-deny for a
// reason indistinguishable from a legitimately role-less player.
//
// SEMANTICS: this generalizes [PostgresRoleStore.PlayerHasRole]'s documented
// any-character-of-the-player reading from one role to the whole set, over the
// same character_roles → characters join on c.player_id. 01-SPEC §10.5 makes
// the player-scoped reading normative, so the web read path and the operator
// socket cannot give two different answers to "is this human an admin" at the
// same moment.
//
// The character-scoped storage with a player-scoped read is a KNOWN ASYMMETRY,
// tracked as issue #4899 — it is a recorded design position, not an accident to
// be tidied away by re-scoping this query to a character.
//
// PlayerRoles is deliberately NOT part of the [RoleStore] interface. The
// interface is implemented and faked in several places; only this concrete type
// can answer the query, and the ABAC stack reaches it through the
// setup.ABACConfig.PlayerRoleLookup func field (the shipped seam shape —
// PlayerKindLookup is already exactly this). Pinned by
// TestRoleStoreInterfaceMethodSetIsUnchangedByPlayerRoles.
func (s *PostgresRoleStore) PlayerRoles(ctx context.Context, playerID string) ([]string, error) {
	if _, err := ulid.Parse(playerID); err != nil {
		return nil, oops.Code("INVALID_PLAYER_ID").
			With("player_id", playerID).
			Wrapf(err, "invalid player ULID")
	}

	// DISTINCT dedupes a role held by two of the player's characters; ORDER BY
	// makes the returned slice stable across calls. A policy's VERDICT is
	// reproducible either way, but its failure messages are not.
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT cr.role
		  FROM character_roles cr
		  JOIN characters c ON cr.character_id = c.id
		 WHERE c.player_id = $1
		 ORDER BY cr.role
	`, playerID)
	if err != nil {
		return nil, oops.Code("ROLE_PLAYER_ROLES_FAILED").
			With("player_id", playerID).Wrap(err)
	}
	defer rows.Close()

	roles := []string{}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, oops.Code("ROLE_PLAYER_ROLES_FAILED").
				With("player_id", playerID).Wrap(err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("ROLE_PLAYER_ROLES_FAILED").
			With("player_id", playerID).Wrap(err)
	}
	return roles, nil
}

// PlayerHasRole returns true iff any character of playerID has role.
func (s *PostgresRoleStore) PlayerHasRole(ctx context.Context, playerID, role string) (bool, error) {
	var found int
	err := s.pool.QueryRow(ctx, `
		SELECT 1
		  FROM character_roles cr
		  JOIN characters c ON cr.character_id = c.id
		 WHERE c.player_id = $1
		   AND cr.role     = $2
		 LIMIT 1
	`, playerID, role).Scan(&found)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, oops.Code("ROLE_PLAYER_HAS_ROLE_FAILED").
			With("player_id", playerID).
			With("role", role).Wrap(err)
	}
	return found == 1, nil
}
