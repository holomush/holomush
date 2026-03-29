// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

// setupTeleportMove configures mock expectations for a successful MoveCharacter + GetLocation.
func setupTeleportMove(t *testing.T, fixture *testutil.WorldServiceFixture, player testutil.PlayerContext, currentLoc, destLoc *world.Location) {
	t.Helper()
	subjectID := access.CharacterSubject(player.CharacterID.String())
	char := &world.Character{
		ID:         player.CharacterID,
		Name:       player.Name,
		LocationID: &currentLoc.ID,
	}

	// MoveCharacter access check (write character)
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

	// GetLocation after move (read location)
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

// setupTeleportMoveOther configures mock expectations for moving another character.
func setupTeleportMoveOther(t *testing.T, fixture *testutil.WorldServiceFixture, admin, target testutil.PlayerContext, targetCurrentLoc, destLoc *world.Location) {
	t.Helper()
	subjectID := access.CharacterSubject(admin.CharacterID.String())
	char := &world.Character{
		ID:         target.CharacterID,
		Name:       target.Name,
		LocationID: &targetCurrentLoc.ID,
	}

	// MoveCharacter access check (write target character)
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "write",
			Resource: access.CharacterResource(target.CharacterID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, target.CharacterID).
		Return(char, nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, destLoc.ID).
		Return(destLoc, nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		UpdateLocation(mock.Anything, target.CharacterID, &destLoc.ID).
		Return(nil)
	fixture.Mocks.EventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(2)

	// GetLocation after move (read location for admin)
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

// setupFindLocationByName configures mock expectations for FindLocationByName.
func setupFindLocationByName(fixture *testutil.WorldServiceFixture, subjectID string, loc *world.Location) {
	// FindLocationByName does a read access check on location:*
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "read",
			Resource: "location:*",
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.LocationRepo.EXPECT().
		FindByName(mock.Anything, loc.Name).
		Return(loc, nil)
}

// setupFindLocationByNameNotFound configures mock expectations for location not found.
func setupFindLocationByNameNotFound(fixture *testutil.WorldServiceFixture, subjectID, name string) {
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "read",
			Resource: "location:*",
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.LocationRepo.EXPECT().
		FindByName(mock.Anything, name).
		Return(nil, world.ErrNotFound)
}

func TestTeleportHandler_NoArgs(t *testing.T) {
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(testutil.RegularPlayer()).
		WithServices(testutil.NewServicesBuilder().Build()).
		WithArgs("").
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

func TestTeleportHandler_TeleportToLocation(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	destLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)
	setupTeleportMove(t, fixture, player, currentLoc, destLoc)

	// Admin-level engine: allow all capability checks
	engine := policytest.AllowAllEngine()
	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "The Library")
	assert.Contains(t, buf.String(), "A grand library.")
}

func TestTeleportHandler_LocationNotFound(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByNameNotFound(fixture, subjectID, "Nowhere")

	engine := policytest.AllowAllEngine()
	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("Nowhere").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `No location found named "Nowhere".`)
}

func TestTeleportHandler_AlreadyAtLocation(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, currentLoc)

	engine := policytest.AllowAllEngine()
	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "You are already at The Library.")
}

func TestTeleportHandler_TeleportOtherAdmin(t *testing.T) {
	admin := testutil.AdminPlayer()
	target := testutil.NewPlayer("Sean")
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	targetLoc := testutil.NewRoom("Target Room", "Target is here.")
	destLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(admin.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)
	setupTeleportMoveOther(t, fixture, admin, target, targetLoc, destLoc)

	engine := policytest.AllowAllEngine()
	mockSess := testutil.NewMockSessionAccess(&session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   target.CharacterID,
		CharacterName: target.Name,
		LocationID:    targetLoc.ID,
		Status:        session.StatusActive,
	})

	store := core.NewMemoryEventStore()
	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		WithSession(mockSess).
		WithEvents(store).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(admin).
		WithLocation(currentLoc).
		WithArgs("Sean=The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "The Library")

	// Verify notification was sent to target
	events, replayErr := store.Replay(context.Background(), "session:"+target.CharacterID.String(), ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.NotEmpty(t, events)
}

func TestTeleportHandler_TeleportOtherTargetNotFound(t *testing.T) {
	admin := testutil.AdminPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	destLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(admin.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)

	engine := policytest.AllowAllEngine()
	// Empty session mock — no one named "Ghost" exists
	mockSess := testutil.NewMockSessionAccess()

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		WithSession(mockSess).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(admin).
		WithLocation(currentLoc).
		WithArgs("Ghost=The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `No character found named "Ghost".`)
}

func TestTeleportHandler_DefaultRoleNonHomeDenied(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	destLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)

	// No home property — use starting location which is different from destLoc
	setupHomePropRead(fixture, player, []*world.EntityProperty{})

	// Deny all capability checks (default role user)
	engine := policytest.DenyAllEngine()
	startingLocID := ulid.Make() // Different from destLoc

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		WithStartingLocationID(startingLocID).
		Build()

	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodePermissionDenied, oopsErr.Code())
}

