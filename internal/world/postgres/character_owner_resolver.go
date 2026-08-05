// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/attribute"
)

// CharacterOwnerResolver implements attribute.CharacterOwnerResolver against a
// PostgreSQL pool. It answers "which players own these characters, and what is
// each of those players' COMPLETE character set".
//
// It lives beside ParentLocationResolver because that is the in-tree precedent
// for a PropertyProvider resolver dependency, and because this is a READ — the
// world-SQL write fence is not engaged.
type CharacterOwnerResolver struct {
	pool *pgxpool.Pool
}

// NewCharacterOwnerResolver constructs a resolver bound to pool.
func NewCharacterOwnerResolver(pool *pgxpool.Pool) *CharacterOwnerResolver {
	return &CharacterOwnerResolver{pool: pool}
}

// Compile-time interface check.
var _ attribute.CharacterOwnerResolver = (*CharacterOwnerResolver)(nil)

// ResolveOwnerScopes returns, for every player owning at least one of the
// supplied character ids, that player's COMPLETE character-id set — keyed by
// player id, each set sorted for determinism.
//
// THE COMPLETE SET IS THE POINT, not an implementation convenience. Per
// 02-CONTEXT.md D-27 the permit-side derived property peers are computed in the
// ALL direction ("every character of this player appears in the row's
// character-keyed field"), which CANNOT be evaluated from a flat list of players
// who merely happen to own one of the listed characters. A flat []string of
// player ids is the shape a plain UNION needs; returning it would make the union
// the only implementable rule.
//
// Characters that do not exist are skipped, as are character rows whose
// player_id is NULL — an unowned character contributes no player, never an
// empty-string key. An empty input short-circuits before the pool is touched.
func (r *CharacterOwnerResolver) ResolveOwnerScopes(
	ctx context.Context, characterIDs []string,
) (map[string][]string, error) {
	if len(characterIDs) == 0 {
		return map[string][]string{}, nil
	}

	// One query: find the players owning any supplied id, then return ALL of
	// those players' characters. A row with a large visible_to list therefore
	// costs one round trip, not N.
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.player_id
		  FROM characters c
		 WHERE c.player_id IN (
		       SELECT o.player_id
		         FROM characters o
		        WHERE o.id = ANY($1)
		          AND o.player_id IS NOT NULL
		 )
	`, characterIDs)
	if err != nil {
		return nil, oops.Code("CHARACTER_OWNER_SCOPES_FAILED").
			With("operation", "resolve character owner scopes").
			With("character_id_count", len(characterIDs)).
			Wrap(err)
	}
	defer rows.Close()

	scopes := map[string][]string{}
	for rows.Next() {
		var charID string
		var playerID *string
		if err := rows.Scan(&charID, &playerID); err != nil {
			return nil, oops.Code("CHARACTER_OWNER_SCOPES_FAILED").
				With("operation", "scan character owner scope row").
				Wrap(err)
		}
		if playerID == nil || *playerID == "" {
			// Defensive: the subquery already excludes NULL player_id, but a
			// NULL here must never become a "" map key — that would be the
			// map-shaped empty-string sentinel.
			continue
		}
		scopes[*playerID] = append(scopes[*playerID], charID)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("CHARACTER_OWNER_SCOPES_FAILED").
			With("operation", "iterate character owner scope rows").
			Wrap(err)
	}

	for _, chars := range scopes {
		sort.Strings(chars)
	}
	return scopes, nil
}
