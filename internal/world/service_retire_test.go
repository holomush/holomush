// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
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

	t.Run("rejects an already-retired character with CHARACTER_ALREADY_RETIRED", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusRetired}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_ALREADY_RETIRED")
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls, "a refused transition writes nothing and emits nothing")
	})

	t.Run("retires an idle character because idle to retired is a legal exit", func(t *testing.T) {
		// D-43/INV-WORLD-5: v0.13 ships no transition INTO idle, but a row
		// constructed there must still be retirable — the missing transition is
		// the inbound one, not the outbound one.
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusIdle}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().SetStatus(mock.Anything, charID, world.StatusRetired, 5).Return(nil, nil)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
		require.NoError(t, err)
		assert.Equal(t, 1, outbox.calls)
		assert.Equal(t, "character_retired", outbox.lastIntent.Kind)
	})

	t.Run("prefers the version conflict over the already-retired guard on a stale caller", func(t *testing.T) {
		// R1 guard order: a stale caller racing a writer that already retired
		// the row must see the CONFLICT, never that writer's outcome.
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 6, Status: world.StatusRetired}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
	})

	t.Run("denies an unrecognized stored status through the exhaustive default arm", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.Status("bogus")}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

		err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_RETIRE_FAILED")
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
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

func TestWorldServiceUnretireCharacter(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())

	t.Run("unretires a retired character and emits exactly one character_unretired envelope", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 6, Status: world.StatusRetired}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().SetStatus(mock.Anything, charID, world.StatusActive, 6).Return(nil, nil)

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 6)
		require.NoError(t, err)
		require.Equal(t, 1, outbox.calls, "an unretire emits exactly one envelope")
		assert.Equal(t, "character_unretired", outbox.lastIntent.Kind)
		assert.Equal(t, charID, outbox.lastIntent.AggregateID)
	})

	t.Run("rejects a zero expected version with CHARACTER_VERSION_REQUIRED before any read", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 0)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_VERSION_REQUIRED")
		mockRepo.AssertNotCalled(t, "Get")
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
	})

	t.Run("rejects a negative expected version with CHARACTER_VERSION_REQUIRED before any read", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, -3)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_VERSION_REQUIRED")
		mockRepo.AssertNotCalled(t, "Get")
		assert.Zero(t, outbox.calls)
	})

	t.Run("prefers the version conflict over the not-retired guard on a stale caller", func(t *testing.T) {
		// R1 guard order: a stale unretire racing a COMPLETED unretire must see
		// WORLD_CONCURRENT_EDIT, never CHARACTER_NOT_RETIRED — the latter would
		// report the racing writer's outcome as if the stale view were current.
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 9, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 4)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
	})

	t.Run("rejects an active character with CHARACTER_NOT_RETIRED", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 4, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 4)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_RETIRED")
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
	})

	t.Run("rejects an idle character with CHARACTER_NOT_RETIRED", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 4, Status: world.StatusIdle}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 4)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_RETIRED")
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
	})

	t.Run("denies an unrecognized stored status through the exhaustive default arm", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 4, Status: world.Status("bogus")}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 4)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "CHARACTER_UNRETIRE_FAILED")
		mockRepo.AssertNotCalled(t, "SetStatus")
		assert.Zero(t, outbox.calls)
	})

	t.Run("returns CHARACTER_NOT_FOUND for a nonexistent character", func(t *testing.T) {
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		mockRepo.EXPECT().Get(ctx, charID).
			Return(nil, oops.Code("CHARACTER_NOT_FOUND").Wrap(world.ErrNotFound))

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
		assert.Zero(t, outbox.calls)
	})

	t.Run("denies a caller granted only the retire action", func(t *testing.T) {
		// D-40 splits retire from unretire precisely so a policy may grant one
		// without the other; a shared "write" (or "retire") grant must not carry.
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		err := svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 4)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrPermissionDenied)
		errutil.AssertErrorCode(t, err, "CHARACTER_ACCESS_DENIED")
		mockRepo.AssertNotCalled(t, "Get")
		assert.Zero(t, outbox.calls)
	})
}

// --- 06-05 Task 1: the widened lifecycle payload at the command layer ---

// decodeLifecyclePayload unmarshals one lifecycle envelope's payload so the
// assertion is made against the BYTES a consumer receives.
func decodeLifecyclePayload(t *testing.T, intent wmodel.EnvelopeIntent) world.CharacterLifecycleChangePayload {
	t.Helper()
	var got world.CharacterLifecycleChangePayload
	require.NoError(t, json.Unmarshal(intent.Payload, &got))
	return got
}

