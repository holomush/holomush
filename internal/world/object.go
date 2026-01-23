// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// ErrInvalidContainment is returned when containment validation fails.
var ErrInvalidContainment = errors.New("object must be in exactly one place")

// Containment represents where an object is located.
// Exactly one field must be set.
type Containment struct {
	LocationID  *ulid.ULID
	CharacterID *ulid.ULID
	ObjectID    *ulid.ULID // Container object
}

// Validate ensures exactly one containment field is set.
func (c *Containment) Validate() error {
	count := 0
	if c.LocationID != nil {
		count++
	}
	if c.CharacterID != nil {
		count++
	}
	if c.ObjectID != nil {
		count++
	}
	if count != 1 {
		return ErrInvalidContainment
	}
	return nil
}

// Type returns the containment type: "location", "character", "object", or "".
func (c *Containment) Type() string {
	if c.LocationID != nil {
		return "location"
	}
	if c.CharacterID != nil {
		return "character"
	}
	if c.ObjectID != nil {
		return "object"
	}
	return ""
}

// ID returns the ID of the container (location, character, or object).
func (c *Containment) ID() *ulid.ULID {
	if c.LocationID != nil {
		return c.LocationID
	}
	if c.CharacterID != nil {
		return c.CharacterID
	}
	return c.ObjectID
}

// Object represents an item in the game world.
type Object struct {
	ID                  ulid.ULID
	Name                string
	Description         string
	LocationID          *ulid.ULID
	HeldByCharacterID   *ulid.ULID
	ContainedInObjectID *ulid.ULID
	IsContainer         bool
	OwnerID             *ulid.ULID
	CreatedAt           time.Time
}

// Containment returns the current containment of this object.
func (o *Object) Containment() Containment {
	return Containment{
		LocationID:  o.LocationID,
		CharacterID: o.HeldByCharacterID,
		ObjectID:    o.ContainedInObjectID,
	}
}

// SetContainment updates the object's location.
// Clears all previous containment and sets the new one.
func (o *Object) SetContainment(c Containment) {
	o.LocationID = c.LocationID
	o.HeldByCharacterID = c.CharacterID
	o.ContainedInObjectID = c.ObjectID
}
