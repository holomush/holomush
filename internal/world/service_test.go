// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/internal/world/worldtest"
	"github.com/holomush/holomush/pkg/errutil"
)

// Compile-time check: policy.Engine must satisfy types.AccessPolicyEngine.
var _ types.AccessPolicyEngine = (*policy.Engine)(nil)

// mockTransactor is a test mock that records whether InTransaction was called
// and executes the function directly (simulating a transaction).
type mockTransactor struct {
	called bool
}

func (m *mockTransactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	m.called = true
	return fn(ctx)
}

// mockOutboxWriter is a test double for world.OutboxWriter. It records the intent
// and delta it received and returns a finalized envelope, or a configured error.
// It stands in for the postgres OutboxStore so unit tests can drive the same-tx
// write seam without a database.
type mockOutboxWriter struct {
	err          error
	calls        int
	lastIntent   wmodel.EnvelopeIntent
	lastDelta    *wmodel.MutationDelta
	lastEnvelope *wmodel.Envelope
}

func (m *mockOutboxWriter) WriteIntent(_ context.Context, intent wmodel.EnvelopeIntent, delta *wmodel.MutationDelta) (*wmodel.Envelope, error) {
	m.calls++
	m.lastIntent = intent
	m.lastDelta = delta
	if m.err != nil {
		return nil, m.err
	}
	// Mirror the postgres writer: allocate a position and finalize from the delta.
	env := wmodel.Finalize(intent, delta, 1, int64(m.calls))
	m.lastEnvelope = env
	return env, nil
}

// withWriteExecutor wires a ServiceConfig with the write executor (a passthrough
// mockTransactor + the given outbox) so location/exit/object write commands route
// through the mutate() seam (05-10). Tests that reach a repo write MUST supply the
// executor or the command reports a configuration error.
func withWriteExecutor(cfg world.ServiceConfig, outbox *mockOutboxWriter) world.ServiceConfig {
	cfg.Transactor = &mockTransactor{}
	cfg.OutboxWriter = outbox
	return cfg
}

func TestWorldService_GetLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockRepo.AssertNotCalled(t, "Get")
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
		mockRepo.AssertNotCalled(t, "Get")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})

	t.Run("preserves decision context on permission denied", func(t *testing.T) {
		// DenyAllEngine returns Decision with Reason="test-deny-all" and PolicyID=""
		engine := policytest.DenyAllEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockRepo.AssertNotCalled(t, "Get")

		// Verify oops context contains decision details
		errutil.AssertErrorContext(t, err, "reason", "test-deny-all")
		errutil.AssertErrorContext(t, err, "policy_id", "")
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
		mockRepo.AssertNotCalled(t, "Get")
		assert.False(t, errors.Is(err, world.ErrPermissionDenied),
			"engine error must not be reported as permission denied")
	})

	t.Run("returns ErrAccessEvaluationFailed on infrastructure failure", func(t *testing.T) {
		// Infrastructure failures (DB errors, session store errors, etc.) return deny decisions
		// with PolicyID starting with "infra:" and should be treated as evaluation failures,
		// not permission denials.
		engine := policytest.NewInfraFailureEngine(t, "session store error", "infra:session-store-error")
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		})

		loc, err := svc.GetLocation(ctx, subjectID, locID)
		assert.Nil(t, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed,
			"infrastructure failure should return ErrAccessEvaluationFailed")
		assert.False(t, errors.Is(err, world.ErrPermissionDenied),
			"infrastructure failure must not be reported as permission denied")
		mockRepo.AssertNotCalled(t, "Get")

		// Verify oops context contains decision details
		errutil.AssertErrorContext(t, err, "reason", "session store error")
		errutil.AssertErrorContext(t, err, "policy_id", "infra:session-store-error")
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
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("creates location when authorized and emits one location_created envelope", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, outbox))

		loc := &world.Location{
			Name:        "New Room",
			Description: "A test room",
			Type:        world.LocationTypePersistent,
		}

		delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateLocation, AfterVersion: 1}}
		engine.Grant(subjectID, "write", "location:*")
		mockRepo.EXPECT().Create(ctx, mock.MatchedBy(func(l *world.Location) bool {
			return l.Name == "New Room" && !l.ID.IsZero()
		})).Return(delta, nil)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.NoError(t, err)
		assert.False(t, loc.ID.IsZero(), "ID should be generated")

		// Exactly one location_created envelope, finalized from the returned delta.
		require.Equal(t, 1, outbox.calls, "exactly one location_created envelope")
		assert.Equal(t, "location_created", outbox.lastIntent.Kind)
		assert.Equal(t, wmodel.AggregateLocation, outbox.lastIntent.AggregateType)
		assert.Equal(t, loc.ID, outbox.lastIntent.AggregateID)
		assert.Equal(t, subjectID, outbox.lastIntent.Actor)
		assert.Same(t, delta, outbox.lastDelta, "the writer finalizes from the returned MutationDelta")

		var payload struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		require.NoError(t, json.Unmarshal(outbox.lastIntent.Payload, &payload))
		assert.Equal(t, loc.ID.String(), payload.ID)
		assert.Equal(t, "New Room", payload.Name)
		assert.Equal(t, "A test room", payload.Description)
	})

	t.Run("preserves existing ID when already set", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, outbox))

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
		})).Return(nil, nil)

		err := svc.CreateLocation(ctx, subjectID, loc)
		require.NoError(t, err)
		assert.Equal(t, existingID, loc.ID, "pre-set ID should be preserved")
		assert.Equal(t, existingID, outbox.lastIntent.AggregateID)
	})

	t.Run("returns permission denied when not authorized (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, outbox))

		loc := &world.Location{Name: "New Room"}

		err := svc.CreateLocation(ctx, subjectID, loc)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockRepo.AssertNotCalled(t, "Create")
		assert.Equal(t, 0, outbox.calls, "a denied command emits nothing")
	})
}

func TestWorldService_UpdateLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("updates location when authorized and emits one location_updated envelope", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, outbox))

		loc := &world.Location{ID: locID, Name: "Updated Room", Type: world.LocationTypePersistent}

		delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateLocation, ID: locID, BeforeVersion: 1, AfterVersion: 2}}
		engine.Grant(subjectID, "write", "location:"+locID.String())
		mockRepo.EXPECT().Update(ctx, loc).Return(delta, nil)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.NoError(t, err)

		require.Equal(t, 1, outbox.calls, "exactly one location_updated envelope")
		assert.Equal(t, "location_updated", outbox.lastIntent.Kind)
		assert.Equal(t, wmodel.AggregateLocation, outbox.lastIntent.AggregateType)
		assert.Equal(t, locID, outbox.lastIntent.AggregateID)
		assert.Same(t, delta, outbox.lastDelta)
	})

	t.Run("returns permission denied when not authorized (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, outbox))

		loc := &world.Location{ID: locID, Name: "Updated Room"}

		err := svc.UpdateLocation(ctx, subjectID, loc)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockRepo.AssertNotCalled(t, "Update")
		assert.Equal(t, 0, outbox.calls)
	})

	t.Run("returns not found when location does not exist (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, outbox))

		loc := &world.Location{ID: locID, Name: "Updated Room", Type: world.LocationTypePersistent}

		engine.Grant(subjectID, "write", "location:"+locID.String())
		mockRepo.EXPECT().Update(ctx, loc).Return(nil, world.ErrNotFound)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
		assert.Equal(t, 0, outbox.calls, "a failed write rolls back with no envelope")
	})

	t.Run("surfaces WORLD_CONCURRENT_EDIT unchanged on a stale write (D-02)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, outbox))

		// Version 3 was read; a concurrent writer advanced it, so the guarded
		// CAS rejects this stale write with the typed conflict.
		loc := &world.Location{ID: locID, Name: "Updated Room", Type: world.LocationTypePersistent, Version: 3}

		engine.Grant(subjectID, "write", "location:"+locID.String())
		conflict := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
		mockRepo.EXPECT().Update(ctx, loc).Return(nil, conflict)

		err := svc.UpdateLocation(ctx, subjectID, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		// D-02: the conflict propagates unchanged — the top-level code stays
		// WORLD_CONCURRENT_EDIT, not a generic LOCATION_UPDATE_FAILED mask.
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
		assert.Equal(t, 0, outbox.calls, "a conflict writes no envelope")
	})
}

func TestWorldService_UpdateCharacterDescription(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("threads the read version into the guarded write", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5}
		engine.Grant(subjectID, "write", access.CharacterResource(charID.String()))
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		// The RMW write MUST carry the version read at the start (5), never a
		// re-read or a zeroed version — that is what arms the guard.
		mockRepo.EXPECT().Update(ctx, mock.MatchedBy(func(c *world.Character) bool {
			return c.Version == 5 && c.Description == "a new description"
		})).Return(nil, nil)

		err := svc.UpdateCharacterDescription(ctx, subjectID, charID, "a new description")
		require.NoError(t, err)
	})

	t.Run("surfaces WORLD_CONCURRENT_EDIT unchanged on a stale write (D-02)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5}
		engine.Grant(subjectID, "write", access.CharacterResource(charID.String()))
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		conflict := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
		mockRepo.EXPECT().Update(ctx, mock.MatchedBy(func(c *world.Character) bool {
			return c.Version == 5
		})).Return(nil, conflict)

		err := svc.UpdateCharacterDescription(ctx, subjectID, charID, "a new description")
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
	})
}

