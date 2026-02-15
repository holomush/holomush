// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
	"github.com/holomush/holomush/pkg/errutil"
)

// Compile-time check: policy.Engine must satisfy world.AccessPolicyEngine.
var _ world.AccessPolicyEngine = (*policy.Engine)(nil)

// mockTransactor is a test mock that records whether InTransaction was called
// and executes the function directly (simulating a transaction).
type mockTransactor struct {
	called bool
}

func (m *mockTransactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	m.called = true
	return fn(ctx)
}

func TestWorldService_GetLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("returns location when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		expectedLoc := &world.Location{ID: locID, Name: "Test Room"}

		engine.Grant(subjectID, "read", "location:"+locID.String())
		mockRepo.EXPECT().Get(ctx, locID).Return(expectedLoc, nil)

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		require.NoError(t, err)
		assert.Equal(t, expectedLoc, loc)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		// DenyAllEngine returns EffectDeny with err == nil (explicit policy denial)
		engine := policytest.DenyAllEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny (EffectDeny) should return ErrPermissionDenied")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})

	t.Run("returns ErrAccessEvaluationFailed when engine errors", func(t *testing.T) {
		engineErr := errors.New("policy store unavailable")
		engine := policytest.NewErrorEngine(engineErr)
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
		assert.False(t, errors.Is(err, world.ErrPermissionDenied),
			"engine error must not be reported as permission denied")
	})

	t.Run("logs error when Evaluate fails", func(t *testing.T) {
		// Capture log output
		var logBuf bytes.Buffer
		oldLogger := slog.Default()
		testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
		slog.SetDefault(testLogger)
		defer slog.SetDefault(oldLogger)

		engineErr := errors.New("policy store unavailable")
		engine := policytest.NewErrorEngine(engineErr)
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		require.Error(t, err)

		// Verify log output contains error and context
		logOutput := logBuf.String()
		assert.Contains(t, logOutput, "access evaluation failed", "log should mention access evaluation failure")
		assert.Contains(t, logOutput, subjectID, "log should contain subject")
		assert.Contains(t, logOutput, "read", "log should contain action")
		assert.Contains(t, logOutput, "location:"+locID.String(), "log should contain resource")
		assert.Contains(t, logOutput, "policy store unavailable", "log should contain error message")
	})
}

func TestWorldService_CreateLocation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("creates location when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{
			Name:        "New Room",
			Description: "A test room",
			Type:        world.LocationTypePersistent,
		}

		engine.Grant(subjectID, "write", "location:*")
		mockRepo.EXPECT().Create(ctx, mock.MatchedBy(func(l *world.Location) bool {
			return l.Name == "New Room" && !l.ID.IsZero()
		})).Return(nil)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.NoError(t, err)
		assert.False(t, loc.ID.IsZero(), "ID should be generated")
	})

	t.Run("preserves existing ID when already set", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		existingID := ulid.Make()
		loc := &world.Location{
			ID:          existingID,
			Name:        "New Room",
			Description: "A test room",
			Type:        world.LocationTypePersistent,
		}

		engine.Grant(subjectID, "write", "location:*")
		mockRepo.EXPECT().Create(ctx, mock.MatchedBy(func(l *world.Location) bool {
			return l.ID == existingID
		})).Return(nil)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.NoError(t, err)
		assert.Equal(t, existingID, loc.ID, "pre-set ID should be preserved")
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{Name: "New Room"}

		err := svc.CreateLocation(ctx, subjectID, loc)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})
}

func TestWorldService_UpdateLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("updates location when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{ID: locID, Name: "Updated Room", Type: world.LocationTypePersistent}

		engine.Grant(subjectID, "write", "location:"+locID.String())
		mockRepo.EXPECT().Update(ctx, loc).Return(nil)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.NoError(t, err)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{ID: locID, Name: "Updated Room"}

		err := svc.UpdateLocation(ctx, subjectID, loc)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns not found when location does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{ID: locID, Name: "Updated Room", Type: world.LocationTypePersistent}

		engine.Grant(subjectID, "write", "location:"+locID.String())
		mockRepo.EXPECT().Update(ctx, loc).Return(world.ErrNotFound)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})
}

func TestWorldService_DeleteLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("deletes location when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, locID).Return(nil)

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.NoError(t, err)
		assert.True(t, tx.called, "expected InTransaction to be called")
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		err := svc.DeleteLocation(ctx, subjectID, locID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, locID).Return(errors.New("db error"))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_GetExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("returns exit when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		expectedExit := &world.Exit{ID: exitID, Name: "north"}

		engine.Grant(subjectID, "read", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Get(ctx, exitID).Return(expectedExit, nil)

		exit, err := svc.GetExit(ctx, subjectID, exitID)
		require.NoError(t, err)
		assert.Equal(t, expectedExit, exit)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit, err := svc.GetExit(ctx, subjectID, exitID)
		assert.Nil(t, exit)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})
}

func TestWorldService_CreateExit(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("creates exit when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
		}

		engine.Grant(subjectID, "write", "exit:*")
		mockExitRepo.EXPECT().Create(ctx, mock.MatchedBy(func(e *world.Exit) bool {
			return e.Name == "north" && !e.ID.IsZero()
		})).Return(nil)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.NoError(t, err)
		assert.False(t, exit.ID.IsZero(), "ID should be generated")
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{Name: "north"}

		err := svc.CreateExit(ctx, subjectID, exit)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})
}

func TestWorldService_UpdateExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("updates exit when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{ID: exitID, Name: "north updated", Visibility: world.VisibilityAll}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Update(ctx, exit).Return(nil)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.NoError(t, err)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		err := svc.UpdateExit(ctx, subjectID, exit)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Update(ctx, exit).Return(errors.New("db error"))

		err := svc.UpdateExit(ctx, subjectID, exit)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})

	t.Run("returns not found when exit does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Update(ctx, exit).Return(world.ErrNotFound)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "EXIT_NOT_FOUND")
	})
}

func TestWorldService_DeleteExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("deletes exit when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Delete(ctx, exitID).Return(nil)

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.NoError(t, err)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})

	t.Run("handles cleanup result for bidirectional exit", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
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

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Delete(ctx, exitID).Return(cleanupResult)

		// Should succeed since primary delete worked
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.NoError(t, err)
	})
}

func TestWorldService_GetObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("returns object when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		expectedObj := &world.Object{ID: objID, Name: "sword"}

		engine.Grant(subjectID, "read", "object:"+objID.String())
		mockObjRepo.EXPECT().Get(ctx, objID).Return(expectedObj, nil)

		obj, err := svc.GetObject(ctx, subjectID, objID)
		require.NoError(t, err)
		assert.Equal(t, expectedObj, obj)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := svc.GetObject(ctx, subjectID, objID)
		assert.Nil(t, obj)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})
}

func TestWorldService_CreateObject(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	locationID := ulid.Make()

	t.Run("creates object when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := world.NewObject("sword", world.InLocation(locationID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:*")
		mockObjRepo.EXPECT().Create(ctx, mock.MatchedBy(func(o *world.Object) bool {
			return o.Name == "sword" && !o.ID.IsZero()
		})).Return(nil)

		err = svc.CreateObject(ctx, subjectID, obj)
		require.NoError(t, err)
		assert.False(t, obj.ID.IsZero(), "ID should be generated")
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := world.NewObject("sword", world.InLocation(locationID))
		require.NoError(t, err)

		err = svc.CreateObject(ctx, subjectID, obj)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})
}

