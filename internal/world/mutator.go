// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"

	"github.com/oklog/ulid/v2"
)

// Mutator extends the read operations with write operations.
// This interface represents the full set of authorized world operations
// available to plugins that need to modify world state.
//
// All methods accept a subjectID parameter for ABAC authorization.
// Plugins typically use "system:plugin:<name>" as their subject ID.
type Mutator interface {
	// Read operations (from Service)

	// GetLocation retrieves a location by ID after checking read authorization.
	GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*Location, error)

	// GetCharacter retrieves a character by ID after checking read authorization.
	GetCharacter(ctx context.Context, subjectID string, id ulid.ULID) (*Character, error)

	// GetCharactersByLocation retrieves characters at a location with pagination
	// after checking read authorization.
	GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts ListOptions) ([]*Character, error)

	// GetObject retrieves an object by ID after checking read authorization.
	GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*Object, error)

	// Write operations

	// CreateLocation creates a new location after checking write authorization.
	CreateLocation(ctx context.Context, subjectID string, loc *Location) error

	// CreateExit creates a new exit between locations after checking write authorization.
	CreateExit(ctx context.Context, subjectID string, exit *Exit) error

	// CreateObject creates a new object with the given containment after checking write authorization.
	CreateObject(ctx context.Context, subjectID string, obj *Object) error

	// UpdateLocation updates an existing location after checking write authorization.
	UpdateLocation(ctx context.Context, subjectID string, loc *Location) error

	// UpdateObject updates an existing object after checking write authorization.
	UpdateObject(ctx context.Context, subjectID string, obj *Object) error

	// FindLocationByName searches for a location by name after checking read authorization.
	// Returns ErrNotFound if no location matches.
	FindLocationByName(ctx context.Context, subjectID, name string) (*Location, error)
}

// Compile-time check that Service implements Mutator.
var _ Mutator = (*Service)(nil)
