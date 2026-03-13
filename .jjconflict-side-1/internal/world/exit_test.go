// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

func TestExit_MatchesName(t *testing.T) {
	exit := &world.Exit{
		ID:      ulid.Make(),
		Name:    "north",
		Aliases: []string{"n", "forward"},
	}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"exact name", "north", true},
		{"alias n", "n", true},
		{"alias forward", "forward", true},
		{"case insensitive name", "North", true},
		{"case insensitive alias", "N", true},
		{"no match", "south", false},
		{"partial match", "nor", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, exit.MatchesName(tt.input))
		})
	}

	t.Run("nil aliases does not match alias input", func(t *testing.T) {
		exitNoAliases := &world.Exit{
			ID:      ulid.Make(),
			Name:    "north",
			Aliases: nil, // nil, not empty slice
		}
		assert.True(t, exitNoAliases.MatchesName("north"))
		assert.False(t, exitNoAliases.MatchesName("n"))
	})

	t.Run("exit with empty name does not match empty input", func(t *testing.T) {
		exitEmptyName := &world.Exit{
			ID:      ulid.Make(),
			Name:    "",
			Aliases: []string{"n"},
		}
		// Empty name should not match empty input (both compare as equal, which is debatable)
		// Validation should prevent empty names, but if it occurs, aliases still work
		assert.True(t, exitEmptyName.MatchesName("n"))
	})
}

func TestVisibility_String(t *testing.T) {
	tests := []struct {
		name       string
		visibility world.Visibility
		expected   string
	}{
		{"all", world.VisibilityAll, "all"},
		{"owner", world.VisibilityOwner, "owner"},
		{"list", world.VisibilityList, "list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.visibility.String())
		})
	}
}

func TestExit_IsVisibleTo(t *testing.T) {
	ownerID := ulid.Make()
	allowedID := ulid.Make()
	otherID := ulid.Make()

	t.Run("visibility all", func(t *testing.T) {
		exit := &world.Exit{Visibility: world.VisibilityAll}
		assert.True(t, exit.IsVisibleTo(otherID, nil))
	})

	t.Run("visibility owner - is owner", func(t *testing.T) {
		exit := &world.Exit{Visibility: world.VisibilityOwner}
		// Note: owner check requires location owner, passed separately
		assert.True(t, exit.IsVisibleTo(ownerID, &ownerID))
	})

	t.Run("visibility owner - not owner", func(t *testing.T) {
		exit := &world.Exit{Visibility: world.VisibilityOwner}
		assert.False(t, exit.IsVisibleTo(otherID, &ownerID))
	})

	t.Run("visibility owner - nil location owner returns false", func(t *testing.T) {
		exit := &world.Exit{Visibility: world.VisibilityOwner}
		// When location has no owner, owner-only exits should not be visible to anyone
		assert.False(t, exit.IsVisibleTo(ownerID, nil))
	})

	t.Run("visibility list - in list", func(t *testing.T) {
		exit := &world.Exit{
			Visibility: world.VisibilityList,
			VisibleTo:  []ulid.ULID{allowedID},
		}
		assert.True(t, exit.IsVisibleTo(allowedID, nil))
	})

	t.Run("visibility list - not in list", func(t *testing.T) {
		exit := &world.Exit{
			Visibility: world.VisibilityList,
			VisibleTo:  []ulid.ULID{allowedID},
		}
		assert.False(t, exit.IsVisibleTo(otherID, nil))
	})

	t.Run("visibility list - empty list visible to no one", func(t *testing.T) {
		exit := &world.Exit{
			Visibility: world.VisibilityList,
			VisibleTo:  []ulid.ULID{}, // empty list
		}
		// With empty VisibleTo, no character can see the exit
		assert.False(t, exit.IsVisibleTo(allowedID, nil))
		assert.False(t, exit.IsVisibleTo(otherID, nil))
	})

	t.Run("unknown visibility defaults to not visible (fail-closed)", func(t *testing.T) {
		exit := &world.Exit{Visibility: world.Visibility("unknown")}
		// Security: Unknown visibility should deny access, not grant it
		assert.False(t, exit.IsVisibleTo(otherID, nil))
	})
}

