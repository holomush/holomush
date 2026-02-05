// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

// PlayerContext captures common player identity fields.
type PlayerContext struct {
	CharacterID ulid.ULID
	PlayerID    ulid.ULID
	Name        string
}

// NewPlayer creates a basic player context with a name.
func NewPlayer(name string) PlayerContext {
	return PlayerContext{
		CharacterID: ulid.Make(),
		PlayerID:    ulid.Make(),
		Name:        name,
	}
}

// AdminPlayer returns a default admin player context.
func AdminPlayer() PlayerContext {
	return NewPlayer("Admin")
}

// RegularPlayer returns a default non-admin player context.
func RegularPlayer() PlayerContext {
	return NewPlayer("Player")
}

// NewRoom creates a persistent location with a name and description.
func NewRoom(name, description string) *world.Location {
	return &world.Location{
		ID:          ulid.Make(),
		Name:        name,
		Description: description,
		Type:        world.LocationTypePersistent,
	}
}

// ExitContext bundles two rooms and a connecting exit.
type ExitContext struct {
	From *world.Location
	To   *world.Location
	Exit *world.Exit
}

// NewExitContext creates an exit and matching rooms for move tests.
func NewExitContext(t *testing.T, direction string, aliases ...string) ExitContext {
	from := NewRoom("From Room", "")
	to := NewRoom("To Room", "")
	exitID := ulid.Make()

	exit, err := world.NewExitWithID(exitID, from.ID, to.ID, direction)
	require.NoError(t, err)
	if len(aliases) > 0 {
		exit.Aliases = aliases
	}

	return ExitContext{
		From: from,
		To:   to,
		Exit: exit,
	}
}
