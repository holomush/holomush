// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/world"
)

func strPtr(s string) *string { return &s }

// setupHomeMove sets up the mock expectations for MoveCharacter + GetLocation.
func setupHomeMove(t *testing.T, fixture *testutil.WorldServiceFixture, player testutil.PlayerContext, currentLocID, destLoc *world.Location) {
	t.Helper()
	subjectID := access.CharacterSubject(player.CharacterID.String())
	char := &world.Character{
		ID:         player.CharacterID,
		Name:       player.Name,
		LocationID: &currentLocID.ID,
	}

	// MoveCharacter access check
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "write",
			Resource: access.CharacterResource(player.CharacterID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, destLoc.ID).
		Return(destLoc, nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		UpdateLocation(mock.Anything, player.CharacterID, &destLoc.ID).
		Return(nil)
	fixture.Mocks.EventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(2)

	// GetLocation after move
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "read",
			Resource: "location:" + destLoc.ID.String(),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, destLoc.ID).
		Return(destLoc, nil)
}

// setupHomePropRead sets up the mock expectations for ListPropertiesByParent (property access check).
func setupHomePropRead(fixture *testutil.WorldServiceFixture, player testutil.PlayerContext, props []*world.EntityProperty) {
	subjectID := access.CharacterSubject(player.CharacterID.String())
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "read",
			Resource: access.PropertyResource("character:" + player.CharacterID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.PropertyRepo.EXPECT().
		ListByParent(mock.Anything, "character", player.CharacterID).
		Return(props, nil)
}

func TestHomeHandler_MoveToHomeLocation(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	homeLoc := testutil.NewRoom("Home Sweet Home", "Your cozy home.")
	homeLocID := homeLoc.ID

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	homeProp := &world.EntityProperty{
		ID:         ulid.Make(),
		ParentType: "character",
		ParentID:   player.CharacterID,
		Name:       "home",
		Value:      strPtr(homeLocID.String()),
	}
	setupHomePropRead(fixture, player, []*world.EntityProperty{homeProp})
	setupHomeMove(t, fixture, player, currentLoc, homeLoc)

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithServices(services).
		Build()

	err := HomeHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Home Sweet Home")
}

func TestHomeHandler_AlreadyAtHome(t *testing.T) {
	player := testutil.RegularPlayer()
	homeLoc := testutil.NewRoom("Home Sweet Home", "Your cozy home.")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	homeProp := &world.EntityProperty{
		ID:         ulid.Make(),
		ParentType: "character",
		ParentID:   player.CharacterID,
		Name:       "home",
		Value:      strPtr(homeLoc.ID.String()),
	}
	setupHomePropRead(fixture, player, []*world.EntityProperty{homeProp})
	// No move expectations — character is already at home

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(homeLoc). // already at home
		WithServices(services).
		Build()

	err := HomeHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "You are already home.")
}

func TestHomeHandler_NoHomeSetUsesDefault(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	defaultLoc := testutil.NewRoom("The Nexus", "The default starting location.")
	defaultLocID := defaultLoc.ID

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	// No home property set
	setupHomePropRead(fixture, player, []*world.EntityProperty{})
	setupHomeMove(t, fixture, player, currentLoc, defaultLoc)

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithStartingLocationID(defaultLocID).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithServices(services).
		Build()

	err := HomeHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "The Nexus")
}

func TestHomeHandler_NoHomeNoDefault(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupHomePropRead(fixture, player, []*world.EntityProperty{})

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		// No StartingLocationID set (zero value)
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithServices(services).
		Build()

	err := HomeHandler(context.Background(), exec)
	require.NoError(t, err) // outputs a message, not an error
	assert.Contains(t, buf.String(), "You have no home location set.")
}

func TestHomeHandler_HomeLocationDeleted(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	deletedLocID := ulid.Make()

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	homeProp := &world.EntityProperty{
		ID:         ulid.Make(),
		ParentType: "character",
		ParentID:   player.CharacterID,
		Name:       "home",
		Value:      strPtr(deletedLocID.String()),
	}
	setupHomePropRead(fixture, player, []*world.EntityProperty{homeProp})

	// MoveCharacter — GetLocation fails (location was deleted)
	subjectID := access.CharacterSubject(player.CharacterID.String())
	char := &world.Character{
		ID:         player.CharacterID,
		Name:       player.Name,
		LocationID: &currentLoc.ID,
	}
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "write",
			Resource: access.CharacterResource(player.CharacterID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, deletedLocID).
		Return(nil, errors.New("location not found"))

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithServices(services).
		Build()

	err := HomeHandler(context.Background(), exec)
	require.NoError(t, err) // outputs a message, not an error
	assert.Contains(t, buf.String(), "Your home location no longer exists.")
}

func TestHomeHandler_RegisteredWithNilCapabilities(t *testing.T) {
	reg := command.NewRegistry()
	RegisterAll(reg)
	entry, found := reg.Get("home")
	require.True(t, found, "home command should be registered")
	assert.Nil(t, entry.GetCapabilities(), "home should require no capabilities")
}
