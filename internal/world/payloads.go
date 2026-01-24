// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

// MovePayload represents a move event for characters or objects.
type MovePayload struct {
	EntityType string `json:"entity_type"` // "character" | "object"
	EntityID   string `json:"entity_id"`
	FromType   string `json:"from_type"` // "location" | "character" | "object"
	FromID     string `json:"from_id"`
	ToType     string `json:"to_type"` // "location" | "character" | "object"
	ToID       string `json:"to_id"`
	ExitID     string `json:"exit_id,omitempty"`
	ExitName   string `json:"exit_name,omitempty"`
}

// Valid entity types for move payloads.
const (
	EntityTypeCharacter = "character"
	EntityTypeObject    = "object"
)

// Valid containment types for move payloads.
const (
	ContainmentTypeLocation  = "location"
	ContainmentTypeCharacter = "character"
	ContainmentTypeObject    = "object"
	ContainmentTypeNone      = "none" // Used for first-time placements (no prior containment)
)

// Validate checks that the MovePayload has all required fields and valid values.
func (p *MovePayload) Validate() error {
	if p.EntityType == "" {
		return &ValidationError{Field: "entity_type", Message: "cannot be empty"}
	}
	if p.EntityType != EntityTypeCharacter && p.EntityType != EntityTypeObject {
		return &ValidationError{Field: "entity_type", Message: "must be 'character' or 'object'"}
	}
	if p.EntityID == "" {
		return &ValidationError{Field: "entity_id", Message: "cannot be empty"}
	}
	if p.FromType == "" {
		return &ValidationError{Field: "from_type", Message: "cannot be empty"}
	}
	if !isValidContainmentTypeOrNone(p.FromType) {
		return &ValidationError{Field: "from_type", Message: "must be 'location', 'character', 'object', or 'none'"}
	}
	// FromID can be empty only for first-time placements (FromType == "none")
	if p.FromID == "" && p.FromType != ContainmentTypeNone {
		return &ValidationError{Field: "from_id", Message: "cannot be empty"}
	}
	if p.ToType == "" {
		return &ValidationError{Field: "to_type", Message: "cannot be empty"}
	}
	if !isValidContainmentType(p.ToType) {
		return &ValidationError{Field: "to_type", Message: "must be 'location', 'character', or 'object'"}
	}
	if p.ToID == "" {
		return &ValidationError{Field: "to_id", Message: "cannot be empty"}
	}
	return nil
}

// isValidContainmentType checks if the type is a valid containment type.
func isValidContainmentType(t string) bool {
	return t == ContainmentTypeLocation || t == ContainmentTypeCharacter || t == ContainmentTypeObject
}

// isValidContainmentTypeOrNone checks if the type is a valid containment type or "none".
func isValidContainmentTypeOrNone(t string) bool {
	return isValidContainmentType(t) || t == ContainmentTypeNone
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
