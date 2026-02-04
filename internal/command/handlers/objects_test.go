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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `object "Iron Sword"`,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `location "Secret Room"`,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `invalid "Test"`,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := CreateHandler(context.Background(), exec)
	require.Error(t, err)

	// Verify structured error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
	assert.Equal(t, "create", oopsErr.Context()["command"])
	assert.Contains(t, oopsErr.Context()["usage"], "valid types: object, location")
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
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: characterID,
				LocationID:  locationID,
				Args:        tt.args,
				Output:      &buf,
				Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
			})

			err := CreateHandler(context.Background(), exec)
			require.Error(t, err)

			// Verify structured error code
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok, "error should be oops error")
			assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
			assert.Equal(t, "create", oopsErr.Context()["command"])
			assert.Contains(t, oopsErr.Context()["usage"], "create <type>")
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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of here to A cozy room.",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "desc of here to Short description.",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "xyz of here to value",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of nonexistent to value",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of #invalid-id to value",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: characterID,
				LocationID:  locationID,
				Args:        tt.args,
				Output:      &buf,
				Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
			})

			err := SetHandler(context.Background(), exec)
			require.Error(t, err)

			// Verify structured error code
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok, "error should be oops error")
			assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
			assert.Equal(t, "set", oopsErr.Context()["command"])
			assert.Contains(t, oopsErr.Context()["usage"], "set <property> of <target> to <value>")
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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "name of here to New Room Name",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of #" + objectID.String() + " to A shiny object.",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `object "Iron Sword"`,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `location "Secret Room"`,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of here to A cozy room.",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	// Handler should return error (not nil) when service fails
	err := SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "optimistic locking conflict")

	// Verify structured error - code comes from world service (deepest error)
	// but handler wrapper adds additional context (entity_type, entity_id, property, operation)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	// World service provides specific code - more specific than generic WORLD_ERROR
	assert.Equal(t, "LOCATION_UPDATE_FAILED", oopsErr.Code())
	// Handler wrapper adds context for debugging
	assert.Equal(t, "location", oopsErr.Context()["entity_type"])
	assert.Equal(t, locationID.String(), oopsErr.Context()["entity_id"])
	assert.Equal(t, "description", oopsErr.Context()["property"])
	assert.Equal(t, "update", oopsErr.Context()["operation"])

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
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of #" + objectID.String() + " to A shiny object.",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	// Handler should return error (not nil) when service fails
	err = SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission revoked")

	// Verify structured error - code comes from world service (deepest error)
	// but handler wrapper adds additional context (entity_type, entity_id, property, operation)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	// World service provides specific code - more specific than generic WORLD_ERROR
	assert.Equal(t, "OBJECT_UPDATE_FAILED", oopsErr.Code())
	// Handler wrapper adds context for debugging
	assert.Equal(t, "object", oopsErr.Context()["entity_type"])
	assert.Equal(t, objectID.String(), oopsErr.Context()["entity_id"])
	assert.Equal(t, "description", oopsErr.Context()["property"])
	assert.Equal(t, "update", oopsErr.Context()["operation"])

	// User sees sanitized message, not internal error details
	output := buf.String()
	assert.Contains(t, output, "Failed to set property. Please try again.")
	assert.NotContains(t, output, "permission revoked")   // internal error not exposed
	assert.NotContains(t, output, "update object failed") // internal error not exposed
}

