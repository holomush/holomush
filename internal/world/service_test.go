// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
	"github.com/holomush/holomush/pkg/errutil"
)

// mockAccessControl is a test mock for access.AccessControl.
type mockAccessControl struct {
	mock.Mock
}

func (m *mockAccessControl) Check(ctx context.Context, subject, action, resource string) bool {
	args := m.Called(ctx, subject, action, resource)
	return args.Bool(0)
}

func TestWorldService_GetLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("returns location when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		expectedLoc := &world.Location{ID: locID, Name: "Test Room"}

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, locID).Return(expectedLoc, nil)

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		require.NoError(t, err)
		assert.Equal(t, expectedLoc, loc)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locID.String()).Return(false)

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateLocation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	t.Run("creates location when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{
			Name:        "New Room",
			Description: "A test room",
			Type:        world.LocationTypePersistent,
		}

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)
		mockRepo.EXPECT().Create(ctx, mock.MatchedBy(func(l *world.Location) bool {
			return l.Name == "New Room" && !l.ID.IsZero()
		})).Return(nil)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.NoError(t, err)
		assert.False(t, loc.ID.IsZero(), "ID should be generated")
		mockAC.AssertExpectations(t)
	})

	t.Run("preserves existing ID when already set", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		existingID := ulid.Make()
		loc := &world.Location{
			ID:          existingID,
			Name:        "New Room",
			Description: "A test room",
			Type:        world.LocationTypePersistent,
		}

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)
		mockRepo.EXPECT().Create(ctx, mock.MatchedBy(func(l *world.Location) bool {
			return l.ID == existingID
		})).Return(nil)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.NoError(t, err)
		assert.Equal(t, existingID, loc.ID, "pre-set ID should be preserved")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{Name: "New Room"}

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(false)

		err := svc.CreateLocation(ctx, subjectID, loc)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_UpdateLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("updates location when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{ID: locID, Name: "Updated Room", Type: world.LocationTypePersistent}

		mockAC.On("Check", ctx, subjectID, "write", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Update(ctx, loc).Return(nil)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{ID: locID, Name: "Updated Room"}

		mockAC.On("Check", ctx, subjectID, "write", "location:"+locID.String()).Return(false)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns not found when location does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{ID: locID, Name: "Updated Room", Type: world.LocationTypePersistent}

		mockAC.On("Check", ctx, subjectID, "write", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Update(ctx, loc).Return(world.ErrNotFound)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_DeleteLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("deletes location when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Delete(ctx, locID).Return(nil)

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "location:"+locID.String()).Return(false)

		err := svc.DeleteLocation(ctx, subjectID, locID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Delete(ctx, locID).Return(errors.New("db error"))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_GetExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("returns exit when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		expectedExit := &world.Exit{ID: exitID, Name: "north"}

		mockAC.On("Check", ctx, subjectID, "read", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Get(ctx, exitID).Return(expectedExit, nil)

		exit, err := svc.GetExit(ctx, subjectID, exitID)
		require.NoError(t, err)
		assert.Equal(t, expectedExit, exit)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "exit:"+exitID.String()).Return(false)

		exit, err := svc.GetExit(ctx, subjectID, exitID)
		assert.Nil(t, exit)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateExit(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("creates exit when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)
		mockExitRepo.EXPECT().Create(ctx, mock.MatchedBy(func(e *world.Exit) bool {
			return e.Name == "north" && !e.ID.IsZero()
		})).Return(nil)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.NoError(t, err)
		assert.False(t, exit.ID.IsZero(), "ID should be generated")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{Name: "north"}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(false)

		err := svc.CreateExit(ctx, subjectID, exit)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_UpdateExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("updates exit when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{ID: exitID, Name: "north updated", Visibility: world.VisibilityAll}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Update(ctx, exit).Return(nil)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(false)

		err := svc.UpdateExit(ctx, subjectID, exit)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Update(ctx, exit).Return(errors.New("db error"))

		err := svc.UpdateExit(ctx, subjectID, exit)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns not found when exit does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Update(ctx, exit).Return(world.ErrNotFound)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "EXIT_NOT_FOUND")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_DeleteExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("deletes exit when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Delete(ctx, exitID).Return(nil)

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "exit:"+exitID.String()).Return(false)

		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})

	t.Run("handles cleanup result for bidirectional exit", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		toLocationID := ulid.Make()
		cleanupResult := &world.BidirectionalCleanupResult{
			ExitID:       exitID,
			ToLocationID: toLocationID,
			ReturnName:   "south",
			Issue: &world.CleanupIssue{
				Type: world.CleanupReturnNotFound,
			},
		}

		mockAC.On("Check", ctx, subjectID, "delete", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Delete(ctx, exitID).Return(cleanupResult)

		// Should succeed since primary delete worked
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.NoError(t, err)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_GetObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("returns object when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		expectedObj := &world.Object{ID: objID, Name: "sword"}

		mockAC.On("Check", ctx, subjectID, "read", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Get(ctx, objID).Return(expectedObj, nil)

		obj, err := svc.GetObject(ctx, subjectID, objID)
		require.NoError(t, err)
		assert.Equal(t, expectedObj, obj)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "object:"+objID.String()).Return(false)

		obj, err := svc.GetObject(ctx, subjectID, objID)
		assert.Nil(t, obj)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateObject(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	locationID := ulid.Make()

	t.Run("creates object when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{Name: "sword", LocationID: &locationID}

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(true)
		mockObjRepo.EXPECT().Create(ctx, mock.MatchedBy(func(o *world.Object) bool {
			return o.Name == "sword" && !o.ID.IsZero()
		})).Return(nil)

		err := svc.CreateObject(ctx, subjectID, obj)
		require.NoError(t, err)
		assert.False(t, obj.ID.IsZero(), "ID should be generated")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{Name: "sword", LocationID: &locationID}

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(false)

		err := svc.CreateObject(ctx, subjectID, obj)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_UpdateObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	locationID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("updates object when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{ID: objID, Name: "sword updated", LocationID: &locationID}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Update(ctx, obj).Return(nil)

		err := svc.UpdateObject(ctx, subjectID, obj)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{ID: objID, Name: "sword", LocationID: &locationID}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(false)

		err := svc.UpdateObject(ctx, subjectID, obj)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{ID: objID, Name: "sword", LocationID: &locationID}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Update(ctx, obj).Return(errors.New("db error"))

		err := svc.UpdateObject(ctx, subjectID, obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns not found when object does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{ID: objID, Name: "sword", LocationID: &locationID}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Update(ctx, obj).Return(world.ErrNotFound)

		err := svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_DeleteObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("deletes object when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Delete(ctx, objID).Return(nil)

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "object:"+objID.String()).Return(false)

		err := svc.DeleteObject(ctx, subjectID, objID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_MoveObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()
	locationID := ulid.Make()

	t.Run("moves object when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		emitter := &mockEventEmitter{} // nil err means Emit succeeds

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		fromLocID := ulid.Make()
		to := world.Containment{LocationID: &locationID}

		existingObj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(nil)

		err := svc.MoveObject(ctx, subjectID, objID, to)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		to := world.Containment{LocationID: &locationID}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(false)

		err := svc.MoveObject(ctx, subjectID, objID, to)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error for invalid containment", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		// Empty containment is invalid
		to := world.Containment{}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)

		err := svc.MoveObject(ctx, subjectID, objID, to)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
		mockAC.AssertExpectations(t)
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		fromLocID := ulid.Make()
		to := world.Containment{LocationID: &locationID}

		existingObj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(errors.New("db error"))

		err := svc.MoveObject(ctx, subjectID, objID, to)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when object repository not configured", func(t *testing.T) {
		mockAC := &mockAccessControl{}

		svc := world.NewService(world.ServiceConfig{
			AccessControl: mockAC,
		})

		to := world.Containment{LocationID: &locationID}

		err := svc.MoveObject(ctx, subjectID, objID, to)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "object repository not configured")
	})

	t.Run("handles object with no current containment", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		emitter := &mockEventEmitter{} // nil err means Emit succeeds

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		to := world.Containment{LocationID: &locationID}

		// Object with no containment set (not yet placed in world)
		existingObj := &world.Object{
			ID:   objID,
			Name: "Unplaced Object",
			// All containment fields nil
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(nil)

		// Should not panic, should succeed
		err := svc.MoveObject(ctx, subjectID, objID, to)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_AddSceneParticipant(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("adds participant when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)
		mockSceneRepo.EXPECT().AddParticipant(ctx, sceneID, charID, world.RoleMember).Return(nil)

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(false)

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_RemoveSceneParticipant(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("removes participant when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)
		mockSceneRepo.EXPECT().RemoveParticipant(ctx, sceneID, charID).Return(nil)

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(false)

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_ListSceneParticipants(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("lists participants when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		expected := []world.SceneParticipant{
			{CharacterID: charID, Role: world.RoleMember},
		}

		mockAC.On("Check", ctx, subjectID, "read", "scene:"+sceneID.String()).Return(true)
		mockSceneRepo.EXPECT().ListParticipants(ctx, sceneID).Return(expected, nil)

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.NoError(t, err)
		assert.Equal(t, expected, participants)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "scene:"+sceneID.String()).Return(false)

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		assert.Nil(t, participants)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

// --- Input Validation Tests ---

func TestWorldService_CreateLocation_Validation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{
			Name:        "", // Empty name
			Description: "A test room",
			Type:        world.LocationTypePersistent,
		}

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects name exceeding max length", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		longName := make([]byte, world.MaxNameLength+1)
		for i := range longName {
			longName[i] = 'a'
		}

		loc := &world.Location{
			Name: string(longName),
			Type: world.LocationTypePersistent,
		}

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateExit_Validation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("rejects empty name", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "", // Empty name
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateObject_Validation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{
			Name: "", // Empty name
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(true)

		err := svc.CreateObject(ctx, subjectID, obj)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects object without containment", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{
			Name: "orphan", // Valid name but no containment
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(true)

		err := svc.CreateObject(ctx, subjectID, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_AddSceneParticipant_Validation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("rejects invalid role", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.ParticipantRole("invalid"))
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidParticipantRole)
		mockAC.AssertExpectations(t)
	})
}

// --- Repository Error Propagation Tests ---

func TestWorldService_GetLocation_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, locID).Return(nil, errors.New("db error"))

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateLocation_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{
			Name: "Test Room",
			Type: world.LocationTypePersistent,
		}

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateLocation(ctx, subjectID, loc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_GetExit_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Get(ctx, exitID).Return(nil, errors.New("db error"))

		exit, err := svc.GetExit(ctx, subjectID, exitID)
		assert.Nil(t, exit)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateExit_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateExit(ctx, subjectID, exit)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_GetObject_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Get(ctx, objID).Return(nil, errors.New("db error"))

		obj, err := svc.GetObject(ctx, subjectID, objID)
		assert.Nil(t, obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateObject_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	locationID := ulid.Make()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{Name: "sword", LocationID: &locationID}

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(true)
		mockObjRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateObject(ctx, subjectID, obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

// --- Scene Repository Error Propagation Tests ---

func TestWorldService_AddSceneParticipant_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)
		mockSceneRepo.EXPECT().AddParticipant(ctx, sceneID, charID, world.RoleMember).Return(errors.New("db error"))

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_RemoveSceneParticipant_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)
		mockSceneRepo.EXPECT().RemoveParticipant(ctx, sceneID, charID).Return(errors.New("db error"))

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_ListSceneParticipants_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockSceneRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "scene:"+sceneID.String()).Return(true)
		mockSceneRepo.EXPECT().ListParticipants(ctx, sceneID).Return(nil, errors.New("db error"))

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		assert.Nil(t, participants)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		mockAC.AssertExpectations(t)
	})
}

// --- DeleteExit Severe Cleanup Test ---

func TestWorldService_DeleteExit_SevereCleanup(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("propagates severe cleanup error for bidirectional exit", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		toLocationID := ulid.Make()
		cleanupResult := &world.BidirectionalCleanupResult{
			ExitID:       exitID,
			ToLocationID: toLocationID,
			ReturnName:   "south",
			Issue: &world.CleanupIssue{
				Type: world.CleanupFindError,
				Err:  errors.New("database connection lost"),
			},
		}

		mockAC.On("Check", ctx, subjectID, "delete", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Delete(ctx, exitID).Return(cleanupResult)

		// Severe error means the entire operation was rolled back - return error
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete exit")
		mockAC.AssertExpectations(t)
	})

	t.Run("propagates delete error cleanup for bidirectional exit", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		toLocationID := ulid.Make()
		returnExitID := ulid.Make()
		cleanupResult := &world.BidirectionalCleanupResult{
			ExitID:       exitID,
			ToLocationID: toLocationID,
			ReturnName:   "south",
			Issue: &world.CleanupIssue{
				Type:         world.CleanupDeleteError,
				ReturnExitID: returnExitID,
				Err:          errors.New("constraint violation"),
			},
		}

		mockAC.On("Check", ctx, subjectID, "delete", "exit:"+exitID.String()).Return(true)
		mockExitRepo.EXPECT().Delete(ctx, exitID).Return(cleanupResult)

		// Severe error means the entire operation was rolled back - return error
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete exit")
		mockAC.AssertExpectations(t)
	})
}

// --- Update Method Validation Tests ---

func TestWorldService_UpdateLocation_Validation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{
			ID:   locID,
			Name: "", // Empty name
			Type: world.LocationTypePersistent,
		}

		mockAC.On("Check", ctx, subjectID, "write", "location:"+locID.String()).Return(true)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects invalid location type", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{
			ID:   locID,
			Name: "Valid Name",
			Type: world.LocationType("invalid"),
		}

		mockAC.On("Check", ctx, subjectID, "write", "location:"+locID.String()).Return(true)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLocationType)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_UpdateExit_Validation(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			ID:   exitID,
			Name: "", // Empty name
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects invalid visibility", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			ID:         exitID,
			Name:       "north",
			Visibility: world.Visibility("invalid"),
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidVisibility)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects invalid lock type when locked", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			ID:         exitID,
			Name:       "north",
			Visibility: world.VisibilityAll,
			Locked:     true,
			LockType:   world.LockType("invalid"),
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLockType)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_UpdateObject_Validation(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{
			ID:   objID,
			Name: "", // Empty name
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)

		err := svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects object without containment", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{
			ID:   objID,
			Name: "orphan", // Valid name but no containment
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)

		err := svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
		mockAC.AssertExpectations(t)
	})
}

// --- Create Method Extended Validation Tests ---

func TestWorldService_CreateLocation_TypeValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	t.Run("rejects invalid location type", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		loc := &world.Location{
			Name: "Valid Name",
			Type: world.LocationType("invalid"),
		}

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLocationType)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateExit_VisibilityValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("rejects invalid visibility", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.Visibility("invalid"),
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidVisibility)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects invalid lock type when locked", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         true,
			LockType:       world.LockType("invalid"),
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLockType)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects invalid lock data when locked", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         true,
			LockType:       world.LockTypeKey,
			LockData:       map[string]any{"": "empty key is invalid"},
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "lock_data", validationErr.Field)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects invalid visible_to when visibility is list", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		// Create duplicate ULIDs in VisibleTo
		duplicateID := ulid.Make()
		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityList,
			VisibleTo:      []ulid.ULID{duplicateID, duplicateID},
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "visible_to", validationErr.Field)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_UpdateExit_LockDataValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	exitID := ulid.Make()

	t.Run("rejects invalid lock data when locked", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			ID:         exitID,
			Name:       "north",
			Visibility: world.VisibilityAll,
			Locked:     true,
			LockType:   world.LockTypeKey,
			LockData:   map[string]any{"invalid key!": "special chars not allowed"},
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "lock_data", validationErr.Field)
		mockAC.AssertExpectations(t)
	})

	t.Run("rejects invalid visible_to when visibility is list", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		duplicateID := ulid.Make()
		exit := &world.Exit{
			ID:         exitID,
			Name:       "north",
			Visibility: world.VisibilityList,
			VisibleTo:  []ulid.ULID{duplicateID, duplicateID},
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "visible_to", validationErr.Field)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_CreateExit_ValidationBypass(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("accepts unlocked exit with invalid lock type", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         false,
			LockType:       world.LockType("garbage"), // Invalid but should be ignored
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(nil)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.NoError(t, err, "unlocked exit with invalid lock type should succeed")
		mockAC.AssertExpectations(t)
	})

	t.Run("accepts non-list visibility with invalid visible_to", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockExitRepo,
			AccessControl: mockAC,
		})

		// Create duplicate ULIDs in VisibleTo - invalid but should be ignored for VisibilityAll
		duplicateID := ulid.Make()
		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll, // Not "list", so VisibleTo should be ignored
			VisibleTo:      []ulid.ULID{duplicateID, duplicateID},
		}

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(nil)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.NoError(t, err, "non-list visibility with invalid visible_to should succeed")
		mockAC.AssertExpectations(t)
	})
}