func TestWorldService_UpdateObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	locationID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("updates object when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := world.NewObjectWithID(objID, "sword updated", world.InLocation(locationID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockObjRepo.EXPECT().Update(ctx, obj).Return(nil)

		err = svc.UpdateObject(ctx, subjectID, obj)
		require.NoError(t, err)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := world.NewObjectWithID(objID, "sword", world.InLocation(locationID))
		require.NoError(t, err)

		err = svc.UpdateObject(ctx, subjectID, obj)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := world.NewObjectWithID(objID, "sword", world.InLocation(locationID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockObjRepo.EXPECT().Update(ctx, obj).Return(errors.New("db error"))

		err = svc.UpdateObject(ctx, subjectID, obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})

	t.Run("returns not found when object does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := world.NewObjectWithID(objID, "sword", world.InLocation(locationID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockObjRepo.EXPECT().Update(ctx, obj).Return(world.ErrNotFound)

		err = svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})
}

func TestWorldService_DeleteObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("deletes object when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockObjRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "object:"+objID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "object", objID).Return(nil)
		mockObjRepo.EXPECT().Delete(mock.Anything, objID).Return(nil)

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.NoError(t, err)
		assert.True(t, tx.called, "expected InTransaction to be called")
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockObjRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		err := svc.DeleteObject(ctx, subjectID, objID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockObjRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})
}

func TestWorldService_MoveObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	locationID := ulid.Make()

	t.Run("moves object when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		emitter := &mockEventEmitter{} // nil err means Emit succeeds

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockObjRepo,
			Engine:       engine,
			EventEmitter: emitter,
		})

		fromLocID := ulid.Make()
		to := world.Containment{LocationID: &locationID}

		existingObj, err := world.NewObjectWithID(objID, "Test Object", world.InLocation(fromLocID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(nil)

		err = svc.MoveObject(ctx, subjectID, objID, to)
		require.NoError(t, err)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		to := world.Containment{LocationID: &locationID}

		err := svc.MoveObject(ctx, subjectID, objID, to)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns error for invalid containment", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		// Empty containment is invalid
		to := world.Containment{}

		engine.Grant(subjectID, "write", "object:"+objID.String())

		err := svc.MoveObject(ctx, subjectID, objID, to)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		fromLocID := ulid.Make()
		to := world.Containment{LocationID: &locationID}

		existingObj, err := world.NewObjectWithID(objID, "Test Object", world.InLocation(fromLocID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(errors.New("db error"))

		err = svc.MoveObject(ctx, subjectID, objID, to)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})

	t.Run("returns error when object repository not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()

		svc := world.NewService(world.ServiceConfig{
			Engine: engine,
		})

		to := world.Containment{LocationID: &locationID}

		err := svc.MoveObject(ctx, subjectID, objID, to)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "object repository not configured")
	})

	// Note: "handles object with no current containment" test was removed.
	// With unexported containment fields and enforced invariants via SetContainment,
	// objects with no containment cannot be created from outside the package.
	// Objects must always have valid containment per the domain invariant.

	t.Run("returns EVENT_EMIT_FAILED when event emitter fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockObjRepo,
			Engine:       engine,
			EventEmitter: emitter,
		})

		fromLocID := ulid.Make()
		to := world.Containment{LocationID: &locationID}

		existingObj, err := world.NewObjectWithID(objID, "Test Object", world.InLocation(fromLocID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(nil)

		err = svc.MoveObject(ctx, subjectID, objID, to)
		require.Error(t, err)
		// Note: oops preserves inner error code (EVENT_EMIT_FAILED from events.go)
		// Service wrapper adds OBJECT_MOVE_EVENT_FAILED but inner code takes precedence
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		errutil.AssertErrorContext(t, err, "move_succeeded", true)
	})
}

func TestWorldService_AddSceneParticipant(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("adds participant when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())
		mockSceneRepo.EXPECT().AddParticipant(ctx, sceneID, charID, world.RoleMember).Return(nil)

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.NoError(t, err)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})
}

func TestWorldService_RemoveSceneParticipant(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("removes participant when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())
		mockSceneRepo.EXPECT().RemoveParticipant(ctx, sceneID, charID).Return(nil)

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.NoError(t, err)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})
}

func TestWorldService_ListSceneParticipants(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("lists participants when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		expected := []world.SceneParticipant{
			{CharacterID: charID, Role: world.RoleMember},
		}

		engine.Grant(subjectID, "read", "scene:"+sceneID.String())
		mockSceneRepo.EXPECT().ListParticipants(ctx, sceneID).Return(expected, nil)

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.NoError(t, err)
		assert.Equal(t, expected, participants)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		assert.Nil(t, participants)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		assert.Nil(t, participants)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})
}

// --- Input Validation Tests ---

func TestWorldService_CreateLocation_Validation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{
			Name:        "", // Empty name
			Description: "A test room",
			Type:        world.LocationTypePersistent,
		}

		engine.Grant(subjectID, "write", "location:*")

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
	})

	t.Run("rejects name exceeding max length", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		longName := make([]byte, world.MaxNameLength+1)
		for i := range longName {
			longName[i] = 'a'
		}

		loc := &world.Location{
			Name: string(longName),
			Type: world.LocationTypePersistent,
		}

		engine.Grant(subjectID, "write", "location:*")

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
	})
}

func TestWorldService_CreateExit_Validation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("rejects empty name", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "", // Empty name
		}

		engine.Grant(subjectID, "write", "exit:*")

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
	})
}

func TestWorldService_CreateObject_Validation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj := &world.Object{
			Name: "", // Empty name
		}

		engine.Grant(subjectID, "write", "object:*")

		err := svc.CreateObject(ctx, subjectID, obj)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
	})

	t.Run("rejects object without containment", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj := &world.Object{
			Name: "orphan", // Valid name but no containment
		}

		engine.Grant(subjectID, "write", "object:*")

		err := svc.CreateObject(ctx, subjectID, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})
}

func TestWorldService_AddSceneParticipant_Validation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("rejects invalid role", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.ParticipantRole("invalid"))
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidParticipantRole)
	})
}

// --- Repository Error Propagation Tests ---

func TestWorldService_GetLocation_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		engine.Grant(subjectID, "read", "location:"+locID.String())
		mockRepo.EXPECT().Get(ctx, locID).Return(nil, errors.New("db error"))

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_CreateLocation_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{
			Name: "Test Room",
			Type: world.LocationTypePersistent,
		}

		engine.Grant(subjectID, "write", "location:*")
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateLocation(ctx, subjectID, loc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_GetExit_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "read", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Get(ctx, exitID).Return(nil, errors.New("db error"))

		exit, err := svc.GetExit(ctx, subjectID, exitID)
		assert.Nil(t, exit)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_CreateExit_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
		}

		engine.Grant(subjectID, "write", "exit:*")
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateExit(ctx, subjectID, exit)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_GetObject_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		engine.Grant(subjectID, "read", "object:"+objID.String())
		mockObjRepo.EXPECT().Get(ctx, objID).Return(nil, errors.New("db error"))

		obj, err := svc.GetObject(ctx, subjectID, objID)
		assert.Nil(t, obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_CreateObject_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	locationID := ulid.Make()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := world.NewObject("sword", world.InLocation(locationID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:*")
		mockObjRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err = svc.CreateObject(ctx, subjectID, obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

// --- Scene Repository Error Propagation Tests ---

func TestWorldService_AddSceneParticipant_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())
		mockSceneRepo.EXPECT().AddParticipant(ctx, sceneID, charID, world.RoleMember).Return(errors.New("db error"))

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_RemoveSceneParticipant_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())
		mockSceneRepo.EXPECT().RemoveParticipant(ctx, sceneID, charID).Return(errors.New("db error"))

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_ListSceneParticipants_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "read", "scene:"+sceneID.String())
		mockSceneRepo.EXPECT().ListParticipants(ctx, sceneID).Return(nil, errors.New("db error"))

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		assert.Nil(t, participants)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

// --- DeleteExit Severe Cleanup Test ---

func TestWorldService_DeleteExit_SevereCleanup(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("propagates severe cleanup error for bidirectional exit", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
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

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Delete(ctx, exitID).Return(cleanupResult)

		// Severe error means the entire operation was rolled back - return error
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete exit")
	})

	t.Run("propagates delete error cleanup for bidirectional exit", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
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

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Delete(ctx, exitID).Return(cleanupResult)

		// Severe error means the entire operation was rolled back - return error
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete exit")
	})
}

