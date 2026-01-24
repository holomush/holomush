// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

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
	EntityID   string          `json:"entity_id"`
	FromType   ContainmentType `json:"from_type"` // "location" | "character" | "object" | "none"
	FromID     string          `json:"from_id,omitempty"`
	ToType     ContainmentType `json:"to_type"` // "location" | "character" | "object"
	ToID       string          `json:"to_id"`
	ExitID     string          `json:"exit_id,omitempty"`
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
	if p.EntityID == "" {
		return &ValidationError{Field: "entity_id", Message: "cannot be empty"}
	}
	if p.FromType == "" {
		return &ValidationError{Field: "from_type", Message: "cannot be empty"}
	}
	if !p.FromType.IsValidOrNone() {
		return &ValidationError{Field: "from_type", Message: "must be 'location', 'character', 'object', or 'none'"}
	}
	// FromID can be empty only for first-time placements (FromType == "none")
	if p.FromID == "" && p.FromType != ContainmentTypeNone {
		return &ValidationError{Field: "from_id", Message: "cannot be empty"}
	}
	if p.ToType == "" {
		return &ValidationError{Field: "to_type", Message: "cannot be empty"}
	}
	if !p.ToType.IsValid() {
		return &ValidationError{Field: "to_type", Message: "must be 'location', 'character', or 'object'"}
	}
	if p.ToID == "" {
		return &ValidationError{Field: "to_id", Message: "cannot be empty"}
	}
	return nil
}

// ObjectGivePayload represents an object transfer between characters.
type ObjectGivePayload struct {
	ObjectID        string `json:"object_id"`
	ObjectName      string `json:"object_name"`
	FromCharacterID string `json:"from_character_id"`
	ToCharacterID   string `json:"to_character_id"`
}

// Validate checks that the ObjectGivePayload has all required fields and valid values.
func (p *ObjectGivePayload) Validate() error {
	if p.ObjectID == "" {
		return &ValidationError{Field: "object_id", Message: "cannot be empty"}
	}
	if p.ObjectName == "" {
		return &ValidationError{Field: "object_name", Message: "cannot be empty"}
	}
	if p.FromCharacterID == "" {
		return &ValidationError{Field: "from_character_id", Message: "cannot be empty"}
	}
	if p.ToCharacterID == "" {
		return &ValidationError{Field: "to_character_id", Message: "cannot be empty"}
	}
	if p.FromCharacterID == p.ToCharacterID {
		return &ValidationError{Field: "to_character_id", Message: "cannot give object to self"}
	}
	return nil
}
