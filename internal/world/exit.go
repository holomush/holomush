// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package world contains the world model domain types and logic.
package world

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Visibility controls who can see an exit.
type Visibility string

// Visibility options.
const (
	VisibilityAll   Visibility = "all"
	VisibilityOwner Visibility = "owner"
	VisibilityList  Visibility = "list"
)

// String returns the string representation of the visibility.
func (v Visibility) String() string {
	return string(v)
}

// LockType identifies how an exit is locked.
type LockType string

// Lock types.
const (
	LockTypeKey       LockType = "key"
	LockTypePassword  LockType = "password"
	LockTypeCondition LockType = "condition"
)

// String returns the string representation of the lock type.
func (l LockType) String() string {
	return string(l)
}

// Exit represents a connection between two locations.
type Exit struct {
	ID             ulid.ULID
	FromLocationID ulid.ULID
	ToLocationID   ulid.ULID
	Name           string
	Aliases        []string
	Bidirectional  bool
	ReturnName     string
	Visibility     Visibility
	VisibleTo      []ulid.ULID // Character IDs when Visibility=list
	Locked         bool
	LockType       LockType
	LockData       map[string]any
	CreatedAt      time.Time
}

// MatchesName returns true if the given input matches the exit name or any alias.
// Matching is case-insensitive.
func (e *Exit) MatchesName(input string) bool {
	if strings.EqualFold(e.Name, input) {
		return true
	}
	for _, alias := range e.Aliases {
		if strings.EqualFold(alias, input) {
			return true
		}
	}
	return false
}

// IsVisibleTo returns true if the given character can see this exit.
// locationOwnerID is the owner of the location this exit is in (for VisibilityOwner).
// Note: Unknown visibility values default to not visible (fail-closed for security).
func (e *Exit) IsVisibleTo(charID ulid.ULID, locationOwnerID *ulid.ULID) bool {
	switch e.Visibility {
	case VisibilityAll:
		return true
	case VisibilityOwner:
		return locationOwnerID != nil && *locationOwnerID == charID
	case VisibilityList:
		return slices.Contains(e.VisibleTo, charID)
	default:
		// Security: Unknown visibility should deny access, not grant it
		return false
	}
}

// ReverseExit creates the return exit for a bidirectional exit.
// Returns nil if not bidirectional or no return name is set.
func (e *Exit) ReverseExit() *Exit {
	if !e.Bidirectional || e.ReturnName == "" {
		return nil
	}

	// Deep copy VisibleTo slice to avoid shared reference
	visibleTo := slices.Clone(e.VisibleTo)

	// Deep copy LockData map to avoid shared reference (including nested structures)
	lockData := deepCopyLockData(e.LockData)

	return &Exit{
		FromLocationID: e.ToLocationID,
		ToLocationID:   e.FromLocationID,
		Name:           e.ReturnName,
		Bidirectional:  true,
		ReturnName:     e.Name,
		Visibility:     e.Visibility,
		VisibleTo:      visibleTo,
		Locked:         e.Locked,
		LockType:       e.LockType,
		LockData:       lockData,
	}
}

// deepCopyLockData creates a true deep copy of LockData, including nested maps/slices.
// Uses JSON round-trip which handles arbitrary nested structures in map[string]any.
// Returns nil if input is nil or if marshaling fails (defensive).
func deepCopyLockData(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		// Should not happen with valid map[string]any, but be defensive
		return nil
	}
	var dst map[string]any
	if err := json.Unmarshal(data, &dst); err != nil {
		// Should not happen with valid JSON, but be defensive
		return nil
	}
	return dst
}