func TestWorldService_GetExitsByLocation(t *testing.T) {
	ctx := context.Background()
	locationID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("returns exits when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		destID := ulid.Make()
		exit1 := &world.Exit{ID: ulid.Make(), Name: "north", FromLocationID: locationID, ToLocationID: destID}
		exit2 := &world.Exit{ID: ulid.Make(), Name: "east", FromLocationID: locationID, ToLocationID: destID}
		expectedExits := []*world.Exit{exit1, exit2}

		engine.Grant(subjectID, "read", "location:"+locationID.String())
		mockExitRepo.EXPECT().ListFromLocation(ctx, locationID).Return(expectedExits, nil)

		exits, err := svc.GetExitsByLocation(ctx, subjectID, locationID)
		require.NoError(t, err)
		assert.Equal(t, expectedExits, exits)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exits, err := svc.GetExitsByLocation(ctx, subjectID, locationID)
		assert.Nil(t, exits)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("returns EXIT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exits, err := svc.GetExitsByLocation(ctx, subjectID, locationID)
		assert.Nil(t, exits)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns error when repository not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()

		svc := world.NewService(world.ServiceConfig{
			Engine: engine,
		})

		exits, err := svc.GetExitsByLocation(ctx, subjectID, locationID)
		assert.Nil(t, exits)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_LIST_FAILED")
	})

	t.Run("returns error when repository fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		dbErr := errors.New("database connection failed")
		engine.Grant(subjectID, "read", "location:"+locationID.String())
		mockExitRepo.EXPECT().ListFromLocation(ctx, locationID).Return(nil, dbErr)

		exits, err := svc.GetExitsByLocation(ctx, subjectID, locationID)
		assert.Nil(t, exits)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_LIST_FAILED")
	})

	t.Run("returns empty slice for location with no exits", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "read", "location:"+locationID.String())
		mockExitRepo.EXPECT().ListFromLocation(ctx, locationID).Return([]*world.Exit{}, nil)

		exits, err := svc.GetExitsByLocation(ctx, subjectID, locationID)
		require.NoError(t, err)
		assert.NotNil(t, exits, "should return empty slice, not nil")
		assert.Empty(t, exits)
	})
}

// --- Update Method Validation Tests ---

func TestWorldService_UpdateLocation_Validation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{
			ID:   locID,
			Name: "", // Empty name
			Type: world.LocationTypePersistent,
		}

		engine.Grant(subjectID, "write", "location:"+locID.String())

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
	})

	t.Run("rejects invalid location type", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{
			ID:   locID,
			Name: "Valid Name",
			Type: world.LocationType("invalid"),
		}

		engine.Grant(subjectID, "write", "location:"+locID.String())

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLocationType)
	})
}

func TestWorldService_UpdateExit_Validation(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			ID:   exitID,
			Name: "", // Empty name
		}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
	})

	t.Run("rejects invalid visibility", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			ID:         exitID,
			Name:       "north",
			Visibility: world.Visibility("invalid"),
		}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidVisibility)
	})

	t.Run("rejects invalid lock type when locked", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			ID:         exitID,
			Name:       "north",
			Visibility: world.VisibilityAll,
			Locked:     true,
			LockType:   world.LockType("invalid"),
		}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLockType)
	})
}

func TestWorldService_UpdateObject_Validation(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("rejects empty name", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj := &world.Object{
			ID:   objID,
			Name: "", // Empty name
		}

		engine.Grant(subjectID, "write", "object:"+objID.String())

		err := svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "name", validationErr.Field)
	})

	t.Run("rejects object without containment", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj := &world.Object{
			ID:   objID,
			Name: "orphan", // Valid name but no containment
		}

		engine.Grant(subjectID, "write", "object:"+objID.String())

		err := svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})
}

// --- Create Method Extended Validation Tests ---

func TestWorldService_CreateLocation_TypeValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("rejects invalid location type", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc := &world.Location{
			Name: "Valid Name",
			Type: world.LocationType("invalid"),
		}

		engine.Grant(subjectID, "write", "location:*")

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLocationType)
	})
}

func TestWorldService_CreateExit_VisibilityValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("rejects invalid visibility", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.Visibility("invalid"),
		}

		engine.Grant(subjectID, "write", "exit:*")

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidVisibility)
	})

	t.Run("rejects invalid lock type when locked", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         true,
			LockType:       world.LockType("invalid"),
		}

		engine.Grant(subjectID, "write", "exit:*")

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLockType)
	})

	t.Run("rejects invalid lock data when locked", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
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

		engine.Grant(subjectID, "write", "exit:*")

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "lock_data", validationErr.Field)
	})

	t.Run("rejects invalid visible_to when visibility is list", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
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

		engine.Grant(subjectID, "write", "exit:*")

		err := svc.CreateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "visible_to", validationErr.Field)
	})
}

func TestWorldService_UpdateExit_LockDataValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	exitID := ulid.Make()

	t.Run("rejects invalid lock data when locked", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			ID:         exitID,
			Name:       "north",
			Visibility: world.VisibilityAll,
			Locked:     true,
			LockType:   world.LockTypeKey,
			LockData:   map[string]any{"invalid key!": "special chars not allowed"},
		}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "lock_data", validationErr.Field)
	})

	t.Run("rejects invalid visible_to when visibility is list", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		duplicateID := ulid.Make()
		exit := &world.Exit{
			ID:         exitID,
			Name:       "north",
			Visibility: world.VisibilityList,
			VisibleTo:  []ulid.ULID{duplicateID, duplicateID},
		}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)

		var validationErr *world.ValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Equal(t, "visible_to", validationErr.Field)
	})
}

func TestWorldService_CreateExit_ValidationBypass(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("accepts unlocked exit with invalid lock type", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         false,
			LockType:       world.LockType("garbage"), // Invalid but should be ignored
		}

		engine.Grant(subjectID, "write", "exit:*")
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(nil)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.NoError(t, err, "unlocked exit with invalid lock type should succeed")
	})

	t.Run("accepts non-list visibility with invalid visible_to", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
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

		engine.Grant(subjectID, "write", "exit:*")
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(nil)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.NoError(t, err, "non-list visibility with invalid visible_to should succeed")
	})
}

// --- NewService Validation Tests ---

func TestNewService_RequiresEngine(t *testing.T) {
	t.Run("panics when Engine is nil", func(t *testing.T) {
		assert.Panics(t, func() {
			world.NewService(world.ServiceConfig{
				Engine: nil,
			})
		})
	})

	t.Run("succeeds with Engine provided", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		assert.NotPanics(t, func() {
			svc := world.NewService(world.ServiceConfig{
				Engine: engine,
			})
			assert.NotNil(t, svc)
		})
	})
}

// --- Nil Input Tests ---