func TestWorldService_DeleteLocation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("deletes location when authorized and emits one location_deleted tombstone with cascade manifest", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
		}, outbox))

		// The repo delta carries the DB-cascaded exit tombstones (preselected under
		// lock in 05-02); the single envelope's manifest must cover them.
		cascadedExit := ulid.Make()
		delta := &wmodel.MutationDelta{
			Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateLocation, ID: locID, Tombstone: true, BeforeVersion: 4},
			Affected: []wmodel.AffectedAggregate{
				{Type: wmodel.AggregateExit, ID: cascadedExit, Tombstone: true, BeforeVersion: 2},
			},
		}
		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, locID, mock.Anything).Return(delta, nil)

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.NoError(t, err)

		require.Equal(t, 1, outbox.calls, "a delete emits exactly one tombstone envelope, not one per cascaded row")
		assert.Equal(t, "location_deleted", outbox.lastIntent.Kind)
		assert.Equal(t, locID, outbox.lastIntent.AggregateID)
		assert.Same(t, delta, outbox.lastDelta, "the manifest is finalized from the repo delta incl. the cascaded exits")
	})

	t.Run("returns permission denied when not authorized (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
		}, outbox))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockRepo.AssertNotCalled(t, "Delete")
		mockPropRepo.AssertNotCalled(t, "DeleteByParent")
		assert.Equal(t, 0, outbox.calls)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
		}, outbox))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		mockRepo.AssertNotCalled(t, "Delete")
		mockPropRepo.AssertNotCalled(t, "DeleteByParent")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})

	t.Run("propagates repository errors (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)
		mockPropRepo := worldtest.NewMockPropertyRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			PropertyRepo: mockPropRepo,
			Engine:       engine,
		}, outbox))

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, locID, mock.Anything).Return(nil, errors.New("db error"))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		assert.Equal(t, 0, outbox.calls, "a rolled-back delete writes no envelope")
	})
}

func TestWorldService_GetExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockExitRepo.AssertNotCalled(t, "Get")
	})
}

func TestWorldService_CreateExit(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("creates exit when authorized and emits one exit_created envelope", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
		}

		delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateExit, AfterVersion: 1}}
		engine.Grant(subjectID, "write", "exit:*")
		mockExitRepo.EXPECT().Create(ctx, mock.MatchedBy(func(e *world.Exit) bool {
			return e.Name == "north" && !e.ID.IsZero()
		})).Return(delta, nil)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.NoError(t, err)
		assert.False(t, exit.ID.IsZero(), "ID should be generated")

		require.Equal(t, 1, outbox.calls, "exactly one exit_created envelope")
		assert.Equal(t, "exit_created", outbox.lastIntent.Kind)
		assert.Equal(t, wmodel.AggregateExit, outbox.lastIntent.AggregateType)
		assert.Equal(t, exit.ID, outbox.lastIntent.AggregateID)
		assert.Same(t, delta, outbox.lastDelta)

		var payload struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			FromLocationID string `json:"from_location_id"`
			ToLocationID   string `json:"to_location_id"`
		}
		require.NoError(t, json.Unmarshal(outbox.lastIntent.Payload, &payload))
		assert.Equal(t, "north", payload.Name)
		assert.Equal(t, fromLocID.String(), payload.FromLocationID)
		assert.Equal(t, toLocID.String(), payload.ToLocationID)
	})

	t.Run("returns permission denied when not authorized (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		exit := &world.Exit{Name: "north"}

		err := svc.CreateExit(ctx, subjectID, exit)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockExitRepo.AssertNotCalled(t, "Create")
		assert.Equal(t, 0, outbox.calls)
	})
}

func TestWorldService_UpdateExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("updates exit when authorized and emits one exit_updated envelope", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		exit := &world.Exit{ID: exitID, Name: "north updated", Visibility: world.VisibilityAll}

		delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateExit, ID: exitID, BeforeVersion: 1, AfterVersion: 2}}
		engine.Grant(subjectID, "write", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Update(ctx, exit).Return(delta, nil)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.NoError(t, err)

		require.Equal(t, 1, outbox.calls, "exactly one exit_updated envelope")
		assert.Equal(t, "exit_updated", outbox.lastIntent.Kind)
		assert.Equal(t, exitID, outbox.lastIntent.AggregateID)
		assert.Same(t, delta, outbox.lastDelta)
	})

	t.Run("returns permission denied when not authorized (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		err := svc.UpdateExit(ctx, subjectID, exit)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockExitRepo.AssertNotCalled(t, "Update")
		assert.Equal(t, 0, outbox.calls)
	})

	t.Run("propagates repository errors (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Update(ctx, exit).Return(nil, errors.New("db error"))

		err := svc.UpdateExit(ctx, subjectID, exit)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
		assert.Equal(t, 0, outbox.calls)
	})

	t.Run("returns not found when exit does not exist (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		exit := &world.Exit{ID: exitID, Name: "north", Visibility: world.VisibilityAll}

		engine.Grant(subjectID, "write", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Update(ctx, exit).Return(nil, world.ErrNotFound)

		err := svc.UpdateExit(ctx, subjectID, exit)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "EXIT_NOT_FOUND")
		assert.Equal(t, 0, outbox.calls)
	})
}

func TestWorldService_DeleteExit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("deletes exit when authorized and emits one exit_deleted tombstone", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		// A bidirectional delete reports the reverse exit in the delta's Affected;
		// the single tombstone envelope's manifest covers it.
		reverseExit := ulid.Make()
		delta := &wmodel.MutationDelta{
			Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateExit, ID: exitID, Tombstone: true, BeforeVersion: 3},
			Affected: []wmodel.AffectedAggregate{
				{Type: wmodel.AggregateExit, ID: reverseExit, Tombstone: true, BeforeVersion: 1},
			},
		}
		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Delete(ctx, exitID, mock.Anything).Return(delta, nil)

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.NoError(t, err)

		require.Equal(t, 1, outbox.calls, "a bidirectional delete emits one tombstone envelope, not one per exit")
		assert.Equal(t, "exit_deleted", outbox.lastIntent.Kind)
		assert.Equal(t, exitID, outbox.lastIntent.AggregateID)
		assert.Same(t, delta, outbox.lastDelta)
	})

	t.Run("returns permission denied when not authorized (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		mockExitRepo.AssertNotCalled(t, "Delete")
		assert.Equal(t, 0, outbox.calls)
	})

	t.Run("returns permission denied on explicit policy deny", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied,
			"explicit policy deny should return ErrPermissionDenied")
		mockExitRepo.AssertNotCalled(t, "Delete")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})

	t.Run("non-severe bidirectional cleanup commits the envelope and succeeds", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)
		outbox := &mockOutboxWriter{}

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, outbox))

		toLocationID := ulid.Make()
		// Non-severe: primary delete committed, the reverse exit was already gone.
		// The repo returns (delta, non-severe-cleanup); the command still emits.
		delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateExit, ID: exitID, Tombstone: true, BeforeVersion: 2}}
		cleanupResult := &world.BidirectionalCleanupResult{
			ExitID:       exitID,
			ToLocationID: toLocationID,
			ReturnName:   "south",
			Issue: &world.CleanupIssue{
				Type: world.CleanupReturnNotFound,
			},
		}

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockExitRepo.EXPECT().Delete(ctx, exitID, mock.Anything).Return(delta, cleanupResult)

		// Should succeed since primary delete worked, AND still emit the tombstone.
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.NoError(t, err)
		require.Equal(t, 1, outbox.calls, "a non-severe cleanup still commits + emits the tombstone")
		assert.Equal(t, "exit_deleted", outbox.lastIntent.Kind)
	})
}

func TestWorldService_GetObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockObjRepo.AssertNotCalled(t, "Get")
	})
}

func TestWorldService_CreateObject(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
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
		})).Return(nil, nil)

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
		mockObjRepo.AssertNotCalled(t, "Create")
	})
}

func TestWorldService_UpdateObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	locationID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockObjRepo.EXPECT().Update(ctx, obj).Return(nil, nil)

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
		mockObjRepo.AssertNotCalled(t, "Update")
	})

	t.Run("surfaces WORLD_CONCURRENT_EDIT unchanged on a stale write (D-02)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo: mockObjRepo,
			Engine:     engine,
		})

		obj, err := world.NewObjectWithID(objID, "sword updated", world.InLocation(locationID))
		require.NoError(t, err)
		obj.Version = 4 // read-time version threaded into the guarded write

		engine.Grant(subjectID, "write", "object:"+objID.String())
		conflict := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
		mockObjRepo.EXPECT().Update(ctx, obj).Return(nil, conflict)

		err = svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
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
		mockObjRepo.EXPECT().Update(ctx, obj).Return(nil, errors.New("db error"))

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
		mockObjRepo.EXPECT().Update(ctx, obj).Return(nil, world.ErrNotFound)

		err = svc.UpdateObject(ctx, subjectID, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})
}

func TestWorldService_DeleteObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockObjRepo.EXPECT().Delete(mock.Anything, objID, mock.Anything).Return(nil, nil)

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
		mockObjRepo.AssertNotCalled(t, "Delete")
		mockPropRepo.AssertNotCalled(t, "DeleteByParent")
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
		mockObjRepo.AssertNotCalled(t, "Delete")
		mockPropRepo.AssertNotCalled(t, "DeleteByParent")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})
}

func TestWorldService_MoveObject(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())
	locationID := ulid.Make()

	t.Run("moves object when authorized", func(t *testing.T) {
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
		mockObjRepo.EXPECT().Move(ctx, objID, to, mock.Anything).Return(nil, nil)

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
		mockObjRepo.EXPECT().Move(ctx, objID, to, mock.Anything).Return(nil, errors.New("db error"))

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
	//
	// The post-commit emit path was deleted in 05-06 (D-03); MoveObject no longer
	// emits (its outbox routing migrates in 05-10/05-11), so the EVENT_EMIT_FAILED
	// subtest was removed with the mechanism it exercised.
}

func TestWorldService_ListSceneParticipants(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockSceneRepo.AssertNotCalled(t, "ListParticipants")
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
		mockSceneRepo.AssertNotCalled(t, "ListParticipants")
		assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
			"explicit deny must not be reported as evaluation error")
	})

	t.Run("returns ErrAccessEvaluationFailed when engine errors", func(t *testing.T) {
		engineErr := errors.New("policy store unavailable")
		engine := policytest.NewErrorEngine(engineErr)
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		assert.Nil(t, participants)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
		assert.False(t, errors.Is(err, world.ErrPermissionDenied),
			"engine error must not be reported as permission denied")
		mockSceneRepo.AssertNotCalled(t, "ListParticipants")
	})

	t.Run("returns ErrAccessEvaluationFailed on infrastructure failure", func(t *testing.T) {
		engine := policytest.NewInfraFailureEngine(t, "session store error", "infra:session-store-error")
		mockSceneRepo := worldtest.NewMockSceneRepository(t)

		svc := world.NewService(world.ServiceConfig{
			SceneRepo: mockSceneRepo,
			Engine:    engine,
		})

		participants, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
		assert.Nil(t, participants)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed,
			"infrastructure failure should return ErrAccessEvaluationFailed")
		assert.False(t, errors.Is(err, world.ErrPermissionDenied),
			"infrastructure failure must not be reported as permission denied")
		mockSceneRepo.AssertNotCalled(t, "ListParticipants")
	})
}

