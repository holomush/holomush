// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
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
		return oops.
			With("location_set", c.LocationID != nil).
			With("character_set", c.CharacterID != nil).
			With("object_set", c.ObjectID != nil).
			With("count", count).
			Wrap(ErrInvalidContainment)
	}
	return nil
}

// Type returns the containment type: "location", "character", "object", or "none".
func (c *Containment) Type() ContainmentType {
	if c.LocationID != nil {
		return ContainmentTypeLocation
	}
	if c.CharacterID != nil {
		return ContainmentTypeCharacter
	}
	if c.ObjectID != nil {
		return ContainmentTypeObject
	}
	return ContainmentTypeNone
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
// Returns ErrInvalidContainment if the containment is invalid (not exactly one field set).
func (o *Object) SetContainment(c Containment) error {
	if err := c.Validate(); err != nil {
		return err
	}
	o.LocationID = c.LocationID
	o.HeldByCharacterID = c.CharacterID
	o.ContainedInObjectID = c.ObjectID
	return nil
}

// Validate validates the object's fields.
// Returns a ValidationError if the name or description is invalid.
func (o *Object) Validate() error {
	if err := ValidateName(o.Name); err != nil {
		return err
	}
	return ValidateDescription(o.Description)
}

// ValidateContainment checks that the object has valid containment (exactly one location).
func (o *Object) ValidateContainment() error {
	c := o.Containment()
	return c.Validate()
}