func TestWorldService_CreateLocation_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockRepo := worldtest.NewMockLocationRepository(t)
	svc := world.NewService(world.ServiceConfig{
		Engine:       engine,
		LocationRepo: mockRepo,
	})

	engine.Grant(subjectID, "write", "location:*")

	err := svc.CreateLocation(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_UpdateLocation_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockRepo := worldtest.NewMockLocationRepository(t)
	svc := world.NewService(world.ServiceConfig{
		Engine:       engine,
		LocationRepo: mockRepo,
	})

	// Note: nil check happens before access check since we need the ID to build resource string
	err := svc.UpdateLocation(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_CreateExit_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockRepo := worldtest.NewMockExitRepository(t)
	svc := world.NewService(world.ServiceConfig{
		Engine:   engine,
		ExitRepo: mockRepo,
	})

	engine.Grant(subjectID, "write", "exit:*")

	err := svc.CreateExit(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_CreateObject_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockRepo := worldtest.NewMockObjectRepository(t)
	svc := world.NewService(world.ServiceConfig{
		Engine:     engine,
		ObjectRepo: mockRepo,
	})

	engine.Grant(subjectID, "write", "object:*")

	err := svc.CreateObject(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_UpdateExit_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockRepo := worldtest.NewMockExitRepository(t)
	svc := world.NewService(world.ServiceConfig{
		Engine:   engine,
		ExitRepo: mockRepo,
	})

	// Note: nil check happens before access check since we need the ID to build resource string
	err := svc.UpdateExit(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestWorldService_UpdateObject_NilInput(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockRepo := worldtest.NewMockObjectRepository(t)
	svc := world.NewService(world.ServiceConfig{
		Engine:     engine,
		ObjectRepo: mockRepo,
	})

	// Note: nil check happens before access check since we need the ID to build resource string
	err := svc.UpdateObject(ctx, subjectID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// --- Nil Repository Tests ---

func TestWorldService_NilLocationRepo(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	locID := ulid.Make()

	engine := policytest.NewGrantEngine()
	svc := world.NewService(world.ServiceConfig{
		Engine: engine,
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
	subjectID := access.SubjectCharacter + ulid.Make().String()
	exitID := ulid.Make()

	engine := policytest.NewGrantEngine()
	svc := world.NewService(world.ServiceConfig{
		Engine: engine,
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
	subjectID := access.SubjectCharacter + ulid.Make().String()
	objID := ulid.Make()

	engine := policytest.NewGrantEngine()
	svc := world.NewService(world.ServiceConfig{
		Engine: engine,
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
	subjectID := access.SubjectCharacter + ulid.Make().String()
	sceneID := ulid.Make()
	charID := ulid.Make()

	engine := policytest.NewGrantEngine()
	svc := world.NewService(world.ServiceConfig{
		Engine: engine,
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
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("GetLocation returns LOCATION_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		engine.Grant(subjectID, "read", "location:"+locID.String())
		mockRepo.EXPECT().Get(ctx, locID).Return(nil, world.ErrNotFound)

		_, err := svc.GetLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("GetLocation returns LOCATION_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		_, err := svc.GetLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("GetLocation returns LOCATION_GET_FAILED for other errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		engine.Grant(subjectID, "read", "location:"+locID.String())
		mockRepo.EXPECT().Get(ctx, locID).Return(nil, errors.New("db connection failed"))

		_, err := svc.GetLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_GET_FAILED")
	})

	t.Run("CreateLocation returns LOCATION_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		err := svc.CreateLocation(ctx, subjectID, &world.Location{Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("CreateLocation returns LOCATION_INVALID for validation errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		engine.Grant(subjectID, "write", "location:*")

		err := svc.CreateLocation(ctx, subjectID, &world.Location{Name: "", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_INVALID")
	})

	t.Run("CreateLocation returns LOCATION_CREATE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		engine.Grant(subjectID, "write", "location:*")
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateLocation(ctx, subjectID, &world.Location{Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_CREATE_FAILED")
	})

	t.Run("UpdateLocation returns LOCATION_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		err := svc.UpdateLocation(ctx, subjectID, &world.Location{ID: locID, Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("UpdateLocation returns LOCATION_INVALID for validation errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		engine.Grant(subjectID, "write", "location:"+locID.String())

		err := svc.UpdateLocation(ctx, subjectID, &world.Location{ID: locID, Name: "", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_INVALID")
	})

	t.Run("UpdateLocation returns LOCATION_UPDATE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		engine.Grant(subjectID, "write", "location:"+locID.String())
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.UpdateLocation(ctx, subjectID, &world.Location{ID: locID, Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_UPDATE_FAILED")
	})

	t.Run("DeleteLocation returns LOCATION_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("DeleteLocation returns LOCATION_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, locID).Return(world.ErrNotFound)

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("DeleteLocation returns LOCATION_DELETE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, locID).Return(errors.New("db error"))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_DELETE_FAILED")
	})

	t.Run("GetLocation returns LOCATION_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		_, err := svc.GetLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("CreateLocation returns LOCATION_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		err := svc.CreateLocation(ctx, subjectID, &world.Location{Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("UpdateLocation returns LOCATION_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		err := svc.UpdateLocation(ctx, subjectID, &world.Location{ID: locID, Name: "Test", Type: world.LocationTypePersistent})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("DeleteLocation returns LOCATION_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})
}

func TestService_ErrorCodes_Exit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("GetExit returns EXIT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "read", "exit:"+exitID.String())
		mockRepo.EXPECT().Get(ctx, exitID).Return(nil, world.ErrNotFound)

		_, err := svc.GetExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_NOT_FOUND")
	})

	t.Run("GetExit returns EXIT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		_, err := svc.GetExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("GetExit returns EXIT_GET_FAILED for other errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "read", "exit:"+exitID.String())
		mockRepo.EXPECT().Get(ctx, exitID).Return(nil, errors.New("db error"))

		_, err := svc.GetExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_GET_FAILED")
	})

	t.Run("CreateExit returns EXIT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		err := svc.CreateExit(ctx, subjectID, &world.Exit{Name: "north", FromLocationID: fromLocID, ToLocationID: toLocID, Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("CreateExit returns EXIT_INVALID for validation errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "write", "exit:*")

		err := svc.CreateExit(ctx, subjectID, &world.Exit{Name: "", FromLocationID: fromLocID, ToLocationID: toLocID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_INVALID")
	})

	t.Run("CreateExit returns EXIT_CREATE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "write", "exit:*")
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.CreateExit(ctx, subjectID, &world.Exit{Name: "north", FromLocationID: fromLocID, ToLocationID: toLocID, Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_CREATE_FAILED")
	})

	t.Run("UpdateExit returns EXIT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		err := svc.UpdateExit(ctx, subjectID, &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("UpdateExit returns EXIT_INVALID for validation errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "write", "exit:"+exitID.String())

		err := svc.UpdateExit(ctx, subjectID, &world.Exit{ID: exitID, Name: ""})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_INVALID")
	})

	t.Run("UpdateExit returns EXIT_UPDATE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "write", "exit:"+exitID.String())
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(errors.New("db error"))

		err := svc.UpdateExit(ctx, subjectID, &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_UPDATE_FAILED")
	})

	t.Run("DeleteExit returns EXIT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
	})

	t.Run("DeleteExit returns EXIT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockRepo.EXPECT().Delete(ctx, exitID).Return(world.ErrNotFound)

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_NOT_FOUND")
	})

	t.Run("DeleteExit returns EXIT_DELETE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockRepo.EXPECT().Delete(ctx, exitID).Return(errors.New("db error"))

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_DELETE_FAILED")
	})

	t.Run("GetExit returns EXIT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		_, err := svc.GetExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("CreateExit returns EXIT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		err := svc.CreateExit(ctx, subjectID, &world.Exit{Name: "north", FromLocationID: fromLocID, ToLocationID: toLocID, Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("UpdateExit returns EXIT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		err := svc.UpdateExit(ctx, subjectID, &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("DeleteExit returns EXIT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		})

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})
}

func TestService_ErrorCodes_Object(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	locationID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("GetObject returns OBJECT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		engine.Grant(subjectID, "read", "object:"+objID.String())
		mockRepo.EXPECT().Get(ctx, objID).Return(nil, world.ErrNotFound)

		_, err := svc.GetObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("GetObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		_, err := svc.GetObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	t.Run("GetObject returns OBJECT_GET_FAILED for other errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		engine.Grant(subjectID, "read", "object:"+objID.String())
		mockRepo.EXPECT().Get(ctx, objID).Return(nil, errors.New("db error"))

		_, err := svc.GetObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_GET_FAILED")
	})

	t.Run("CreateObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		obj, err := world.NewObject("sword", world.InLocation(locationID))
		require.NoError(t, err)
		err = svc.CreateObject(ctx, subjectID, obj)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	// Note: "CreateObject returns OBJECT_INVALID for validation errors" test was removed.
	// With unexported containment fields and enforced invariants via constructors,
	// invalid objects (empty name) cannot be created from outside the package.
	// Validation is tested at the constructor level in object_test.go.

	t.Run("CreateObject returns OBJECT_CREATE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		engine.Grant(subjectID, "write", "object:*")
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(errors.New("db error"))

		obj, err := world.NewObject("sword", world.InLocation(locationID))
		require.NoError(t, err)
		err = svc.CreateObject(ctx, subjectID, obj)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_CREATE_FAILED")
	})

	t.Run("UpdateObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		obj, err := world.NewObjectWithID(objID, "sword", world.InLocation(locationID))
		require.NoError(t, err)
		err = svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	// Note: "UpdateObject returns OBJECT_INVALID for validation errors" test was removed.
	// With unexported containment fields and enforced invariants via constructors,
	// invalid objects (empty name) cannot be created from outside the package.
	// Validation is tested at the constructor level in object_test.go.

	t.Run("UpdateObject returns OBJECT_UPDATE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(errors.New("db error"))

		obj, err := world.NewObjectWithID(objID, "sword", world.InLocation(locationID))
		require.NoError(t, err)
		err = svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_UPDATE_FAILED")
	})

	t.Run("DeleteObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	t.Run("DeleteObject returns OBJECT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "object:"+objID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "object", objID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, objID).Return(world.ErrNotFound)

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("DeleteObject returns OBJECT_DELETE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "object:"+objID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "object", objID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, objID).Return(errors.New("db error"))

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_DELETE_FAILED")
	})

	t.Run("MoveObject returns OBJECT_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_DENIED")
	})

	t.Run("MoveObject returns OBJECT_INVALID for invalid containment", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		engine.Grant(subjectID, "write", "object:"+objID.String())

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_INVALID")
	})

	t.Run("MoveObject returns OBJECT_NOT_FOUND for ErrNotFound from Get", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockRepo.EXPECT().Get(ctx, objID).Return(nil, world.ErrNotFound)

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("MoveObject returns OBJECT_NOT_FOUND for ErrNotFound from Move", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		fromLocID := ulid.Make()
		existingObj, err := world.NewObjectWithID(objID, "Test Object", world.InLocation(fromLocID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockRepo.EXPECT().Move(ctx, objID, mock.Anything).Return(world.ErrNotFound)

		err = svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("MoveObject returns OBJECT_MOVE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		fromLocID := ulid.Make()
		existingObj, err := world.NewObjectWithID(objID, "Test Object", world.InLocation(fromLocID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockRepo.EXPECT().Move(ctx, objID, mock.Anything).Return(errors.New("db error"))

		err = svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_MOVE_FAILED")
	})

	t.Run("MoveObject returns EVENT_EMIT_FAILED when event emission fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockObjectRepository(t)
		// mockEventEmitter is defined in events_test.go (same package)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockRepo,
			Engine:       engine,
			EventEmitter: emitter,
		})

		fromLocID := ulid.Make()
		existingObj, err := world.NewObjectWithID(objID, "Test Object", world.InLocation(fromLocID))
		require.NoError(t, err)

		engine.Grant(subjectID, "write", "object:"+objID.String())
		mockRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockRepo.EXPECT().Move(ctx, objID, mock.Anything).Return(nil)

		err = svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		// Note: oops preserves inner error code (EVENT_EMIT_FAILED from events.go)
		// Service wrapper adds OBJECT_MOVE_EVENT_FAILED but inner code takes precedence
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		// Verify the error context indicates the move succeeded in the database
		errutil.AssertErrorContext(t, err, "move_succeeded", true)
	})

	t.Run("GetObject returns OBJECT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		_, err := svc.GetObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("CreateObject returns OBJECT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		obj, err := world.NewObject("sword", world.InLocation(locationID))
		require.NoError(t, err)
		err = svc.CreateObject(ctx, subjectID, obj)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("UpdateObject returns OBJECT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		obj, err := world.NewObjectWithID(objID, "sword", world.InLocation(locationID))
		require.NoError(t, err)
		err = svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("DeleteObject returns OBJECT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("MoveObject returns OBJECT_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockRepo,
			Engine:     engine,
		})

		err := svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})
}

func TestService_ErrorCodes_Scene(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("AddSceneParticipant returns SCENE_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_DENIED")
	})

	t.Run("AddSceneParticipant returns SCENE_INVALID for invalid role", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.ParticipantRole("invalid"))
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_INVALID")
	})

	t.Run("AddSceneParticipant returns SCENE_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())
		mockRepo.EXPECT().AddParticipant(ctx, sceneID, charID, world.RoleMember).Return(world.ErrNotFound)

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
	})

	t.Run("AddSceneParticipant returns SCENE_ADD_PARTICIPANT_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())
		mockRepo.EXPECT().AddParticipant(ctx, sceneID, charID, world.RoleMember).Return(errors.New("db error"))

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ADD_PARTICIPANT_FAILED")
	})

	t.Run("RemoveSceneParticipant returns SCENE_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_DENIED")
	})

	t.Run("RemoveSceneParticipant returns SCENE_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())
		mockRepo.EXPECT().RemoveParticipant(ctx, sceneID, charID).Return(world.ErrNotFound)

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
	})

	t.Run("RemoveSceneParticipant returns SCENE_REMOVE_PARTICIPANT_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "write", "scene:"+sceneID.String())
		mockRepo.EXPECT().RemoveParticipant(ctx, sceneID, charID).Return(errors.New("db error"))

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_REMOVE_PARTICIPANT_FAILED")
	})

	t.Run("ListSceneParticipants returns SCENE_ACCESS_DENIED for permission denied", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_DENIED")
	})

	t.Run("ListSceneParticipants returns SCENE_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "read", "scene:"+sceneID.String())
		mockRepo.EXPECT().ListParticipants(ctx, sceneID).Return(nil, world.ErrNotFound)

		_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
	})

	t.Run("ListSceneParticipants returns SCENE_LIST_PARTICIPANTS_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		engine.Grant(subjectID, "read", "scene:"+sceneID.String())
		mockRepo.EXPECT().ListParticipants(ctx, sceneID).Return(nil, errors.New("db error"))

		_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_LIST_PARTICIPANTS_FAILED")
	})

	t.Run("AddSceneParticipant returns SCENE_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		err := svc.AddSceneParticipant(ctx, subjectID, sceneID, charID, world.RoleMember)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("RemoveSceneParticipant returns SCENE_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		err := svc.RemoveSceneParticipant(ctx, subjectID, sceneID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("ListSceneParticipants returns SCENE_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockRepo,
			Engine:    engine,
		})

		_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SCENE_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})
}

