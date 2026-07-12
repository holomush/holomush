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

	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// LocationRepository implements world.LocationRepository using PostgreSQL.
type LocationRepository struct {
	pool *pgxpool.Pool
}

// NewLocationRepository creates a new LocationRepository.
func NewLocationRepository(pool *pgxpool.Pool) *LocationRepository {
	return &LocationRepository{pool: pool}
}

// Get retrieves a location by ID.
func (r *LocationRepository) Get(ctx context.Context, id ulid.ULID) (*world.Location, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at, version
		FROM locations WHERE id = $1
	`, id.String())
	loc, err := scanLocationRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("LOCATION_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "get location").With("id", id.String()).Wrap(err)
	}
	return loc, nil
}

// Create persists a new location.
// Callers must validate the location before calling this method.
// The struct's Version is refreshed to the DB-assigned initial version (1) so a
// reused struct does not later carry a stale version and spuriously conflict
// (finding 12).
func (r *LocationRepository) Create(ctx context.Context, loc *world.Location) (*wmodel.MutationDelta, error) {
	var archivedAt *pgnanos.Time
	if loc.ArchivedAt != nil {
		t := pgnanos.From(*loc.ArchivedAt)
		archivedAt = &t
	}
	var newVersion int
	err := querierFromCtx(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO locations (id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING version
	`, loc.ID.String(), loc.Type, ulidToStringPtr(loc.ShadowsID), loc.Name, loc.Description,
		ulidToStringPtr(loc.OwnerID), loc.ReplayPolicy, pgnanos.From(loc.CreatedAt), archivedAt).Scan(&newVersion)
	if err != nil {
		return nil, oops.With("operation", "create location").With("id", loc.ID.String()).Wrap(err)
	}
	loc.Version = newVersion
	return primaryDeltaVersioned(wmodel.AggregateLocation, loc.ID, false, 0, newVersion), nil
}