// --- Input Validation Tests ---

func TestWorldService_CreateLocationValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_CreateExitValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
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

func TestWorldService_CreateObjectValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

// --- Repository Error Propagation Tests ---

func TestWorldService_GetLocationErrorPropagation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_CreateLocationErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockLocationRepository(t)

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, &mockOutboxWriter{}))

		loc := &world.Location{
			Name: "Test Room",
			Type: world.LocationTypePersistent,
		}

		engine.Grant(subjectID, "write", "location:*")
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, errors.New("db error"))

		err := svc.CreateLocation(ctx, subjectID, loc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_GetExitErrorPropagation(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_CreateExitErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("propagates repository errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
		}

		engine.Grant(subjectID, "write", "exit:*")
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, errors.New("db error"))

		err := svc.CreateExit(ctx, subjectID, exit)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

func TestWorldService_GetObjectErrorPropagation(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_CreateObjectErrorPropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
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
		mockObjRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, errors.New("db error"))

		err = svc.CreateObject(ctx, subjectID, obj)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db error")
	})
}

// --- Scene Repository Error Propagation Tests ---

func TestWorldService_ListSceneParticipantsErrorPropagation(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_DeleteExitSevereCleanup(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("propagates severe cleanup error for bidirectional exit", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

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
		mockExitRepo.EXPECT().Delete(ctx, exitID, mock.Anything).Return(nil, cleanupResult)

		// Severe error means the entire operation was rolled back - return error
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete exit")
	})

	t.Run("propagates delete error cleanup for bidirectional exit", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

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
		mockExitRepo.EXPECT().Delete(ctx, exitID, mock.Anything).Return(nil, cleanupResult)

		// Severe error means the entire operation was rolled back - return error
		err := svc.DeleteExit(ctx, subjectID, exitID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "delete exit")
	})
}

func TestWorldService_GetExitsByLocation(t *testing.T) {
	ctx := context.Background()
	locationID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
		mockExitRepo.AssertNotCalled(t, "ListFromLocation")
	})

	t.Run("returns LOCATION_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		})

		exits, err := svc.GetExitsByLocation(ctx, subjectID, locationID)
		assert.Nil(t, exits)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
		mockExitRepo.AssertNotCalled(t, "ListFromLocation")
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

func TestWorldService_UpdateLocationValidation(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_UpdateExitValidation(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_UpdateObjectValidation(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_CreateLocationTypeValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())

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

func TestWorldService_CreateExitVisibilityValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
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

func TestWorldService_UpdateExitLockDataValidation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
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

func TestWorldService_CreateExitValidationBypass(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("accepts unlocked exit with invalid lock type", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

		exit := &world.Exit{
			FromLocationID: fromLocID,
			ToLocationID:   toLocID,
			Name:           "north",
			Visibility:     world.VisibilityAll,
			Locked:         false,
			LockType:       world.LockType("garbage"), // Invalid but should be ignored
		}

		engine.Grant(subjectID, "write", "exit:*")
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, nil)

		err := svc.CreateExit(ctx, subjectID, exit)
		require.NoError(t, err, "unlocked exit with invalid lock type should succeed")
	})

	t.Run("accepts non-list visibility with invalid visible_to", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockExitRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockExitRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

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
		mockExitRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, nil)

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())
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
	subjectID := access.CharacterSubject(ulid.Make().String())
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
	subjectID := access.CharacterSubject(ulid.Make().String())
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
	subjectID := access.CharacterSubject(ulid.Make().String())
	sceneID := ulid.Make()

	engine := policytest.NewGrantEngine()
	svc := world.NewService(world.ServiceConfig{
		Engine: engine,
		// SceneRepo intentionally nil
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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockRepo.AssertNotCalled(t, "Get")
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
		mockRepo.AssertNotCalled(t, "Create")
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

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, &mockOutboxWriter{}))

		engine.Grant(subjectID, "write", "location:*")
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Update")
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

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			LocationRepo: mockRepo,
			Engine:       engine,
		}, &mockOutboxWriter{}))

		engine.Grant(subjectID, "write", "location:"+locID.String())
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Delete")
		mockPropRepo.AssertNotCalled(t, "DeleteByParent")
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
			OutboxWriter: &mockOutboxWriter{},
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, locID, mock.Anything).Return(nil, world.ErrNotFound)

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
			OutboxWriter: &mockOutboxWriter{},
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockRepo.EXPECT().Delete(mock.Anything, locID, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Get")
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
		mockRepo.AssertNotCalled(t, "Create")
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
		mockRepo.AssertNotCalled(t, "Update")
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
		mockRepo.AssertNotCalled(t, "Delete")
		mockPropRepo.AssertNotCalled(t, "DeleteByParent")
	})
}

func TestService_ErrorCodes_Exit(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockRepo.AssertNotCalled(t, "Get")
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
		mockRepo.AssertNotCalled(t, "Create")
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

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

		engine.Grant(subjectID, "write", "exit:*")
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Update")
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

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

		engine.Grant(subjectID, "write", "exit:"+exitID.String())
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Delete")
	})

	t.Run("DeleteExit returns EXIT_NOT_FOUND for ErrNotFound", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockRepo.EXPECT().Delete(ctx, exitID, mock.Anything).Return(nil, world.ErrNotFound)

		err := svc.DeleteExit(ctx, subjectID, exitID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_NOT_FOUND")
	})

	t.Run("DeleteExit returns EXIT_DELETE_FAILED for repo errors", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockRepo := worldtest.NewMockExitRepository(t)

		svc := world.NewService(withWriteExecutor(world.ServiceConfig{
			ExitRepo: mockRepo,
			Engine:   engine,
		}, &mockOutboxWriter{}))

		engine.Grant(subjectID, "delete", "exit:"+exitID.String())
		mockRepo.EXPECT().Delete(ctx, exitID, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Get")
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
		mockRepo.AssertNotCalled(t, "Create")
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
		mockRepo.AssertNotCalled(t, "Update")
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
		mockRepo.AssertNotCalled(t, "Delete")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})
}

func TestService_ErrorCodes_Object(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	locationID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockRepo.AssertNotCalled(t, "Get")
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
		mockRepo.AssertNotCalled(t, "Create")
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
		mockRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Update")
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
		mockRepo.EXPECT().Update(ctx, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Delete")
		mockPropRepo.AssertNotCalled(t, "DeleteByParent")
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
		mockRepo.EXPECT().Delete(mock.Anything, objID, mock.Anything).Return(nil, world.ErrNotFound)

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
		mockRepo.EXPECT().Delete(mock.Anything, objID, mock.Anything).Return(nil, errors.New("db error"))

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
		mockRepo.AssertNotCalled(t, "Get")
		mockRepo.AssertNotCalled(t, "Update")
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
		mockRepo.EXPECT().Move(ctx, objID, mock.Anything, mock.Anything).Return(nil, world.ErrNotFound)

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
		mockRepo.EXPECT().Move(ctx, objID, mock.Anything, mock.Anything).Return(nil, errors.New("db error"))

		err = svc.MoveObject(ctx, subjectID, objID, world.Containment{LocationID: &locationID})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_MOVE_FAILED")
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
		mockRepo.AssertNotCalled(t, "Get")
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
		mockRepo.AssertNotCalled(t, "Create")
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
		mockRepo.AssertNotCalled(t, "Update")
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
		mockRepo.AssertNotCalled(t, "Delete")
		mockPropRepo.AssertNotCalled(t, "DeleteByParent")
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
		mockRepo.AssertNotCalled(t, "Get")
		mockRepo.AssertNotCalled(t, "Move")
	})
}

func TestService_ErrorCodes_Scene(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockRepo.AssertNotCalled(t, "ListParticipants")
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
		mockRepo.AssertNotCalled(t, "ListParticipants")
	})
}