func TestWorldService_GetCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("returns character when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		locID := ulid.Make()
		expectedChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &locID,
		}

		engine.Grant(subjectID, "read", "character:"+charID.String())
		mockRepo.EXPECT().Get(ctx, charID).Return(expectedChar, nil)

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		require.NoError(t, err)
		assert.Equal(t, expectedChar, char)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		assert.Nil(t, char)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
	})

	t.Run("returns CHARACTER_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns error when repository not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()

		svc := world.NewService(world.ServiceConfig{
			Engine: engine,
		})

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		assert.Nil(t, char)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_GET_FAILED")
	})

	t.Run("returns not found when character does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		engine.Grant(subjectID, "read", "character:"+charID.String())
		mockRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		char, err := svc.GetCharacter(ctx, subjectID, charID)
		assert.Nil(t, char)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns error when repository fails with generic error", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		dbErr := errors.New("database connection failed")
		engine.Grant(subjectID, "read", "character:"+charID.String())
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
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("returns characters when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		char1 := &world.Character{ID: ulid.Make(), Name: "Char1", LocationID: &locationID}
		char2 := &world.Character{ID: ulid.Make(), Name: "Char2", LocationID: &locationID}
		expectedChars := []*world.Character{char1, char2}

		engine.Grant(subjectID, "list_characters", "location:"+locationID.String())
		mockRepo.EXPECT().GetByLocation(ctx, locationID, world.ListOptions{}).Return(expectedChars, nil)

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
		require.NoError(t, err)
		assert.Equal(t, expectedChars, chars)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
		assert.Nil(t, chars)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
	})

	t.Run("returns CHARACTER_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
		assert.Nil(t, chars)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns error when repository not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()

		svc := world.NewService(world.ServiceConfig{
			Engine: engine,
		})

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
		assert.Nil(t, chars)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_QUERY_FAILED")
	})

	t.Run("returns error when repository fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		dbErr := errors.New("database connection failed")
		engine.Grant(subjectID, "list_characters", "location:"+locationID.String())
		mockRepo.EXPECT().GetByLocation(ctx, locationID, world.ListOptions{}).Return(nil, dbErr)

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
		assert.Nil(t, chars)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_QUERY_FAILED")
	})

	t.Run("returns empty slice for location with no characters", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		engine.Grant(subjectID, "list_characters", "location:"+locationID.String())
		mockRepo.EXPECT().GetByLocation(ctx, locationID, world.ListOptions{}).Return([]*world.Character{}, nil)

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
		require.NoError(t, err)
		assert.Empty(t, chars)
	})

	t.Run("passes pagination options to repository", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		opts := world.ListOptions{Limit: 10, Offset: 5}
		expectedChars := []*world.Character{{ID: ulid.Make(), Name: "Char1"}}

		engine.Grant(subjectID, "list_characters", "location:"+locationID.String())
		mockRepo.EXPECT().GetByLocation(ctx, locationID, opts).Return(expectedChars, nil)

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, opts)
		require.NoError(t, err)
		assert.Equal(t, expectedChars, chars)
	})
}

