// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"time"

	"github.com/oklog/ulid/v2"
)

// Character represents a player character in the world.
type Character struct {
	ID          ulid.ULID
	PlayerID    ulid.ULID
	Name        string
	Description string
	LocationID  *ulid.ULID // Current location (nil if not in world)
	CreatedAt   time.Time
}

// NewCharacter creates a new Character with a generated ID.
// The character is validated before being returned.
func NewCharacter(playerID ulid.ULID, name string) (*Character, error) {
	return NewCharacterWithID(ulid.Make(), playerID, name)
}

// NewCharacterWithID creates a new Character with the provided ID.
// The character is validated before being returned.
func NewCharacterWithID(id, playerID ulid.ULID, name string) (*Character, error) {
	c := &Character{
		ID:        id,
		PlayerID:  playerID,
		Name:      name,
		CreatedAt: time.Now(),
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Validate checks that the character has required fields.
func (c *Character) Validate() error {
	if c.ID.IsZero() {
		return &ValidationError{Field: "id", Message: "cannot be zero"}
	}
	if c.PlayerID.IsZero() {
		return &ValidationError{Field: "player_id", Message: "cannot be zero"}
	}
	if err := ValidateName(c.Name); err != nil {
		return err
	}
	return ValidateDescription(c.Description)
}
