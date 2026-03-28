// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
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
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

func TestDescribeHandler_Me(t *testing.T) {
	charID := ulid.Make()
	locationID := ulid.Make()

	charRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// UpdateCharacterDescription checks "write" on the character resource.
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  access.CharacterSubject(charID.String()),
			Action:   "write",
			Resource: access.CharacterResource(charID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)

	charRepo.EXPECT().
		Get(mock.Anything, charID).
		Return(&world.Character{ID: charID, Name: "TestChar"}, nil)
	charRepo.EXPECT().
		Update(mock.Anything, mock.MatchedBy(func(c *world.Character) bool {
			return c.Description == "A tall figure"
		})).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		CharacterRepo: charRepo,
		Engine:        accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: charID,
		LocationID:  locationID,
		Args:        "me A tall figure",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := DescribeHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Description set.")
}

func TestDescribeHandler_Here(t *testing.T) {
	charID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// applyProperty calls GetLocation (read) then UpdateLocation (write).
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  access.CharacterSubject(charID.String()),
			Action:   "read",
			Resource: access.LocationResource(locationID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	locationRepo.EXPECT().
		Get(mock.Anything, locationID).
		Return(&world.Location{ID: locationID, Name: "Test Room", Type: world.LocationTypePersistent}, nil)

	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  access.CharacterSubject(charID.String()),
			Action:   "write",
			Resource: access.LocationResource(locationID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	locationRepo.EXPECT().
		Update(mock.Anything, mock.MatchedBy(func(loc *world.Location) bool {
			return loc.Description == "A dusty chamber"
		})).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo: locationRepo,
		Engine:       accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: charID,
		LocationID:  locationID,
		Args:        "here A dusty chamber",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := DescribeHandler(context.Background(), exec)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Description set.")
}

func TestDescribeHandler_TargetEquals(t *testing.T) {
	charID := ulid.Make()
	locationID := ulid.Make()
	objectID := ulid.Make()

	objectRepo := worldtest.NewMockObjectRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// applyProperty calls GetObject (read) then UpdateObject (write).
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  access.CharacterSubject(charID.String()),
			Action:   "read",
			Resource: access.ObjectResource(objectID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	sword, err := world.NewObjectWithID(objectID, "Sword", world.InLocation(locationID))
	require.NoError(t, err)
	objectRepo.EXPECT().
		Get(mock.Anything, objectID).
		Return(sword, nil)

	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  access.CharacterSubject(charID.String()),
			Action:   "write",
			Resource: access.ObjectResource(objectID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	objectRepo.EXPECT().
		Update(mock.Anything, mock.MatchedBy(func(obj *world.Object) bool {
			return obj.Description == "A gleaming blade"
		})).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		ObjectRepo: objectRepo,
		Engine:     accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: charID,
		LocationID:  locationID,
		Args:        "#" + objectID.String() + "=A gleaming blade",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	descErr := DescribeHandler(context.Background(), exec)
	require.NoError(t, descErr)
	assert.Contains(t, buf.String(), "Description set.")
}

func TestDescribeHandler_NoText(t *testing.T) {
	charID := ulid.Make()
	locationID := ulid.Make()

	accessControl := worldtest.NewMockAccessPolicyEngine(t)
	worldService := world.NewService(world.ServiceConfig{Engine: accessControl})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: charID,
		LocationID:  locationID,
		Args:        "me",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := DescribeHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error")
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

func TestDescribeHandler_EmptyArgs(t *testing.T) {
	charID := ulid.Make()
	locationID := ulid.Make()

	accessControl := worldtest.NewMockAccessPolicyEngine(t)
	worldService := world.NewService(world.ServiceConfig{Engine: accessControl})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: charID,
		LocationID:  locationID,
		Args:        "",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := DescribeHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error")
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

func TestDescribeHandler_HerePermissionDenied(t *testing.T) {
	charID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessPolicyEngine(t)

	// GetLocation (read) succeeds.
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  access.CharacterSubject(charID.String()),
			Action:   "read",
			Resource: access.LocationResource(locationID.String()),
		}).
		Return(types.NewDecision(types.EffectAllow, "", ""), nil)
	locationRepo.EXPECT().
		Get(mock.Anything, locationID).
		Return(&world.Location{ID: locationID, Name: "Test Room", Type: world.LocationTypePersistent}, nil)

	// UpdateLocation (write) is denied.
	accessControl.EXPECT().
		Evaluate(mock.Anything, types.AccessRequest{
			Subject:  access.CharacterSubject(charID.String()),
			Action:   "write",
			Resource: access.LocationResource(locationID.String()),
		}).
		Return(types.NewDecision(types.EffectDeny, "", ""), nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo: locationRepo,
		Engine:       accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: charID,
		LocationID:  locationID,
		Args:        "here A dusty chamber",
		Output:      &buf,
		Services: command.NewTestServices(command.ServicesConfig{
			World:  worldService,
			Engine: policytest.AllowAllEngine(),
		}),
	})

	err := DescribeHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, world.ErrPermissionDenied), "expected permission denied error, got: %v", err)
}