func TestWorldService_GetCharactersByLocation_UsesDecomposedResource(t *testing.T) {
	ctx := context.Background()
	locationID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockCharacterRepository(t)

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockRepo,
		Engine:        mockEngine,
	})

	expectedChars := []*world.Character{{ID: ulid.Make(), Name: "Char1", LocationID: &locationID}}

	// Capture the AccessRequest using mock.MatchedBy to verify ADR #76 decomposition
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().GetByLocation(ctx, locationID, world.ListOptions{}).Return(expectedChars, nil)

	_, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
	require.NoError(t, err)

	// Verify ADR #76 decomposition: action=list_characters, resource=location:<id> (not location:<id>:characters)
	assert.Equal(t, "list_characters", capturedRequest.Action, "action should be 'list_characters' per ADR #76")
	assert.Equal(t, "location:"+locationID.String(), capturedRequest.Resource, "resource should be location:<id> per ADR #76 decomposition")
	assert.NotContains(t, capturedRequest.Resource, ":characters", "resource must NOT contain :characters suffix (ADR #76 requires decomposition)")
}

func TestWorldService_GetCharactersByLocation_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	locationID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + charID.String()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockCharacterRepository(t)

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockRepo,
		Engine:        mockEngine,
	})

	expectedChars := []*world.Character{{ID: ulid.Make(), Name: "Char1", LocationID: &locationID}}

	// Capture the AccessRequest using mock.MatchedBy
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().GetByLocation(ctx, locationID, world.ListOptions{}).Return(expectedChars, nil)

	_, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "list_characters", capturedRequest.Action, "action should be 'list_characters'")
	assert.Equal(t, "location:"+locationID.String(), capturedRequest.Resource, "resource should be location:<id>")
}