func TestWorldService_GetCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
	})

	t.Run("returns LOCATION_ACCESS_EVALUATION_FAILED for engine errors", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
		assert.Nil(t, chars)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
	})

	t.Run("returns ErrAccessEvaluationFailed on infrastructure failure", func(t *testing.T) {
		engine := policytest.NewInfraFailureEngine(t, "session store error", "infra:session-store-error")
		mockRepo := worldtest.NewMockCharacterRepository(t)

		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        engine,
		})

		chars, err := svc.GetCharactersByLocation(ctx, subjectID, locationID, world.ListOptions{})
		assert.Nil(t, chars)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed,
			"infrastructure failure should return ErrAccessEvaluationFailed")
		assert.False(t, errors.Is(err, world.ErrPermissionDenied),
			"infrastructure failure must not be reported as permission denied")
		errutil.AssertErrorContext(t, err, "reason", "session store error")
		errutil.AssertErrorContext(t, err, "policy_id", "infra:session-store-error")
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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(charID.String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	// newMoveSvc wires a Service with the write executor (transactor + outbox) so
	// MoveCharacter routes the guarded write + its move envelope through the same-tx
	// seam (05-06).
	newMoveSvc := func(t *testing.T, engine *policytest.GrantEngine, charRepo *worldtest.MockCharacterRepository, locRepo *worldtest.MockLocationRepository, outbox *mockOutboxWriter) *world.Service {
		t.Helper()
		return world.NewService(world.ServiceConfig{
			CharacterRepo: charRepo,
			LocationRepo:  locRepo,
			Engine:        engine,
			Transactor:    &mockTransactor{},
			OutboxWriter:  outbox,
		})
	}

	t.Run("successful move commits state and exactly one envelope atomically", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := newMoveSvc(t, engine, mockCharRepo, mockLocRepo, outbox)

		delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: charID}}
		existingChar := &world.Character{ID: charID, Name: "Test Character", LocationID: &fromLocID, Version: 3}

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		// The character's read version is threaded as the CAS guard.
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID, 3).Return(delta, nil)

		require.NoError(t, svc.MoveCharacter(ctx, subjectID, charID, toLocID))

		// Exactly one envelope was written, finalized from the returned delta, and
		// carrying the new-values-only move intent.
		require.Equal(t, 1, outbox.calls, "exactly one move envelope must be written")
		assert.Equal(t, "character_moved", outbox.lastIntent.Kind)
		assert.Equal(t, wmodel.AggregateCharacter, outbox.lastIntent.AggregateType)
		assert.Equal(t, charID, outbox.lastIntent.AggregateID)
		assert.Equal(t, subjectID, outbox.lastIntent.Actor)
		assert.Same(t, delta, outbox.lastDelta, "the writer finalizes from the returned MutationDelta")

		var payload struct {
			CharacterID    string  `json:"character_id"`
			ToLocationID   string  `json:"to_location_id"`
			FromLocationID *string `json:"from_location_id"`
		}
		require.NoError(t, json.Unmarshal(outbox.lastIntent.Payload, &payload))
		assert.Equal(t, charID.String(), payload.CharacterID)
		assert.Equal(t, toLocID.String(), payload.ToLocationID)
		require.NotNil(t, payload.FromLocationID)
		assert.Equal(t, fromLocID.String(), *payload.FromLocationID)
	})

	t.Run("first-time placement writes an envelope with no from-location", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := newMoveSvc(t, engine, mockCharRepo, mockLocRepo, outbox)

		existingChar := &world.Character{ID: charID, Name: "New Character", LocationID: nil}

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID, mock.Anything).Return(&wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: charID}}, nil)

		require.NoError(t, svc.MoveCharacter(ctx, subjectID, charID, toLocID))

		require.Equal(t, 1, outbox.calls)
		var payload struct {
			FromLocationID *string `json:"from_location_id"`
		}
		require.NoError(t, json.Unmarshal(outbox.lastIntent.Payload, &payload))
		assert.Nil(t, payload.FromLocationID, "first-time placement omits from_location_id")
	})

	t.Run("returns CHARACTER_NOT_FOUND when character does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := newMoveSvc(t, engine, mockCharRepo, mockLocRepo, outbox)

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
		assert.Equal(t, 0, outbox.calls, "a pre-commit failure writes no envelope")
	})

	t.Run("returns CHARACTER_MOVE_FAILED when Get fails with generic error", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := newMoveSvc(t, engine, mockCharRepo, mockLocRepo, outbox)

		dbErr := errors.New("database connection lost")
		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(nil, dbErr)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
		assert.Contains(t, err.Error(), "get character")
		assert.Equal(t, 0, outbox.calls)
	})

	t.Run("returns LOCATION_NOT_FOUND when destination does not exist", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := newMoveSvc(t, engine, mockCharRepo, mockLocRepo, outbox)

		existingChar := &world.Character{ID: charID, Name: "Test Character", LocationID: &fromLocID}

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(nil, world.ErrNotFound)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
		assert.Equal(t, 0, outbox.calls, "a missing destination writes no envelope")
	})

	t.Run("returns CHARACTER_ACCESS_DENIED when not authorized", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		svc := newMoveSvc(t, engine, mockCharRepo, mockLocRepo, &mockOutboxWriter{})

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
			Transactor:    &mockTransactor{},
			OutboxWriter:  &mockOutboxWriter{},
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
			Transactor:   &mockTransactor{},
			OutboxWriter: &mockOutboxWriter{},
		})

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
	})

	t.Run("returns CHARACTER_MOVE_FAILED when the write executor is not configured", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)

		// No OutboxWriter/Transactor → no executor.
		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockCharRepo,
			LocationRepo:  mockLocRepo,
			Engine:        engine,
		})

		existingChar := &world.Character{ID: charID, Name: "Test Character", LocationID: &fromLocID}
		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
	})

	t.Run("returns CHARACTER_MOVE_FAILED when UpdateLocation fails (no envelope)", func(t *testing.T) {
		engine := policytest.NewGrantEngine()
		mockCharRepo := worldtest.NewMockCharacterRepository(t)
		mockLocRepo := worldtest.NewMockLocationRepository(t)
		outbox := &mockOutboxWriter{}

		svc := newMoveSvc(t, engine, mockCharRepo, mockLocRepo, outbox)

		existingChar := &world.Character{ID: charID, Name: "Test Character", LocationID: &fromLocID}
		dbErr := errors.New("database error")

		engine.Grant(subjectID, "write", "character:"+charID.String())
		mockCharRepo.EXPECT().Get(ctx, charID).Return(existingChar, nil)
		mockLocRepo.EXPECT().Get(ctx, toLocID).Return(&world.Location{ID: toLocID}, nil)
		mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID, mock.Anything).Return(nil, dbErr)

		err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_MOVE_FAILED")
		assert.Equal(t, 0, outbox.calls, "a failed guarded write rolls back with no envelope")
	})
}

func TestWorldService_FindLocationByName(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())
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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockCharRepo.EXPECT().Delete(mock.Anything, charID, mock.Anything).Return(nil, nil)

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

	t.Run("returns ErrAccessEvaluationFailed on infrastructure failure", func(t *testing.T) {
		engine := policytest.NewInfraFailureEngine(t, "session store error", "infra:session-store-error")
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
		assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed,
			"infrastructure failure should return ErrAccessEvaluationFailed")
		assert.False(t, errors.Is(err, world.ErrPermissionDenied),
			"infrastructure failure must not be reported as permission denied")
		errutil.AssertErrorContext(t, err, "reason", "session store error")
		errutil.AssertErrorContext(t, err, "policy_id", "infra:session-store-error")
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
		mockCharRepo.EXPECT().Delete(mock.Anything, charID, mock.Anything).Return(nil, world.ErrNotFound)

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
		mockCharRepo.EXPECT().Delete(mock.Anything, charID, mock.Anything).Return(nil, errors.New("db error"))

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockCharRepo.EXPECT().Delete(mock.Anything, charID, mock.Anything).Return(nil, nil)

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
		mockCharRepo.EXPECT().Delete(mock.Anything, charID, mock.Anything).Return(nil, errors.New("db error"))

		err := svc.DeleteCharacter(ctx, subjectID, charID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_DELETE_FAILED")
	})
}

func TestWorldService_DeleteLocation_CascadesProperties(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
			OutboxWriter: &mockOutboxWriter{},
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockLocRepo.EXPECT().Delete(mock.Anything, locID, mock.Anything).Return(nil, nil)

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
			OutboxWriter: &mockOutboxWriter{},
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
			OutboxWriter: &mockOutboxWriter{},
		})

		engine.Grant(subjectID, "delete", "location:"+locID.String())
		mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
		mockLocRepo.EXPECT().Delete(mock.Anything, locID, mock.Anything).Return(nil, errors.New("db error"))

		err := svc.DeleteLocation(ctx, subjectID, locID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_DELETE_FAILED")
	})
}

func TestWorldService_DeleteObject_CascadesProperties(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
		mockObjRepo.EXPECT().Delete(mock.Anything, objID, mock.Anything).Return(nil, nil)

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
		mockObjRepo.EXPECT().Delete(mock.Anything, objID, mock.Anything).Return(nil, errors.New("db error"))

		err := svc.DeleteObject(ctx, subjectID, objID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "OBJECT_DELETE_FAILED")
	})
}

func TestWorldService_DeleteLocation_PropertyDeleteFails(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	engine := policytest.NewGrantEngine()
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockLocRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
		Transactor:   tx,
		OutboxWriter: &mockOutboxWriter{},
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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	subjectID := access.CharacterSubject(ulid.Make().String())

	engine := policytest.NewGrantEngine()
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockLocRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
		Transactor:   tx,
		OutboxWriter: &mockOutboxWriter{},
	})

	engine.Grant(subjectID, "delete", "location:"+locID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
	mockLocRepo.EXPECT().Delete(mock.Anything, locID, mock.Anything).Return(nil, nil)

	err := svc.DeleteLocation(ctx, subjectID, locID)
	require.NoError(t, err)
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteObject_UsesTransactor(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	mockObjRepo.EXPECT().Delete(mock.Anything, objID, mock.Anything).Return(nil, nil)

	err := svc.DeleteObject(ctx, subjectID, objID)
	require.NoError(t, err)
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteCharacter_UsesTransactor(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

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
	mockCharRepo.EXPECT().Delete(mock.Anything, charID, mock.Anything).Return(nil, nil)

	err := svc.DeleteCharacter(ctx, subjectID, charID)
	require.NoError(t, err)
	assert.True(t, tx.called, "expected InTransaction to be called")
}

func TestWorldService_DeleteLocation_TransactorRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	engine := policytest.NewGrantEngine()
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	tx := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockLocRepo,
		PropertyRepo: mockPropRepo,
		Engine:       engine,
		Transactor:   tx,
		OutboxWriter: &mockOutboxWriter{},
	})

	engine.Grant(subjectID, "delete", "location:"+locID.String())
	mockPropRepo.EXPECT().DeleteByParent(mock.Anything, "location", locID).Return(nil)
	mockLocRepo.EXPECT().Delete(mock.Anything, locID, mock.Anything).Return(nil, errors.New("db error"))

	err := svc.DeleteLocation(ctx, subjectID, locID)
	require.Error(t, err)
	assert.True(t, tx.called, "expected InTransaction to be called")
}

// AccessRequest Verification Tests (PR #88 Priority 1)