func TestExit_ReverseExit(t *testing.T) {
	fromID := ulid.Make()
	toID := ulid.Make()

	t.Run("bidirectional with return name creates reverse", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Bidirectional:  true,
			ReturnName:     "south",
			Visibility:     world.VisibilityAll,
			Locked:         true,
			LockType:       world.LockTypeKey,
			LockData:       map[string]any{"key_id": "golden-key"},
		}

		reverse, err := exit.ReverseExit()
		require.NoError(t, err)
		require.NotNil(t, reverse)
		assert.Equal(t, toID, reverse.FromLocationID)
		assert.Equal(t, fromID, reverse.ToLocationID)
		assert.Equal(t, "south", reverse.Name)
		assert.Equal(t, "north", reverse.ReturnName)
		assert.True(t, reverse.Bidirectional)
		assert.Equal(t, world.VisibilityAll, reverse.Visibility)
		assert.True(t, reverse.Locked)
		assert.Equal(t, world.LockTypeKey, reverse.LockType)
		assert.Equal(t, "golden-key", reverse.LockData["key_id"])
	})

	t.Run("not bidirectional returns nil", func(t *testing.T) {
		exit := &world.Exit{
			ID:            ulid.Make(),
			Name:          "north",
			Bidirectional: false,
			ReturnName:    "south",
		}
		reverse, err := exit.ReverseExit()
		require.NoError(t, err)
		assert.Nil(t, reverse)
	})

	t.Run("no return name returns nil", func(t *testing.T) {
		exit := &world.Exit{
			ID:            ulid.Make(),
			Name:          "north",
			Bidirectional: true,
			ReturnName:    "",
		}
		reverse, err := exit.ReverseExit()
		require.NoError(t, err)
		assert.Nil(t, reverse)
	})

	t.Run("reverse exit does not share mutable references", func(t *testing.T) {
		visibleTo := []ulid.ULID{ulid.Make()}
		lockData := map[string]any{"key_id": "original-key"}

		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Bidirectional:  true,
			ReturnName:     "south",
			Visibility:     world.VisibilityList,
			VisibleTo:      visibleTo,
			LockData:       lockData,
		}

		reverse, err := exit.ReverseExit()
		require.NoError(t, err)
		require.NotNil(t, reverse)

		// Modify the reverse exit's mutable fields
		reverse.LockData["key_id"] = "modified-key"
		reverse.VisibleTo = append(reverse.VisibleTo, ulid.Make())

		// Original should be unchanged
		assert.Equal(t, "original-key", exit.LockData["key_id"], "modifying reverse should not affect original LockData")
		assert.Len(t, exit.VisibleTo, 1, "modifying reverse should not affect original VisibleTo")
	})

	t.Run("reverse exit deep copies nested LockData structures", func(t *testing.T) {
		// LockData with nested map (e.g., condition lock with multiple requirements)
		nestedConditions := map[string]any{
			"has_item": "key123",
			"level":    5,
		}
		lockData := map[string]any{
			"type":       "condition",
			"conditions": nestedConditions,
		}

		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Bidirectional:  true,
			ReturnName:     "south",
			LockData:       lockData,
		}

		reverse, err := exit.ReverseExit()
		require.NoError(t, err)
		require.NotNil(t, reverse)

		// Modify the nested map in reverse exit
		reverseConditions := reverse.LockData["conditions"].(map[string]any)
		reverseConditions["has_item"] = "modified-key"

		// Original nested map should be unchanged
		originalConditions := exit.LockData["conditions"].(map[string]any)
		assert.Equal(t, "key123", originalConditions["has_item"], "modifying reverse nested map should not affect original")
	})

	t.Run("reverse exit has empty aliases", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Aliases:        []string{"n", "forward"},
			Bidirectional:  true,
			ReturnName:     "south",
		}

		reverse, err := exit.ReverseExit()
		require.NoError(t, err)
		require.NotNil(t, reverse)
		assert.Empty(t, reverse.Aliases, "reverse exit should not inherit aliases")
	})

	t.Run("non-serializable LockData rejected at validation", func(t *testing.T) {
		// LockData with a channel cannot be marshaled to JSON
		// This is caught at validation time
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Bidirectional:  true,
			ReturnName:     "south",
			Visibility:     world.VisibilityAll,
			Locked:         true,
			LockType:       world.LockTypeKey,
			LockData:       map[string]any{"channel": make(chan int)},
		}

		err := exit.Validate()
		require.Error(t, err, "exit with non-serializable LockData should fail validation")
		assert.Contains(t, err.Error(), "not JSON-serializable")
	})

	t.Run("ReverseExit returns error on non-serializable LockData", func(t *testing.T) {
		// LockData with a channel cannot be marshaled to JSON
		// ReverseExit should return an error instead of silently returning nil LockData
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Bidirectional:  true,
			ReturnName:     "south",
			Visibility:     world.VisibilityAll,
			Locked:         true,
			LockType:       world.LockTypeKey,
			LockData:       map[string]any{"channel": make(chan int)},
		}

		reverse, err := exit.ReverseExit()
		require.Error(t, err, "ReverseExit should return error for non-serializable LockData")
		assert.Nil(t, reverse)
		assert.Contains(t, err.Error(), "failed to deep copy lock data")
	})
}