// TestWorldServiceLifecycleWithNoOptionsLeavesTheAdminContextEmpty is the
// player-path control for the variadic widening: RetireCharacter and
// UnretireCharacter called with NO options emit the same envelope they always
// did, plus a real before-status and an EMPTY section/action.
//
// It is what proves the widening touched no non-admin caller.
func TestWorldServiceLifecycleWithNoOptionsLeavesTheAdminContextEmpty(t *testing.T) {
	ctx := context.Background()

	t.Run("retire", func(t *testing.T) {
		charID := ulid.Make()
		subjectID := access.CharacterSubject(ulid.Make().String())
		svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().SetStatus(mock.Anything, charID, world.StatusRetired, 5).Return(nil, nil)

		require.NoError(t, svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5))
		require.Equal(t, 1, outbox.calls)

		got := decodeLifecyclePayload(t, outbox.lastIntent)
		assert.Equal(t, string(world.StatusActive), got.BeforeStatus)
		assert.Equal(t, string(world.StatusRetired), got.Status)
		assert.Empty(t, got.Section, "a player-initiated transition carries no admin section")
		assert.Empty(t, got.Action, "a player-initiated transition carries no admin action")
	})

	t.Run("unretire", func(t *testing.T) {
		charID := ulid.Make()
		subjectID := access.CharacterSubject(ulid.Make().String())
		svc, mockRepo, outbox := retireFixture(t, subjectID, "unretire", charID)

		stored := &world.Character{ID: charID, Name: "Alice", Version: 4, Status: world.StatusRetired}
		mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
		mockRepo.EXPECT().SetStatus(mock.Anything, charID, world.StatusActive, 4).Return(nil, nil)

		require.NoError(t, svc.UnretireCharacter(ctx, world.HumanCaller(subjectID), charID, 4))
		require.Equal(t, 1, outbox.calls)

		got := decodeLifecyclePayload(t, outbox.lastIntent)
		assert.Equal(t, string(world.StatusRetired), got.BeforeStatus)
		assert.Equal(t, string(world.StatusActive), got.Status)
		assert.Empty(t, got.Section)
		assert.Empty(t, got.Action)
	})
}

// TestWorldServiceLifecycleWithAuditContextCarriesItIntoThePayload pins the
// typed seam the admin handler supplies its evaluated section and action
// through: a trailing variadic option, so every existing caller compiles
// unchanged and Caller is not overloaded with an admin concern.
func TestWorldServiceLifecycleWithAuditContextCarriesItIntoThePayload(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.PlayerSubject(ulid.Make().String())
	svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

	stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusActive}
	mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)
	mockRepo.EXPECT().SetStatus(mock.Anything, charID, world.StatusRetired, 5).Return(nil, nil)

	require.NoError(t, svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5,
		world.WithAuditContext(world.AuditContext{Section: "characters", Action: "write"})))
	require.Equal(t, 1, outbox.calls)

	got := decodeLifecyclePayload(t, outbox.lastIntent)
	assert.Equal(t, "characters", got.Section)
	assert.Equal(t, "write", got.Action)
	assert.Equal(t, string(world.StatusActive), got.BeforeStatus)
	assert.Equal(t, string(world.StatusRetired), got.Status)
	assert.NotEqual(t, got.BeforeStatus, got.Status)
}

// TestWorldServiceRetireOfAnAlreadyRetiredCharacterEmitsNothing is the reason
// an emitted lifecycle payload can never carry equal before/after values: the
// lifecycle guard refuses the no-op transition BEFORE any payload is built.
func TestWorldServiceRetireOfAnAlreadyRetiredCharacterEmitsNothing(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	subjectID := access.CharacterSubject(ulid.Make().String())
	svc, mockRepo, outbox := retireFixture(t, subjectID, "retire", charID)

	stored := &world.Character{ID: charID, Name: "Alice", Version: 5, Status: world.StatusRetired}
	mockRepo.EXPECT().Get(ctx, charID).Return(stored, nil)

	err := svc.RetireCharacter(ctx, world.HumanCaller(subjectID), charID, 5)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_ALREADY_RETIRED")
	assert.Equal(t, 0, outbox.calls,
		"the guard refuses first, so before and after can never be equal in an EMITTED payload")
}
