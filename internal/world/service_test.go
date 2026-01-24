// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
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

	t.Run("creates object when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{Name: "sword"}

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

		obj := &world.Object{Name: "sword"}

		mockAC.On("Check", ctx, subjectID, "write", "object:*").Return(false)

		err := svc.CreateObject(ctx, subjectID, obj)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockAC.AssertExpectations(t)
	})
}

func TestWorldService_UpdateObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()

	t.Run("updates object when authorized", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{ID: objID, Name: "sword updated"}

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

		obj := &world.Object{ID: objID, Name: "sword"}

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

		obj := &world.Object{ID: objID, Name: "sword"}

		mockAC.On("Check", ctx, subjectID, "write", "object:"+objID.String()).Return(true)
		mockObjRepo.EXPECT().Update(ctx, obj).Return(errors.New("db error"))

		err := svc.UpdateObject(ctx, subjectID, obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
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

	t.Run("propagates repository errors", func(t *testing.T) {
		mockAC := &mockAccessControl{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: mockAC,
		})

		obj := &world.Object{Name: "sword"}

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

	t.Run("handles severe cleanup error for bidirectional exit", func(t *testing.T) {
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

		// Should still succeed since primary delete worked, but severe issue is logged
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.NoError(t, err)
		mockAC.AssertExpectations(t)
	})

	t.Run("handles delete error cleanup for bidirectional exit", func(t *testing.T) {
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

		// Should still succeed since primary delete worked, but severe issue is logged
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.NoError(t, err)
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
