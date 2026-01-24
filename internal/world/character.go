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

// Validate checks that the character has required fields.
func (c *Character) Validate() error {
	if c.PlayerID.IsZero() {
		return &ValidationError{Field: "player_id", Message: "cannot be zero"}
	}
	if err := ValidateName(c.Name); err != nil {
		return err
	}
	return ValidateDescription(c.Description)
}
