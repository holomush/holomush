// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package world contains the world model domain types and logic.
//
// For creating domain objects (Character, Object, Exit, Location), prefer using
// constructor functions (NewX or NewXWithID) over direct struct initialization.
// Constructors ensure validation and proper initialization of required fields.
//
// Example:
//
//	// Preferred: use constructor
//	char, err := world.NewCharacter(playerID, "Hero")
//
//	// Avoid: direct initialization (bypasses validation)
//	char := &world.Character{Name: "Hero", PlayerID: playerID} // Missing ID, CreatedAt
package world

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
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

// ErrInvalidVisibility indicates an unrecognized visibility value.
var ErrInvalidVisibility = errors.New("invalid visibility")

// Validate checks that the visibility is a recognized value.
func (v Visibility) Validate() error {
	switch v {
	case VisibilityAll, VisibilityOwner, VisibilityList:
		return nil
	default:
		return ErrInvalidVisibility
	}
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

// ErrInvalidLockType indicates an unrecognized lock type.
var ErrInvalidLockType = errors.New("invalid lock type")

// Validate checks that the lock type is a recognized value.
func (l LockType) Validate() error {
	switch l {
	case LockTypeKey, LockTypePassword, LockTypeCondition:
		return nil
	default:
		return ErrInvalidLockType
	}
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

// NewExit creates a new Exit with a generated ID.
// The exit is validated before being returned.
// Visibility defaults to VisibilityAll.
func NewExit(fromLocationID, toLocationID ulid.ULID, name string) (*Exit, error) {
	return NewExitWithID(ulid.Make(), fromLocationID, toLocationID, name)
}

// NewExitWithID creates a new Exit with the provided ID.
// The exit is validated before being returned.
// Visibility defaults to VisibilityAll.
func NewExitWithID(id, fromLocationID, toLocationID ulid.ULID, name string) (*Exit, error) {
	e := &Exit{
		ID:             id,
		FromLocationID: fromLocationID,
		ToLocationID:   toLocationID,
		Name:           name,
		Visibility:     VisibilityAll,
		CreatedAt:      time.Now(),
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return e, nil
}

// Validate validates the exit's fields.
// Returns a ValidationError if any field is invalid.
// Returns ErrSelfReferentialExit if from and to locations are the same.
func (e *Exit) Validate() error {
	if e.ID.IsZero() {
		return &ValidationError{Field: "id", Message: "cannot be zero"}
	}
	if err := ValidateName(e.Name); err != nil {
		return err
	}
	if err := ValidateAliases(e.Aliases); err != nil {
		return err
	}
	if err := e.Visibility.Validate(); err != nil {
		return err
	}
	// Check for self-referential exit (same from and to location)
	if !e.FromLocationID.IsZero() && e.FromLocationID == e.ToLocationID {
		return ErrSelfReferentialExit
	}
	if e.Locked {
		if err := e.LockType.Validate(); err != nil {
			return err
		}
		if err := ValidateLockData(e.LockData); err != nil {
			return err
		}
	}
	if e.Visibility == VisibilityList {
		if err := ValidateVisibleTo(e.VisibleTo); err != nil {
			return err
		}
	}
	return nil
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
// Returns (nil, nil) if not bidirectional or no return name is set.
// Returns (nil, error) if LockData cannot be deep copied (e.g., non-serializable types).
func (e *Exit) ReverseExit() (*Exit, error) {
	if !e.Bidirectional || e.ReturnName == "" {
		return nil, nil
	}

	// Deep copy VisibleTo slice to avoid shared reference
	visibleTo := slices.Clone(e.VisibleTo)

	// Deep copy LockData map to avoid shared reference (including nested structures)
	lockData, err := deepCopyLockData(e.LockData)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to deep copy lock data")
	}

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
	}, nil
}

// deepCopyLockData creates a true deep copy of LockData, including nested maps/slices.
// Uses JSON round-trip which handles arbitrary nested structures in map[string]any.
// Returns (nil, nil) if input is nil.
// Returns (nil, error) if marshaling or unmarshaling fails.
func deepCopyLockData(src map[string]any) (map[string]any, error) {
	if src == nil {
		return nil, nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		return nil, oops.Code("LOCK_DATA_MARSHAL_FAILED").
			With("keys", len(src)).
			Wrapf(err, "failed to marshal lock data")
	}
	var dst map[string]any
	if err := json.Unmarshal(data, &dst); err != nil {
		// This should not happen with valid JSON from Marshal
		return nil, oops.Code("LOCK_DATA_UNMARSHAL_FAILED").
			With("json_length", len(data)).
			Wrapf(err, "failed to unmarshal lock data")
	}
	return dst, nil
}
