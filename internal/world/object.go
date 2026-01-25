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
// Exactly one field must be set; use the factory functions [InLocation],
// [HeldByCharacter], or [ContainedInObject] to construct valid instances.
//
// # Zero Value Warning
//
// The zero value Containment{} is INVALID and will fail validation.
// Always use the factory functions to construct Containment values.
//
// # Design Decision
//
// This type uses a struct with explicit validation rather than a sealed interface
// pattern. While a sealed interface would make invalid states unrepresentable at
// compile time, the struct approach was chosen because:
//
//   - Simpler API for callers (direct field access vs method calls)
//   - Factory functions provide safe construction for external callers
//   - Validation catches misuse at runtime with clear error messages
//   - Go convention: many stdlib types have invalid zero values (time.Time{}, etc.)
//
// Fields are public to allow repository code to hydrate objects directly from
// database queries without going through factories. Application code SHOULD use
// the factory functions to avoid validation errors.
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

// InLocation creates a Containment for a location.
func InLocation(id ulid.ULID) Containment {
	return Containment{LocationID: &id}
}

// HeldByCharacter creates a Containment for a character.
func HeldByCharacter(id ulid.ULID) Containment {
	return Containment{CharacterID: &id}
}

// ContainedInObject creates a Containment for an object container.
func ContainedInObject(id ulid.ULID) Containment {
	return Containment{ObjectID: &id}
}

// HeldBy is a shorthand for HeldByCharacter.
func HeldBy(id ulid.ULID) Containment {
	return HeldByCharacter(id)
}

// InContainer is a shorthand for ContainedInObject.
func InContainer(id ulid.ULID) Containment {
	return ContainedInObject(id)
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
	locationID          *ulid.ULID // unexported: use SetContainment/LocationID()
	heldByCharacterID   *ulid.ULID // unexported: use SetContainment/HeldByCharacterID()
	containedInObjectID *ulid.ULID // unexported: use SetContainment/ContainedInObjectID()
	IsContainer         bool
	OwnerID             *ulid.ULID
	CreatedAt           time.Time
}

// LocationID returns the location ID if the object is in a location, or nil.
func (o *Object) LocationID() *ulid.ULID { return o.locationID }

// HeldByCharacterID returns the character ID if the object is held, or nil.
func (o *Object) HeldByCharacterID() *ulid.ULID { return o.heldByCharacterID }

// ContainedInObjectID returns the container object ID if contained, or nil.
func (o *Object) ContainedInObjectID() *ulid.ULID { return o.containedInObjectID }

// NewObject creates a new Object with a generated ID.
// The object is validated before being returned.
// Containment specifies where the object is located (required).
func NewObject(name string, containment Containment) (*Object, error) {
	return NewObjectWithID(ulid.Make(), name, containment)
}

// NewObjectWithID creates a new Object with the provided ID.
// The object is validated before being returned.
// Containment specifies where the object is located (required).
func NewObjectWithID(id ulid.ULID, name string, containment Containment) (*Object, error) {
	o := &Object{
		ID:                  id,
		Name:                name,
		locationID:          containment.LocationID,
		heldByCharacterID:   containment.CharacterID,
		containedInObjectID: containment.ObjectID,
		CreatedAt:           time.Now(),
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if err := o.ValidateContainment(); err != nil {
		return nil, err
	}
	return o, nil
}

// Containment returns the current containment of this object.
func (o *Object) Containment() Containment {
	return Containment{
		LocationID:  o.locationID,
		CharacterID: o.heldByCharacterID,
		ObjectID:    o.containedInObjectID,
	}
}

// SetContainment updates the object's location.
// Clears all previous containment and sets the new one.
// Returns ErrInvalidContainment if the containment is invalid (not exactly one field set).
func (o *Object) SetContainment(c Containment) error {
	if err := c.Validate(); err != nil {
		return err
	}
	o.locationID = c.LocationID
	o.heldByCharacterID = c.CharacterID
	o.containedInObjectID = c.ObjectID
	return nil
}

// Validate validates the object's fields.
// Returns a ValidationError if the ID is zero, or if name or description is invalid.
func (o *Object) Validate() error {
	if o.ID.IsZero() {
		return &ValidationError{Field: "id", Message: "cannot be zero"}
	}
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
