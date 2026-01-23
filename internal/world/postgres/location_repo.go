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
	var loc world.Location
	var idStr string
	var shadowsIDStr *string
	var ownerIDStr *string

	err := r.pool.QueryRow(ctx, `
		SELECT id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at
		FROM locations WHERE id = $1
	`, id.String()).Scan(
		&idStr, &loc.Type, &shadowsIDStr, &loc.Name, &loc.Description,
		&ownerIDStr, &loc.ReplayPolicy, &loc.CreatedAt, &loc.ArchivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.With("id", id.String()).Wrap(ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "get location").With("id", id.String()).Wrap(err)
	}

	loc.ID = ulid.MustParse(idStr)
	if shadowsIDStr != nil {
		sid := ulid.MustParse(*shadowsIDStr)
		loc.ShadowsID = &sid
	}
	if ownerIDStr != nil {
		oid := ulid.MustParse(*ownerIDStr)
		loc.OwnerID = &oid
	}

	return &loc, nil
}

// Create persists a new location.
func (r *LocationRepository) Create(ctx context.Context, loc *world.Location) error {
	var shadowsID, ownerID *string
	if loc.ShadowsID != nil {
		s := loc.ShadowsID.String()
		shadowsID = &s
	}
	if loc.OwnerID != nil {
		o := loc.OwnerID.String()
		ownerID = &o
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO locations (id, type, shadows_id, name, description, owner_id, replay_policy, created_at, archived_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, loc.ID.String(), loc.Type, shadowsID, loc.Name, loc.Description,
		ownerID, loc.ReplayPolicy, loc.CreatedAt, loc.ArchivedAt)
	if err != nil {
		return oops.With("operation", "create location").With("id", loc.ID.String()).Wrap(err)
	}
	return nil
}

// Update modifies an existing location.
func (r *LocationRepository) Update(ctx context.Context, loc *world.Location) error {
	var shadowsID, ownerID *string
	if loc.ShadowsID != nil {
		s := loc.ShadowsID.String()
		shadowsID = &s
	}
	if loc.OwnerID != nil {
		o := loc.OwnerID.String()
		ownerID = &o
	}

	result, err := r.pool.Exec(ctx, `
		UPDATE locations SET type = $2, shadows_id = $3, name = $4, description = $5,
		owner_id = $6, replay_policy = $7, archived_at = $8
		WHERE id = $1
	`, loc.ID.String(), loc.Type, shadowsID, loc.Name, loc.Description,
		ownerID, loc.ReplayPolicy, loc.ArchivedAt)
	if err != nil {
		return oops.With("operation", "update location").With("id", loc.ID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.With("id", loc.ID.String()).Wrap(ErrNotFound)
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
		return oops.With("id", id.String()).Wrap(ErrNotFound)
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

func scanLocations(rows pgx.Rows) ([]*world.Location, error) {
	var locations []*world.Location
	for rows.Next() {
		var loc world.Location
		var idStr string
		var shadowsIDStr, ownerIDStr *string

		if err := rows.Scan(
			&idStr, &loc.Type, &shadowsIDStr, &loc.Name, &loc.Description,
			&ownerIDStr, &loc.ReplayPolicy, &loc.CreatedAt, &loc.ArchivedAt,
		); err != nil {
			return nil, oops.With("operation", "scan location").Wrap(err)
		}

		loc.ID = ulid.MustParse(idStr)
		if shadowsIDStr != nil {
			sid := ulid.MustParse(*shadowsIDStr)
			loc.ShadowsID = &sid
		}
		if ownerIDStr != nil {
			oid := ulid.MustParse(*ownerIDStr)
			loc.OwnerID = &oid
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