func TestWorldService_GetLocation_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

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
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockLocationRepository(t)

	svc := world.NewService(withWriteExecutor(world.ServiceConfig{
		LocationRepo: mockRepo,
		Engine:       mockEngine,
	}, &mockOutboxWriter{}))

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

	mockRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, nil)

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
	subjectID := access.CharacterSubject(callerID.String())
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockCharRepo := worldtest.NewMockCharacterRepository(t)
	mockLocRepo := worldtest.NewMockLocationRepository(t)

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockCharRepo,
		LocationRepo:  mockLocRepo,
		Engine:        mockEngine,
		Transactor:    &mockTransactor{},
		OutboxWriter:  &mockOutboxWriter{},
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
	mockCharRepo.EXPECT().UpdateLocation(ctx, charID, &toLocID, mock.Anything).Return(nil, nil)

	err := svc.MoveCharacter(ctx, subjectID, charID, toLocID)
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, "character:"+charID.String(), capturedRequest.Resource, "resource should be character:<id>")
}

func TestWorldService_CreateExit_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockExitRepository(t)

	svc := world.NewService(withWriteExecutor(world.ServiceConfig{
		ExitRepo: mockRepo,
		Engine:   mockEngine,
	}, &mockOutboxWriter{}))

	exit := &world.Exit{
		Name:           "North",
		Aliases:        []string{"n"},
		FromLocationID: fromLocID,
		ToLocationID:   toLocID,
		Visibility:     world.VisibilityAll,
	}

	// Capture the AccessRequest using mock.MatchedBy
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, nil)

	err := svc.CreateExit(ctx, subjectID, exit)
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, "exit:*", capturedRequest.Resource, "resource should be exit:*")
}

func TestWorldService_DeleteExit_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockExitRepository(t)

	svc := world.NewService(withWriteExecutor(world.ServiceConfig{
		ExitRepo: mockRepo,
		Engine:   mockEngine,
	}, &mockOutboxWriter{}))

	// Capture the AccessRequest using mock.MatchedBy
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Delete(ctx, exitID, mock.Anything).Return(nil, nil)

	err := svc.DeleteExit(ctx, subjectID, exitID)
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "delete", capturedRequest.Action, "action should be 'delete'")
	assert.Equal(t, "exit:"+exitID.String(), capturedRequest.Resource, "resource should be exit:<id>")
}

func TestWorldService_DeleteCharacter_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	callerID := ulid.Make()
	subjectID := access.CharacterSubject(callerID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockCharRepo := worldtest.NewMockCharacterRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	mockTransactor := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockCharRepo,
		PropertyRepo:  mockPropRepo,
		Engine:        mockEngine,
		Transactor:    mockTransactor,
	})

	// Capture the AccessRequest using mock.MatchedBy
	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockPropRepo.EXPECT().DeleteByParent(ctx, "character", charID).Return(nil)
	mockCharRepo.EXPECT().Delete(ctx, charID, mock.Anything).Return(nil, nil)

	err := svc.DeleteCharacter(ctx, subjectID, charID)
	require.NoError(t, err)

	// Verify AccessRequest fields
	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "delete", capturedRequest.Action, "action should be 'delete'")
	assert.Equal(t, "character:"+charID.String(), capturedRequest.Resource, "resource should be character:<id>")
}

// --- Exit VerifiesAccessRequest tests ---

func TestWorldService_GetExit_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	exitID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockExitRepository(t)

	svc := world.NewService(world.ServiceConfig{
		ExitRepo: mockRepo,
		Engine:   mockEngine,
	})

	expectedExit := &world.Exit{ID: exitID, Name: "North"}

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Get(ctx, exitID).Return(expectedExit, nil)

	_, err := svc.GetExit(ctx, subjectID, exitID)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "read", capturedRequest.Action, "action should be 'read'")
	assert.Equal(t, "exit:"+exitID.String(), capturedRequest.Resource, "resource should be exit:<id>")
}

func TestWorldService_UpdateExit_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockExitRepository(t)

	svc := world.NewService(withWriteExecutor(world.ServiceConfig{
		ExitRepo: mockRepo,
		Engine:   mockEngine,
	}, &mockOutboxWriter{}))

	exit := &world.Exit{
		ID:             ulid.Make(),
		Name:           "North",
		Aliases:        []string{"n"},
		FromLocationID: fromLocID,
		ToLocationID:   toLocID,
		Visibility:     world.VisibilityAll,
	}

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Update(ctx, exit).Return(nil, nil)

	err := svc.UpdateExit(ctx, subjectID, exit)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, "exit:"+exit.ID.String(), capturedRequest.Resource, "resource should be exit:<id>")
}

func TestWorldService_GetExitsByLocation_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	locationID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockExitRepository(t)

	svc := world.NewService(world.ServiceConfig{
		ExitRepo: mockRepo,
		Engine:   mockEngine,
	})

	expectedExits := []*world.Exit{{ID: ulid.Make(), Name: "North"}}

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().ListFromLocation(ctx, locationID).Return(expectedExits, nil)

	_, err := svc.GetExitsByLocation(ctx, subjectID, locationID)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "read", capturedRequest.Action, "action should be 'read'")
	assert.Equal(t, "location:"+locationID.String(), capturedRequest.Resource, "resource should be location:<id>")
}

// --- Object VerifiesAccessRequest tests ---

func TestWorldService_GetObject_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockObjectRepository(t)

	svc := world.NewService(world.ServiceConfig{
		ObjectRepo: mockRepo,
		Engine:     mockEngine,
	})

	expectedObj := &world.Object{ID: objID, Name: "Sword"}

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Get(ctx, objID).Return(expectedObj, nil)

	_, err := svc.GetObject(ctx, subjectID, objID)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "read", capturedRequest.Action, "action should be 'read'")
	assert.Equal(t, "object:"+objID.String(), capturedRequest.Resource, "resource should be object:<id>")
}

func TestWorldService_CreateObject_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())
	locID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockObjectRepository(t)

	svc := world.NewService(world.ServiceConfig{
		ObjectRepo: mockRepo,
		Engine:     mockEngine,
	})

	obj, objErr := world.NewObject("Sword", world.InLocation(locID))
	require.NoError(t, objErr)

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Create(ctx, mock.Anything).Return(nil, nil)

	err := svc.CreateObject(ctx, subjectID, obj)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, access.ObjectResource("*"), capturedRequest.Resource, "resource should match typed helper")
}

func TestWorldService_UpdateObject_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())
	locID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockObjectRepository(t)

	svc := world.NewService(world.ServiceConfig{
		ObjectRepo: mockRepo,
		Engine:     mockEngine,
	})

	objID := ulid.Make()
	obj, objErr := world.NewObjectWithID(objID, "Sword", world.InLocation(locID))
	require.NoError(t, objErr)

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Update(ctx, obj).Return(nil, nil)

	err := svc.UpdateObject(ctx, subjectID, obj)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, "object:"+objID.String(), capturedRequest.Resource, "resource should be object:<id>")
}

func TestWorldService_DeleteObject_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockObjRepo := worldtest.NewMockObjectRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	mockTransactor := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		ObjectRepo:   mockObjRepo,
		PropertyRepo: mockPropRepo,
		Engine:       mockEngine,
		Transactor:   mockTransactor,
	})

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockPropRepo.EXPECT().DeleteByParent(ctx, "object", objID).Return(nil)
	mockObjRepo.EXPECT().Delete(ctx, objID, mock.Anything).Return(nil, nil)

	err := svc.DeleteObject(ctx, subjectID, objID)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "delete", capturedRequest.Action, "action should be 'delete'")
	assert.Equal(t, "object:"+objID.String(), capturedRequest.Resource, "resource should be object:<id>")
}

func TestWorldService_MoveObject_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockObjRepo := worldtest.NewMockObjectRepository(t)

	svc := world.NewService(world.ServiceConfig{
		ObjectRepo: mockObjRepo,
		Engine:     mockEngine,
	})

	existingObj, objErr := world.NewObjectWithID(objID, "Sword", world.InLocation(fromLocID))
	require.NoError(t, objErr)

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
	mockObjRepo.EXPECT().Move(ctx, objID, world.InLocation(toLocID), mock.Anything).Return(nil, nil)

	err := svc.MoveObject(ctx, subjectID, objID, world.InLocation(toLocID))
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, "object:"+objID.String(), capturedRequest.Resource, "resource should be object:<id>")
}

// --- Scene VerifiesAccessRequest tests ---

func TestWorldService_ListSceneParticipants_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	sceneID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockSceneRepository(t)

	svc := world.NewService(world.ServiceConfig{
		SceneRepo: mockRepo,
		Engine:    mockEngine,
	})

	expectedParticipants := []world.SceneParticipant{{CharacterID: ulid.Make(), Role: world.RoleMember}}

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().ListParticipants(ctx, sceneID).Return(expectedParticipants, nil)

	_, err := svc.ListSceneParticipants(ctx, subjectID, sceneID)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "read", capturedRequest.Action, "action should be 'read'")
	assert.Equal(t, "scene:"+sceneID.String(), capturedRequest.Resource, "resource should be scene:<id>")
}

// --- Location VerifiesAccessRequest tests (additional) ---

func TestWorldService_UpdateLocation_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockLocationRepository(t)

	svc := world.NewService(withWriteExecutor(world.ServiceConfig{
		LocationRepo: mockRepo,
		Engine:       mockEngine,
	}, &mockOutboxWriter{}))

	loc := &world.Location{
		ID:          ulid.Make(),
		Name:        "Updated Room",
		Description: "A modified room",
		Type:        world.LocationTypePersistent,
	}

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Update(ctx, loc).Return(nil, nil)

	err := svc.UpdateLocation(ctx, subjectID, loc)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "write", capturedRequest.Action, "action should be 'write'")
	assert.Equal(t, "location:"+loc.ID.String(), capturedRequest.Resource, "resource should be location:<id>")
}

