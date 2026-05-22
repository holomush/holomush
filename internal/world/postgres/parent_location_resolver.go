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

	"github.com/holomush/holomush/internal/access/policy/attribute"
)

// maxParentChainDepth bounds the recursive CTE that walks an object's
// containment chain to its effective location. The bound is the SOLE
// cycle defense — a true containment cycle terminates at depth
// exhaustion rather than via revisit-detection, and the resolver's
// contract (nil on un-resolvable) holds either way. Mirrors the
// depth-only precedent of object_repo.go::checkCircularContainmentTx
// (which uses internal/world/postgres/helpers.go::maxCTERecursionDepth).
// The 20-step value matches docs/specs/abac/03-property-model.md:188.
const maxParentChainDepth = 20

// ParentLocationResolver implements attribute.ParentLocationResolver
// against a PostgreSQL pool. It resolves the effective location of a
// property's parent entity. Per docs/specs/abac/03-property-model.md:
//
//   - parent_type=location → return parent_id directly (no DB query)
//   - parent_type=character → JOIN characters.location_id
//   - parent_type=object → recursive CTE walking
//     held_by_character_id / contained_in_object_id chain, bounded by
//     maxParentChainDepth and terminated via depth exhaustion for cycles.
//   - any other parent_type → return nil (the property's parent_location
//     attribute will be omitted, per ADR holomush-ti1b)
type ParentLocationResolver struct {
	pool *pgxpool.Pool
}

// NewParentLocationResolver constructs a new resolver bound to pool.
func NewParentLocationResolver(pool *pgxpool.Pool) *ParentLocationResolver {
	return &ParentLocationResolver{pool: pool}
}

// Compile-time interface check.
var _ attribute.ParentLocationResolver = (*ParentLocationResolver)(nil)

// ResolveParentLocation returns the ULID of the parent entity's
// effective location, or nil if unresolvable (NULL location, broken
// chain, cycle exhausted via depth bound, unknown parent_type).
func (r *ParentLocationResolver) ResolveParentLocation(
	ctx context.Context, parentType string, parentID ulid.ULID,
) (*ulid.ULID, error) {
	switch parentType {
	case "location":
		// Short-circuit: the location IS the parent.
		id := parentID
		return &id, nil

	case "character":
		var locStr *string
		err := r.pool.QueryRow(ctx, `
			SELECT location_id FROM characters WHERE id = $1
		`, parentID.String()).Scan(&locStr)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, oops.
				With("operation", "resolve character parent_location").
				With("character_id", parentID.String()).
				Wrap(err)
		}
		if locStr == nil {
			return nil, nil
		}
		parsed, perr := ulid.Parse(*locStr)
		if perr != nil {
			return nil, oops.
				With("operation", "parse character location_id").
				With("character_id", parentID.String()).
				Wrap(perr)
		}
		return &parsed, nil

	case "object":
		// Recursive CTE: walk the chain via contained_in_object_id, then
		// pick the first terminator row (location_id non-NULL, or
		// held_by_character_id non-NULL → JOIN characters for location_id).
		// Bounded by maxParentChainDepth as the sole cycle defense.
		//
		// COALESCE(direct, held) is correct, not biased: the schema
		// constraint chk_exactly_one_containment (migration
		// 000001_baseline.up.sql:146) guarantees each row has EXACTLY
		// ONE of {location_id, held_by_character_id, contained_in_object_id}
		// non-NULL. The chain recurses only via contained_in_object_id, so
		// intermediate rows have NULL location AND NULL held — only the
		// terminator row has either field non-NULL, never both. The
		// direct and held subqueries are therefore mutually exclusive;
		// at most ONE returns a row across both. COALESCE picks
		// whichever is non-empty without ambiguity.
		var locStr *string
		err := r.pool.QueryRow(ctx, `
			WITH RECURSIVE chain AS (
				SELECT id, location_id, held_by_character_id, contained_in_object_id, 1 AS depth
				FROM objects WHERE id = $1
				UNION ALL
				SELECT o.id, o.location_id, o.held_by_character_id, o.contained_in_object_id, c.depth + 1
				FROM objects o
				JOIN chain c ON o.id = c.contained_in_object_id
				WHERE c.depth < $2
			),
			direct AS (
				SELECT location_id, depth FROM chain WHERE location_id IS NOT NULL
				ORDER BY depth ASC LIMIT 1
			),
			held AS (
				SELECT ch.location_id, c.depth FROM chain c
				JOIN characters ch ON ch.id = c.held_by_character_id
				WHERE c.held_by_character_id IS NOT NULL
				ORDER BY c.depth ASC LIMIT 1
			)
			SELECT COALESCE(
				(SELECT location_id FROM direct),
				(SELECT location_id FROM held)
			) AS location_id
		`, parentID.String(), maxParentChainDepth).Scan(&locStr)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, oops.
				With("operation", "resolve object parent_location").
				With("object_id", parentID.String()).
				Wrap(err)
		}
		if locStr == nil {
			return nil, nil
		}
		parsed, perr := ulid.Parse(*locStr)
		if perr != nil {
			return nil, oops.
				With("operation", "parse object location_id").
				With("object_id", parentID.String()).
				Wrap(perr)
		}
		return &parsed, nil

	default:
		// Unknown parent_type — return nil so the property's
		// parent_location attr is OMITTED per ADR holomush-ti1b.
		return nil, nil
	}
}