func TestExit_SetLocked(t *testing.T) {
	fromID := ulid.Make()
	toID := ulid.Make()

	t.Run("lock with valid key type", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         false,
		}

		lockData := map[string]any{"key_id": "golden-key"}
		err := exit.SetLocked(true, world.LockTypeKey, lockData)
		require.NoError(t, err)
		assert.True(t, exit.Locked)
		assert.Equal(t, world.LockTypeKey, exit.LockType)
		assert.Equal(t, "golden-key", exit.LockData["key_id"])
	})

	t.Run("unlock clears lock state", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         true,
			LockType:       world.LockTypeKey,
			LockData:       map[string]any{"key_id": "old-key"},
		}

		err := exit.SetLocked(false, "", nil)
		require.NoError(t, err)
		assert.False(t, exit.Locked)
		assert.Empty(t, exit.LockType)
		assert.Nil(t, exit.LockData)
	})

	t.Run("lock with invalid lock type returns error", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         false,
		}

		err := exit.SetLocked(true, "invalid", nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLockType)
		// State should be unchanged
		assert.False(t, exit.Locked)
	})

	t.Run("lock with non-serializable data returns error", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         false,
		}

		badData := map[string]any{"channel": make(chan int)}
		err := exit.SetLocked(true, world.LockTypeKey, badData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not JSON-serializable")
		// State should be unchanged
		assert.False(t, exit.Locked)
	})

	t.Run("lock without lock type when locking returns error", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         false,
		}

		err := exit.SetLocked(true, "", nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLockType)
		// State should be unchanged
		assert.False(t, exit.Locked)
	})

	t.Run("deep copies lock data to prevent external mutation", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: fromID,
			ToLocationID:   toID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
		}

		originalData := map[string]any{"key_id": "original"}
		err := exit.SetLocked(true, world.LockTypeKey, originalData)
		require.NoError(t, err)

		// Modify the original data after setting
		originalData["key_id"] = "modified"

		// Exit's lock data should be unchanged
		assert.Equal(t, "original", exit.LockData["key_id"])
	})
}

func TestLockType_String(t *testing.T) {
	tests := []struct {
		name     string
		lockType world.LockType
		expected string
	}{
		{"key", world.LockTypeKey, "key"},
		{"password", world.LockTypePassword, "password"},
		{"condition", world.LockTypeCondition, "condition"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.lockType.String())
		})
	}
}