// Update modifies an existing location with a version-predicated CAS (MODEL-03).
// Callers must validate the location before calling this method.
//
// When loc.Version > 0 the UPDATE's WHERE clause matches both id and version, so
// a stale writer affects zero rows; a locked follow-up read on the same
// connection then classifies the zero-row result into WORLD_CONCURRENT_EDIT (the
// row exists with a different version) or LOCATION_NOT_FOUND (the row is absent).
// When loc.Version == 0 the write is unversioned (id-only) for callers that have
// not yet threaded a read version. On success loc.Version is refreshed to the
// committed value (finding 12).
func (r *LocationRepository) Update(ctx context.Context, loc *world.Location) (*wmodel.MutationDelta, error) {
	var archivedAt *pgnanos.Time
	if loc.ArchivedAt != nil {
		t := pgnanos.From(*loc.ArchivedAt)
		archivedAt = &t
	}

	query := `
		UPDATE locations SET type = $2, shadows_id = $3, name = $4, description = $5,
		owner_id = $6, replay_policy = $7, archived_at = $8, version = version + 1
		WHERE id = $1`
	args := []any{loc.ID.String(), loc.Type, ulidToStringPtr(loc.ShadowsID), loc.Name, loc.Description,
		ulidToStringPtr(loc.OwnerID), loc.ReplayPolicy, archivedAt}
	if loc.Version > 0 {
		query += ` AND version = $9`
		args = append(args, loc.Version)
	}
	query += ` RETURNING version`

	var delta *wmodel.MutationDelta
	txErr := withTx(ctx, r.pool, func(txCtx context.Context) error {
		tx := txFromContext(txCtx)
		var newVersion int
		err := tx.QueryRow(txCtx, query, args...).Scan(&newVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return classifyCASZeroRow(txCtx, tx,
				`SELECT version FROM locations WHERE id = $1 FOR UPDATE`,
				loc.ID,
				oops.Code("LOCATION_NOT_FOUND").With("id", loc.ID.String()).Wrap(world.ErrNotFound))
		}
		if err != nil {
			return oops.With("operation", "update location").With("id", loc.ID.String()).Wrap(err)
		}
		loc.Version = newVersion
		delta = primaryDeltaVersioned(wmodel.AggregateLocation, loc.ID, false, newVersion-1, newVersion)
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return delta, nil
}

// Delete removes a location by ID with a version-predicated CAS (MODEL-03) and a
// cascade-aware MutationDelta.
//
// Ordering is load-bearing (round-6 R6-4): inside the same transaction the method
// FIRST locks the parent location row with SELECT version ... FOR UPDATE. That
// lock is the existence check, the version read, AND the FK phantom fence —
// holding it conflicts with the FK key-share lock a concurrent child-exit INSERT
// must take on the parent, so a referencing-exit insert either committed before
// the lock (and is caught by the preselect below) or blocks and then fails once
// the parent row is gone. THEN it preselects, under lock, every exit whose
// from_location_id OR to_location_id equals the deleted location (the exits FK
// cascades on BOTH, 000001_baseline.up.sql:115-116) and records each as a
// tombstone AffectedAggregate, so the outbox manifest reflects every row the DB
// cascade removes (INV-WORLD-2 delta-parity, finding 4). THEN it deletes the
// parent.
//
// The zero-row classifier is TWO outcomes only (round-5 Codex MEDIUM): an absent
// row → LOCATION_NOT_FOUND (a concurrent delete that already committed is
// correctly observed as not-found); an existing row whose version differs from a
// non-zero expectedVersion → WORLD_CONCURRENT_EDIT. expectedVersion == 0 is an
// unversioned delete (existence-checked only).
func (r *LocationRepository) Delete(ctx context.Context, id ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error) {
	var delta *wmodel.MutationDelta
	txErr := withTx(ctx, r.pool, func(txCtx context.Context) error {
		tx := txFromContext(txCtx)

		// 1. Lock the parent location row FIRST (round-6 R6-4): existence check +
		// version read + FK child-insert phantom fence.
		var currentVersion int
		err := tx.QueryRow(txCtx, `SELECT version FROM locations WHERE id = $1 FOR UPDATE`, id.String()).Scan(&currentVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("LOCATION_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
		}
		if err != nil {
			return oops.With("operation", "lock location for delete").With("id", id.String()).Wrap(err)
		}
		if expectedVersion > 0 && currentVersion != expectedVersion {
			return oops.Code(world.CodeConcurrentEdit).
				With("id", id.String()).
				With("expected_version", expectedVersion).
				With("current_version", currentVersion).
				Wrap(world.ErrConcurrentEdit)
		}

		// 2. Preselect the cascaded exits UNDER LOCK so the delta covers every row
		// the FK cascade will remove.
		affected, err := preselectCascadedExits(txCtx, tx, id)
		if err != nil {
			return err
		}

		// 3. Delete the parent; the FK cascade removes the preselected exits.
		if _, err := tx.Exec(txCtx, `DELETE FROM locations WHERE id = $1`, id.String()); err != nil {
			return oops.With("operation", "delete location").With("id", id.String()).Wrap(err)
		}

		delta = primaryDeltaVersioned(wmodel.AggregateLocation, id, true, currentVersion, 0)
		delta.Affected = affected
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return delta, nil
}

// preselectCascadedExits reads, under FOR UPDATE lock, every exit the location
// FK cascade will remove (exits reference locations on BOTH from_location_id and
// to_location_id with ON DELETE CASCADE, 000001_baseline.up.sql:115-116) and
// returns them as tombstone AffectedAggregates carrying id + current version, so
// the location DELETE's MutationDelta accounts for the DB-cascaded children
// (INV-WORLD-2 delta-parity, finding 4).
func preselectCascadedExits(ctx context.Context, tx pgx.Tx, locationID ulid.ULID) ([]wmodel.AffectedAggregate, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, version FROM exits
		WHERE from_location_id = $1 OR to_location_id = $1
		FOR UPDATE
	`, locationID.String())
	if err != nil {
		return nil, oops.With("operation", "preselect cascaded exits").With("location_id", locationID.String()).Wrap(err)
	}
	defer rows.Close()

	var affected []wmodel.AffectedAggregate
	for rows.Next() {
		var idStr string
		var version int
		if err := rows.Scan(&idStr, &version); err != nil {
			return nil, oops.With("operation", "scan cascaded exit").Wrap(err)
		}
		exitID, err := ulid.Parse(idStr)
		if err != nil {
			return nil, oops.With("operation", "parse cascaded exit id").With("id", idStr).Wrap(err)
		}
		affected = append(affected, wmodel.AffectedAggregate{
			Type:          wmodel.AggregateExit,
			ID:            exitID,
			Tombstone:     true,
			BeforeVersion: version,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate cascaded exits").With("location_id", locationID.String()).Wrap(err)
	}
	return affected, nil
}

// ListByType returns all locations of the given type.
func (r *LocationRepository) ListByType(ctx context.Context, locType world.LocationType) ([]*world.Location, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at, version
		FROM locations WHERE type = $1 ORDER BY created_at DESC, id DESC
	`, string(locType)) // tiebreaker for sub-ns insert collisions across dual-clock writers (holomush-gfo6.33)
	if err != nil {
		return nil, oops.With("operation", "list locations by type").With("type", string(locType)).Wrap(err)
	}
	defer rows.Close()

	return scanLocations(rows)
}

// GetShadowedBy returns scenes that shadow the given location.
func (r *LocationRepository) GetShadowedBy(ctx context.Context, id ulid.ULID) ([]*world.Location, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at, version
		FROM locations WHERE shadows_id = $1 ORDER BY created_at DESC, id DESC
	`, id.String()) // tiebreaker for sub-ns insert collisions across dual-clock writers (holomush-gfo6.33)
	if err != nil {
		return nil, oops.With("operation", "get shadowed by").With("id", id.String()).Wrap(err)
	}
	defer rows.Close()

	return scanLocations(rows)
}

// FindByName searches for a location by exact name match.
// Returns ErrNotFound if no location matches.
func (r *LocationRepository) FindByName(ctx context.Context, name string) (*world.Location, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at, version
		FROM locations WHERE name = $1
	`, name)
	loc, err := scanLocationRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("LOCATION_NOT_FOUND").With("name", name).Wrap(world.ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "find location by name").With("name", name).Wrap(err)
	}
	return loc, nil
}

// locationScanFields holds intermediate scan values for location parsing.
type locationScanFields struct {
	idStr        string
	shadowsIDStr *string
	ownerIDStr   *string
	createdAt    pgnanos.Time
	archivedAt   *pgnanos.Time
}

// scanLocationRow scans a single location from a row.
func scanLocationRow(row pgx.Row) (*world.Location, error) {
	var loc world.Location
	var f locationScanFields

	err := row.Scan(
		&f.idStr, &loc.Type, &f.shadowsIDStr, &loc.Name, &loc.Description,
		&f.ownerIDStr, &loc.ReplayPolicy, &f.createdAt, &f.archivedAt, &loc.Version,
	)
	if err != nil {
		return nil, oops.With("operation", "scan location").Wrap(err)
	}

	if err := parseLocationFromFields(&f, &loc); err != nil {
		return nil, err
	}

	return &loc, nil
}

// parseLocationFromFields converts scan fields to location fields.
func parseLocationFromFields(f *locationScanFields, loc *world.Location) error {
	var err error
	loc.ID, err = ulid.Parse(f.idStr)
	if err != nil {
		return oops.With("operation", "parse location id").With("id", f.idStr).Wrap(err)
	}
	loc.ShadowsID, err = parseOptionalULID(f.shadowsIDStr, "shadows_id")
	if err != nil {
		return err
	}
	loc.OwnerID, err = parseOptionalULID(f.ownerIDStr, "owner_id")
	if err != nil {
		return err
	}
	loc.CreatedAt = f.createdAt.Time()
	if f.archivedAt != nil {
		t := f.archivedAt.Time()
		loc.ArchivedAt = &t
	}
	return nil
}

func scanLocations(rows pgx.Rows) ([]*world.Location, error) {
	locations := make([]*world.Location, 0)
	for rows.Next() {
		var loc world.Location
		var f locationScanFields

		if err := rows.Scan(
			&f.idStr, &loc.Type, &f.shadowsIDStr, &loc.Name, &loc.Description,
			&f.ownerIDStr, &loc.ReplayPolicy, &f.createdAt, &f.archivedAt, &loc.Version,
		); err != nil {
			return nil, oops.With("operation", "scan location").Wrap(err)
		}

		if err := parseLocationFromFields(&f, &loc); err != nil {
			return nil, err
		}

		locations = append(locations, &loc)
	}

	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate locations").Wrap(err)
	}

	return locations, nil
}

// Compile-time interface check.
var _ world.LocationRepository = (*LocationRepository)(nil)
