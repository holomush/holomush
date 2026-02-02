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

func TestMoveHandler_SuccessfulMoveShowsNewRoom(t *testing.T) {
	characterID := ulid.Make()
	fromLocationID := ulid.Make()
	toLocationID := ulid.Make()
	exitID := ulid.Make()

	toLoc := &world.Location{
		ID:          toLocationID,
		Name:        "Destination Room",
		Description: "A beautiful garden with flowers.",
		Type:        world.LocationTypePersistent,
	}

	exit, err := world.NewExitWithID(exitID, fromLocationID, toLocationID, "north")
	require.NoError(t, err)
	exit.Aliases = []string{"n"}

	char := &world.Character{
		ID:         characterID,
		Name:       "TestChar",
		LocationID: &fromLocationID,
	}

	exitRepo := worldtest.NewMockExitRepository(t)
	locationRepo := worldtest.NewMockLocationRepository(t)
	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)
	eventEmitter := worldtest.NewMockEventEmitter(t)

	subjectID := "char:" + characterID.String()

	// GetExitsByLocation check
	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+fromLocationID.String()).
		Return(true)
	exitRepo.EXPECT().
		ListFromLocation(mock.Anything, fromLocationID).
		Return([]*world.Exit{exit}, nil)

	// MoveCharacter checks
	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "write", "character:"+characterID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, characterID).
		Return(char, nil)
	locationRepo.EXPECT().
		Get(mock.Anything, toLocationID).
		Return(toLoc, nil)
	characterRepo.EXPECT().
		UpdateLocation(mock.Anything, characterID, &toLocationID).
		Return(nil)
	eventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	// GetLocation for new room display
	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+toLocationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, toLocationID).
		Return(toLoc, nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		ExitRepo:      exitRepo,
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
		EventEmitter:  eventEmitter,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  fromLocationID,
		Args:        "north",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err = MoveHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Destination Room")
	assert.Contains(t, output, "A beautiful garden with flowers.")
}

func TestMoveHandler_MatchesExitAlias(t *testing.T) {
	characterID := ulid.Make()
	fromLocationID := ulid.Make()
	toLocationID := ulid.Make()
	exitID := ulid.Make()

	toLoc := &world.Location{
		ID:          toLocationID,
		Name:        "Garden",
		Description: "A lovely garden.",
		Type:        world.LocationTypePersistent,
	}

	exit, err := world.NewExitWithID(exitID, fromLocationID, toLocationID, "north")
	require.NoError(t, err)
	exit.Aliases = []string{"n", "forward"}

	char := &world.Character{
		ID:         characterID,
		Name:       "TestChar",
		LocationID: &fromLocationID,
	}

	exitRepo := worldtest.NewMockExitRepository(t)
	locationRepo := worldtest.NewMockLocationRepository(t)
	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)
	eventEmitter := worldtest.NewMockEventEmitter(t)

	subjectID := "char:" + characterID.String()

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+fromLocationID.String()).
		Return(true)
	exitRepo.EXPECT().
		ListFromLocation(mock.Anything, fromLocationID).
		Return([]*world.Exit{exit}, nil)

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "write", "character:"+characterID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, characterID).
		Return(char, nil)
	locationRepo.EXPECT().
		Get(mock.Anything, toLocationID).
		Return(toLoc, nil)
	characterRepo.EXPECT().
		UpdateLocation(mock.Anything, characterID, &toLocationID).
		Return(nil)
	eventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+toLocationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, toLocationID).
		Return(toLoc, nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		ExitRepo:      exitRepo,
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
		EventEmitter:  eventEmitter,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  fromLocationID,
		Args:        "n", // Using alias
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err = MoveHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Garden")
}

func TestMoveHandler_InvalidDirectionReturnsError(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()
	exitID := ulid.Make()

	// Only a north exit exists
	exit, err := world.NewExitWithID(exitID, locationID, ulid.Make(), "north")
	require.NoError(t, err)

	exitRepo := worldtest.NewMockExitRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	subjectID := "char:" + characterID.String()

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+locationID.String()).
		Return(true)
	exitRepo.EXPECT().
		ListFromLocation(mock.Anything, locationID).
		Return([]*world.Exit{exit}, nil)

	worldService := world.NewService(world.ServiceConfig{
		ExitRepo:      exitRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "south", // Invalid - no south exit
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err = MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.Contains(t, msg, "can't go that way")
}

func TestMoveHandler_NoExitsReturnsError(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	exitRepo := worldtest.NewMockExitRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	subjectID := "char:" + characterID.String()

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+locationID.String()).
		Return(true)
	exitRepo.EXPECT().
		ListFromLocation(mock.Anything, locationID).
		Return([]*world.Exit{}, nil)

	worldService := world.NewService(world.ServiceConfig{
		ExitRepo:      exitRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "north",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.Contains(t, msg, "can't go that way")
}

func TestMoveHandler_NoDirectionReturnsError(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	worldService := world.NewService(world.ServiceConfig{
		AccessControl: worldtest.NewMockAccessControl(t),
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.Contains(t, msg, "Usage:")
}

func TestMoveHandler_CaseInsensitiveMatching(t *testing.T) {
	characterID := ulid.Make()
	fromLocationID := ulid.Make()
	toLocationID := ulid.Make()
	exitID := ulid.Make()

	toLoc := &world.Location{
		ID:          toLocationID,
		Name:        "Garden",
		Description: "A lovely garden.",
		Type:        world.LocationTypePersistent,
	}

	exit, err := world.NewExitWithID(exitID, fromLocationID, toLocationID, "North")
	require.NoError(t, err)

	char := &world.Character{
		ID:         characterID,
		Name:       "TestChar",
		LocationID: &fromLocationID,
	}

	exitRepo := worldtest.NewMockExitRepository(t)
	locationRepo := worldtest.NewMockLocationRepository(t)
	characterRepo := worldtest.NewMockCharacterRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)
	eventEmitter := worldtest.NewMockEventEmitter(t)

	subjectID := "char:" + characterID.String()

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+fromLocationID.String()).
		Return(true)
	exitRepo.EXPECT().
		ListFromLocation(mock.Anything, fromLocationID).
		Return([]*world.Exit{exit}, nil)

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "write", "character:"+characterID.String()).
		Return(true)
	characterRepo.EXPECT().
		Get(mock.Anything, characterID).
		Return(char, nil)
	locationRepo.EXPECT().
		Get(mock.Anything, toLocationID).
		Return(toLoc, nil)
	characterRepo.EXPECT().
		UpdateLocation(mock.Anything, characterID, &toLocationID).
		Return(nil)
	eventEmitter.EXPECT().
		Emit(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+toLocationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, toLocationID).
		Return(toLoc, nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		ExitRepo:      exitRepo,
		CharacterRepo: characterRepo,
		AccessControl: accessControl,
		EventEmitter:  eventEmitter,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  fromLocationID,
		Args:        "NORTH", // uppercase
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err = MoveHandler(context.Background(), exec)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Garden")
}

func TestMoveHandler_GetExitsFailureReturnsError(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	exitRepo := worldtest.NewMockExitRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	subjectID := "char:" + characterID.String()

	accessControl.EXPECT().
		Check(mock.Anything, subjectID, "read", "location:"+locationID.String()).
		Return(true)
	exitRepo.EXPECT().
		ListFromLocation(mock.Anything, locationID).
		Return(nil, errors.New("database error"))

	worldService := world.NewService(world.ServiceConfig{
		ExitRepo:      exitRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "north",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := MoveHandler(context.Background(), exec)
	require.Error(t, err)

	msg := command.PlayerMessage(err)
	assert.NotEmpty(t, msg)
}
