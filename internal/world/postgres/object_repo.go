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

// ObjectRepository implements world.ObjectRepository using PostgreSQL.
type ObjectRepository struct {
	pool *pgxpool.Pool
}

// NewObjectRepository creates a new ObjectRepository.
func NewObjectRepository(pool *pgxpool.Pool) *ObjectRepository {
	return &ObjectRepository{pool: pool}
}

// Get retrieves an object by ID.
func (r *ObjectRepository) Get(ctx context.Context, id ulid.ULID) (*world.Object, error) {
	var obj world.Object
	var idStr string
	var locationIDStr, heldByStr, containedInStr, ownerIDStr *string

	err := r.pool.QueryRow(ctx, `
		SELECT id, name, description, location_id, held_by_character_id,
		       contained_in_object_id, is_container, owner_id, created_at
		FROM objects WHERE id = $1
	`, id.String()).Scan(
		&idStr, &obj.Name, &obj.Description, &locationIDStr, &heldByStr,
		&containedInStr, &obj.IsContainer, &ownerIDStr, &obj.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.With("id", id.String()).Wrap(ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "get object").With("id", id.String()).Wrap(err)
	}

	obj.ID, err = ulid.Parse(idStr)
	if err != nil {
		return nil, oops.With("operation", "parse object id").With("id", idStr).Wrap(err)
	}
	if locationIDStr != nil {
		lid, err := ulid.Parse(*locationIDStr)
		if err != nil {
			return nil, oops.With("operation", "parse location_id").With("location_id", *locationIDStr).Wrap(err)
		}
		obj.LocationID = &lid
	}
	if heldByStr != nil {
		hid, err := ulid.Parse(*heldByStr)
		if err != nil {
			return nil, oops.With("operation", "parse held_by_character_id").With("held_by_character_id", *heldByStr).Wrap(err)
		}
		obj.HeldByCharacterID = &hid
	}
	if containedInStr != nil {
		cid, err := ulid.Parse(*containedInStr)
		if err != nil {
			return nil, oops.With("operation", "parse contained_in_object_id").With("contained_in_object_id", *containedInStr).Wrap(err)
		}
		obj.ContainedInObjectID = &cid
	}
	if ownerIDStr != nil {
		oid, err := ulid.Parse(*ownerIDStr)
		if err != nil {
			return nil, oops.With("operation", "parse owner_id").With("owner_id", *ownerIDStr).Wrap(err)
		}
		obj.OwnerID = &oid
	}

	return &obj, nil
}