func TestWorldService_DeleteLocation_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockLocRepo := worldtest.NewMockLocationRepository(t)
	mockPropRepo := worldtest.NewMockPropertyRepository(t)
	mockTransactor := &mockTransactor{}

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockLocRepo,
		PropertyRepo: mockPropRepo,
		Engine:       mockEngine,
		Transactor:   mockTransactor,
		OutboxWriter: &mockOutboxWriter{},
	})

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockPropRepo.EXPECT().DeleteByParent(ctx, "location", locID).Return(nil)
	mockLocRepo.EXPECT().Delete(ctx, locID, mock.Anything).Return(nil, nil)

	err := svc.DeleteLocation(ctx, subjectID, locID)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "delete", capturedRequest.Action, "action should be 'delete'")
	assert.Equal(t, "location:"+locID.String(), capturedRequest.Resource, "resource should be location:<id>")
}

func TestWorldService_FindLocationByName_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(charID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockLocationRepository(t)

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockRepo,
		Engine:       mockEngine,
	})

	expectedLoc := &world.Location{ID: ulid.Make(), Name: "Town Square"}

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().FindByName(ctx, "Town Square").Return(expectedLoc, nil)

	_, err := svc.FindLocationByName(ctx, subjectID, "Town Square")
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "read", capturedRequest.Action, "action should be 'read'")
	assert.Equal(t, "location:*", capturedRequest.Resource, "resource should be location:*")
}

// --- Character VerifiesAccessRequest tests (additional) ---

func TestWorldService_GetCharacter_VerifiesAccessRequest(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	callerID := ulid.Make()
	subjectID := access.CharacterSubject(callerID.String())

	mockEngine := policytest.NewMockAccessPolicyEngine(t)
	mockRepo := worldtest.NewMockCharacterRepository(t)

	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: mockRepo,
		Engine:        mockEngine,
	})

	expectedChar := &world.Character{ID: charID, Name: "Test Character"}

	var capturedRequest types.AccessRequest
	mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
		capturedRequest = req
		return true
	})).Return(types.NewDecision(types.EffectAllow, "test", ""), nil)

	mockRepo.EXPECT().Get(ctx, charID).Return(expectedChar, nil)

	_, err := svc.GetCharacter(ctx, subjectID, charID)
	require.NoError(t, err)

	assert.Equal(t, subjectID, capturedRequest.Subject, "subject should be character:<id>")
	assert.Equal(t, "read", capturedRequest.Action, "action should be 'read'")
	assert.Equal(t, "character:"+charID.String(), capturedRequest.Resource, "resource should be character:<id>")
}

// TestWorldService_ErrorCodePropagation verifies that error codes propagate correctly
// through actual service methods when access checks fail. This tests end-to-end that:
//   - Engine errors (ErrAccessEvaluationFailed) result in *_ACCESS_EVALUATION_FAILED codes
//   - Policy denials (ErrPermissionDenied) result in *_ACCESS_DENIED codes
func TestWorldService_ErrorCodePropagation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())

	tests := []struct {
		name              string
		setupService      func() (*world.Service, ulid.ULID)
		invokeMethod      func(*world.Service, ulid.ULID) error
		engineBehavior    string // "error" or "deny"
		expectedErrorCode string
		expectedSentinel  error
	}{
		// Location operations
		{
			name: "GetLocation - engine error produces LOCATION_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				locID := ulid.Make()
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockRepo := worldtest.NewMockLocationRepository(t)
				svc := world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
				return svc, locID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				_, err := svc.GetLocation(ctx, subjectID, id)
				return err
			},
			engineBehavior:    "error",
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "GetLocation - policy deny produces LOCATION_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				locID := ulid.Make()
				engine := policytest.DenyAllEngine()
				mockRepo := worldtest.NewMockLocationRepository(t)
				svc := world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
				return svc, locID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				_, err := svc.GetLocation(ctx, subjectID, id)
				return err
			},
			engineBehavior:    "deny",
			expectedErrorCode: "LOCATION_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},
		{
			name: "CreateLocation - engine error produces LOCATION_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockRepo := worldtest.NewMockLocationRepository(t)
				svc := world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
				return svc, ulid.ULID{} // ID not used for create
			},
			invokeMethod: func(svc *world.Service, _ ulid.ULID) error {
				loc := &world.Location{
					Name:        "Test Room",
					Description: "A test",
					Type:        world.LocationTypePersistent,
				}
				return svc.CreateLocation(ctx, subjectID, loc)
			},
			engineBehavior:    "error",
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "CreateLocation - policy deny produces LOCATION_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				engine := policytest.DenyAllEngine()
				mockRepo := worldtest.NewMockLocationRepository(t)
				svc := world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
				return svc, ulid.ULID{}
			},
			invokeMethod: func(svc *world.Service, _ ulid.ULID) error {
				loc := &world.Location{
					Name:        "Test Room",
					Description: "A test",
					Type:        world.LocationTypePersistent,
				}
				return svc.CreateLocation(ctx, subjectID, loc)
			},
			engineBehavior:    "deny",
			expectedErrorCode: "LOCATION_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},
		{
			name: "UpdateLocation - engine error produces LOCATION_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				locID := ulid.Make()
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockRepo := worldtest.NewMockLocationRepository(t)
				svc := world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
				return svc, locID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				loc := &world.Location{
					ID:          id,
					Name:        "Updated Room",
					Description: "Updated",
					Type:        world.LocationTypePersistent,
				}
				return svc.UpdateLocation(ctx, subjectID, loc)
			},
			engineBehavior:    "error",
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "UpdateLocation - policy deny produces LOCATION_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				locID := ulid.Make()
				engine := policytest.DenyAllEngine()
				mockRepo := worldtest.NewMockLocationRepository(t)
				svc := world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
				return svc, locID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				loc := &world.Location{
					ID:          id,
					Name:        "Updated Room",
					Description: "Updated",
					Type:        world.LocationTypePersistent,
				}
				return svc.UpdateLocation(ctx, subjectID, loc)
			},
			engineBehavior:    "deny",
			expectedErrorCode: "LOCATION_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},
		{
			name: "DeleteLocation - engine error produces LOCATION_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				locID := ulid.Make()
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockLocRepo := worldtest.NewMockLocationRepository(t)
				mockPropRepo := worldtest.NewMockPropertyRepository(t)
				transactor := &mockTransactor{}
				svc := world.NewService(world.ServiceConfig{
					LocationRepo: mockLocRepo,
					PropertyRepo: mockPropRepo,
					Transactor:   transactor,
					Engine:       engine,
				})
				return svc, locID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				return svc.DeleteLocation(ctx, subjectID, id)
			},
			engineBehavior:    "error",
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "DeleteLocation - policy deny produces LOCATION_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				locID := ulid.Make()
				engine := policytest.DenyAllEngine()
				mockLocRepo := worldtest.NewMockLocationRepository(t)
				mockPropRepo := worldtest.NewMockPropertyRepository(t)
				transactor := &mockTransactor{}
				svc := world.NewService(world.ServiceConfig{
					LocationRepo: mockLocRepo,
					PropertyRepo: mockPropRepo,
					Transactor:   transactor,
					Engine:       engine,
				})
				return svc, locID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				return svc.DeleteLocation(ctx, subjectID, id)
			},
			engineBehavior:    "deny",
			expectedErrorCode: "LOCATION_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},

		// Exit operations
		{
			name: "GetExit - engine error produces EXIT_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				exitID := ulid.Make()
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockRepo := worldtest.NewMockExitRepository(t)
				svc := world.NewService(world.ServiceConfig{
					ExitRepo: mockRepo,
					Engine:   engine,
				})
				return svc, exitID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				_, err := svc.GetExit(ctx, subjectID, id)
				return err
			},
			engineBehavior:    "error",
			expectedErrorCode: "EXIT_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "GetExit - policy deny produces EXIT_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				exitID := ulid.Make()
				engine := policytest.DenyAllEngine()
				mockRepo := worldtest.NewMockExitRepository(t)
				svc := world.NewService(world.ServiceConfig{
					ExitRepo: mockRepo,
					Engine:   engine,
				})
				return svc, exitID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				_, err := svc.GetExit(ctx, subjectID, id)
				return err
			},
			engineBehavior:    "deny",
			expectedErrorCode: "EXIT_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},

		// Object operations
		{
			name: "GetObject - engine error produces OBJECT_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				objID := ulid.Make()
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockRepo := worldtest.NewMockObjectRepository(t)
				svc := world.NewService(world.ServiceConfig{
					ObjectRepo: mockRepo,
					Engine:     engine,
				})
				return svc, objID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				_, err := svc.GetObject(ctx, subjectID, id)
				return err
			},
			engineBehavior:    "error",
			expectedErrorCode: "OBJECT_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "GetObject - policy deny produces OBJECT_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				objID := ulid.Make()
				engine := policytest.DenyAllEngine()
				mockRepo := worldtest.NewMockObjectRepository(t)
				svc := world.NewService(world.ServiceConfig{
					ObjectRepo: mockRepo,
					Engine:     engine,
				})
				return svc, objID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				_, err := svc.GetObject(ctx, subjectID, id)
				return err
			},
			engineBehavior:    "deny",
			expectedErrorCode: "OBJECT_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},
		{
			name: "MoveObject - engine error produces OBJECT_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				objID := ulid.Make()
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockRepo := worldtest.NewMockObjectRepository(t)
				svc := world.NewService(world.ServiceConfig{
					ObjectRepo: mockRepo,
					Engine:     engine,
				})
				return svc, objID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				locID := ulid.Make()
				to := world.InLocation(locID)
				return svc.MoveObject(ctx, subjectID, id, to)
			},
			engineBehavior:    "error",
			expectedErrorCode: "OBJECT_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "MoveObject - policy deny produces OBJECT_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				objID := ulid.Make()
				engine := policytest.DenyAllEngine()
				mockRepo := worldtest.NewMockObjectRepository(t)
				svc := world.NewService(world.ServiceConfig{
					ObjectRepo: mockRepo,
					Engine:     engine,
				})
				return svc, objID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				locID := ulid.Make()
				to := world.InLocation(locID)
				return svc.MoveObject(ctx, subjectID, id, to)
			},
			engineBehavior:    "deny",
			expectedErrorCode: "OBJECT_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},

		// Character operations
		{
			name: "GetCharacter - engine error produces CHARACTER_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				charID := ulid.Make()
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockRepo := worldtest.NewMockCharacterRepository(t)
				svc := world.NewService(world.ServiceConfig{
					CharacterRepo: mockRepo,
					Engine:        engine,
				})
				return svc, charID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				_, err := svc.GetCharacter(ctx, subjectID, id)
				return err
			},
			engineBehavior:    "error",
			expectedErrorCode: "CHARACTER_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "GetCharacter - policy deny produces CHARACTER_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				charID := ulid.Make()
				engine := policytest.DenyAllEngine()
				mockRepo := worldtest.NewMockCharacterRepository(t)
				svc := world.NewService(world.ServiceConfig{
					CharacterRepo: mockRepo,
					Engine:        engine,
				})
				return svc, charID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				_, err := svc.GetCharacter(ctx, subjectID, id)
				return err
			},
			engineBehavior:    "deny",
			expectedErrorCode: "CHARACTER_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},
		{
			name: "MoveCharacter - engine error produces CHARACTER_ACCESS_EVALUATION_FAILED",
			setupService: func() (*world.Service, ulid.ULID) {
				charID := ulid.Make()
				engine := policytest.NewErrorEngine(errors.New("policy store unavailable"))
				mockRepo := worldtest.NewMockCharacterRepository(t)
				svc := world.NewService(world.ServiceConfig{
					CharacterRepo: mockRepo,
					Engine:        engine,
				})
				return svc, charID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				locID := ulid.Make()
				return svc.MoveCharacter(ctx, subjectID, id, locID)
			},
			engineBehavior:    "error",
			expectedErrorCode: "CHARACTER_ACCESS_EVALUATION_FAILED",
			expectedSentinel:  world.ErrAccessEvaluationFailed,
		},
		{
			name: "MoveCharacter - policy deny produces CHARACTER_ACCESS_DENIED",
			setupService: func() (*world.Service, ulid.ULID) {
				charID := ulid.Make()
				engine := policytest.DenyAllEngine()
				mockRepo := worldtest.NewMockCharacterRepository(t)
				svc := world.NewService(world.ServiceConfig{
					CharacterRepo: mockRepo,
					Engine:        engine,
				})
				return svc, charID
			},
			invokeMethod: func(svc *world.Service, id ulid.ULID) error {
				locID := ulid.Make()
				return svc.MoveCharacter(ctx, subjectID, id, locID)
			},
			engineBehavior:    "deny",
			expectedErrorCode: "CHARACTER_ACCESS_DENIED",
			expectedSentinel:  world.ErrPermissionDenied,
		},

		// Scene operations
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, id := tt.setupService()
			err := tt.invokeMethod(svc, id)

			require.Error(t, err, "operation should fail with access error")

			// Verify error wraps expected sentinel
			assert.ErrorIs(t, err, tt.expectedSentinel,
				"error should wrap %v", tt.expectedSentinel)

			// Verify correct error code
			errutil.AssertErrorCode(t, err, tt.expectedErrorCode)

			// Ensure the other sentinel is NOT present
			if tt.engineBehavior == "error" {
				assert.False(t, errors.Is(err, world.ErrPermissionDenied),
					"engine error must not be reported as permission denied")
			} else {
				assert.False(t, errors.Is(err, world.ErrAccessEvaluationFailed),
					"policy denial must not be reported as evaluation error")
			}
		})
	}
}