// --- NewService Validation Tests ---

func TestNewService_RequiresAccessControl(t *testing.T) {
	t.Run("panics when AccessControl is nil", func(t *testing.T) {
		assert.Panics(t, func() {
			world.NewService(world.ServiceConfig{
				AccessControl: nil,
			})
		})
	})

	t.Run("succeeds with AccessControl provided", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		assert.NotPanics(t, func() {
			svc := world.NewService(world.ServiceConfig{
				AccessControl: mockAC,
			})
			assert.NotNil(t, svc)
		})
	})
}

// --- Nil Input Tests ---

func TestWorldService_CreateLocation_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	mockAC := &mockAccessControl{}
	mockRepo := worldtest.NewMockLocationRepository(t)
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		LocationRepo:  mockRepo,
	})

	mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)

	err := svc.CreateLocation(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_UpdateLocation_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	mockAC := &mockAccessControl{}
	mockRepo := worldtest.NewMockLocationRepository(t)
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		LocationRepo:  mockRepo,
	})

	// Note: nil check happens before access check since we need the ID to build resource string
	err := svc.UpdateLocation(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_CreateExit_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	mockAC := &mockAccessControl{}
	mockRepo := worldtest.NewMockExitRepository(t)
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		ExitRepo:      mockRepo,
	})

	mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)

	err := svc.CreateExit(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_CreateObject_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	mockAC := &mockAccessControl{}
	mockRepo := worldtest.NewMockObjectRepository(t)
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		ObjectRepo:    mockRepo,
	})

	mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(true)

	err := svc.CreateObject(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_UpdateExit_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	mockAC := &mockAccessControl{}
	mockRepo := worldtest.NewMockExitRepository(t)
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		ExitRepo:      mockRepo,
	})

	// Note: nil check happens before access check since we need the ID to build resource string
	err := svc.UpdateExit(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_UpdateObject_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()

	mockAC := &mockAccessControl{}
	mockRepo := worldtest.NewMockObjectRepository(t)
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		ObjectRepo:    mockRepo,
	})

	// Note: nil check happens before access check since we need the ID to build resource string
	err := svc.UpdateObject(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// --- Nil Repository Tests ---

func TestWorldService_NilLocationRepo(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	locID := ulid.Make()

	mockAC := &mockAccessControl{}
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		// LocationRepo intentionally nil
	})

	t.Run("GetLocation returns error", func(t *testing.T) {
		_, err := svc.GetLocation(ctx, subjectID, locID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("CreateLocation returns error", func(t *testing.T) {
		err := svc.CreateLocation(ctx, subjectID, &world.Location{Name: "Test"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("UpdateLocation returns error", func(t *testing.T) {
		err := svc.UpdateLocation(ctx, subjectID, &world.Location{ID: locID, Name: "Test"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("DeleteLocation returns error", func(t *testing.T) {
		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestWorldService_NilExitRepo(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	exitID := ulid.Make()

	mockAC := &mockAccessControl{}
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		// ExitRepo intentionally nil
	})

	t.Run("GetExit returns error", func(t *testing.T) {
		_, err := svc.GetExit(ctx, subjectID, exitID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("CreateExit returns error", func(t *testing.T) {
		err := svc.CreateExit(ctx, subjectID, &world.Exit{Name: "north"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("UpdateExit returns error", func(t *testing.T) {
		err := svc.UpdateExit(ctx, subjectID, &world.Exit{ID: exitID, Name: "north"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("DeleteExit returns error", func(t *testing.T) {
		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestWorldService_NilObjectRepo(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	objID := ulid.Make()

	mockAC := &mockAccessControl{}
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		// ObjectRepo intentionally nil
	})

	t.Run("GetObject returns error", func(t *testing.T) {
		_, err := svc.GetObject(ctx, subjectID, objID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("CreateObject returns error", func(t *testing.T) {
		err := svc.CreateObject(ctx, subjectID, &world.Object{Name: "item"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("UpdateObject returns error", func(t *testing.T) {
		err := svc.UpdateObject(ctx, subjectID, &world.Object{ID: objID, Name: "item"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("DeleteObject returns error", func(t *testing.T) {
		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

func TestWorldService_NilSceneRepo(t *testing.T) {
	ctx := context.Background()
	subjectID := "char:" + ulid.Make().String()
	sceneID := ulid.Make()
	charID := ulid.Make()

	mockAC := &mockAccessControl{}
	svc := world.NewService(world.ServiceConfig{
		AccessControl: mockAC,
		// SceneRepo intentionally nil
	})

	t.Run("AddSceneParticipant returns error", func(t *testing.T) {
		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("RemoveSceneParticipant returns error", func(t *testing.T) {
		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("ListSceneParticipants returns error", func(t *testing.T) {
		_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
	})
}

// --- Error Code Tests ---
// These tests verify that service layer methods return proper oops error codes
// as required for API boundaries (see CLAUDE.md).

func TestService_ErrorCodes_Location(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("GetLocation returns LOCATION_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, locID).Return(nil, world.ErrNotFound)

		_, err := svc.GetLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("GetLocation returns LOCATION_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locID.String()).Return(false)

		_, err := svc.GetLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("GetLocation returns LOCATION_GET_FAILED for other errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, locID).Return(nil, errors.New("db connection failed"))

		_, err := svc.GetLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_GET_FAILED")
	})

	t.Run("CreateLocation returns LOCATION_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(false)

		err := svc.CreateLocation(ctx, subjectID, &world.Location{Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("CreateLocation returns LOCATION_INVALID for validation errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)

		err := svc.CreateLocation(ctx, subjectID, &world.Location{Name: "", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_INVALID")
	})

	t.Run("CreateLocation returns LOCATION_CREATE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "location:*").Return(true)
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateLocation(ctx, subjectID, &world.Location{Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_CREATE_FAILED")
	})

	t.Run("UpdateLocation returns LOCATION_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "location:"+locID.String()).Return(false)

		err := svc.UpdateLocation(ctx, subjectID, &world.Location{ID: locID, Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("UpdateLocation returns LOCATION_INVALID for validation errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "location:"+locID.String()).Return(true)

		err := svc.UpdateLocation(ctx, subjectID, &world.Location{ID: locID, Name: "", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_INVALID")
	})

	t.Run("UpdateLocation returns LOCATION_UPDATE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.UpdateLocation(ctx, subjectID, &world.Location{ID: locID, Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_UPDATE_FAILED")
	})

	t.Run("DeleteLocation returns LOCATION_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "location:"+locID.String()).Return(false)

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("DeleteLocation returns LOCATION_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Delete(ctx, locID).Return(world.ErrNotFound)

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("DeleteLocation returns LOCATION_DELETE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "location:"+locID.String()).Return(true)
		mockRepo.EXPECT().Delete(ctx, locID).Return(errors.New("db error"))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_DELETE_FAILED")
	})
}

func TestService_ErrorCodes_Exit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("GetExit returns EXIT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "exit:"+exitID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, exitID).Return(nil, world.ErrNotFound)

		_, err := svc.GetExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_NOT_FOUND")
	})

	t.Run("GetExit returns EXIT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "exit:"+exitID.String()).Return(false)

		_, err := svc.GetExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("GetExit returns EXIT_GET_FAILED for other errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "exit:"+exitID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, exitID).Return(nil, errors.New("db error"))

		_, err := svc.GetExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_GET_FAILED")
	})

	t.Run("CreateExit returns EXIT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(false)

		err := svc.CreateExit(ctx, subjectID, &world.Exit{Name: "north", FromLocationID: fromLocID, ToLocationID: toLocID, Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("CreateExit returns EXIT_INVALID for validation errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)

		err := svc.CreateExit(ctx, subjectID, &world.Exit{Name: "", FromLocationID: fromLocID, ToLocationID: toLocID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_INVALID")
	})

	t.Run("CreateExit returns EXIT_CREATE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "exit:*").Return(true)
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateExit(ctx, subjectID, &world.Exit{Name: "north", FromLocationID: fromLocID, ToLocationID: toLocID, Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_CREATE_FAILED")
	})

	t.Run("UpdateExit returns EXIT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(false)

		err := svc.UpdateExit(ctx, subjectID, &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("UpdateExit returns EXIT_INVALID for validation errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)

		err := svc.UpdateExit(ctx, subjectID, &world.Exit{ID: exitID, Name: ""})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_INVALID")
	})

	t.Run("UpdateExit returns EXIT_UPDATE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "exit:"+exitID.String()).Return(true)
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.UpdateExit(ctx, subjectID, &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_UPDATE_FAILED")
	})

	t.Run("DeleteExit returns EXIT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "exit:"+exitID.String()).Return(false)

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("DeleteExit returns EXIT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "exit:"+exitID.String()).Return(true)
		mockRepo.EXPECT().Delete(ctx, exitID).Return(world.ErrNotFound)

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_NOT_FOUND")
	})

	t.Run("DeleteExit returns EXIT_DELETE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo:      mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "exit:"+exitID.String()).Return(true)
		mockRepo.EXPECT().Delete(ctx, exitID).Return(errors.New("db error"))

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_DELETE_FAILED")
	})
}

func TestService_ErrorCodes_Object(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	locationID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("GetObject returns OBJECT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, objID).Return(nil, world.ErrNotFound)

		_, err := svc.GetObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("GetObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "object:"+objID.String()).Return(false)

		_, err := svc.GetObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	t.Run("GetObject returns OBJECT_GET_FAILED for other errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, objID).Return(nil, errors.New("db error"))

		_, err := svc.GetObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_GET_FAILED")
	})

	t.Run("CreateObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(false)

		err := svc.CreateObject(ctx, subjectID, &world.Object{Name: "sword", LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	t.Run("CreateObject returns OBJECT_INVALID for validation errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(true)

		err := svc.CreateObject(ctx, subjectID, &world.Object{Name: "", LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_INVALID")
	})

	t.Run("CreateObject returns OBJECT_CREATE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(true)
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateObject(ctx, subjectID, &world.Object{Name: "sword", LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_CREATE_FAILED")
	})

	t.Run("UpdateObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(false)

		err := svc.UpdateObject(ctx, subjectID, &world.Object{ID: objID, Name: "sword", LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	t.Run("UpdateObject returns OBJECT_INVALID for validation errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)

		err := svc.UpdateObject(ctx, subjectID, &world.Object{ID: objID, Name: "", LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_INVALID")
	})

	t.Run("UpdateObject returns OBJECT_UPDATE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.UpdateObject(ctx, subjectID, &world.Object{ID: objID, Name: "sword", LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_UPDATE_FAILED")
	})

	t.Run("DeleteObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "object:"+objID.String()).Return(false)

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	t.Run("DeleteObject returns OBJECT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Delete(ctx, objID).Return(world.ErrNotFound)

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("DeleteObject returns OBJECT_DELETE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "delete", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Delete(ctx, objID).Return(errors.New("db error"))

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_DELETE_FAILED")
	})

	t.Run("MoveObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(false)

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	t.Run("MoveObject returns OBJECT_INVALID for invalid containment", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_INVALID")
	})

	t.Run("MoveObject returns OBJECT_NOT_FOUND for ErrNotFound from Get", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, objID).Return(nil, world.ErrNotFound)

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("MoveObject returns OBJECT_NOT_FOUND for ErrNotFound from Move", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		fromLocID := ulid.Make()
		existingObj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockRepo.EXPECT().Move(ctx, objID, mock.Anything).Return(world.ErrNotFound)

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("MoveObject returns OBJECT_MOVE_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
		})

		fromLocID := ulid.Make()
		existingObj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockRepo.EXPECT().Move(ctx, objID, mock.Anything).Return(errors.New("db error"))

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_MOVE_FAILED")
	})

	t.Run("MoveObject returns EVENT_EMIT_FAILED when event emission fails", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockObjectRepository(t)
		// mockEventEmitter is defined in events_test.go (same package)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		fromLocID := ulid.Make()
		existingObj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockRepo.EXPECT().Move(ctx, objID, mock.Anything).Return(nil)

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		// The inner EVENT_EMIT_FAILED code is returned (from events.go),
		// wrapped by service.go with move_succeeded context
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		// Verify the error context indicates the move succeeded in the database
		errutil.AssertErrorContext(t, err, "move_succeeded", true)
	})
}

func TestService_ErrorCodes_Scene(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("AddSceneParticipant returns SCENE_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(false)

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_DENIED")
	})

	t.Run("AddSceneParticipant returns SCENE_INVALID for invalid role", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.ParticipantRole("invalid"))
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_INVALID")
	})

	t.Run("AddSceneParticipant returns SCENE_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)
		mockRepo.EXPECT().AddParticipant(ctx, sceneID, charID, world.RoleMember).Return(world.ErrNotFound)

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
	})

	t.Run("AddSceneParticipant returns SCENE_ADD_PARTICIPANT_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)
		mockRepo.EXPECT().AddParticipant(ctx, sceneID, charID, world.RoleMember).Return(errors.New("db error"))

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ADD_PARTICIPANT_FAILED")
	})

	t.Run("RemoveSceneParticipant returns SCENE_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(false)

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_DENIED")
	})

	t.Run("RemoveSceneParticipant returns SCENE_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)
		mockRepo.EXPECT().RemoveParticipant(ctx, sceneID, charID).Return(world.ErrNotFound)

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
	})

	t.Run("RemoveSceneParticipant returns SCENE_REMOVE_PARTICIPANT_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "scene:"+sceneID.String()).Return(true)
		mockRepo.EXPECT().RemoveParticipant(ctx, sceneID, charID).Return(errors.New("db error"))

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_REMOVE_PARTICIPANT_FAILED")
	})

	t.Run("ListSceneParticipants returns SCENE_ACCESS_DENIED for permission denied", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "scene:"+sceneID.String()).Return(false)

		_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_DENIED")
	})

	t.Run("ListSceneParticipants returns SCENE_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "scene:"+sceneID.String()).Return(true)
		mockRepo.EXPECT().ListParticipants(ctx, sceneID).Return(nil, world.ErrNotFound)

		_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
	})

	t.Run("ListSceneParticipants returns SCENE_LIST_PARTICIPANTS_FAILED for repo errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo:     mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "scene:"+sceneID.String()).Return(true)
		mockRepo.EXPECT().ListParticipants(ctx, sceneID).Return(nil, errors.New("db error"))

		_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_LIST_PARTICIPANTS_FAILED")
	})
}

func TestWorldService_GetCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("returns character when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			AccessControl: mockAC,
		})

		locID := ulid.Make()
		expectedChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &locID,
		}

		mockAC.On("Check", ctx, subjectID, "read", "character:"+charID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, charID).Return(expectedChar, nil)

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		require.NoError(t, err)
		assert.Equal(t, expectedChar, char)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "character:"+charID.String()).Return(false)

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		assert.Nil(t, char)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when repository not configured", func(t *testing.T) {
		mockAC := &mockAccessControl{}

		svc := world.NewService(world.ServiceConfig{
			AccessControl: mockAC,
		})

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_GET_FAILED")
	})

	t.Run("returns not found when character does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "character:"+charID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		assert.Nil(t, char)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns error when repository fails with generic error", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			AccessControl: mockAC,
		})

		dbErr := errors.New("database connection failed")
		mockAC.On("Check", ctx, subjectID, "read", "character:"+charID.String()).Return(true)
		mockRepo.EXPECT().Get(ctx, charID).Return(nil, dbErr)

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_GET_FAILED")
	})
}

func TestWorldService_GetCharactersByLocation(t *testing.T) {
	ctx := context.Background()
	locationID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("returns characters when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			AccessControl: mockAC,
		})

		char1 := &world.Character{ID: ulid.Make(), Name: "Char1", LocationID: &locationID}
		char2 := &world.Character{ID: ulid.Make(), Name: "Char2", LocationID: &locationID}
		expectedChars := []*world.Character{char1, char2}

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locationID.String()+":characters").Return(true)
		mockRepo.EXPECT().GetByLocation(ctx, locationID).Return(expectedChars, nil)

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID)
		require.NoError(t, err)
		assert.Equal(t, expectedChars, chars)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locationID.String()+":characters").Return(false)

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID)
		assert.Nil(t, chars)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when repository not configured", func(t *testing.T) {
		mockAC := &mockAccessControl{}

		svc := world.NewService(world.ServiceConfig{
			AccessControl: mockAC,
		})

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID)
		assert.Nil(t, chars)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_QUERY_FAILED")
	})

	t.Run("returns error when repository fails", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			AccessControl: mockAC,
		})

		dbErr := errors.New("database connection failed")
		mockAC.On("Check", ctx, subjectID, "read", "location:"+locationID.String()+":characters").Return(true)
		mockRepo.EXPECT().GetByLocation(ctx, locationID).Return(nil, dbErr)

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID)
		assert.Nil(t, chars)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_QUERY_FAILED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns empty slice for location with no characters", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "read", "location:"+locationID.String()+":characters").Return(true)
		mockRepo.EXPECT().GetByLocation(ctx, locationID).Return([]*world.Character{}, nil)

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID)
		require.NoError(t, err)
		assert.Empty(t, chars)
	})
}

func TestWorldService_MoveCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("successful move emits event", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(nil)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.NoError(t, err)

		// Verify event was emitted
		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(toLocID), call.Stream)
		assert.Equal(t, "move", call.EventType)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns CHARACTER_NOT_FOUND when character does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns CHARACTER_MOVE_FAILED when Get fails with generic error", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		dbErr := errors.New("database connection lost")
		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, dbErr)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
		assert.Contains(t, err.Error(), "get character")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns LOCATION_NOT_FOUND when destination does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(nil, world.ErrNotFound)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns CHARACTER_MOVE_FAILED when location verification fails with generic error", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		dbErr := errors.New("database timeout")
		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(nil, dbErr)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
		assert.Contains(t, err.Error(), "verify destination location")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when event emission fails but move_succeeded=true", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(nil)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "event")
		errutil.AssertErrorContext(t, err, "move_succeeded", true)
		mockAC.AssertExpectations(t)
	})

	t.Run("first-time placement emits event with from_type none", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		// Character with no prior location (first-time placement)
		existingChar := &world.Character{
			ID:         charID,
			Name:       "New Character",
			LocationID: nil,
		}

		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(nil)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.NoError(t, err)

		// Verify event was emitted with from_type "none"
		require.Len(t, emitter.calls, 1)
		// Decode the payload to verify from_type
		var payload world.MovePayload
		err = json.Unmarshal(emitter.calls[0].Payload, &payload)
		require.NoError(t, err)
		assert.Equal(t, world.EntityTypeCharacter, payload.EntityType)
		assert.Equal(t, charID, payload.EntityID)
		assert.Equal(t, world.ContainmentTypeNone, payload.FromType)
		assert.Nil(t, payload.FromID)
		assert.Equal(t, world.ContainmentTypeLocation, payload.ToType)
		assert.Equal(t, toLocID, payload.ToID)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns CHARACTER_ACCESS_DENIED when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(false)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when character repository not configured", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
	})

	t.Run("returns ErrNoEventEmitter when emitter not configured", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
			// No EventEmitter configured
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(nil)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNoEventEmitter)
		errutil.AssertErrorCode(t, err, "EVENT_EMITTER_MISSING")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when UpdateLocation fails", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}
		dbErr := errors.New("database error")

		mockAC.On("Check", ctx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(dbErr)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
		mockAC.AssertExpectations(t)
	})

	t.Run("propagates context cancellation error", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{err: errors.New("transient error")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		mockAC.On("Check", cancelCtx, subjectID, "write", "character:"+charID.String()).Return(true)
		mockCharRepo.EXPECT().Get(mock.Anything, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(mock.Anything, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(mock.Anything, charID, &toLocID).Return(nil)

		err := svc.MoveCharacter(cancelCtx, subjectID, charID, toLocID)

		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_ExamineLocation(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	targetLocID := ulid.Make()
	charLocID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("successful examine emits event", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetLoc := &world.Location{
			ID:   targetLocID,
			Name: "Grand Hall",
		}

		mockAC.On("Check", ctx, subjectID, "read", "location:"+targetLocID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockLocRepo.EXPECT().Get(ctx, targetLocID).Return(targetLoc, nil)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.NoError(t, err)

		// Verify event was emitted
		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(charLocID), call.Stream)
		assert.Equal(t, "object_examine", call.EventType)

		var payload world.ExaminePayload
		err = json.Unmarshal(call.Payload, &payload)
		require.NoError(t, err)
		assert.Equal(t, charID, payload.CharacterID)
		assert.Equal(t, world.TargetTypeLocation, payload.TargetType)
		assert.Equal(t, targetLocID, payload.TargetID)
		assert.Equal(t, "Grand Hall", payload.TargetName)
		assert.Equal(t, charLocID, payload.LocationID)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns CHARACTER_NOT_FOUND when examiner does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns LOCATION_NOT_FOUND when target does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockLocRepo.EXPECT().Get(ctx, targetLocID).Return(nil, world.ErrNotFound)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("returns EXAMINE_ACCESS_DENIED when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetLoc := &world.Location{
			ID:   targetLocID,
			Name: "Grand Hall",
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockLocRepo.EXPECT().Get(ctx, targetLocID).Return(targetLoc, nil)
		mockAC.On("Check", ctx, subjectID, "read", "location:"+targetLocID.String()).Return(false)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_DENIED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns ErrNoEventEmitter when emitter not configured", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
			// No EventEmitter configured
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetLoc := &world.Location{
			ID:   targetLocID,
			Name: "Grand Hall",
		}

		mockAC.On("Check", ctx, subjectID, "read", "location:"+targetLocID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockLocRepo.EXPECT().Get(ctx, targetLocID).Return(targetLoc, nil)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNoEventEmitter)
		errutil.AssertErrorCode(t, err, "EVENT_EMITTER_MISSING")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when event emitter fails", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetLoc := &world.Location{
			ID:   targetLocID,
			Name: "Grand Hall",
		}

		mockAC.On("Check", ctx, subjectID, "read", "location:"+targetLocID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockLocRepo.EXPECT().Get(ctx, targetLocID).Return(targetLoc, nil)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when examiner not in world", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: nil, // Not in world yet
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXAMINE_FAILED")
		assert.Contains(t, err.Error(), "not in world")
	})
}

func TestWorldService_ExamineObject(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	targetObjID := ulid.Make()
	charLocID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("successful examine emits event", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetObj := &world.Object{
			ID:   targetObjID,
			Name: "Ancient Chest",
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockObjRepo.EXPECT().Get(ctx, targetObjID).Return(targetObj, nil)
		mockAC.On("Check", ctx, subjectID, "read", "object:"+targetObjID.String()).Return(true)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.NoError(t, err)

		// Verify event was emitted
		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(charLocID), call.Stream)
		assert.Equal(t, "object_examine", call.EventType)

		var payload world.ExaminePayload
		err = json.Unmarshal(call.Payload, &payload)
		require.NoError(t, err)
		assert.Equal(t, charID, payload.CharacterID)
		assert.Equal(t, world.TargetTypeObject, payload.TargetType)
		assert.Equal(t, targetObjID, payload.TargetID)
		assert.Equal(t, "Ancient Chest", payload.TargetName)
		assert.Equal(t, charLocID, payload.LocationID)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns CHARACTER_NOT_FOUND when examiner does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns OBJECT_NOT_FOUND when target does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockObjRepo.EXPECT().Get(ctx, targetObjID).Return(nil, world.ErrNotFound)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("returns EXAMINE_ACCESS_DENIED when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetObj := &world.Object{
			ID:   targetObjID,
			Name: "Ancient Chest",
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockObjRepo.EXPECT().Get(ctx, targetObjID).Return(targetObj, nil)
		mockAC.On("Check", ctx, subjectID, "read", "object:"+targetObjID.String()).Return(false)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_DENIED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns ErrNoEventEmitter when emitter not configured", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
			// No EventEmitter configured
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetObj := &world.Object{
			ID:   targetObjID,
			Name: "Ancient Chest",
		}

		mockAC.On("Check", ctx, subjectID, "read", "object:"+targetObjID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockObjRepo.EXPECT().Get(ctx, targetObjID).Return(targetObj, nil)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNoEventEmitter)
		errutil.AssertErrorCode(t, err, "EVENT_EMITTER_MISSING")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when event emitter fails", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetObj := &world.Object{
			ID:   targetObjID,
			Name: "Ancient Chest",
		}

		mockAC.On("Check", ctx, subjectID, "read", "object:"+targetObjID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockObjRepo.EXPECT().Get(ctx, targetObjID).Return(targetObj, nil)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when examiner not in world", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: nil, // Not in world
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXAMINE_FAILED")
		assert.Contains(t, err.Error(), "not in world")
	})
}

func TestWorldService_ExamineCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	targetCharID := ulid.Make()
	charLocID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("successful examine emits event", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetChar := &world.Character{
			ID:   targetCharID,
			Name: "Mysterious Stranger",
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockCharRepo.EXPECT().Get(ctx, targetCharID).Return(targetChar, nil)
		mockAC.On("Check", ctx, subjectID, "read", "character:"+targetCharID.String()).Return(true)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.NoError(t, err)

		// Verify event was emitted
		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(charLocID), call.Stream)
		assert.Equal(t, "object_examine", call.EventType)

		var payload world.ExaminePayload
		err = json.Unmarshal(call.Payload, &payload)
		require.NoError(t, err)
		assert.Equal(t, charID, payload.CharacterID)
		assert.Equal(t, world.TargetTypeCharacter, payload.TargetType)
		assert.Equal(t, targetCharID, payload.TargetID)
		assert.Equal(t, "Mysterious Stranger", payload.TargetName)
		assert.Equal(t, charLocID, payload.LocationID)
		mockAC.AssertExpectations(t)
	})

	t.Run("returns CHARACTER_NOT_FOUND when examiner does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			AccessControl: mockAC,
		})

		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns error when examiner not in world", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: nil, // Not in world
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXAMINE_FAILED")
		assert.Contains(t, err.Error(), "not in world")
	})

	t.Run("returns CHARACTER_NOT_FOUND when target does not exist", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockCharRepo.EXPECT().Get(ctx, targetCharID).Return(nil, world.ErrNotFound)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		// Target character not found uses same error code since it's also a character
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns EXAMINE_ACCESS_DENIED when not authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			AccessControl: mockAC,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetChar := &world.Character{
			ID:   targetCharID,
			Name: "Mysterious Stranger",
		}

		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockCharRepo.EXPECT().Get(ctx, targetCharID).Return(targetChar, nil)
		mockAC.On("Check", ctx, subjectID, "read", "character:"+targetCharID.String()).Return(false)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_DENIED")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns ErrNoEventEmitter when emitter not configured", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			AccessControl: mockAC,
			// No EventEmitter configured
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetChar := &world.Character{
			ID:   targetCharID,
			Name: "Mysterious Stranger",
		}

		mockAC.On("Check", ctx, subjectID, "read", "character:"+targetCharID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockCharRepo.EXPECT().Get(ctx, targetCharID).Return(targetChar, nil)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNoEventEmitter)
		errutil.AssertErrorCode(t, err, "EVENT_EMITTER_MISSING")
		mockAC.AssertExpectations(t)
	})

	t.Run("returns error when event emitter fails", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			AccessControl: mockAC,
			EventEmitter:  emitter,
		})

		examiner := &world.Character{
			ID:         charID,
			Name:       "Explorer",
			LocationID: &charLocID,
		}
		targetChar := &world.Character{
			ID:   targetCharID,
			Name: "Mysterious Stranger",
		}

		mockAC.On("Check", ctx, subjectID, "read", "character:"+targetCharID.String()).Return(true)
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockCharRepo.EXPECT().Get(ctx, targetCharID).Return(targetChar, nil)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		mockAC.AssertExpectations(t)
	})
}