// Create persists a new object.
func (r *ObjectRepository) Create(ctx context.Context, obj *world.Object) error {
	var locationID, heldBy, containedIn, ownerID *string
	if obj.LocationID != nil {
		s := obj.LocationID.String()
		locationID = &s
	}
	if obj.HeldByCharacterID != nil {
		s := obj.HeldByCharacterID.String()
		heldBy = &s
	}
	if obj.ContainedInObjectID != nil {
		s := obj.ContainedInObjectID.String()
		containedIn = &s
	}
	if obj.OwnerID != nil {
		s := obj.OwnerID.String()
		ownerID = &s
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO objects (id, name, description, location_id, held_by_character_id,
		                     contained_in_object_id, is_container, owner_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, obj.ID.String(), obj.Name, obj.Description, locationID, heldBy,
		containedIn, obj.IsContainer, ownerID, obj.CreatedAt)
	if err != nil {
		return oops.With("operation", "create object").With("id", obj.ID.String()).Wrap(err)
	}
	return nil
}

// Update modifies an existing object.
func (r *ObjectRepository) Update(ctx context.Context, obj *world.Object) error {
	var locationID, heldBy, containedIn, ownerID *string
	if obj.LocationID != nil {
		s := obj.LocationID.String()
		locationID = &s
	}
	if obj.HeldByCharacterID != nil {
		s := obj.HeldByCharacterID.String()
		heldBy = &s
	}
	if obj.ContainedInObjectID != nil {
		s := obj.ContainedInObjectID.String()
		containedIn = &s
	}
	if obj.OwnerID != nil {
		s := obj.OwnerID.String()
		ownerID = &s
	}

	result, err := r.pool.Exec(ctx, `
		UPDATE objects SET name = $2, description = $3, location_id = $4,
		       held_by_character_id = $5, contained_in_object_id = $6,
		       is_container = $7, owner_id = $8
		WHERE id = $1
	`, obj.ID.String(), obj.Name, obj.Description, locationID, heldBy,
		containedIn, obj.IsContainer, ownerID)
	if err != nil {
		return oops.With("operation", "update object").With("id", obj.ID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("id", obj.ID.String()).Wrap(ErrNotFound)
	}
	return nil
}

// Delete removes an object by ID.
func (r *ObjectRepository) Delete(ctx context.Context, id ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM objects WHERE id = $1`, id.String())
	if err != nil {
		return oops.With("operation", "delete object").With("id", id.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("id", id.String()).Wrap(ErrNotFound)
	}
	return nil
}

// ListAtLocation returns all objects at a location.
func (r *ObjectRepository) ListAtLocation(ctx context.Context, locationID ulid.ULID) ([]*world.Object, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, description, location_id, held_by_character_id,
		       contained_in_object_id, is_container, owner_id, created_at
		FROM objects WHERE location_id = $1 ORDER BY created_at DESC
	`, locationID.String())
	if err != nil {
		return nil, oops.With("operation", "list objects at location").With("location_id", locationID.String()).Wrap(err)
	}
	defer rows.Close()

	return scanObjects(rows)
}

// ListHeldBy returns all objects held by a character.
func (r *ObjectRepository) ListHeldBy(ctx context.Context, characterID ulid.ULID) ([]*world.Object, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, description, location_id, held_by_character_id,
		       contained_in_object_id, is_container, owner_id, created_at
		FROM objects WHERE held_by_character_id = $1 ORDER BY created_at DESC
	`, characterID.String())
	if err != nil {
		return nil, oops.With("operation", "list objects held by").With("character_id", characterID.String()).Wrap(err)
	}
	defer rows.Close()

	return scanObjects(rows)
}

// ListContainedIn returns all objects inside a container object.
func (r *ObjectRepository) ListContainedIn(ctx context.Context, objectID ulid.ULID) ([]*world.Object, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, description, location_id, held_by_character_id,
		       contained_in_object_id, is_container, owner_id, created_at
		FROM objects WHERE contained_in_object_id = $1 ORDER BY created_at DESC
	`, objectID.String())
	if err != nil {
		return nil, oops.With("operation", "list objects contained in").With("object_id", objectID.String()).Wrap(err)
	}
	defer rows.Close()

	return scanObjects(rows)
}

// DefaultMaxNestingDepth is the maximum allowed nesting depth for object containment.
const DefaultMaxNestingDepth = 3

