// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// EntityProperty is a first-class property attached to a world entity.
// Properties have their own identity, ownership, and access control attributes.
// See docs/specs/abac/03-property-model.md for the full specification.
type EntityProperty struct {
	ID           ulid.ULID
	ParentType   string // "character", "location", "object"
	ParentID     ulid.ULID
	Name         string
	Value        *string // NULL for flag-style properties
	Owner        *string
	Visibility   string // "public", "private", "restricted", "system", "admin"
	Flags        []string
	VisibleTo    []string
	ExcludedFrom []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PropertyRepository manages entity properties.
type PropertyRepository interface {
	// Create persists a new entity property.
	// Enforces visibility defaults: when visibility is "restricted" and VisibleTo
	// is nil, auto-populates VisibleTo with [owner] and ExcludedFrom with [].
	Create(ctx context.Context, p *EntityProperty) error

	// Get retrieves an entity property by ID.
	// Returns PROPERTY_NOT_FOUND error if not found.
	Get(ctx context.Context, id ulid.ULID) (*EntityProperty, error)

	// ListByParent returns all properties for the given parent entity.
	ListByParent(ctx context.Context, parentType string, parentID ulid.ULID) ([]*EntityProperty, error)

	// Update modifies an existing entity property.
	// Applies the same visibility validation as Create.
	Update(ctx context.Context, p *EntityProperty) error

	// Delete removes an entity property by ID.
	// Returns PROPERTY_NOT_FOUND error if not found.
	Delete(ctx context.Context, id ulid.ULID) error

	// DeleteByParent removes all properties for the given parent entity.
	// Returns nil even if no properties exist for the parent.
	DeleteByParent(ctx context.Context, parentType string, parentID ulid.ULID) error
}
