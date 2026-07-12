// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// DefaultLimit is the default number of results when ListOptions.Limit is 0.
const DefaultLimit = 100

// ListOptions configures pagination for list operations.
type ListOptions struct {
	Limit  int // Maximum results to return (0 = use DefaultLimit)
	Offset int // Number of results to skip
}

// LocationReader is the read-only view of location persistence. The compile-time
// write fence (05-06/05-11) gives world.Service a reader plus a write executor so
// an envelope-less direct write does not type-check.
type LocationReader interface {
	// Get retrieves a location by ID.
	Get(ctx context.Context, id ulid.ULID) (*Location, error)

	// ListByType returns all locations of the given type.
	ListByType(ctx context.Context, locType LocationType) ([]*Location, error)

	// GetShadowedBy returns scenes that shadow the given location.
	GetShadowedBy(ctx context.Context, id ulid.ULID) ([]*Location, error)

	// FindByName searches for a location by exact name match.
	// Returns ErrNotFound if no location matches.
	FindByName(ctx context.Context, name string) (*Location, error)
}

// LocationRepository manages location persistence.
//
// Write methods return a *wmodel.MutationDelta reporting the rows the write
// touched (05-14 populates a primary-only delta; the version predicate and real
// before/after population land in 05-02/05-03).
type LocationRepository interface {
	LocationReader

	// Create persists a new location.
	Create(ctx context.Context, loc *Location) (*wmodel.MutationDelta, error)

	// Update modifies an existing location.
	Update(ctx context.Context, loc *Location) (*wmodel.MutationDelta, error)

	// Delete removes a location by ID.
	// expectedVersion is the optimistic-concurrency guard (accepted and ignored
	// in 05-14; the version predicate lands in 05-02/05-03).
	Delete(ctx context.Context, id ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error)
}

// ExitReader is the read-only view of exit persistence.
type ExitReader interface {
	// Get retrieves an exit by ID.
	Get(ctx context.Context, id ulid.ULID) (*Exit, error)

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

// ExitRepository manages exit persistence.
type ExitRepository interface {
	ExitReader

	// Create persists a new exit.
	// If bidirectional, also creates the return exit.
	Create(ctx context.Context, exit *Exit) (*wmodel.MutationDelta, error)

	// Update modifies an existing exit.
	Update(ctx context.Context, exit *Exit) (*wmodel.MutationDelta, error)

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
	// expectedVersion is the optimistic-concurrency guard (accepted and ignored
	// in 05-14).
	//
	// Returns:
	//   - (delta, nil) on success
	//   - ErrNotFound if exit doesn't exist
	//   - *BidirectionalCleanupResult for partial cleanup issues
	//   - Other errors for database failures
	Delete(ctx context.Context, id ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error)
}

// ObjectReader is the read-only view of object persistence.
type ObjectReader interface {
	// Get retrieves an object by ID.
	Get(ctx context.Context, id ulid.ULID) (*Object, error)

	// ListAtLocation returns all objects at a location.
	ListAtLocation(ctx context.Context, locationID ulid.ULID) ([]*Object, error)

	// ListHeldBy returns all objects held by a character.
	ListHeldBy(ctx context.Context, characterID ulid.ULID) ([]*Object, error)

	// ListContainedIn returns all objects inside a container object.
	ListContainedIn(ctx context.Context, objectID ulid.ULID) ([]*Object, error)
}

// ObjectRepository manages object persistence.
type ObjectRepository interface {
	ObjectReader

	// Create persists a new object.
	Create(ctx context.Context, obj *Object) (*wmodel.MutationDelta, error)

	// Update modifies an existing object.
	Update(ctx context.Context, obj *Object) (*wmodel.MutationDelta, error)

	// Delete removes an object by ID.
	// expectedVersion is the optimistic-concurrency guard (accepted and ignored
	// in 05-14).
	Delete(ctx context.Context, id ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error)

	// Move changes an object's containment.
	// Validates containment and enforces business rules.
	// expectedVersion is the move/containment CAS guard (accepted and ignored in
	// 05-14; the read version is threaded from service.go in 05-03/05-11).
	Move(ctx context.Context, objectID ulid.ULID, to Containment, expectedVersion int) (*wmodel.MutationDelta, error)
}

// SceneReader is the read-only view of scene persistence. The world-layer scene
// WRITE surface (AddParticipant/RemoveParticipant) was removed in 05-14 (D-07:
// vestigial, no production caller); only reads remain. The public.scene_participants
// table is KEPT because the reads still SELECT/JOIN it (physical DROP deferred to #4815).
type SceneReader interface {
	// ListParticipants returns all participants in a scene.
	ListParticipants(ctx context.Context, sceneID ulid.ULID) ([]SceneParticipant, error)

	// GetScenesFor returns all scenes a character is participating in.
	GetScenesFor(ctx context.Context, characterID ulid.ULID) ([]*Location, error)
}

// SceneRepository manages scene-specific read operations. The vestigial world
// scene-participant write surface was removed in 05-14 (round-5 D-07).
type SceneRepository interface {
	SceneReader
}

// SceneParticipant represents a character's membership in a scene.
type SceneParticipant struct {
	CharacterID ulid.ULID
	Role        ParticipantRole
}

// CharacterReader is the read-only view of character persistence.
type CharacterReader interface {
	// Get retrieves a character by ID.
	Get(ctx context.Context, id ulid.ULID) (*Character, error)

	// GetByLocation retrieves characters at a location with pagination.
	// Pass empty ListOptions{} to use default pagination (limit=100, offset=0).
	GetByLocation(ctx context.Context, locationID ulid.ULID, opts ListOptions) ([]*Character, error)

	// IsOwnedByPlayer checks if a character is owned by a specific player.
	// Returns false (not an error) if the character does not exist.
	IsOwnedByPlayer(ctx context.Context, characterID, playerID ulid.ULID) (bool, error)

	// GetNamesByIDs returns a map[characterID]name for the given IDs.
	// Missing IDs are omitted from the result map (not an error).
	// Returns empty map (not nil error) for an empty input slice.
	GetNamesByIDs(ctx context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error)
}

// CharacterRepository defines operations for character persistence.
type CharacterRepository interface {
	CharacterReader

	// Create persists a new character.
	Create(ctx context.Context, char *Character) (*wmodel.MutationDelta, error)

	// Update modifies an existing character.
	Update(ctx context.Context, char *Character) (*wmodel.MutationDelta, error)

	// Delete removes a character by ID.
	// expectedVersion is the optimistic-concurrency guard (accepted and ignored
	// in 05-14).
	Delete(ctx context.Context, id ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error)

	// UpdateLocation moves a character to a new location.
	// expectedVersion is the move CAS guard (accepted and ignored in 05-14; the
	// read version is threaded from service.go in 05-03/05-11).
	UpdateLocation(ctx context.Context, characterID ulid.ULID, locationID *ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error)
}