func TestTeleportHandler_DefaultRoleHomeAllowed(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	homeLoc := testutil.NewRoom("Home Sweet Home", "Your cozy home.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, homeLoc)

	// Set home property to homeLoc
	homeProp := &world.EntityProperty{
		ID:         ulid.Make(),
		ParentType: "character",
		ParentID:   player.CharacterID,
		Name:       "home",
		Value:      strPtr(homeLoc.ID.String()),
	}
	setupHomePropRead(fixture, player, []*world.EntityProperty{homeProp})
	setupTeleportMove(t, fixture, player, currentLoc, homeLoc)

	// Deny all capability checks — default role
	engine := policytest.DenyAllEngine()

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("Home Sweet Home").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Home Sweet Home")
}

func TestTeleportHandler_BuilderAnySelf(t *testing.T) {
	player := testutil.NewPlayer("Builder")
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	destLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)
	setupTeleportMove(t, fixture, player, currentLoc, destLoc)

	// Custom engine: deny admin.boot (not admin), allow build.teleport (is builder)
	engine := &policytest.MockAccessPolicyEngine{}
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "admin.boot",
	}).Return(types.NewDecision(types.EffectDeny, "test", ""), nil)
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "build.teleport",
	}).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "The Library")
}

func TestTeleportHandler_BuilderCannotTeleportOthers(t *testing.T) {
	player := testutil.NewPlayer("Builder")
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	destLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)

	// Builder engine: deny admin capabilities
	engine := &policytest.MockAccessPolicyEngine{}
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "admin.boot",
	}).Return(types.NewDecision(types.EffectDeny, "test", ""), nil)

	target := testutil.NewPlayer("Sean")
	mockSess := testutil.NewMockSessionAccess(&session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   target.CharacterID,
		CharacterName: target.Name,
		Status:        session.StatusActive,
	})

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		WithSession(mockSess).
		Build()

	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("Sean=The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodePermissionDenied, oopsErr.Code())
}

func TestTeleportHandler_AdminAnyLocationAnyTarget(t *testing.T) {
	admin := testutil.AdminPlayer()
	target := testutil.NewPlayer("Sean")
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	targetLoc := testutil.NewRoom("Target Room", "Target is here.")
	destLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(admin.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)
	setupTeleportMoveOther(t, fixture, admin, target, targetLoc, destLoc)

	engine := policytest.AllowAllEngine()
	mockSess := testutil.NewMockSessionAccess(&session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   target.CharacterID,
		CharacterName: target.Name,
		LocationID:    targetLoc.ID,
		Status:        session.StatusActive,
	})
	store := core.NewMemoryEventStore()

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		WithSession(mockSess).
		WithEvents(store).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(admin).
		WithLocation(currentLoc).
		WithArgs("Sean=The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "The Library")
}

func TestTeleportHandler_BuilderAllowedDepartureAllowedDestination(t *testing.T) {
	player := testutil.NewPlayer("Builder")
	currentLoc := testutil.NewRoom("Workshop", "A builder's workshop.")
	destLoc := testutil.NewRoom("Gallery", "An art gallery.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)
	setupTeleportMove(t, fixture, player, currentLoc, destLoc)

	// Builder: deny admin, allow build
	engine := &policytest.MockAccessPolicyEngine{}
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "admin.boot",
	}).Return(types.NewDecision(types.EffectDeny, "test", ""), nil)
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "build.teleport",
	}).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("Gallery").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Gallery")
}

