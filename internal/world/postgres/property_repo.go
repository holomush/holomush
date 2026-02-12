// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// maxVisibilityListSize is the maximum number of entries in visible_to or excluded_from.
const maxVisibilityListSize = 100

// PropertyRepository implements world.PropertyRepository using PostgreSQL.
type PropertyRepository struct {
	pool *pgxpool.Pool
}

// NewPropertyRepository creates a new PropertyRepository.
func NewPropertyRepository(pool *pgxpool.Pool) *PropertyRepository {
	return &PropertyRepository{pool: pool}
}

// Create persists a new entity property.
func (r *PropertyRepository) Create(ctx context.Context, p *world.EntityProperty) error {
	if err := applyVisibilityDefaults(p); err != nil {
		return err
	}
	if err := validateVisibilityLists(p); err != nil {
		return err
	}

	flagsJSON, err := marshalStringSlice(p.Flags)
	if err != nil {
		return oops.Code("PROPERTY_CREATE_FAILED").With("id", p.ID.String()).Wrap(err)
	}
	visibleToJSON, err := marshalNullableStringSlice(p.VisibleTo)
	if err != nil {
		return oops.Code("PROPERTY_CREATE_FAILED").With("id", p.ID.String()).Wrap(err)
	}
	excludedFromJSON, err := marshalNullableStringSlice(p.ExcludedFrom)
	if err != nil {
		return oops.Code("PROPERTY_CREATE_FAILED").With("id", p.ID.String()).Wrap(err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO entity_properties (id, parent_type, parent_id, name, value, owner, visibility, flags, visible_to, excluded_from, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, p.ID.String(), p.ParentType, p.ParentID.String(), p.Name, p.Value, p.Owner,
		p.Visibility, flagsJSON, visibleToJSON, excludedFromJSON, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "entity_properties_parent_name_unique" {
			return oops.Code("PROPERTY_DUPLICATE_NAME").
				With("parent_type", p.ParentType).
				With("parent_id", p.ParentID.String()).
				With("name", p.Name).
				Wrapf(err, "property %q already exists for parent %s/%s", p.Name, p.ParentType, p.ParentID.String())
		}
		return oops.Code("PROPERTY_CREATE_FAILED").With("id", p.ID.String()).Wrap(err)
	}
	return nil
}

// Get retrieves an entity property by ID.
func (r *PropertyRepository) Get(ctx context.Context, id ulid.ULID) (*world.EntityProperty, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, parent_type, parent_id, name, value, owner, visibility, flags, visible_to, excluded_from, created_at, updated_at
		FROM entity_properties WHERE id = $1
	`, id.String())

	prop, err := scanPropertyRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("PROPERTY_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("PROPERTY_GET_FAILED").With("id", id.String()).Wrap(err)
	}
	return prop, nil
}

// ListByParent returns all properties for the given parent entity.
func (r *PropertyRepository) ListByParent(ctx context.Context, parentType string, parentID ulid.ULID) ([]*world.EntityProperty, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, parent_type, parent_id, name, value, owner, visibility, flags, visible_to, excluded_from, created_at, updated_at
		FROM entity_properties WHERE parent_type = $1 AND parent_id = $2
		ORDER BY name
	`, parentType, parentID.String())
	if err != nil {
		return nil, oops.Code("PROPERTY_QUERY_FAILED").
			With("parent_type", parentType).
			With("parent_id", parentID.String()).
			Wrap(err)
	}
	defer rows.Close()

	return scanProperties(rows)
}

// Update modifies an existing entity property.
func (r *PropertyRepository) Update(ctx context.Context, p *world.EntityProperty) error {
	if err := applyVisibilityDefaults(p); err != nil {
		return err
	}
	if err := validateVisibilityLists(p); err != nil {
		return err
	}

	flagsJSON, err := marshalStringSlice(p.Flags)
	if err != nil {
		return oops.Code("PROPERTY_UPDATE_FAILED").With("id", p.ID.String()).Wrap(err)
	}
	visibleToJSON, err := marshalNullableStringSlice(p.VisibleTo)
	if err != nil {
		return oops.Code("PROPERTY_UPDATE_FAILED").With("id", p.ID.String()).Wrap(err)
	}
	excludedFromJSON, err := marshalNullableStringSlice(p.ExcludedFrom)
	if err != nil {
		return oops.Code("PROPERTY_UPDATE_FAILED").With("id", p.ID.String()).Wrap(err)
	}

	result, err := r.pool.Exec(ctx, `
		UPDATE entity_properties
		SET name = $2, value = $3, owner = $4, visibility = $5, flags = $6,
		    visible_to = $7, excluded_from = $8, updated_at = now()
		WHERE id = $1
	`, p.ID.String(), p.Name, p.Value, p.Owner, p.Visibility,
		flagsJSON, visibleToJSON, excludedFromJSON)
	if err != nil {
		return oops.Code("PROPERTY_UPDATE_FAILED").With("id", p.ID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("PROPERTY_NOT_FOUND").With("id", p.ID.String()).Wrap(world.ErrNotFound)
	}
	return nil
}

// Delete removes an entity property by ID.
func (r *PropertyRepository) Delete(ctx context.Context, id ulid.ULID) error {
	result, err := execerFromCtx(ctx, r.pool).Exec(ctx, `DELETE FROM entity_properties WHERE id = $1`, id.String())
	if err != nil {
		return oops.Code("PROPERTY_DELETE_FAILED").With("id", id.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("PROPERTY_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound)
	}
	return nil
}

// DeleteByParent removes all properties for the given parent entity.
func (r *PropertyRepository) DeleteByParent(ctx context.Context, parentType string, parentID ulid.ULID) error {
	_, err := execerFromCtx(ctx, r.pool).Exec(ctx, `
		DELETE FROM entity_properties WHERE parent_type = $1 AND parent_id = $2
	`, parentType, parentID.String())
	if err != nil {
		return oops.Code("PROPERTY_DELETE_FAILED").
			With("parent_type", parentType).
			With("parent_id", parentID.String()).
			Wrap(err)
	}
	return nil
}

// applyVisibilityDefaults sets default visible_to and excluded_from for restricted visibility.
// Per spec (03-property-model.md), visible_to defaults to [parent_id] to prevent "nobody can see it".
func applyVisibilityDefaults(p *world.EntityProperty) error {
	if p.Visibility != "restricted" {
		return nil
	}
	if p.VisibleTo == nil {
		p.VisibleTo = []string{p.ParentID.String()}
	}
	if p.ExcludedFrom == nil {
		p.ExcludedFrom = []string{}
	}
	return nil
}

// validateVisibilityLists checks visibility list constraints.
func validateVisibilityLists(p *world.EntityProperty) error {
	if p.Visibility != "restricted" {
		if len(p.VisibleTo) > 0 {
			return oops.Code("PROPERTY_INVALID_VISIBILITY").
				With("visibility", p.Visibility).
				With("field", "visible_to").
				Errorf("visible_to must be empty for non-restricted visibility")
		}
		if len(p.ExcludedFrom) > 0 {
			return oops.Code("PROPERTY_INVALID_VISIBILITY").
				With("visibility", p.Visibility).
				With("field", "excluded_from").
				Errorf("excluded_from must be empty for non-restricted visibility")
		}
		return nil
	}
	if len(p.VisibleTo) > maxVisibilityListSize {
		return oops.Code("PROPERTY_VISIBLE_TO_LIMIT").
			With("count", len(p.VisibleTo)).
			With("max", maxVisibilityListSize).
			Errorf("visible_to exceeds maximum of %d entries", maxVisibilityListSize)
	}
	if len(p.ExcludedFrom) > maxVisibilityListSize {
		return oops.Code("PROPERTY_EXCLUDED_FROM_LIMIT").
			With("count", len(p.ExcludedFrom)).
			With("max", maxVisibilityListSize).
			Errorf("excluded_from exceeds maximum of %d entries", maxVisibilityListSize)
	}

	// Check for overlap
	visibleSet := make(map[string]struct{}, len(p.VisibleTo))
	for _, v := range p.VisibleTo {
		visibleSet[v] = struct{}{}
	}
	for _, e := range p.ExcludedFrom {
		if _, ok := visibleSet[e]; ok {
			return oops.Code("PROPERTY_VISIBILITY_OVERLAP").
				With("overlapping", e).
				Errorf("visible_to and excluded_from must not overlap")
		}
	}
	return nil
}

// propertyScanFields holds intermediate scan values for property parsing.
type propertyScanFields struct {
	idStr       string
	parentIDStr string
	flagsJSON   []byte
	visibleTo   []byte
	excludedFr  []byte
}

// scanPropertyRow scans a single property from a row.
func scanPropertyRow(row pgx.Row) (*world.EntityProperty, error) {
	var prop world.EntityProperty
	var f propertyScanFields

	err := row.Scan(
		&f.idStr, &prop.ParentType, &f.parentIDStr, &prop.Name, &prop.Value, &prop.Owner,
		&prop.Visibility, &f.flagsJSON, &f.visibleTo, &f.excludedFr, &prop.CreatedAt, &prop.UpdatedAt,
	)
	if err != nil {
		return nil, oops.Code("PROPERTY_SCAN_FAILED").Wrap(err)
	}

	if err := parsePropertyFromFields(&f, &prop); err != nil {
		return nil, err
	}

	return &prop, nil
}

// parsePropertyFromFields converts scan fields to property fields.
func parsePropertyFromFields(f *propertyScanFields, prop *world.EntityProperty) error {
	var err error
	prop.ID, err = ulid.Parse(f.idStr)
	if err != nil {
		return oops.Code("PROPERTY_PARSE_FAILED").With("field", "id").With("value", f.idStr).Wrap(err)
	}
	prop.ParentID, err = ulid.Parse(f.parentIDStr)
	if err != nil {
		return oops.Code("PROPERTY_PARSE_FAILED").With("field", "parent_id").With("value", f.parentIDStr).Wrap(err)
	}

	prop.Flags, err = unmarshalStringSlice(f.flagsJSON)
	if err != nil {
		return oops.Code("PROPERTY_PARSE_FAILED").With("field", "flags").Wrap(err)
	}
	prop.VisibleTo, err = unmarshalNullableStringSlice(f.visibleTo)
	if err != nil {
		return oops.Code("PROPERTY_PARSE_FAILED").With("field", "visible_to").Wrap(err)
	}
	prop.ExcludedFrom, err = unmarshalNullableStringSlice(f.excludedFr)
	if err != nil {
		return oops.Code("PROPERTY_PARSE_FAILED").With("field", "excluded_from").Wrap(err)
	}

	return nil
}

func scanProperties(rows pgx.Rows) ([]*world.EntityProperty, error) {
	properties := make([]*world.EntityProperty, 0)
	for rows.Next() {
		var prop world.EntityProperty
		var f propertyScanFields

		if err := rows.Scan(
			&f.idStr, &prop.ParentType, &f.parentIDStr, &prop.Name, &prop.Value, &prop.Owner,
			&prop.Visibility, &f.flagsJSON, &f.visibleTo, &f.excludedFr, &prop.CreatedAt, &prop.UpdatedAt,
		); err != nil {
			return nil, oops.Code("PROPERTY_SCAN_FAILED").Wrap(err)
		}

		if err := parsePropertyFromFields(&f, &prop); err != nil {
			return nil, err
		}

		properties = append(properties, &prop)
	}

	if err := rows.Err(); err != nil {
		return nil, oops.Code("PROPERTY_ITERATE_FAILED").Wrap(err)
	}

	return properties, nil
}

// marshalStringSlice marshals a string slice to JSON bytes.
func marshalStringSlice(s []string) ([]byte, error) {
	if s == nil {
		s = []string{}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, oops.With("operation", "marshal string slice").Wrap(err)
	}
	return b, nil
}

// marshalNullableStringSlice marshals a nullable string slice to JSON bytes.
// Returns nil for nil input (SQL NULL).
func marshalNullableStringSlice(s []string) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, oops.With("operation", "marshal nullable string slice").Wrap(err)
	}
	return b, nil
}

// unmarshalStringSlice unmarshals JSON bytes into a string slice.
// Returns empty slice for nil or empty input.
func unmarshalStringSlice(data []byte) ([]string, error) {
	if len(data) == 0 {
		return []string{}, nil
	}
	var result []string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, oops.With("operation", "unmarshal string slice").Wrap(err)
	}
	if result == nil {
		return []string{}, nil
	}
	return result, nil
}

// unmarshalNullableStringSlice unmarshals JSON bytes into a nullable string slice.
// Returns nil for nil input (SQL NULL).
func unmarshalNullableStringSlice(data []byte) ([]string, error) {
	if data == nil {
		return nil, nil
	}
	var result []string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, oops.With("operation", "unmarshal nullable string slice").Wrap(err)
	}
	return result, nil
}

// Compile-time interface check.
var _ world.PropertyRepository = (*PropertyRepository)(nil)
