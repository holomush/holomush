// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/world"
)

func TestMoveHandler_SuccessfulMoveShowsNewRoom(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "north", "n")
	path.To.Name = "Destination Room"
	path.To.Description = "A beautiful garden with flowers."

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       "TestChar",
		LocationID: &path.From.ID,
	}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "write", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(path.To, nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		UpdateLocation(mock.Anything, player.CharacterID, &path.To.ID).
		Return(nil)
	fixture.Mocks.EventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.To.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(path.To, nil)

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("north").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Destination Room")
	assert.Contains(t, buf.String(), "A beautiful garden with flowers.")
}

func TestMoveHandler_MatchesExitAlias(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "north", "n", "forward")
	path.To.Name = "Garden"
	path.To.Description = "A lovely garden."

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       "TestChar",
		LocationID: &path.From.ID,
	}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "write", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(path.To, nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		UpdateLocation(mock.Anything, player.CharacterID, &path.To.ID).
		Return(nil)
	fixture.Mocks.EventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.To.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(path.To, nil)

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("n").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Garden")
}

func TestMoveHandler_InvalidDirectionReturnsError(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "north")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("south").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.Contains(t, msg, "can't go that way")
}

func TestMoveHandler_NoExitsReturnsError(t *testing.T) {
	player := testutil.RegularPlayer()
	location := testutil.NewRoom("Lonely Room", "")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + location.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, location.ID).
		Return([]*world.Exit{}, nil)

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(location).
		WithArgs("north").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.Contains(t, msg, "can't go that way")
}

func TestMoveHandler_NoDirectionReturnsError(t *testing.T) {
	player := testutil.RegularPlayer()
	location := testutil.NewRoom("Silent Room", "")
	fixture := testutil.NewWorldServiceBuilder(t).Build()

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(location).
		WithArgs("").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.Contains(t, msg, "Usage:")
}

func TestMoveHandler_CaseInsensitiveMatching(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "North")
	path.To.Name = "Garden"
	path.To.Description = "A lovely garden."

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       "TestChar",
		LocationID: &path.From.ID,
	}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "write", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(path.To, nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		UpdateLocation(mock.Anything, player.CharacterID, &path.To.ID).
		Return(nil)
	fixture.Mocks.EventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.To.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(path.To, nil)

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("NORTH").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Garden")
}

func TestMoveHandler_GetExitsFailureReturnsError(t *testing.T) {
	player := testutil.RegularPlayer()
	location := testutil.NewRoom("Hallway", "")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + location.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, location.ID).
		Return(nil, errors.New("database error"))

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(location).
		WithArgs("north").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.NotEmpty(t, msg)
}

func TestMoveHandler_MoveCharacterFailure(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "north")

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       "TestChar",
		LocationID: &path.From.ID,
	}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "write", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(nil, errors.New("concurrent modification: location deleted"))

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("north").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify the error is wrapped with player-facing message in context
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be an oops error")
	assert.Equal(t, "Something prevents you from going that way.", oopsErr.Context()["message"])
}

func TestMoveHandler_LockedExitReturnsError(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "north")

	err := path.Exit.SetLocked(true, world.LockTypeKey, map[string]any{"key_id": "golden-key"})
	require.NoError(t, err)

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("north").
		WithServices(services).
		Build()

	err = MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.Contains(t, msg, "locked")
}

func TestMoveHandler_GetLocationFailureAfterMove(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "north")
	path.To.Name = "Destination Room"
	path.To.Description = "A room that will disappear."

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       "TestChar",
		LocationID: &path.From.ID,
	}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "write", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Once()
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil).Once()
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(path.To, nil).Once()
	fixture.Mocks.CharacterRepo.EXPECT().
		UpdateLocation(mock.Anything, player.CharacterID, &path.To.ID).
		Return(nil).Once()
	fixture.Mocks.EventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.To.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil).Once()
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(nil, errors.New("location not found: deleted between move and lookup")).Once()

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("north").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify the error is wrapped with player-facing message in context
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be an oops error")
	assert.Equal(t, "You arrive somewhere strange...", oopsErr.Context()["message"])
}

func TestMoveHandler_AccessEvaluationFailureOnGetExits(t *testing.T) {
	player := testutil.RegularPlayer()
	location := testutil.NewRoom("Hallway", "")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + location.ID.String()}).
		Return(types.Decision{}, errors.New("policy engine timeout"))

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(location).
		WithArgs("north").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify the error contains ErrAccessEvaluationFailed
	assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	// Verify it's an oops error with the world-specific code (not the generic command handler code)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be an oops error")
	assert.Equal(t, "EXIT_ACCESS_EVALUATION_FAILED", oopsErr.Code(),
		"handler should preserve world service's specific code, not wrap as WORLD_ERROR")
}

func TestMoveHandler_AccessEvaluationFailureOnMoveCharacter(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "north")

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	// Engine fails during character move permission check
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "write", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.Decision{}, errors.New("policy engine database error"))

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("north").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify the error contains ErrAccessEvaluationFailed
	assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	// Verify it's an oops error with the world-specific code (not the generic command handler code)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be an oops error")
	assert.Equal(t, "CHARACTER_ACCESS_EVALUATION_FAILED", oopsErr.Code(),
		"handler should preserve world service's specific code, not wrap as WORLD_ERROR")
}

func TestMoveHandler_AccessEvaluationFailureOnGetLocationAfterMove(t *testing.T) {
	player := testutil.RegularPlayer()
	path := testutil.NewExitContext(t, "north")
	path.To.Name = "Destination Room"

	char := &world.Character{
		ID:         player.CharacterID,
		Name:       "TestChar",
		LocationID: &path.From.ID,
	}

	fixture := testutil.NewWorldServiceBuilder(t).Build()
	subjectID := access.CharacterSubject(player.CharacterID.String())

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.From.ID.String()}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.ExitRepo.EXPECT().
		ListFromLocation(mock.Anything, path.From.ID).
		Return([]*world.Exit{path.Exit}, nil)

	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "write", Resource: access.CharacterSubject(player.CharacterID.String())}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		Get(mock.Anything, player.CharacterID).
		Return(char, nil)
	fixture.Mocks.LocationRepo.EXPECT().
		Get(mock.Anything, path.To.ID).
		Return(path.To, nil)
	fixture.Mocks.CharacterRepo.EXPECT().
		UpdateLocation(mock.Anything, player.CharacterID, &path.To.ID).
		Return(nil)
	fixture.Mocks.EventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	// Engine fails when trying to read the new location
	fixture.Mocks.Engine.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{Subject: subjectID, Action: "read", Resource: "location:" + path.To.ID.String()}).
		Return(types.Decision{}, errors.New("policy engine connection lost"))

	services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(player).
		WithLocation(path.From).
		WithArgs("north").
		WithServices(services).
		Build()

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify the error contains ErrAccessEvaluationFailed
	assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	// Verify it's an oops error with the world-specific code (not the generic command handler code)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be an oops error")
	assert.Equal(t, "LOCATION_ACCESS_EVALUATION_FAILED", oopsErr.Code(),
		"handler should preserve world service's specific code, not wrap as WORLD_ERROR")
}