// TestWorldService_MalformedAccessParams verifies fail-closed behavior when
// NewAccessRequest construction fails due to empty/invalid parameters.
// This ensures operations fail safely rather than bypassing authorization.
func TestWorldService_MalformedAccessParams(t *testing.T) {
	ctx := context.Background()
	validSubjectID := access.CharacterSubject(ulid.Make().String())
	locID := ulid.Make()
	charID := ulid.Make()
	objID := ulid.Make()

	tests := []struct {
		name              string
		setupService      func() *world.Service
		invokeOperation   func(*world.Service) error
		expectedErrorCode string
	}{
		{
			name: "GetLocation with empty subject fails closed",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockRepo := worldtest.NewMockLocationRepository(t)
				return world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				_, err := svc.GetLocation(ctx, "", locID) // Empty subject
				return err
			},
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
		},
		{
			name: "CreateLocation with empty subject fails closed",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockRepo := worldtest.NewMockLocationRepository(t)
				return world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				loc := &world.Location{
					ID:          ulid.Make(),
					Name:        "Test Location",
					Description: "A test location",
				}
				return svc.CreateLocation(ctx, "", loc) // Empty subject
			},
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
		},
		{
			name: "UpdateLocation with empty subject fails closed",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockRepo := worldtest.NewMockLocationRepository(t)
				return world.NewService(world.ServiceConfig{
					LocationRepo: mockRepo,
					Engine:       engine,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				loc := &world.Location{
					ID:          locID,
					Name:        "Updated Location",
					Description: "An updated location",
				}
				return svc.UpdateLocation(ctx, "", loc) // Empty subject
			},
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
		},
		{
			name: "CreateExit with empty subject fails closed",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockRepo := worldtest.NewMockExitRepository(t)
				return world.NewService(world.ServiceConfig{
					ExitRepo: mockRepo,
					Engine:   engine,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				exit := &world.Exit{
					ID:             ulid.Make(),
					Name:           "north",
					FromLocationID: locID,
					ToLocationID:   ulid.Make(),
					Visibility:     world.VisibilityAll,
				}
				return svc.CreateExit(ctx, "", exit) // Empty subject
			},
			expectedErrorCode: "EXIT_ACCESS_EVALUATION_FAILED",
		},
		{
			name: "GetObject with empty subject fails closed",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockRepo := worldtest.NewMockObjectRepository(t)
				return world.NewService(world.ServiceConfig{
					ObjectRepo: mockRepo,
					Engine:     engine,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				_, err := svc.GetObject(ctx, "", objID) // Empty subject
				return err
			},
			expectedErrorCode: "OBJECT_ACCESS_EVALUATION_FAILED",
		},
		{
			name: "MoveCharacter with empty subject fails closed",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockCharRepo := worldtest.NewMockCharacterRepository(t)
				mockLocRepo := worldtest.NewMockLocationRepository(t)
				return world.NewService(world.ServiceConfig{
					CharacterRepo: mockCharRepo,
					LocationRepo:  mockLocRepo,
					Engine:        engine,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				return svc.MoveCharacter(ctx, "", charID, locID) // Empty subject
			},
			expectedErrorCode: "CHARACTER_ACCESS_EVALUATION_FAILED",
		},
		{
			name: "GetCharacter with empty subject fails closed",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockRepo := worldtest.NewMockCharacterRepository(t)
				return world.NewService(world.ServiceConfig{
					CharacterRepo: mockRepo,
					Engine:        engine,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				_, err := svc.GetCharacter(ctx, "", charID) // Empty subject
				return err
			},
			expectedErrorCode: "CHARACTER_ACCESS_EVALUATION_FAILED",
		},
		{
			name: "DeleteLocation with empty subject fails closed",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockLocRepo := worldtest.NewMockLocationRepository(t)
				mockPropRepo := worldtest.NewMockPropertyRepository(t)
				mockTransactor := &mockTransactor{}
				return world.NewService(world.ServiceConfig{
					LocationRepo: mockLocRepo,
					PropertyRepo: mockPropRepo,
					Engine:       engine,
					Transactor:   mockTransactor,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				return svc.DeleteLocation(ctx, "", locID) // Empty subject
			},
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
		},
		{
			name: "GetCharactersByLocation - valid subject but tests access check boundary",
			setupService: func() *world.Service {
				engine := policytest.NewGrantEngine()
				mockRepo := worldtest.NewMockCharacterRepository(t)
				// Grant access so we can verify the operation would work with valid params
				engine.Grant(validSubjectID, "list_characters", access.LocationResource(locID.String()))
				return world.NewService(world.ServiceConfig{
					CharacterRepo: mockRepo,
					Engine:        engine,
				})
			},
			invokeOperation: func(svc *world.Service) error {
				// Test with empty subject to trigger NewAccessRequest failure
				_, err := svc.GetCharactersByLocation(ctx, "", locID, world.ListOptions{})
				return err
			},
			expectedErrorCode: "LOCATION_ACCESS_EVALUATION_FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.setupService()
			err := tt.invokeOperation(svc)

			// Verify the operation failed
			require.Error(t, err, "operation with malformed access params should fail")

			// Verify it's classified as an evaluation failure (fail-closed)
			assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed,
				"malformed access params should return ErrAccessEvaluationFailed")

			// Verify it's NOT a permission denial (which implies auth check succeeded)
			assert.False(t, errors.Is(err, world.ErrPermissionDenied),
				"malformed params should not be reported as permission denial")

			// Verify the correct error code
			errutil.AssertErrorCode(t, err, tt.expectedErrorCode)
		})
	}
}

// TestWorldService_GetLocation_AllowWithInfraPrefix verifies that checkAccess
// honors allow decisions even if the policyID has an "infra:" prefix. Since
// IsInfraFailure() is only checked inside the !IsAllowed() branch, an allow
// decision with infra: prefix must succeed. This test documents that invariant.
func TestWorldService_GetLocation_AllowWithInfraPrefix(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	// Engine returns EffectAllow with an infra: prefix policyID.
	// In production this shouldn't happen, but we verify the code
	// handles it safely (allow takes precedence).
	mockEngine := &policytest.MockAccessPolicyEngine{}
	mockEngine.On("Evaluate", mock.Anything, mock.Anything).
		Return(types.NewDecision(types.EffectAllow, "allowed", "infra:should-not-happen"), nil)

	mockRepo := worldtest.NewMockLocationRepository(t)
	expectedLoc := &world.Location{ID: locID, Name: "Test Room"}
	mockRepo.EXPECT().Get(ctx, locID).Return(expectedLoc, nil)

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: mockRepo,
		Engine:       mockEngine,
	})

	loc, err := svc.GetLocation(ctx, subjectID, locID)
	require.NoError(t, err, "allow decision must succeed even with infra: prefix policyID")
	assert.Equal(t, expectedLoc, loc)
}

