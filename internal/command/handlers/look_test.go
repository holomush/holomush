// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

func TestLookHandler_OutputsRoomNameAndDescription(t *testing.T) {
	locationID := ulid.Make()
	characterID := ulid.Make()

	loc := &world.Location{
		ID:          locationID,
		Name:        "Test Room",
		Description: "A cozy room with a fireplace.",
		Type:        world.LocationTypePersistent,
	}

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, locationID).
		Return(loc, nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := LookHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Test Room")
	assert.Contains(t, output, "A cozy room with a fireplace.")
}

func TestLookHandler_ReturnsWorldErrorOnFailure(t *testing.T) {
	locationID := ulid.Make()
	characterID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, locationID).
		Return(nil, errors.New("database error"))

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := LookHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify it returns a WorldError
	msg := command.PlayerMessage(err)
	assert.NotEmpty(t, msg)
}

func TestLookHandler_ReturnsWorldErrorOnAccessDenied(t *testing.T) {
	locationID := ulid.Make()
	characterID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "location:"+locationID.String()).
		Return(false)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := LookHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.NotEmpty(t, msg)
}
