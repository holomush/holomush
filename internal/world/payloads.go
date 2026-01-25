// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import "github.com/oklog/ulid/v2"

// EntityType represents the type of entity being moved.
type EntityType string

// Valid entity types for move payloads.
const (
	EntityTypeCharacter EntityType = "character"
	EntityTypeObject    EntityType = "object"
)

// IsValid returns true if the entity type is a valid move entity.
func (e EntityType) IsValid() bool {
	return e == EntityTypeCharacter || e == EntityTypeObject
}

// ContainmentType represents where an entity is contained.
type ContainmentType string

// Valid containment types for move payloads.
const (
	ContainmentTypeLocation  ContainmentType = "location"
	ContainmentTypeCharacter ContainmentType = "character"
	ContainmentTypeObject    ContainmentType = "object"
	ContainmentTypeNone      ContainmentType = "none" // Used for first-time placements (no prior containment)
)

// IsValid returns true if the containment type is a valid destination (not "none").
func (c ContainmentType) IsValid() bool {
	return c == ContainmentTypeLocation || c == ContainmentTypeCharacter || c == ContainmentTypeObject
}

// IsValidOrNone returns true if the containment type is valid or "none" (for sources).
func (c ContainmentType) IsValidOrNone() bool {
	return c.IsValid() || c == ContainmentTypeNone
}

// MovePayload represents a move event for characters or objects.
type MovePayload struct {
	EntityType EntityType      `json:"entity_type"` // "character" | "object"
	EntityID   ulid.ULID       `json:"entity_id"`
	FromType   ContainmentType `json:"from_type"` // "location" | "character" | "object" | "none"
	FromID     *ulid.ULID      `json:"from_id,omitempty"`
	ToType     ContainmentType `json:"to_type"` // "location" | "character" | "object"
	ToID       ulid.ULID       `json:"to_id"`
	ExitID     *ulid.ULID      `json:"exit_id,omitempty"`
	ExitName   string          `json:"exit_name,omitempty"`
}

// Validate checks that the MovePayload has all required fields and valid values.
func (p *MovePayload) Validate() error {
	if p.EntityType == "" {
		return &ValidationError{Field: "entity_type", Message: "cannot be empty"}
	}
	if !p.EntityType.IsValid() {
		return &ValidationError{Field: "entity_type", Message: "must be 'character' or 'object'"}
	}
	if p.EntityID.IsZero() {
		return &ValidationError{Field: "entity_id", Message: "cannot be zero"}
	}
	if p.FromType == "" {
		return &ValidationError{Field: "from_type", Message: "cannot be empty"}
	}
	if !p.FromType.IsValidOrNone() {
		return &ValidationError{Field: "from_type", Message: "must be 'location', 'character', 'object', or 'none'"}
	}
	// FromID must be nil for first-time placements (FromType == "none")
	// and must be non-nil otherwise
	if p.FromType == ContainmentTypeNone {
		if p.FromID != nil {
			return &ValidationError{Field: "from_id", Message: "must be nil when from_type is 'none'"}
		}
	} else if p.FromID == nil {
		return &ValidationError{Field: "from_id", Message: "cannot be nil"}
	}
	if p.ToType == "" {
		return &ValidationError{Field: "to_type", Message: "cannot be empty"}
	}
	if !p.ToType.IsValid() {
		return &ValidationError{Field: "to_type", Message: "must be 'location', 'character', or 'object'"}
	}
	if p.ToID.IsZero() {
		return &ValidationError{Field: "to_id", Message: "cannot be zero"}
	}
	return nil
}

// ObjectGivePayload represents an object transfer between characters.
type ObjectGivePayload struct {
	ObjectID        ulid.ULID `json:"object_id"`
	ObjectName      string    `json:"object_name"`
	FromCharacterID ulid.ULID `json:"from_character_id"`
	ToCharacterID   ulid.ULID `json:"to_character_id"`
}

// Validate checks that the ObjectGivePayload has all required fields and valid values.
func (p *ObjectGivePayload) Validate() error {
	if p.ObjectID.IsZero() {
		return &ValidationError{Field: "object_id", Message: "cannot be zero"}
	}
	if p.ObjectName == "" {
		return &ValidationError{Field: "object_name", Message: "cannot be empty"}
	}
	if p.FromCharacterID.IsZero() {
		return &ValidationError{Field: "from_character_id", Message: "cannot be zero"}
	}
	if p.ToCharacterID.IsZero() {
		return &ValidationError{Field: "to_character_id", Message: "cannot be zero"}
	}
	if p.FromCharacterID == p.ToCharacterID {
		return &ValidationError{Field: "to_character_id", Message: "cannot give object to self"}
	}
	return nil
}