func TestTeleportHandler_BuilderDeniedDestination(t *testing.T) {
	player := testutil.NewPlayer("Builder")
	currentLoc := testutil.NewRoom("Workshop", "A builder's workshop.")
	destLoc := testutil.NewRoom("Forbidden Zone", "You shall not pass.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       player.Name,
		LocationID: &currentLoc.ID,
	}

	// MoveCharacter access check — denied at character write
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "write",
			Resource: access.CharacterResource(player.CharacterID.String()),
		}).
		Return(types.NewDecision(types.EffectDeny, "access-denied", ""), nil)

	// The world service also gets the character for the move
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil).Maybe()

	// Builder engine: deny admin, allow build
	engine := &policytest.MockAccessPolicyEngine{}
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "admin.boot",
	}).Return(types.NewDecision(types.EffectDeny, "test", ""), nil)
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "build.teleport",
	}).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("Forbidden Zone").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.Error(t, err)
	// The error should propagate from the world service — could be permission denied or access eval failed
}

func TestTeleportHandler_BuilderDeniedDepartureAllowedDestination(t *testing.T) {
	// This is the same as denied destination — MoveCharacter handles both departure
	// and destination as a single operation. The world service's access check is on
	// the character write, not per-location. So "denied departure" is the same as
	// "denied destination" from the handler's perspective.
	player := testutil.NewPlayer("Builder")
	currentLoc := testutil.NewRoom("Locked Room", "You're locked in.")
	destLoc := testutil.NewRoom("Gallery", "An art gallery.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)

	// MoveCharacter — world service denies the move
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "write",
			Resource: access.CharacterResource(player.CharacterID.String()),
		}).
		Return(types.NewDecision(types.EffectDeny, "access-denied", ""), nil)

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       player.Name,
		LocationID: &currentLoc.ID,
	}
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil).Maybe()

	// Builder: deny admin, allow build
	engine := &policytest.MockAccessPolicyEngine{}
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "admin.boot",
	}).Return(types.NewDecision(types.EffectDeny, "test", ""), nil)
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "build.teleport",
	}).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("Gallery").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.Error(t, err)
}

func TestTeleportHandler_BuilderDeniedBoth(t *testing.T) {
	player := testutil.NewPlayer("Builder")
	currentLoc := testutil.NewRoom("Locked Room", "You're locked in.")
	destLoc := testutil.NewRoom("Forbidden Zone", "You shall not pass.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)

	// MoveCharacter — world service denies the move
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  subjectID,
			Action:   "write",
			Resource: access.CharacterResource(player.CharacterID.String()),
		}).
		Return(types.NewDecision(types.EffectDeny, "access-denied", ""), nil)

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       player.Name,
		LocationID: &currentLoc.ID,
	}
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil).Maybe()

	// Builder: deny admin, allow build
	engine := &policytest.MockAccessPolicyEngine{}
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "admin.boot",
	}).Return(types.NewDecision(types.EffectDeny, "test", ""), nil)
	engine.On("Evaluate", mock.Anything, types.AccessRequest{
		Subject:  subjectID,
		Action:   "execute",
		Resource: "build.teleport",
	}).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("Forbidden Zone").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.Error(t, err)
}

func TestTeleportHandler_MoveCharacterError(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	destLoc := testutil.NewRoom("The Library", "A grand library.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, destLoc)

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       player.Name,
		LocationID: &currentLoc.ID,
	}

	// MoveCharacter — write check passes but UpdateLocation fails
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
		Return(errors.New("database error"))

	engine := policytest.AllowAllEngine()
	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("The Library").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Teleport failed.")
}

func TestTeleportHandler_RegisteredWithNilCapabilities(t *testing.T) {
	reg := command.NewRegistry()
	RegisterAll(reg)
	entry, found := reg.Get("teleport")
	require.True(t, found, "teleport command should be registered")
	assert.Nil(t, entry.GetCapabilities(), "teleport should require no capabilities")
}

func TestTeleportHandler_DefaultRoleUsesStartingLocationAsHome(t *testing.T) {
	player := testutil.RegularPlayer()
	currentLoc := testutil.NewRoom("Starting Room", "You are here.")
	defaultLoc := testutil.NewRoom("The Nexus", "The default starting location.")
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	setupFindLocationByName(fixture, subjectID, defaultLoc)

	// No home property set — falls back to starting location
	setupHomePropRead(fixture, player, []*world.EntityProperty{})
	setupTeleportMove(t, fixture, player, currentLoc, defaultLoc)

	// Deny all capability checks — default role
	engine := policytest.DenyAllEngine()

	services := testutil.NewServicesBuilder().
		WithWorldFixture(fixture).
		WithEngine(engine).
		WithStartingLocationID(defaultLoc.ID).
		Build()

	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(currentLoc).
		WithArgs("The Nexus").
		WithServices(services).
		Build()

	err := TeleportHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "The Nexus")
}