func TestWorldService_MoveCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("successful move emits event", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
			EventEmitter:  emitter,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		engine.Grant(subjectID, "write", "character:"+charID.String())
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
	})

	t.Run("returns CHARACTER_NOT_FOUND when character does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns CHARACTER_MOVE_FAILED when Get fails with generic error", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		dbErr := errors.New("database connection lost")
		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, dbErr)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
		assert.Contains(t, err.Error(), "get character")
	})

	t.Run("returns LOCATION_NOT_FOUND when destination does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(nil, world.ErrNotFound)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("returns CHARACTER_MOVE_FAILED when location verification fails with generic error", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		dbErr := errors.New("database timeout")
		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(nil, dbErr)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
		assert.Contains(t, err.Error(), "verify destination location")
	})

	t.Run("returns EVENT_EMIT_FAILED when event emission fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
			EventEmitter:  emitter,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(nil)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		// Note: oops preserves inner error code (EVENT_EMIT_FAILED from events.go)
		// Service wrapper adds CHARACTER_MOVE_EVENT_FAILED but inner code takes precedence
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		errutil.AssertErrorContext(t, err, "move_succeeded", true)
	})

	t.Run("first-time placement emits event with from_type none", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
			EventEmitter:  emitter,
		})

		// Character with no prior location (first-time placement)
		existingChar := &world.Character{
			ID:         charID,
			Name:       "New Character",
			LocationID: nil,
		}

		engine.Grant(subjectID, "write", "character:"+charID.String())
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
	})

	t.Run("returns CHARACTER_ACCESS_DENIED when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
	})

	t.Run("returns CHARACTER_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns error when character repository not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockLocRepo,
			Engine:       engine,
		})

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
	})

	t.Run("returns ErrNoEventEmitter when emitter not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
			// No EventEmitter configured
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(nil)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNoEventEmitter)
		errutil.AssertErrorCode(t, err, "EVENT_EMITTER_MISSING")
	})

	t.Run("returns error when UpdateLocation fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}
		dbErr := errors.New("database error")

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(dbErr)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
	})

	t.Run("propagates context cancellation error", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{err: errors.New("transient error")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
			EventEmitter:  emitter,
		})

		existingChar := &world.Character{
			ID:         charID,
			Name:       "Test Character",
			LocationID: &fromLocID,
		}

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(mock.Anything, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(mock.Anything, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(mock.Anything, charID, &toLocID).Return(nil)

		err := svc.MoveCharacter(cancelCtx, subjectID, charID, toLocID)

		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestWorldService_ExamineLocation(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	targetLocID := ulid.Make()
	charLocID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("successful examine emits event", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
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

		engine.Grant(subjectID, "read", "location:"+targetLocID.String())
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
	})

	t.Run("returns CHARACTER_NOT_FOUND when examiner does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns LOCATION_NOT_FOUND when target does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
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
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
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

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_DENIED")
	})

	t.Run("returns EXAMINE_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
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

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns ErrNoEventEmitter when emitter not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
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

		engine.Grant(subjectID, "read", "location:"+targetLocID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockLocRepo.EXPECT().Get(ctx, targetLocID).Return(targetLoc, nil)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNoEventEmitter)
		errutil.AssertErrorCode(t, err, "EVENT_EMITTER_MISSING")
	})

	t.Run("returns error when event emitter fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
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

		engine.Grant(subjectID, "read", "location:"+targetLocID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockLocRepo.EXPECT().Get(ctx, targetLocID).Return(targetLoc, nil)

		err := svc.ExamineLocation(ctx, subjectID, charID, targetLocID)
		require.Error(t, err)
		// Inner code preserved by oops error chaining (see EmitExamineEvent)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		// Verify operation context is present
		errutil.AssertErrorContext(t, err, "character_id", charID.String())
		errutil.AssertErrorContext(t, err, "target_id", targetLocID.String())
	})

	t.Run("returns error when examiner not in world", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
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
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("successful examine emits event", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			Engine:        engine,
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
		engine.Grant(subjectID, "read", "object:"+targetObjID.String())

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
	})

	t.Run("returns CHARACTER_NOT_FOUND when examiner does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			Engine:        engine,
		})

		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns OBJECT_NOT_FOUND when target does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			Engine:        engine,
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
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			Engine:        engine,
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

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_DENIED")
	})

	t.Run("returns EXAMINE_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			Engine:        engine,
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

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns ErrNoEventEmitter when emitter not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			Engine:        engine,
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

		engine.Grant(subjectID, "read", "object:"+targetObjID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockObjRepo.EXPECT().Get(ctx, targetObjID).Return(targetObj, nil)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNoEventEmitter)
		errutil.AssertErrorCode(t, err, "EVENT_EMITTER_MISSING")
	})

	t.Run("returns error when event emitter fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			Engine:        engine,
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

		engine.Grant(subjectID, "read", "object:"+targetObjID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockObjRepo.EXPECT().Get(ctx, targetObjID).Return(targetObj, nil)

		err := svc.ExamineObject(ctx, subjectID, charID, targetObjID)
		require.Error(t, err)
		// Inner code preserved by oops error chaining (see EmitExamineEvent)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		// Verify operation context is present
		errutil.AssertErrorContext(t, err, "character_id", charID.String())
		errutil.AssertErrorContext(t, err, "target_id", targetObjID.String())
	})

	t.Run("returns error when examiner not in world", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			ObjectRepo:    mockObjRepo,
			Engine:        engine,
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
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("successful examine emits event", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		emitter := &mockEventEmitter{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			Engine:        engine,
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
		engine.Grant(subjectID, "read", "character:"+targetCharID.String())

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
	})

	t.Run("returns CHARACTER_NOT_FOUND when examiner does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			Engine:        engine,
		})

		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns error when examiner not in world", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			Engine:        engine,
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
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			Engine:        engine,
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
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			Engine:        engine,
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

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_DENIED")
	})

	t.Run("returns EXAMINE_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			Engine:        engine,
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

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXAMINE_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns ErrNoEventEmitter when emitter not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			Engine:        engine,
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

		engine.Grant(subjectID, "read", "character:"+targetCharID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockCharRepo.EXPECT().Get(ctx, targetCharID).Return(targetChar, nil)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNoEventEmitter)
		errutil.AssertErrorCode(t, err, "EVENT_EMITTER_MISSING")
	})

	t.Run("returns error when event emitter fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		emitter := &mockEventEmitter{err: errors.New("event bus unavailable")}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			Engine:        engine,
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

		engine.Grant(subjectID, "read", "character:"+targetCharID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(examiner, nil)
		mockCharRepo.EXPECT().Get(ctx, targetCharID).Return(targetChar, nil)

		err := svc.ExamineCharacter(ctx, subjectID, charID, targetCharID)
		require.Error(t, err)
		// Inner code preserved by oops error chaining (see EmitExamineEvent)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		// Verify operation context is present
		errutil.AssertErrorContext(t, err, "character_id", charID.String())
		errutil.AssertErrorContext(t, err, "target_id", targetCharID.String())
	})
}

func TestWorldService_FindLocationByName(t *testing.T) {
	ctx := context.Background()
	subjectID := access.SubjectCharacter + ulid.Make().String()
	locID := ulid.Make()

	t.Run("finds location when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		expectedLoc := &world.Location{ID: locID, Name: "Test Room", Type: world.LocationTypePersistent}

		engine.Grant(subjectID, "read", "location:*")
		mockRepo.EXPECT().FindByName(ctx, "Test Room").Return(expectedLoc, nil)

		loc, err := svc.FindLocationByName(ctx, subjectID, "Test Room")
		require.NoError(t, err)
		assert.Equal(t, locID, loc.ID)
		assert.Equal(t, "Test Room", loc.Name)
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		_, err := svc.FindLocationByName(ctx, subjectID, "Test Room")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("returns LOCATION_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		_, err := svc.FindLocationByName(ctx, subjectID, "Test Room")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns not found when location does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		engine.Grant(subjectID, "read", "location:*")
		mockRepo.EXPECT().FindByName(ctx, "Non-Existent").Return(nil, world.ErrNotFound)

		_, err := svc.FindLocationByName(ctx, subjectID, "Non-Existent")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("returns error when repository not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()

		svc := world.NewService(world.ServiceConfig{
			Engine: engine,
		})

		_, err := svc.FindLocationByName(ctx, subjectID, "Test Room")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_FIND_FAILED")
	})
}

func TestWorldService_DeleteCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("deletes character when authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		engine.Grant(subjectID, "delete", "character:"+charID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "character", charID).Return(nil)
		mockCharRepo.EXPECT().Delete(mock.Anything, charID).Return(nil)

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.NoError(t, err)
		assert.True(t, tx.called, "expected InTransaction to be called")
	})

	t.Run("returns permission denied when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})

	t.Run("returns CHARACTER_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns CHARACTER_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		engine.Grant(subjectID, "delete", "character:"+charID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "character", charID).Return(nil)
		mockCharRepo.EXPECT().Delete(mock.Anything, charID).Return(world.ErrNotFound)

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("returns CHARACTER_DELETE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		engine.Grant(subjectID, "delete", "character:"+charID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "character", charID).Return(nil)
		mockCharRepo.EXPECT().Delete(mock.Anything, charID).Return(errors.New("db error"))

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_DELETE_FAILED")
	})

	t.Run("returns CHARACTER_DELETE_FAILED when character repo nil", func(t *testing.T) {
		engine := policytest.NewGrantEngine()

		svc := world.NewService(world.ServiceConfig{
			Engine: engine,
			// CharacterRepo intentionally nil
		})

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not configured")
		errutil.AssertErrorCode(t, err, "CHARACTER_DELETE_FAILED")
	})
}

func TestWorldService_DeleteCharacter_CascadesProperties(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("cleans up properties then deletes character", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		engine.Grant(subjectID, "delete", "character:"+charID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "character", charID).Return(nil)
		mockCharRepo.EXPECT().Delete(mock.Anything, charID).Return(nil)

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.NoError(t, err)
		assert.True(t, tx.called, "expected InTransaction to be called")
	})

	t.Run("returns error when property delete fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		engine.Grant(subjectID, "delete", "character:"+charID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "character", charID).Return(errors.New("db error"))

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_DELETE_FAILED")
	})

	t.Run("returns error when character delete fails after properties deleted", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			PropertyRepo:  mockPropRepo,
			Engine:        engine,
			Transactor:    tx,
		})

		engine.Grant(subjectID, "delete", "character:"+charID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "character", charID).Return(nil)
		mockCharRepo.EXPECT().Delete(mock.Anything, charID).Return(errors.New("db error"))

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_DELETE_FAILED")
	})
}

