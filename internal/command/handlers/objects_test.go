// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

func TestCreateHandler_Object(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	objectRepo := worldtest.NewMockObjectRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "object:*").
		Return(true)
	objectRepo.EXPECT().
		Create(mock.Anything, mock.MatchedBy(func(obj *world.Object) bool {
			return obj.Name == "Iron Sword" && obj.LocationID() != nil && *obj.LocationID() == locationID
		})).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		ObjectRepo:    objectRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `object "Iron Sword"`,
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := CreateHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Created object")
	assert.Contains(t, output, "Iron Sword")
}

func TestCreateHandler_Location(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "location:*").
		Return(true)
	locationRepo.EXPECT().
		Create(mock.Anything, mock.MatchedBy(func(loc *world.Location) bool {
			return loc.Name == "Secret Room" && loc.Type == world.LocationTypePersistent
		})).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `location "Secret Room"`,
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := CreateHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Created location")
	assert.Contains(t, output, "Secret Room")
}

func TestCreateHandler_InvalidType(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	accessControl := worldtest.NewMockAccessControl(t)

	worldService := world.NewService(world.ServiceConfig{
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `invalid "Test"`,
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := CreateHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Unknown type")
}

func TestCreateHandler_InvalidSyntax(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"empty args", ""},
		{"no quotes", "object Sword"},
		{"missing type", `"Sword"`},
		{"missing name", "object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			characterID := ulid.Make()
			locationID := ulid.Make()

			accessControl := worldtest.NewMockAccessControl(t)

			worldService := world.NewService(world.ServiceConfig{
				AccessControl: accessControl,
			})

			var buf bytes.Buffer
			exec := &command.CommandExecution{
				CharacterID: characterID,
				LocationID:  locationID,
				Args:        tt.args,
				Output:      &buf,
				Services:    &command.Services{World: worldService},
			}

			err := CreateHandler(context.Background(), exec)
			require.NoError(t, err)

			output := buf.String()
			assert.Contains(t, output, "Usage:")
		})
	}
}

func TestSetHandler_Description(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// For GetLocation
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, locationID).
		Return(&world.Location{
			ID:   locationID,
			Name: "Test Room",
			Type: world.LocationTypePersistent,
		}, nil)

	// For UpdateLocation
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Update(mock.Anything, mock.MatchedBy(func(loc *world.Location) bool {
			return loc.Description == "A cozy room."
		})).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of here to A cozy room.",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := SetHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Set description")
}

func TestSetHandler_PrefixMatch(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// For GetLocation
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, locationID).
		Return(&world.Location{
			ID:   locationID,
			Name: "Test Room",
			Type: world.LocationTypePersistent,
		}, nil)

	// For UpdateLocation
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Update(mock.Anything, mock.Anything).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "desc of here to Short description.",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := SetHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	// Should resolve "desc" to "description"
	assert.Contains(t, output, "Set description")
}

func TestSetHandler_PropertyNotFound(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	accessControl := worldtest.NewMockAccessControl(t)

	worldService := world.NewService(world.ServiceConfig{
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "xyz of here to value",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := SetHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "property not found")
}

func TestSetHandler_InvalidTarget(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	accessControl := worldtest.NewMockAccessControl(t)

	worldService := world.NewService(world.ServiceConfig{
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of nonexistent to value",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := SetHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Error:")
}

func TestSetHandler_InvalidSyntax(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"empty args", ""},
		{"missing of", "description here to value"},
		{"missing to", "description of here value"},
		{"incomplete", "description of"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			characterID := ulid.Make()
			locationID := ulid.Make()

			accessControl := worldtest.NewMockAccessControl(t)

			worldService := world.NewService(world.ServiceConfig{
				AccessControl: accessControl,
			})

			var buf bytes.Buffer
			exec := &command.CommandExecution{
				CharacterID: characterID,
				LocationID:  locationID,
				Args:        tt.args,
				Output:      &buf,
				Services:    &command.Services{World: worldService},
			}

			err := SetHandler(context.Background(), exec)
			require.NoError(t, err)

			output := buf.String()
			assert.Contains(t, output, "Usage:")
		})
	}
}

func TestSetHandler_SetName(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// For GetLocation
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, locationID).
		Return(&world.Location{
			ID:   locationID,
			Name: "Test Room",
			Type: world.LocationTypePersistent,
		}, nil)

	// For UpdateLocation
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Update(mock.Anything, mock.MatchedBy(func(loc *world.Location) bool {
			return loc.Name == "New Room Name"
		})).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "name of here to New Room Name",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err := SetHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Set name")
}

func TestSetHandler_DirectIDReference(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()
	objectID := ulid.Make()

	// Create a properly constructed object with containment
	obj, err := world.NewObjectWithID(objectID, "Test Object", world.InLocation(locationID))
	require.NoError(t, err)

	objectRepo := worldtest.NewMockObjectRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// For GetObject
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "object:"+objectID.String()).
		Return(true)
	objectRepo.EXPECT().
		Get(mock.Anything, objectID).
		Return(obj, nil)

	// For UpdateObject
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "object:"+objectID.String()).
		Return(true)
	objectRepo.EXPECT().
		Update(mock.Anything, mock.MatchedBy(func(obj *world.Object) bool {
			return obj.Description == "A shiny object."
		})).
		Return(nil)

	worldService := world.NewService(world.ServiceConfig{
		ObjectRepo:    objectRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of #" + objectID.String() + " to A shiny object.",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	err = SetHandler(context.Background(), exec)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Set description")
}