// Move changes an object's containment.
// Validates containment and enforces business rules:
// - Target must be a valid container if moving to an object
// - Max nesting depth is enforced (default 3)
// - Circular containment is prevented
func (r *ObjectRepository) Move(ctx context.Context, objectID ulid.ULID, to world.Containment) error {
	// Validate containment
	if err := to.Validate(); err != nil {
		return oops.With("operation", "move object").With("object_id", objectID.String()).Wrap(err)
	}

	// If moving to a container, verify the container exists and is actually a container
	if to.ObjectID != nil {
		var isContainer bool
		err := r.pool.QueryRow(ctx, `
			SELECT is_container FROM objects WHERE id = $1
		`, to.ObjectID.String()).Scan(&isContainer)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.With("operation", "move object").
				With("object_id", objectID.String()).
				With("container_id", to.ObjectID.String()).
				Wrap(errors.New("container object not found"))
		}
		if err != nil {
			return oops.With("operation", "move object").With("object_id", objectID.String()).Wrap(err)
		}
		if !isContainer {
			return oops.With("operation", "move object").
				With("object_id", objectID.String()).
				With("container_id", to.ObjectID.String()).
				Wrap(errors.New("target object is not a container"))
		}

		// Check for circular containment: object cannot be placed inside itself
		// or inside any object that is contained within it
		if err := r.checkCircularContainment(ctx, objectID, *to.ObjectID); err != nil {
			return err
		}

		// Check max nesting depth: verify that target container depth + object subtree depth
		// won't exceed the maximum allowed depth
		if err := r.checkNestingDepth(ctx, objectID, *to.ObjectID); err != nil {
			return err
		}
	}

	// Clear all containment fields and set the new one
	var locationID, heldBy, containedIn *string
	if to.LocationID != nil {
		s := to.LocationID.String()
		locationID = &s
	}
	if to.CharacterID != nil {
		s := to.CharacterID.String()
		heldBy = &s
	}
	if to.ObjectID != nil {
		s := to.ObjectID.String()
		containedIn = &s
	}

	result, err := r.pool.Exec(ctx, `
		UPDATE objects SET location_id = $2, held_by_character_id = $3, contained_in_object_id = $4
		WHERE id = $1
	`, objectID.String(), locationID, heldBy, containedIn)
	if err != nil {
		return oops.With("operation", "move object").With("object_id", objectID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("object_id", objectID.String()).Wrap(ErrNotFound)
	}
	return nil
}

// checkCircularContainment verifies that placing objectID into targetContainerID
// won't create a circular containment loop (e.g., A in B, then trying to put B in A).
// Uses a recursive CTE to traverse the containment hierarchy.
func (r *ObjectRepository) checkCircularContainment(ctx context.Context, objectID, targetContainerID ulid.ULID) error {
	// Self-containment check
	if objectID == targetContainerID {
		return oops.With("operation", "move object").
			With("object_id", objectID.String()).
			With("container_id", targetContainerID.String()).
			Wrap(errors.New("circular containment: cannot place object inside itself"))
	}

	// Check if targetContainer is contained (directly or transitively) inside objectID
	// This would create a loop: objectID contains targetContainer, so putting objectID
	// into targetContainer would be circular.
	var isCircular bool
	err := r.pool.QueryRow(ctx, `
		WITH RECURSIVE containment_chain AS (
			-- Start from the target container
			SELECT id, contained_in_object_id, 1 as depth
			FROM objects WHERE id = $1

			UNION ALL

			-- Walk up the containment chain
			SELECT o.id, o.contained_in_object_id, cc.depth + 1
			FROM objects o
			JOIN containment_chain cc ON o.id = cc.contained_in_object_id
			WHERE cc.depth < 100  -- Safety limit
		)
		SELECT EXISTS(
			SELECT 1 FROM containment_chain WHERE contained_in_object_id = $2
		)
	`, targetContainerID.String(), objectID.String()).Scan(&isCircular)
	if err != nil {
		return oops.With("operation", "check circular containment").With("object_id", objectID.String()).Wrap(err)
	}

	if isCircular {
		return oops.With("operation", "move object").
			With("object_id", objectID.String()).
			With("container_id", targetContainerID.String()).
			Wrap(errors.New("circular containment: target container is inside this object"))
	}

	return nil
}

