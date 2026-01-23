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

// ObjectGivePayload represents an object transfer between characters.
type ObjectGivePayload struct {
	ObjectID        string `json:"object_id"`
	ObjectName      string `json:"object_name"`
	FromCharacterID string `json:"from_character_id"`
	ToCharacterID   string `json:"to_character_id"`
}