func TestService_ListPropertiesByParent(t *testing.T) {
	parentType := "character"
	parentID := ulid.Make()
	p1ID, p2ID, p3ID := ulid.Make(), ulid.Make(), ulid.Make()
	p1 := &world.EntityProperty{ID: p1ID, ParentType: parentType, ParentID: parentID, Name: "name1", Visibility: "public"}
	p2 := &world.EntityProperty{ID: p2ID, ParentType: parentType, ParentID: parentID, Name: "name2", Visibility: "private"}
	p3 := &world.EntityProperty{ID: p3ID, ParentType: parentType, ParentID: parentID, Name: "name3", Visibility: "public"}

	tests := []struct {
		name           string
		repoProps      []*world.EntityProperty
		repoErr        error
		engineDecide   func(resourceID string) (types.Decision, error)
		expectIDs      []ulid.ULID
		expectErr      bool
		expectErrCode  string
		expectErrIsAny []error
	}{
		{
			name:         "empty parent → empty list, nil error",
			repoProps:    nil,
			engineDecide: alwaysAllow,
			expectIDs:    nil,
		},
		{
			name:         "all-permit → full list, nil error",
			repoProps:    []*world.EntityProperty{p1, p2, p3},
			engineDecide: alwaysAllow,
			expectIDs:    []ulid.ULID{p1ID, p2ID, p3ID},
		},
		{
			name:         "all-deny → empty list, nil error",
			repoProps:    []*world.EntityProperty{p1, p2, p3},
			engineDecide: alwaysDeny,
			expectIDs:    nil,
		},
		{
			name:      "mixed permit/deny → filtered subset, nil error",
			repoProps: []*world.EntityProperty{p1, p2, p3},
			engineDecide: func(rid string) (types.Decision, error) {
				if strings.Contains(rid, p2ID.String()) {
					return types.NewDecision(types.EffectDefaultDeny, "private", "seed:test"), nil
				}
				return types.NewDecision(types.EffectAllow, "permit", "seed:test"), nil
			},
			expectIDs: []ulid.ULID{p1ID, p3ID},
		},
		{
			name:          "repo error → wrapped PROPERTY_QUERY_FAILED",
			repoProps:     nil,
			repoErr:       errors.New("db down"),
			engineDecide:  alwaysAllow,
			expectErr:     true,
			expectErrCode: "PROPERTY_QUERY_FAILED",
		},
		{
			name:      "engine returns Evaluate error on one prop → wrapped ErrAccessEvaluationFailed",
			repoProps: []*world.EntityProperty{p1, p2, p3},
			engineDecide: func(rid string) (types.Decision, error) {
				if strings.Contains(rid, p2ID.String()) {
					return types.Decision{}, errors.New("engine boom")
				}
				return types.NewDecision(types.EffectAllow, "permit", "seed:test"), nil
			},
			expectErr:      true,
			expectErrCode:  "PROPERTY_ACCESS_EVALUATION_FAILED",
			expectErrIsAny: []error{world.ErrAccessEvaluationFailed},
		},
		{
			// IsInfraFailure() is detected via PolicyID prefix "infra:"
			// (see internal/access/policy/types/types.go). Construct an
			// infra-failure Decision by passing an "infra:" PolicyID with
			// EffectDefaultDeny — checkAccess in service.go then takes
			// the IsInfraFailure() branch.
			name:      "engine returns InfraFailure decision on one prop → wrapped ErrAccessEvaluationFailed",
			repoProps: []*world.EntityProperty{p1, p2, p3},
			engineDecide: func(rid string) (types.Decision, error) {
				if strings.Contains(rid, p2ID.String()) {
					return types.NewDecision(types.EffectDefaultDeny, "resolver down", "infra:session"), nil
				}
				return types.NewDecision(types.EffectAllow, "permit", "seed:test"), nil
			},
			expectErr:      true,
			expectErrCode:  "PROPERTY_ACCESS_EVALUATION_FAILED",
			expectErrIsAny: []error{world.ErrAccessEvaluationFailed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProp := worldtest.NewMockPropertyRepository(t)
			mockEng := policytest.NewMockAccessPolicyEngine(t)
			mockProp.EXPECT().ListByParent(mock.Anything, parentType, parentID).Return(tt.repoProps, tt.repoErr).Once()

			// Observed resource strings — used to pin INV-1 (per-property
			// shape, not parent-shaped). Collected per-call inside the
			// engineDecide closure.
			var observedResources []string
			if tt.repoErr == nil {
				for range tt.repoProps {
					// .Maybe() allows the expectation to be UNCONSUMED:
					// abort cases (engine-error / infra-failure) short-
					// circuit the loop after the failing property, so
					// the remaining expectations would otherwise fail
					// AssertExpectations in cleanup.
					mockEng.EXPECT().Evaluate(mock.Anything, mock.Anything).
						RunAndReturn(func(_ context.Context, req types.AccessRequest) (types.Decision, error) {
							observedResources = append(observedResources, req.Resource)
							return tt.engineDecide(req.Resource)
						}).Maybe()
				}
			}
			svc := world.NewService(world.ServiceConfig{
				PropertyRepo: mockProp, Engine: mockEng,
			})

			got, err := svc.ListPropertiesByParent(context.Background(), "character:"+ulid.Make().String(), parentType, parentID)

			// INV-1 pin: every resource passed to engine.Evaluate MUST
			// be exactly "property:<ulid>" — not a parent-shaped composite
			// like "property:character:<ulid>".
			for _, rid := range observedResources {
				assert.Regexp(t, `^property:[0-9A-Z]{26}$`, rid,
					"INV-1: per-property resource shape MUST be property:<ulid>, not parent-shaped")
			}

			if tt.expectErr {
				require.Error(t, err)
				if tt.expectErrCode != "" {
					errutil.AssertErrorCode(t, err, tt.expectErrCode)
				}
				for _, target := range tt.expectErrIsAny {
					assert.ErrorIs(t, err, target)
				}
				return
			}
			require.NoError(t, err)
			var gotIDs []ulid.ULID
			for _, p := range got {
				gotIDs = append(gotIDs, p.ID)
			}
			assert.Equal(t, tt.expectIDs, gotIDs)
		})
	}
}

func alwaysAllow(_ string) (types.Decision, error) {
	return types.NewDecision(types.EffectAllow, "permit", "seed:test"), nil
}

func alwaysDeny(_ string) (types.Decision, error) {
	return types.NewDecision(types.EffectDefaultDeny, "default deny", "seed:test"), nil
}

// TestWorldService_UpdateCharacterPreferences covers the folded-in
// character-settings write (round-4 C5 / D-05): the preferences mutation routes
// through the same-tx outbox seam, is version-guarded (MODEL-03), emits exactly
// one character_preferences_update envelope, and surfaces the typed conflict.
func TestWorldService_UpdateCharacterPreferences(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	prefs := []byte(`{"host":"eyJrIjoidiJ9"}`)

	newSvc := func(t *testing.T) (*world.Service, *worldtest.MockCharacterRepository, *mockOutboxWriter, *mockTransactor) {
		t.Helper()
		mockRepo := worldtest.NewMockCharacterRepository(t)
		outbox := &mockOutboxWriter{}
		tx := &mockTransactor{}
		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        policytest.AllowAllEngine(),
			Transactor:    tx,
			OutboxWriter:  outbox,
			GameID:        "main",
		})
		return svc, mockRepo, outbox, tx
	}

	t.Run("version-guarded write emits exactly one character_preferences_update envelope", func(t *testing.T) {
		svc, mockRepo, outbox, tx := newSvc(t)
		mockRepo.EXPECT().Get(ctx, charID).Return(&world.Character{ID: charID, Version: 3}, nil)
		delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{Type: wmodel.AggregateCharacter, ID: charID, BeforeVersion: 3, AfterVersion: 4}}
		mockRepo.EXPECT().UpdatePreferences(mock.Anything, charID, prefs, 3).Return(delta, nil)

		err := svc.UpdateCharacterPreferences(ctx, charID, prefs)
		require.NoError(t, err)
		assert.True(t, tx.called, "the write runs inside a transaction")
		require.Equal(t, 1, outbox.calls, "exactly one envelope is written in the same tx")
		assert.Equal(t, "character_preferences_update", outbox.lastIntent.Kind)
		assert.Equal(t, wmodel.AggregateCharacter, outbox.lastIntent.AggregateType)
		assert.Equal(t, charID, outbox.lastIntent.AggregateID)
	})

	t.Run("concurrent conflict surfaces WORLD_CONCURRENT_EDIT (no auto-retry)", func(t *testing.T) {
		svc, mockRepo, outbox, _ := newSvc(t)
		mockRepo.EXPECT().Get(ctx, charID).Return(&world.Character{ID: charID, Version: 2}, nil)
		conflict := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
		mockRepo.EXPECT().UpdatePreferences(mock.Anything, charID, prefs, 2).Return(nil, conflict)

		err := svc.UpdateCharacterPreferences(ctx, charID, prefs)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
		assert.Equal(t, 0, outbox.calls, "a conflicting write emits no envelope")
	})

	t.Run("absent character surfaces CHARACTER_NOT_FOUND", func(t *testing.T) {
		svc, mockRepo, _, _ := newSvc(t)
		mockRepo.EXPECT().Get(ctx, charID).Return(nil, world.ErrNotFound)

		err := svc.UpdateCharacterPreferences(ctx, charID, prefs)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("missing write executor is a configuration error", func(t *testing.T) {
		mockRepo := worldtest.NewMockCharacterRepository(t)
		svc := world.NewService(world.ServiceConfig{
			CharacterRepo: mockRepo,
			Engine:        policytest.AllowAllEngine(),
			// no OutboxWriter / Transactor -> no mutator
		})
		err := svc.UpdateCharacterPreferences(ctx, charID, prefs)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_PREFERENCES_UPDATE_FAILED")
	})
}