// checkNestingDepth verifies that placing objectID into targetContainerID
// won't exceed the maximum nesting depth.
// This checks both:
// 1. How deep the target container is (walking up)
// 2. How deep the descendants of objectID are (walking down)
// The total must not exceed DefaultMaxNestingDepth.
func (r *ObjectRepository) checkNestingDepth(ctx context.Context, objectID, targetContainerID ulid.ULID) error {
	var targetDepth, objectSubtreeDepth int

	// Query 1: Find how deep the target container is (walk up to root)
	err := r.pool.QueryRow(ctx, `
		WITH RECURSIVE ancestors AS (
			-- Start from the target container
			SELECT id, contained_in_object_id, 1 as depth
			FROM objects WHERE id = $1

			UNION ALL

			-- Walk up the containment chain
			SELECT o.id, o.contained_in_object_id, a.depth + 1
			FROM objects o
			JOIN ancestors a ON o.id = a.contained_in_object_id
			WHERE a.depth < 100  -- Safety limit
		)
		SELECT COALESCE(MAX(depth), 0) FROM ancestors
	`, targetContainerID.String()).Scan(&targetDepth)
	if err != nil {
		return oops.With("operation", "check target depth").With("container_id", targetContainerID.String()).Wrap(err)
	}

	// Query 2: Find the deepest descendant of the object being moved (walk down)
	err = r.pool.QueryRow(ctx, `
		WITH RECURSIVE descendants AS (
			-- Start from the object being moved (depth 0 = the object itself)
			SELECT id, 0 as depth
			FROM objects WHERE id = $1

			UNION ALL

			-- Walk down to find all contained objects
			SELECT o.id, d.depth + 1
			FROM objects o
			JOIN descendants d ON o.contained_in_object_id = d.id
			WHERE d.depth < 100  -- Safety limit
		)
		SELECT COALESCE(MAX(depth), 0) FROM descendants
	`, objectID.String()).Scan(&objectSubtreeDepth)
	if err != nil {
		return oops.With("operation", "check object subtree depth").With("object_id", objectID.String()).Wrap(err)
	}

	// Total depth = target container depth + 1 (placing the object) + object's deepest descendant
	totalDepth := targetDepth + objectSubtreeDepth + 1
	if totalDepth > DefaultMaxNestingDepth {
		return oops.With("operation", "move object").
			With("object_id", objectID.String()).
			With("container_id", targetContainerID.String()).
			With("target_depth", targetDepth).
			With("object_subtree_depth", objectSubtreeDepth).
			With("total_depth", totalDepth).
			With("max_depth", DefaultMaxNestingDepth).
			Wrap(errors.New("max nesting depth exceeded"))
	}

	return nil
}

func scanObjects(rows pgx.Rows) ([]*world.Object, error) {
	objects := make([]*world.Object, 0)
	for rows.Next() {
		var obj world.Object
		var idStr string
		var locationIDStr, heldByStr, containedInStr, ownerIDStr *string

		if err := rows.Scan(
			&idStr, &obj.Name, &obj.Description, &locationIDStr, &heldByStr,
			&containedInStr, &obj.IsContainer, &ownerIDStr, &obj.CreatedAt,
		); err != nil {
			return nil, oops.With("operation", "scan object").Wrap(err)
		}

		var err error
		obj.ID, err = ulid.Parse(idStr)
		if err != nil {
			return nil, oops.With("operation", "parse object id").With("id", idStr).Wrap(err)
		}
		if locationIDStr != nil {
			lid, err := ulid.Parse(*locationIDStr)
			if err != nil {
				return nil, oops.With("operation", "parse location_id").With("location_id", *locationIDStr).Wrap(err)
			}
			obj.LocationID = &lid
		}
		if heldByStr != nil {
			hid, err := ulid.Parse(*heldByStr)
			if err != nil {
				return nil, oops.With("operation", "parse held_by_character_id").With("held_by_character_id", *heldByStr).Wrap(err)
			}
			obj.HeldByCharacterID = &hid
		}
		if containedInStr != nil {
			cid, err := ulid.Parse(*containedInStr)
			if err != nil {
				return nil, oops.With("operation", "parse contained_in_object_id").With("contained_in_object_id", *containedInStr).Wrap(err)
			}
			obj.ContainedInObjectID = &cid
		}
		if ownerIDStr != nil {
			oid, err := ulid.Parse(*ownerIDStr)
			if err != nil {
				return nil, oops.With("operation", "parse owner_id").With("owner_id", *ownerIDStr).Wrap(err)
			}
			obj.OwnerID = &oid
		}

		objects = append(objects, &obj)
	}

	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate objects").Wrap(err)
	}

	return objects, nil
}

// Compile-time interface check.
var _ world.ObjectRepository = (*ObjectRepository)(nil)
