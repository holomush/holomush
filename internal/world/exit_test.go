// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

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

		reverse := exit.ReverseExit()
		assert.NotNil(t, reverse)
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
		assert.Nil(t, exit.ReverseExit())
	})

	t.Run("no return name returns nil", func(t *testing.T) {
		exit := &world.Exit{
			ID:            ulid.Make(),
			Name:          "north",
			Bidirectional: true,
			ReturnName:    "",
		}
		assert.Nil(t, exit.ReverseExit())
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

		reverse := exit.ReverseExit()
		assert.NotNil(t, reverse)

		// Modify the reverse exit's mutable fields
		reverse.LockData["key_id"] = "modified-key"
		reverse.VisibleTo = append(reverse.VisibleTo, ulid.Make())

		// Original should be unchanged
		assert.Equal(t, "original-key", exit.LockData["key_id"], "modifying reverse should not affect original LockData")
		assert.Len(t, exit.VisibleTo, 1, "modifying reverse should not affect original VisibleTo")
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

		reverse := exit.ReverseExit()
		assert.NotNil(t, reverse)
		assert.Empty(t, reverse.Aliases, "reverse exit should not inherit aliases")
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
