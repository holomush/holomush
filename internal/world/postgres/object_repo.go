// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// ObjectRepository implements world.ObjectRepository using PostgreSQL.
type ObjectRepository struct {
	pool            *pgxpool.Pool
	maxNestingDepth int
}

// NewObjectRepository creates a new ObjectRepository.
// Uses DefaultMaxNestingDepth for nesting limit.
func NewObjectRepository(pool *pgxpool.Pool) *ObjectRepository {
	return &ObjectRepository{pool: pool, maxNestingDepth: DefaultMaxNestingDepth}
}

// NewObjectRepositoryWithDepth creates a new ObjectRepository with a custom max nesting depth.
// Use this for games that need different containment depth limits.
func NewObjectRepositoryWithDepth(pool *pgxpool.Pool, maxNestingDepth int) *ObjectRepository {
	return &ObjectRepository{pool: pool, maxNestingDepth: maxNestingDepth}
}

// Get retrieves an object by ID.
func (r *ObjectRepository) Get(ctx context.Context, id ulid.ULID) (*world.Object, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, description, location_id, held_by_character_id,
		       contained_in_object_id, is_container, owner_id, created_at
		FROM objects WHERE id = $1
	`, id.String())
	obj, err := scanObjectRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.With("id", id.String()).Wrap(world.ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "get object").With("id", id.String()).Wrap(err)
	}
	return obj, nil
}

// Create persists a new object.
// Callers must validate the object before calling this method.
func (r *ObjectRepository) Create(ctx context.Context, obj *world.Object) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO objects (id, name, description, location_id, held_by_character_id,
		                     contained_in_object_id, is_container, owner_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, obj.ID.String(), obj.Name, obj.Description,
		ulidToStringPtr(obj.LocationID),
		ulidToStringPtr(obj.HeldByCharacterID),
		ulidToStringPtr(obj.ContainedInObjectID),
		obj.IsContainer,
		ulidToStringPtr(obj.OwnerID),
		obj.CreatedAt)
	if err != nil {
		return oops.With("operation", "create object").With("id", obj.ID.String()).Wrap(err)
	}
	return nil
}

// Update modifies an existing object.
// Callers must validate the object before calling this method.
func (r *ObjectRepository) Update(ctx context.Context, obj *world.Object) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE objects SET name = $2, description = $3, location_id = $4,
		       held_by_character_id = $5, contained_in_object_id = $6,
		       is_container = $7, owner_id = $8
		WHERE id = $1
	`, obj.ID.String(), obj.Name, obj.Description,
		ulidToStringPtr(obj.LocationID),
		ulidToStringPtr(obj.HeldByCharacterID),
		ulidToStringPtr(obj.ContainedInObjectID),
		obj.IsContainer,
		ulidToStringPtr(obj.OwnerID))
	if err != nil {
		return oops.With("operation", "update object").With("id", obj.ID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("id", obj.ID.String()).Wrap(world.ErrNotFound)
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
		return oops.With("id", id.String()).Wrap(world.ErrNotFound)
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
// - Max nesting depth is enforced (configurable via NewObjectRepositoryWithDepth)
// - Circular containment is prevented
// Uses a transaction with SELECT FOR UPDATE to ensure atomicity and prevent TOCTOU
// vulnerabilities - both the object and container (if any) are locked for the duration.
func (r *ObjectRepository) Move(ctx context.Context, objectID ulid.ULID, to world.Containment) error {
	// Validate containment
	if err := to.Validate(); err != nil {
		return oops.With("operation", "move object").With("object_id", objectID.String()).Wrap(err)
	}

	// Use a transaction to ensure validation and update are atomic
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return oops.With("operation", "begin transaction").Wrap(err)
	}
	defer func() {
		// Rollback is a no-op if tx was committed
		_ = tx.Rollback(ctx) //nolint:errcheck // Rollback error after commit is meaningless
	}()

	// Lock the object being moved to prevent concurrent move operations
	var exists bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM objects WHERE id = $1 FOR UPDATE)
	`, objectID.String()).Scan(&exists)
	if err != nil {
		return oops.With("operation", "lock object").With("object_id", objectID.String()).Wrap(err)
	}
	if !exists {
		return oops.With("object_id", objectID.String()).Wrap(world.ErrNotFound)
	}

	// If moving to a container, verify the container exists and is actually a container
	// FOR UPDATE locks the container row to prevent deletion/modification during this transaction
	if to.ObjectID != nil {
		var isContainer bool
		err = tx.QueryRow(ctx, `
			SELECT is_container FROM objects WHERE id = $1 FOR UPDATE
		`, to.ObjectID.String()).Scan(&isContainer)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.
				With("operation", "move object").
				With("object_id", objectID.String()).
				With("container_id", to.ObjectID.String()).
				Wrap(world.ErrNotFound)
		}
		if err != nil {
			return oops.With("operation", "move object").With("object_id", objectID.String()).Wrap(err)
		}
		if !isContainer {
			return oops.
				With("operation", "move object").
				With("object_id", objectID.String()).
				With("container_id", to.ObjectID.String()).
				Wrap(world.ErrInvalidContainment)
		}

		// Check for circular containment: object cannot be placed inside itself
		// or inside any object that is contained within it
		err = r.checkCircularContainmentTx(ctx, tx, objectID, *to.ObjectID)
		if err != nil {
			return err
		}

		// Check max nesting depth: verify that target container depth + object subtree depth
		// won't exceed the maximum allowed depth
		err = r.checkNestingDepthTx(ctx, tx, objectID, *to.ObjectID)
		if err != nil {
			return err
		}
	}

	// Clear all containment fields and set the new one
	result, err := tx.Exec(ctx, `
		UPDATE objects SET location_id = $2, held_by_character_id = $3, contained_in_object_id = $4
		WHERE id = $1
	`, objectID.String(), ulidToStringPtr(to.LocationID), ulidToStringPtr(to.CharacterID), ulidToStringPtr(to.ObjectID))
	if err != nil {
		return oops.With("operation", "move object").With("object_id", objectID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("object_id", objectID.String()).Wrap(world.ErrNotFound)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.With("operation", "commit transaction").Wrap(err)
	}
	return nil
}

// checkCircularContainmentTx verifies that placing objectID into targetContainerID
// won't create a circular containment loop (e.g., A in B, then trying to put B in A).
// Uses a recursive CTE to traverse the containment hierarchy.
// Accepts a querier interface to work within a transaction.
func (r *ObjectRepository) checkCircularContainmentTx(ctx context.Context, q querier, objectID, targetContainerID ulid.ULID) error {
	// Self-containment check
	if objectID == targetContainerID {
		return oops.
			With("operation", "move object").
			With("object_id", objectID.String()).
			With("container_id", targetContainerID.String()).
			Errorf("circular containment: cannot place object inside itself")
	}

	// Check if targetContainer is contained (directly or transitively) inside objectID
	var isCircular bool
	err := q.QueryRow(ctx, fmt.Sprintf(`
		WITH RECURSIVE containment_chain AS (
			SELECT id, contained_in_object_id, 1 as depth
			FROM objects WHERE id = $1
			UNION ALL
			SELECT o.id, o.contained_in_object_id, cc.depth + 1
			FROM objects o
			JOIN containment_chain cc ON o.id = cc.contained_in_object_id
			WHERE cc.depth < %d
		)
		SELECT EXISTS(
			SELECT 1 FROM containment_chain WHERE contained_in_object_id = $2
		)
	`, maxCTERecursionDepth), targetContainerID.String(), objectID.String()).Scan(&isCircular)
	if err != nil {
		return oops.With("operation", "check circular containment").With("object_id", objectID.String()).Wrap(err)
	}

	if isCircular {
		return oops.
			With("operation", "move object").
			With("object_id", objectID.String()).
			With("container_id", targetContainerID.String()).
			Errorf("circular containment: target container is inside this object")
	}

	return nil
}

// checkNestingDepthTx verifies that moving objectID into targetContainerID
// won't exceed the maximum nesting depth. Accepts a querier interface to work
// within a transaction.
func (r *ObjectRepository) checkNestingDepthTx(ctx context.Context, q querier, objectID, targetContainerID ulid.ULID) error {
	var targetDepth, objectSubtreeDepth int

	// Query 1: Find how deep the target container is (walk up to root)
	err := q.QueryRow(ctx, fmt.Sprintf(`
		WITH RECURSIVE ancestors AS (
			SELECT id, contained_in_object_id, 1 as depth
			FROM objects WHERE id = $1
			UNION ALL
			SELECT o.id, o.contained_in_object_id, a.depth + 1
			FROM objects o
			JOIN ancestors a ON o.id = a.contained_in_object_id
			WHERE a.depth < %d
		)
		SELECT COALESCE(MAX(depth), 0) FROM ancestors
	`, maxCTERecursionDepth), targetContainerID.String()).Scan(&targetDepth)
	if err != nil {
		return oops.With("operation", "check target depth").With("container_id", targetContainerID.String()).Wrap(err)
	}

	// Query 2: Find the deepest descendant of the object being moved (walk down)
	err = q.QueryRow(ctx, fmt.Sprintf(`
		WITH RECURSIVE descendants AS (
			SELECT id, 0 as depth
			FROM objects WHERE id = $1
			UNION ALL
			SELECT o.id, d.depth + 1
			FROM objects o
			JOIN descendants d ON o.contained_in_object_id = d.id
			WHERE d.depth < %d
		)
		SELECT COALESCE(MAX(depth), 0) FROM descendants
	`, maxCTERecursionDepth), objectID.String()).Scan(&objectSubtreeDepth)
	if err != nil {
		return oops.With("operation", "check object subtree depth").With("object_id", objectID.String()).Wrap(err)
	}

	totalDepth := targetDepth + objectSubtreeDepth + 1
	if totalDepth > r.maxNestingDepth {
		return oops.
			With("operation", "move object").
			With("object_id", objectID.String()).
			With("container_id", targetContainerID.String()).
			With("target_depth", targetDepth).
			With("object_subtree_depth", objectSubtreeDepth).
			With("total_depth", totalDepth).
			With("max_depth", r.maxNestingDepth).
			Errorf("max nesting depth exceeded")
	}

	return nil
}

// objectScanFields holds intermediate scan values for object parsing.
type objectScanFields struct {
	idStr         string
	locationIDStr *string
	heldByStr     *string
	containedIn   *string
	ownerIDStr    *string
}

// scanObjectRow scans a single object from a row.
func scanObjectRow(row pgx.Row) (*world.Object, error) {
	var obj world.Object
	var f objectScanFields

	err := row.Scan(
		&f.idStr, &obj.Name, &obj.Description, &f.locationIDStr, &f.heldByStr,
		&f.containedIn, &obj.IsContainer, &f.ownerIDStr, &obj.CreatedAt,
	)
	if err != nil {
		return nil, oops.With("operation", "scan object").Wrap(err)
	}

	if err := parseObjectFromFields(&f, &obj); err != nil {
		return nil, err
	}

	return &obj, nil
}

// parseObjectFromFields converts scan fields to object fields.
func parseObjectFromFields(f *objectScanFields, obj *world.Object) error {
	var err error
	obj.ID, err = ulid.Parse(f.idStr)
	if err != nil {
		return oops.With("operation", "parse object id").With("id", f.idStr).Wrap(err)
	}
	obj.LocationID, err = parseOptionalULID(f.locationIDStr, "location_id")
	if err != nil {
		return err
	}
	obj.HeldByCharacterID, err = parseOptionalULID(f.heldByStr, "held_by_character_id")
	if err != nil {
		return err
	}
	obj.ContainedInObjectID, err = parseOptionalULID(f.containedIn, "contained_in_object_id")
	if err != nil {
		return err
	}
	obj.OwnerID, err = parseOptionalULID(f.ownerIDStr, "owner_id")
	if err != nil {
		return err
	}
	return nil
}

func scanObjects(rows pgx.Rows) ([]*world.Object, error) {
	objects := make([]*world.Object, 0)
	for rows.Next() {
		var obj world.Object
		var f objectScanFields

		if err := rows.Scan(
			&f.idStr, &obj.Name, &obj.Description, &f.locationIDStr, &f.heldByStr,
			&f.containedIn, &obj.IsContainer, &f.ownerIDStr, &obj.CreatedAt,
		); err != nil {
			return nil, oops.With("operation", "scan object").Wrap(err)
		}

		if err := parseObjectFromFields(&f, &obj); err != nil {
			return nil, err
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