func TestWorldService_DeleteLocation_CascadesProperties(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("cleans up properties then deletes location", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockLocRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockLocRepo.EXPECT().Delete(mock.Anything, locID).Return(nil)

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.NoError(t, err)
		assert.True(t, tx.called, "expected InTransaction to be called")
	})

	t.Run("returns error when property delete fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockLocRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(errors.New("db error"))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_DELETE_FAILED")
	})

	t.Run("returns error when location delete fails after properties deleted", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockLocRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockLocRepo.EXPECT().Delete(mock.Anything, locID).Return(errors.New("db error"))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_DELETE_FAILED")
	})
}

func TestWorldService_DeleteObject_CascadesProperties(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	t.Run("cleans up properties then deletes object", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockObjRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "object:"+objID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "object", objID).Return(nil)
		mockObjRepo.EXPECT().Delete(mock.Anything, objID).Return(nil)

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.NoError(t, err)
		assert.True(t, tx.called, "expected InTransaction to be called")
	})

	t.Run("returns error when property delete fails", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockObjRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "object:"+objID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "object", objID).Return(errors.New("db error"))

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_DELETE_FAILED")
	})

	t.Run("returns error when object delete fails after properties deleted", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		tx := &mockTransactor{}

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:   mockObjRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
			Transactor:   tx,
		})

		engine.Grant(subjectID, "delete", "object:"+objID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "object", objID).Return(nil)
		mockObjRepo.EXPECT().Delete(mock.Anything, objID).Return(errors.New("db error"))

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_DELETE_FAILED")
	})
}

func TestWorldService_DeleteLocation_PropertyDeleteFails(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockLocRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
		Transactor:   tx,
	})

	engine.Grant(subjectID, "delete", "location:"+locID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(errors.New("db error"))

	err := svc.DeleteLocation(ctx, subjectID, locID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "LOCATION_DELETE_FAILED")
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteObject_PropertyDeleteFails(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockObjRepo := worldtest.NewMockObjectRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		ObjectRepo:   mockObjRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
		Transactor:   tx,
	})

	engine.Grant(subjectID, "delete", "object:"+objID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "object", objID).Return(errors.New("db error"))

	err := svc.DeleteObject(ctx, subjectID, objID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "OBJECT_DELETE_FAILED")
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteCharacter_PropertyDeleteFails(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockCharRepo := worldtest.NewMockCharacterRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockCharRepo,
		PropertyRepo:  mockPropRepo,
		Engine:        engine,
		Transactor:    tx,
	})

	engine.Grant(subjectID, "delete", "character:"+charID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "character", charID).Return(errors.New("db error"))

	err := svc.DeleteCharacter(ctx, subjectID, charID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_DELETE_FAILED")
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteLocation_ErrorsWithPropertyRepoButNoTransactor(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockLocRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
	})

	engine.Grant(subjectID, "delete", "location:"+locID.String())

	err := svc.DeleteLocation(ctx, subjectID, locID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "LOCATION_DELETE_FAILED")
	assert.Contains(t, err.Error(), "transactor required")
}

func TestWorldService_DeleteObject_ErrorsWithPropertyRepoButNoTransactor(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockObjRepo := worldtest.NewMockObjectRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)

	svc := world.NewService(world.ServiceConfig{
		ObjectRepo:   mockObjRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
	})

	engine.Grant(subjectID, "delete", "object:"+objID.String())

	err := svc.DeleteObject(ctx, subjectID, objID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "OBJECT_DELETE_FAILED")
	assert.Contains(t, err.Error(), "transactor required")
}

func TestWorldService_DeleteCharacter_ErrorsWithPropertyRepoButNoTransactor(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockCharRepo := worldtest.NewMockCharacterRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockCharRepo,
		PropertyRepo:  mockPropRepo,
		Engine:        engine,
	})

	engine.Grant(subjectID, "delete", "character:"+charID.String())

	err := svc.DeleteCharacter(ctx, subjectID, charID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_DELETE_FAILED")
	assert.Contains(t, err.Error(), "transactor required")
}

func TestWorldService_DeleteLocation_UsesTransactor(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockLocRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
		Transactor:   tx,
	})

	engine.Grant(subjectID, "delete", "location:"+locID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
	mockLocRepo.EXPECT().Delete(mock.Anything, locID).Return(nil)

	err := svc.DeleteLocation(ctx, subjectID, locID)
	require.NoError(t, err)
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteObject_UsesTransactor(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockObjRepo := worldtest.NewMockObjectRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		ObjectRepo:   mockObjRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
		Transactor:   tx,
	})

	engine.Grant(subjectID, "delete", "object:"+objID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "object", objID).Return(nil)
	mockObjRepo.EXPECT().Delete(mock.Anything, objID).Return(nil)

	err := svc.DeleteObject(ctx, subjectID, objID)
	require.NoError(t, err)
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteCharacter_UsesTransactor(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockCharRepo := worldtest.NewMockCharacterRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockCharRepo,
		PropertyRepo:  mockPropRepo,
		Engine:        engine,
		Transactor:    tx,
	})

	engine.Grant(subjectID, "delete", "character:"+charID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "character", charID).Return(nil)
	mockCharRepo.EXPECT().Delete(mock.Anything, charID).Return(nil)

	err := svc.DeleteCharacter(ctx, subjectID, charID)
	require.NoError(t, err)
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteLocation_TransactorRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.SubjectCharacter + ulid.Make().String()

	engine := policytest.NewGrantEngine()
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockLocRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
		Transactor:   tx,
	})

	engine.Grant(subjectID, "delete", "location:"+locID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
	mockLocRepo.EXPECT().Delete(mock.Anything, locID).Return(errors.New("db error"))

	err := svc.DeleteLocation(ctx, subjectID, locID)
	require.Error(t, err)
	assert.True(t, tx.called, "expected InTransaction to be called")
}

// AccessRequest Verification Tests (PR #88 Priority 1)

func TestWorldService_GetLocation_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + charID.String()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockLocationRepository(t)

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockRepo,
		Engine:       mockEngine,
	})

	expectedLoc := &world.Location{ID: locID, Name: "Test Room"}

	// Capture the AccessRequest using mock.MatchedBy
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Get(ctx, locID).Return(expectedLoc, nil)

	_, err := svc.GetLocation(ctx, subjectID, locID)
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "read", capturedRequest.Action, "action should be 'read'")
	assert.Equal(t, "location:"+locID.String(), capturedRequest.Resource, "resource should be location:<id>")
}

func TestWorldService_CreateLocation_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.SubjectCharacter + charID.String()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockLocationRepository(t)

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockRepo,
		Engine:       mockEngine,
	})

	loc := &world.Location{
		Name:        "New Room",
		Description: "A test room",
		Type:        world.LocationTypePersistent,
	}

	// Capture the AccessRequest using mock.MatchedBy
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Create(ctx, mock.Anything).Return(nil)

	err := svc.CreateLocation(ctx, subjectID, loc)
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, "location:*", capturedRequest.Resource, "resource should be location:*")
}

func TestWorldService_MoveCharacter_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	callerID := ulid.Make()
	subjectID := access.SubjectCharacter + callerID.String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockCharRepo := worldtest.NewMockCharacterRepository(t)
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	emitter := &mockEventEmitter{}

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockCharRepo,
		LocationRepo:  mockLocRepo,
		Engine:        mockEngine,
		EventEmitter:  emitter,
	})

	existingChar := &world.Character{
		ID:         charID,
		Name:       "Test Character",
		LocationID: &fromLocID,
	}

	// Capture the AccessRequest using mock.MatchedBy
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
	mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
	mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID).Return(nil)

	err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, "character:"+charID.String(), capturedRequest.Resource, "resource should be character:<id>")
}