func TestSetHandler_GetLocationFailure(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()

	locationRepo := worldtest.NewMockLocationRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// For GetLocation - fails (e.g., location not found in database)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "location:"+locationID.String()).
		Return(true)
	locationRepo.EXPECT().
		Get(mock.Anything, locationID).
		Return(nil, errors.New("location not found in database"))

	worldService := world.NewService(world.ServiceConfig{
		LocationRepo:  locationRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of here to A cozy room.",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "location not found in database")

	// Verify structured error - code comes from world service (deepest error)
	// but handler wrapper adds additional context (entity_type, entity_id, operation)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	// World service provides specific code - more specific than generic WORLD_ERROR
	assert.Equal(t, "LOCATION_GET_FAILED", oopsErr.Code())
	// Handler wrapper adds context for debugging
	assert.Equal(t, "location", oopsErr.Context()["entity_type"])
	assert.Equal(t, locationID.String(), oopsErr.Context()["entity_id"])
	assert.Equal(t, "get", oopsErr.Context()["operation"])

	// User sees sanitized message
	output := buf.String()
	assert.Contains(t, output, "Failed to set property. Please try again.")
}

func TestSetHandler_GetObjectFailure(t *testing.T) {
	characterID := ulid.Make()
	locationID := ulid.Make()
	objectID := ulid.Make()

	objectRepo := worldtest.NewMockObjectRepository(t)
	accessControl := worldtest.NewMockAccessControl(t)

	// For GetObject - fails (e.g., object not found in database)
	accessControl.EXPECT().
		Check(mock.Anything, "char:"+characterID.String(), "read", "object:"+objectID.String()).
		Return(true)
	objectRepo.EXPECT().
		Get(mock.Anything, objectID).
		Return(nil, errors.New("object not found in database"))

	worldService := world.NewService(world.ServiceConfig{
		ObjectRepo:    objectRepo,
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of #" + objectID.String() + " to A shiny object.",
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object not found in database")

	// Verify structured error - code comes from world service (deepest error)
	// but handler wrapper adds additional context (entity_type, entity_id, operation)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	// World service provides specific code - more specific than generic WORLD_ERROR
	assert.Equal(t, "OBJECT_GET_FAILED", oopsErr.Code())
	// Handler wrapper adds context for debugging
	assert.Equal(t, "object", oopsErr.Context()["entity_type"])
	assert.Equal(t, objectID.String(), oopsErr.Context()["entity_id"])
	assert.Equal(t, "get", oopsErr.Context()["operation"])

	// User sees sanitized message
	output := buf.String()
	assert.Contains(t, output, "Failed to set property. Please try again.")
}

func TestSetHandler_UnsupportedEntityType(t *testing.T) {
	// This tests the applyProperty function's handling of unsupported entity types.
	// Currently, "character" entity type is not yet supported.
	// The "me" target resolves to character type.

	characterID := ulid.Make()
	locationID := ulid.Make()

	accessControl := worldtest.NewMockAccessControl(t)

	worldService := world.NewService(world.ServiceConfig{
		AccessControl: accessControl,
	})

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        "description of me to Some bio text.", // "me" resolves to character type
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := SetHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setting properties on characters not yet supported")

	// Verify structured error with context
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
	assert.Equal(t, "character", oopsErr.Context()["entity_type"])
	assert.Equal(t, "description", oopsErr.Context()["property"])

	// User sees sanitized message
	output := buf.String()
	assert.Contains(t, output, "Failed to set property. Please try again.")
}

func TestCreateHandler_Object_InvalidName(t *testing.T) {
	// Tests that CreateHandler properly handles validation errors from world.NewObject.
	// A name exceeding MaxNameLength (100 chars) triggers a ValidationError during object construction.
	// Note: Empty names are rejected at the regex parsing level (createPattern requires at least one char),
	// so we need a name that passes parsing but fails validation.

	characterID := ulid.Make()
	locationID := ulid.Make()

	accessControl := worldtest.NewMockAccessControl(t)

	worldService := world.NewService(world.ServiceConfig{
		AccessControl: accessControl,
	})

	// Create a name that exceeds MaxNameLength (100 chars)
	longNameBytes := make([]byte, world.MaxNameLength+1)
	for i := range longNameBytes {
		longNameBytes[i] = 'a'
	}
	longName := string(longNameBytes)

	var buf bytes.Buffer
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID: characterID,
		LocationID:  locationID,
		Args:        `object "` + longName + `"`,
		Output:      &buf,
		Services:    command.NewTestServices(command.ServicesConfig{World: worldService}),
	})

	err := CreateHandler(context.Background(), exec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum length")

	// Verify structured error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "error should be oops error")
	assert.Equal(t, command.CodeWorldError, oopsErr.Code())

	// User sees sanitized message, not internal error details
	output := buf.String()
	assert.Contains(t, output, "Failed to create object.")
	assert.NotContains(t, output, "exceeds maximum length") // internal error not exposed
}
