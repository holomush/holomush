// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package world contains the world model domain types and logic.
package world

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// LocationType identifies the kind of location.
type LocationType string

// Location types.
const (
	LocationTypePersistent LocationType = "persistent"
	LocationTypeScene      LocationType = "scene"
	LocationTypeInstance   LocationType = "instance"
)

// String returns the string representation of the location type.
func (t LocationType) String() string {
	return string(t)
}

// ErrInvalidLocationType indicates an unrecognized location type.
var ErrInvalidLocationType = errors.New("invalid location type")

// Validate checks that the location type is a recognized value.
func (t LocationType) Validate() error {
	switch t {
	case LocationTypePersistent, LocationTypeScene, LocationTypeInstance:
		return nil
	default:
		return ErrInvalidLocationType
	}
}

// Location represents a room in the game world.
type Location struct {
	ID           ulid.ULID
	Type         LocationType
	ShadowsID    *ulid.ULID // For scenes cloning a persistent location
	Name         string
	Description  string
	OwnerID      *ulid.ULID
	ReplayPolicy string
	CreatedAt    time.Time
	ArchivedAt   *time.Time
}

// NewLocation creates a new Location with a generated ID.
// The location is validated before being returned.
func NewLocation(name, description string, locType LocationType) (*Location, error) {
	return NewLocationWithID(ulid.Make(), name, description, locType)
}

// NewLocationWithID creates a new Location with the provided ID.
// The location is validated before being returned.
func NewLocationWithID(id ulid.ULID, name, description string, locType LocationType) (*Location, error) {
	l := &Location{
		ID:           id,
		Name:         name,
		Description:  description,
		Type:         locType,
		ReplayPolicy: DefaultReplayPolicy(locType),
		CreatedAt:    time.Now(),
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return l, nil
}

// Validate validates the location's fields.
// Returns a ValidationError if any field is invalid.
func (l *Location) Validate() error {
	if l.ID.IsZero() {
		return &ValidationError{Field: "id", Message: "cannot be zero"}
	}
	if err := ValidateName(l.Name); err != nil {
		return err
	}
	if err := ValidateDescription(l.Description); err != nil {
		return err
	}
	return l.Type.Validate()
}

// EffectiveDescription returns the description to show, falling back to shadow if empty.
// If this location shadows another and has an empty description, returns the parent's description.
// The parent parameter should be the shadowed location, or nil if not shadowing.
func (l *Location) EffectiveDescription(parent *Location) string {
	if l.Description != "" {
		return l.Description
	}
	if l.ShadowsID != nil && parent != nil {
		return parent.Description
	}
	return l.Description
}

// EffectiveName returns the name to show, falling back to shadow if empty.
func (l *Location) EffectiveName(parent *Location) string {
	if l.Name != "" {
		return l.Name
	}
	if l.ShadowsID != nil && parent != nil {
		return parent.Name
	}
	return l.Name
}

// ParseReplayPolicy extracts the count from "last:N" format.
// Returns (N, nil) on success where N is the parsed integer.
// Returns (0, error) if the format is invalid or the count cannot be parsed.
// By convention: -1 means unlimited replay, 0 means no replay, positive N means last N events.
func ParseReplayPolicy(policy string) (int, error) {
	if !strings.HasPrefix(policy, "last:") {
		return 0, oops.Code("INVALID_REPLAY_POLICY").
			With("policy", policy).
			With("expected", "last:N").
			Errorf("invalid replay policy format: must start with 'last:'")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(policy, "last:"))
	if err != nil {
		return 0, oops.Code("INVALID_REPLAY_POLICY_COUNT").
			With("policy", policy).
			Wrapf(err, "failed to parse count from replay policy")
	}
	return n, nil
}

// DefaultReplayPolicy returns the default replay policy for a location type.
func DefaultReplayPolicy(locType LocationType) string {
	switch locType {
	case LocationTypeScene:
		return "last:-1"
	default:
		return "last:0"
	}
}
