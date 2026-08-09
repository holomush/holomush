// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
	"github.com/holomush/holomush/pkg/errutil"
)

// retireFixture wires a Service over a mocked character repository and a
// passthrough write executor, granting the given ABAC action on the character.
// It returns the service, the repo mock and the outbox spy so each subtest can
// assert both the write it expected and the writes it did NOT expect.
func retireFixture(t *testing.T, subjectID, action string, charID ulid.ULID) (*world.Service, *worldtest.MockCharacterRepository, *mockOutboxWriter) {
	t.Helper()
	engine := policytest.NewGrantEngine()
	mockRepo := worldtest.NewMockCharacterRepository(t)
	outbox := &mockOutboxWriter{}
	svc := world.NewService(withWriteExecutor(world.ServiceConfig{
		CharacterRepo: mockRepo,
		Engine:        engine,
	}, outbox))
	engine.Grant(subjectID, action, access.CharacterResource(charID.String()))
	return svc, mockRepo, outbox
}

func TestWorldServiceRetireCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("retires an active character and emits exactly one character_retired envelope", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		// The CAS carries the CALLER's expected version, never a freshly-read one:
		// passing char.Version would make the guard vacuous for caller staleness.
		mockRepo.EXPECT().SetStatus(mock.Anything, charID, world.StatusRetired, 5).Return(nil, nil)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
		require.NoError(t, err)
		require.Equal(t, 1, outbox.calls, "a retire emits exactly one envelope")
		assert.Equal(t, "character_retired", outbox.lastIntent.Kind)
		assert.Equal(t, charID, outbox.lastIntent.AggregateID)
	})

	t.Run("rejects a zero expected version with CHARACTER_VERSION_REQUIRED before any read", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 0)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_VERSION_REQUIRED")
		mockRepo.AssertNotCalled(t, "Get")
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls, "a rejected command writes nothing and emits nothing")
	})

	t.Run("rejects a negative expected version with CHARACTER_VERSION_REQUIRED before any read", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, -1)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_VERSION_REQUIRED")
		mockRepo.AssertNotCalled(t, "Get")
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
	})

	t.Run("surfaces WORLD_CONCURRENT_EDIT from the version precheck without invoking the executor", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 7, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 3)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
	})

	t.Run("returns CHARACTER_NOT_FOUND for a nonexistent character", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		mockRepo.EXPECT().Get(ctx, charID).
			Return(nil, oops.Code("CHARACTER_NOT_FOUND").Wrap(world.ErrNotFound))

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
		assert.Zero(t, outbox.calls)
	})

	t.Run("maps an executor concurrent edit to WORLD_CONCURRENT_EDIT", func(t *testing.T) {
		svc, mockRepo, _ := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		conflict := oops.Code(world.CodeConcurrentEdit).Wrap(world.ErrConcurrentEdit)
		mockRepo.EXPECT().SetStatus(mock.Anything, charID, world.StatusRetired, 5).Return(nil, conflict)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
	})

	t.Run("maps an executor not-found to CHARACTER_NOT_FOUND", func(t *testing.T) {
		svc, mockRepo, _ := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().SetStatus(mock.Anything, charID, world.StatusRetired, 5).
			Return(nil, oops.Code("CHARACTER_NOT_FOUND").Wrap(world.ErrNotFound))

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("denies a caller without the retire action", func(t *testing.T) {
		// The grant is on "write", NOT on "retire" (D-40: distinct actions), so
		// the command must be denied — reusing write to retire is exactly the
		// action-granularity collapse D-40 rules out.
		svc, mockRepo, outbox := retireFixture(t, subjectID, "write", charID)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
		mockRepo.AssertNotCalled(t, "Get")
		assert.Zero(t, outbox.calls)
	})
}
