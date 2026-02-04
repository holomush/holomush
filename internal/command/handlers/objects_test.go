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

	// Handler should return error (not nil) when property is not found
	err := SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "property not found")

	// User sees sanitized message, not internal error details
	output := buf.String()
	assert.Contains(t, output, "Unknown property: xyz")
	assert.NotContains(t, output, "property not found") // internal error not exposed
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

	// Handler should return error (not nil) when target is not found
	err := SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target not found")

	// Verify structured error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	assert.Equal(t, command.CodeTargetNotFound, oopsErr.Code())
	assert.Equal(t, "nonexistent", oopsErr.Context()["target"])

	// User sees sanitized message, not internal error details
	output := buf.String()
	assert.Contains(t, output, "Could not find target: nonexistent")
	assert.NotContains(t, output, "target not found:") // internal error not exposed
}

func TestSetHandler_InvalidIDFormat(t *testing.T) {
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
		Args:        "description of #invalid-id to value",
		Output:      &buf,
		Services:    &command.Services{World: worldService},
	}

	// Handler should return error (not nil) when ID is invalid
	err := SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target ID format")

	// Verify structured error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
	assert.Equal(t, "#invalid-id", oopsErr.Context()["target"])

	// User sees sanitized message, not internal error details
	output := buf.String()
	assert.Contains(t, output, "Could not find target: #invalid-id")
	assert.NotContains(t, output, "invalid target ID format") // internal error not exposed
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

func TestCreateHandler_ObjectServiceError(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	objectRepo := worldtest.NewMockObjectRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "object:*").
		Return(true)
	objectRepo.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(errors.New("database unavailable"))

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

	// Handler should return error (not nil) when service fails
	err := CreateHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database unavailable")

	output := buf.String()
	assert.Contains(t, output, "Failed to create object")
}

func TestCreateHandler_LocationServiceError(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "location:*").
		Return(true)
	locationRepo.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(errors.New("creation failed: constraint violation"))

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

	// Handler should return error (not nil) when service fails
	err := CreateHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "constraint violation")

	output := buf.String()
	assert.Contains(t, output, "Failed to create location")
}

func TestSetHandler_UpdateLocationFailure(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// For GetLocation - succeeds
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

	// For UpdateLocation - fails (e.g., optimistic locking conflict)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Update(mock.Anything, mock.Anything).
		Return(errors.New("optimistic locking conflict"))

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

	// Handler should return error (not nil) when service fails
	err := SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "optimistic locking conflict")

	// User sees sanitized message, not internal error details
	output := buf.String()
	assert.Contains(t, output, "Failed to set property. Please try again.")
	assert.NotContains(t, output, "optimistic locking conflict") // internal error not exposed
	assert.NotContains(t, output, "update location failed")      // internal error not exposed
}

func TestSetHandler_UpdateObjectFailure(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()
	objectID := ulid.Make()

	// Create a properly constructed object with containment
	obj, err := world.NewObjectWithID(objectID, "Test Object", world.InLocation(locationID))
	require.NoError(t, err)

	objectRepo := worldtest.NewMockObjectRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// For GetObject - succeeds
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "object:"+objectID.String()).
		Return(true)
	objectRepo.EXPECT().
		Get(mock.Anything, objectID).
		Return(obj, nil)

	// For UpdateObject - fails (e.g., access control change)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "write", "object:"+objectID.String()).
		Return(true)
	objectRepo.EXPECT().
		Update(mock.Anything, mock.Anything).
		Return(errors.New("access denied: permission revoked"))

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

	// Handler should return error (not nil) when service fails
	err = SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission revoked")

	// User sees sanitized message, not internal error details
	output := buf.String()
	assert.Contains(t, output, "Failed to set property. Please try again.")
	assert.NotContains(t, output, "permission revoked")    // internal error not exposed
	assert.NotContains(t, output, "update object failed")  // internal error not exposed
}
