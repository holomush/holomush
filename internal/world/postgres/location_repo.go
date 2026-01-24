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
		SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at
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
func (r *LocationRepository) Create(ctx context.Context, loc *world.Location) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO locations (id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, loc.ID.String(), loc.Type, ulidToStringPtr(loc.ShadowsID), loc.Name, loc.Description,
		ulidToStringPtr(loc.OwnerID), loc.ReplayPolicy, loc.CreatedAt, loc.ArchivedAt)
	if err != nil {
		return oops.With("operation", "create location").With("id", loc.ID.String()).Wrap(err)
	}
	return nil
}

// Update modifies an existing location.
// Callers must validate the location before calling this method.
func (r *LocationRepository) Update(ctx context.Context, loc *world.Location) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE locations SET type = $2, shadows_id = $3, name = $4, description = $5,
		owner_id = $6, replay_policy = $7, archived_at = $8
		WHERE id = $1
	`, loc.ID.String(), loc.Type, ulidToStringPtr(loc.ShadowsID), loc.Name, loc.Description,
		ulidToStringPtr(loc.OwnerID), loc.ReplayPolicy, loc.ArchivedAt)
	if err != nil {
		return oops.With("operation", "update location").With("id", loc.ID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("LOCATION_NOT_FOUND").With("id", loc.ID.String()).Wrap(world.ErrNotFound)
	}
	return nil
}

// Delete removes a location by ID.
func (r *LocationRepository) Delete(ctx context.Context, id ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, id.String())
	if err != nil {
		return oops.With("operation", "delete location").With("id", id.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("LOCATION_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	return nil
}

// ListByType returns all locations of the given type.
func (r *LocationRepository) ListByType(ctx context.Context, locType world.LocationType) ([]*world.Location, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at
		FROM locations WHERE type = $1 ORDER BY created_at DESC
	`, string(locType))
	if err != nil {
		return nil, oops.With("operation", "list locations by type").With("type", string(locType)).Wrap(err)
	}
	defer rows.Close()

	return scanLocations(rows)
}

// GetShadowedBy returns scenes that shadow the given location.
func (r *LocationRepository) GetShadowedBy(ctx context.Context, id ulid.ULID) ([]*world.Location, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at
		FROM locations WHERE shadows_id = $1 ORDER BY created_at DESC
	`, id.String())
	if err != nil {
		return nil, oops.With("operation", "get shadowed by").With("id", id.String()).Wrap(err)
	}
	defer rows.Close()

	return scanLocations(rows)
}

// locationScanFields holds intermediate scan values for location parsing.
type locationScanFields struct {
	idStr        string
	shadowsIDStr *string
	ownerIDStr   *string
}

// scanLocationRow scans a single location from a row.
func scanLocationRow(row pgx.Row) (*world.Location, error) {
	var loc world.Location
	var f locationScanFields

	err := row.Scan(
		&f.idStr, &loc.Type, &f.shadowsIDStr, &loc.Name, &loc.Description,
		&f.ownerIDStr, &loc.ReplayPolicy, &loc.CreatedAt, &loc.ArchivedAt,
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
	return nil
}

func scanLocations(rows pgx.Rows) ([]*world.Location, error) {
	locations := make([]*world.Location, 0)
	for rows.Next() {
		var loc world.Location
		var f locationScanFields

		if err := rows.Scan(
			&f.idStr, &loc.Type, &f.shadowsIDStr, &loc.Name, &loc.Description,
			&f.ownerIDStr, &loc.ReplayPolicy, &loc.CreatedAt, &loc.ArchivedAt,
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