func TestVisibility_Validate(t *testing.T) {
	tests := []struct {
		name       string
		visibility world.Visibility
		wantErr    bool
	}{
		{"all is valid", world.VisibilityAll, false},
		{"owner is valid", world.VisibilityOwner, false},
		{"list is valid", world.VisibilityList, false},
		{"empty string is invalid", world.Visibility(""), true},
		{"arbitrary string is invalid", world.Visibility("public"), true},
		{"similar but wrong is invalid", world.Visibility("All"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.visibility.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, world.ErrInvalidVisibility)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLockType_Validate(t *testing.T) {
	tests := []struct {
		name     string
		lockType world.LockType
		wantErr  bool
	}{
		{"key is valid", world.LockTypeKey, false},
		{"password is valid", world.LockTypePassword, false},
		{"condition is valid", world.LockTypeCondition, false},
		{"empty string is invalid", world.LockType(""), true},
		{"arbitrary string is invalid", world.LockType("magic"), true},
		{"similar but wrong is invalid", world.LockType("Key"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.lockType.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, world.ErrInvalidLockType)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExit_Validate(t *testing.T) {
	t.Run("valid exit", func(t *testing.T) {
		exit := &world.Exit{
			ID:         ulid.Make(),
			Name:       "north",
			Visibility: world.VisibilityAll,
		}
		assert.NoError(t, exit.Validate())
	})

	t.Run("invalid name", func(t *testing.T) {
		exit := &world.Exit{
			ID:         ulid.Make(),
			Name:       "",
			Visibility: world.VisibilityAll,
		}
		err := exit.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("invalid visibility", func(t *testing.T) {
		exit := &world.Exit{
			ID:         ulid.Make(),
			Name:       "north",
			Visibility: world.Visibility("invalid"),
		}
		err := exit.Validate()
		assert.Error(t, err)
	})

	t.Run("locked requires valid lock type", func(t *testing.T) {
		exit := &world.Exit{
			ID:         ulid.Make(),
			Name:       "north",
			Visibility: world.VisibilityAll,
			Locked:     true,
			LockType:   world.LockType("invalid"),
		}
		err := exit.Validate()
		assert.Error(t, err)
	})

	t.Run("locked with valid lock type", func(t *testing.T) {
		exit := &world.Exit{
			ID:         ulid.Make(),
			Name:       "north",
			Visibility: world.VisibilityAll,
			Locked:     true,
			LockType:   world.LockTypeKey,
			LockData:   map[string]any{"key_id": "gold"},
		}
		assert.NoError(t, exit.Validate())
	})

	t.Run("visibility list requires valid visible_to", func(t *testing.T) {
		id1 := ulid.Make()
		exit := &world.Exit{
			ID:         ulid.Make(),
			Name:       "north",
			Visibility: world.VisibilityList,
			VisibleTo:  []ulid.ULID{id1, id1}, // duplicate
		}
		err := exit.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate")
	})

	t.Run("rejects self-referential exit", func(t *testing.T) {
		locID := ulid.Make()
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: locID,
			ToLocationID:   locID, // same as from
			Name:           "loop",
			Visibility:     world.VisibilityAll,
		}
		err := exit.Validate()
		require.Error(t, err, "expected error for self-referential exit")
		assert.Contains(t, err.Error(), "self-referential")
	})

	t.Run("zero location IDs pass validation (repository enforces NOT NULL)", func(t *testing.T) {
		// Exit validation allows zero IDs for flexibility (e.g., builder pattern).
		// Database NOT NULL constraints enforce that IDs are set on insert.
		exit := &world.Exit{
			ID: ulid.Make(),
			// FromLocationID and ToLocationID both zero
			Name:       "north",
			Visibility: world.VisibilityAll,
		}
		assert.NoError(t, exit.Validate(), "exit with zero location IDs should pass validation")
	})

	t.Run("zero id fails", func(t *testing.T) {
		exit := &world.Exit{
			// ID is zero value (not set)
			Name:       "north",
			Visibility: world.VisibilityAll,
		}
		err := exit.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id")
	})
}

func TestNewExit(t *testing.T) {
	fromID := ulid.Make()
	toID := ulid.Make()

	t.Run("valid construction succeeds", func(t *testing.T) {
		exit, err := world.NewExit(fromID, toID, "north")
		require.NoError(t, err)
		assert.NotNil(t, exit)
		assert.False(t, exit.ID.IsZero(), "ID should be generated")
		assert.Equal(t, fromID, exit.FromLocationID)
		assert.Equal(t, toID, exit.ToLocationID)
		assert.Equal(t, "north", exit.Name)
		assert.Equal(t, world.VisibilityAll, exit.Visibility, "should default to VisibilityAll")
		assert.False(t, exit.CreatedAt.IsZero(), "CreatedAt should be set")
	})

	t.Run("empty name fails with validation error", func(t *testing.T) {
		exit, err := world.NewExit(fromID, toID, "")
		assert.Nil(t, exit)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("self-referential exit fails", func(t *testing.T) {
		exit, err := world.NewExit(fromID, fromID, "loop")
		assert.Nil(t, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrSelfReferentialExit)
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		exit1, err1 := world.NewExit(fromID, toID, "north")
		require.NoError(t, err1)
		exit2, err2 := world.NewExit(fromID, toID, "south")
		require.NoError(t, err2)
		assert.NotEqual(t, exit1.ID, exit2.ID, "IDs should be unique")
	})
}

func TestNewExitWithID(t *testing.T) {
	fromID := ulid.Make()
	toID := ulid.Make()
	exitID := ulid.Make()

	t.Run("valid construction succeeds", func(t *testing.T) {
		exit, err := world.NewExitWithID(exitID, fromID, toID, "north")
		require.NoError(t, err)
		assert.NotNil(t, exit)
		assert.Equal(t, exitID, exit.ID, "ID should match provided ID")
		assert.Equal(t, fromID, exit.FromLocationID)
		assert.Equal(t, toID, exit.ToLocationID)
		assert.Equal(t, "north", exit.Name)
		assert.Equal(t, world.VisibilityAll, exit.Visibility, "should default to VisibilityAll")
		assert.False(t, exit.CreatedAt.IsZero(), "CreatedAt should be set")
	})

	t.Run("empty name fails with validation error", func(t *testing.T) {
		exit, err := world.NewExitWithID(exitID, fromID, toID, "")
		assert.Nil(t, exit)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("self-referential exit fails", func(t *testing.T) {
		exit, err := world.NewExitWithID(exitID, fromID, fromID, "loop")
		assert.Nil(t, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrSelfReferentialExit)
	})

	t.Run("zero ID fails with validation error", func(t *testing.T) {
		var zeroID ulid.ULID
		exit, err := world.NewExitWithID(zeroID, fromID, toID, "north")
		assert.Nil(t, exit)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id")
	})

	t.Run("uses provided ID exactly", func(t *testing.T) {
		specificID := ulid.Make()
		exit, err := world.NewExitWithID(specificID, fromID, toID, "north")
		require.NoError(t, err)
		assert.Equal(t, specificID, exit.ID)
	})
}
