// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package world contains the world model domain types and logic.
package world

import (
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
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
// Returns the parsed integer N, or 0 if the format is invalid.
// By convention: -1 means unlimited replay, 0 means no replay, positive N means last N events.
func ParseReplayPolicy(policy string) int {
	if !strings.HasPrefix(policy, "last:") {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(policy, "last:"))
	if err != nil {
		return 0
	}
	return n
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
