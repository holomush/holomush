// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"

	"github.com/oklog/ulid/v2"
)

// LocationRepository manages location persistence.
type LocationRepository interface {
	// Get retrieves a location by ID.
	Get(ctx context.Context, id ulid.ULID) (*Location, error)

	// Create persists a new location.
	Create(ctx context.Context, loc *Location) error

	// Update modifies an existing location.
	Update(ctx context.Context, loc *Location) error

	// Delete removes a location by ID.
	Delete(ctx context.Context, id ulid.ULID) error

	// ListByType returns all locations of the given type.
	ListByType(ctx context.Context, locType LocationType) ([]*Location, error)

	// GetShadowedBy returns scenes that shadow the given location.
	GetShadowedBy(ctx context.Context, id ulid.ULID) ([]*Location, error)
}

// ExitRepository manages exit persistence.
type ExitRepository interface {
	// Get retrieves an exit by ID.
	Get(ctx context.Context, id ulid.ULID) (*Exit, error)

	// Create persists a new exit.
	// If bidirectional, also creates the return exit.
	Create(ctx context.Context, exit *Exit) error

	// Update modifies an existing exit.
	Update(ctx context.Context, exit *Exit) error

	// Delete removes an exit by ID.
	// If bidirectional, deletes the return exit atomically in a single transaction.
	//
	// When the primary exit deletion succeeds but return exit cleanup has issues,
	// returns a *BidirectionalCleanupResult (which implements error). Callers can
	// type-assert to access cleanup details:
	//
	//     var cleanupErr *world.BidirectionalCleanupResult
	//     if errors.As(err, &cleanupErr) && !cleanupErr.IsSevere() {
	//         // Non-severe: return exit already deleted, operation succeeded
	//     }
	//
	// Returns:
	//   - nil on success
	//   - ErrNotFound if exit doesn't exist
	//   - *BidirectionalCleanupResult for partial cleanup issues
	//   - Other errors for database failures
	Delete(ctx context.Context, id ulid.ULID) error

	// ListFromLocation returns all exits from a location.
	ListFromLocation(ctx context.Context, locationID ulid.ULID) ([]*Exit, error)

	// FindByName finds an exit by name or alias from a location.
	FindByName(ctx context.Context, locationID ulid.ULID, name string) (*Exit, error)

	// FindBySimilarity finds an exit by name using fuzzy matching (pg_trgm).
	// Returns the best match above the similarity threshold, or ErrNotFound.
	FindBySimilarity(ctx context.Context, locationID ulid.ULID, name string, threshold float64) (*Exit, error)

	// ListVisibleExits returns exits from a location that are visible to a character.
	// The visibility check is atomic - the location owner is fetched and compared in a single query.
	// This prevents TOCTOU issues where the owner could change between lookup and check.
	ListVisibleExits(ctx context.Context, locationID, characterID ulid.ULID) ([]*Exit, error)
}

// ObjectRepository manages object persistence.
type ObjectRepository interface {
	// Get retrieves an object by ID.
	Get(ctx context.Context, id ulid.ULID) (*Object, error)

	// Create persists a new object.
	Create(ctx context.Context, obj *Object) error

	// Update modifies an existing object.
	Update(ctx context.Context, obj *Object) error

	// Delete removes an object by ID.
	Delete(ctx context.Context, id ulid.ULID) error

	// ListAtLocation returns all objects at a location.
	ListAtLocation(ctx context.Context, locationID ulid.ULID) ([]*Object, error)

	// ListHeldBy returns all objects held by a character.
	ListHeldBy(ctx context.Context, characterID ulid.ULID) ([]*Object, error)

	// ListContainedIn returns all objects inside a container object.
	ListContainedIn(ctx context.Context, objectID ulid.ULID) ([]*Object, error)

	// Move changes an object's containment.
	// Validates containment and enforces business rules.
	Move(ctx context.Context, objectID ulid.ULID, to Containment) error
}

// SceneRepository manages scene-specific operations.
type SceneRepository interface {
	// AddParticipant adds a character to a scene.
	AddParticipant(ctx context.Context, sceneID, characterID ulid.ULID, role ParticipantRole) error

	// RemoveParticipant removes a character from a scene.
	RemoveParticipant(ctx context.Context, sceneID, characterID ulid.ULID) error

	// ListParticipants returns all participants in a scene.
	ListParticipants(ctx context.Context, sceneID ulid.ULID) ([]SceneParticipant, error)

	// GetScenesFor returns all scenes a character is participating in.
	GetScenesFor(ctx context.Context, characterID ulid.ULID) ([]*Location, error)
}

// SceneParticipant represents a character's membership in a scene.
type SceneParticipant struct {
	CharacterID ulid.ULID
	Role        ParticipantRole
}

// CharacterRepository defines operations for character persistence.
type CharacterRepository interface {
	// Get retrieves a character by ID.
	Get(ctx context.Context, id ulid.ULID) (*Character, error)

	// GetByLocation retrieves all characters at a location.
	GetByLocation(ctx context.Context, locationID ulid.ULID) ([]*Character, error)

	// UpdateLocation moves a character to a new location.
	UpdateLocation(ctx context.Context, characterID ulid.ULID, locationID *ulid.ULID) error
}
