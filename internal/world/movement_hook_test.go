// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

// movementHookFn adapts a plain function to the world.MovementHook interface.
type movementHookFn func(ctx context.Context, charID, locID ulid.ULID, ts time.Time) error

func (f movementHookFn) OnCharacterMoved(ctx context.Context, charID, locID ulid.ULID, ts time.Time) error {
	return f(ctx, charID, locID, ts)
}

// testMoveHookFixture holds all test dependencies for MovementHook tests.
type testMoveHookFixture struct {
	svc      *world.Service
	engine   *policytest.GrantEngine
	charRepo *worldtest.MockCharacterRepository
	locRepo  *worldtest.MockLocationRepository
	outbox   *mockOutboxWriter
}

// newTestServiceWithHook builds a world.Service with mock repos, a GrantEngine,
// the write executor (transactor + outbox), and installs the given MovementHook.
// The movement hook fires AFTER the same-tx move+envelope commit (05-06).
func newTestServiceWithHook(t *testing.T, hook world.MovementHook) testMoveHookFixture {
	t.Helper()
	engine := policytest.NewGrantEngine()
	charRepo := worldtest.NewMockCharacterRepository(t)
	locRepo := worldtest.NewMockLocationRepository(t)
	outbox := &mockOutboxWriter{}
	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: charRepo,
		LocationRepo:  locRepo,
		Engine:        engine,
		Transactor:    &mockTransactor{},
		OutboxWriter:  outbox,
	})
	svc.SetMovementHook(hook)
	return testMoveHookFixture{svc: svc, engine: engine, charRepo: charRepo, locRepo: locRepo, outbox: outbox}
}

// seedCharacterAndTwoLocations sets up mock expectations for a complete MoveCharacter
// call: a character at a from-location and a destination toLoc. Returns the
// character and destination IDs. The caller must pass subjectID so access can be
// granted.
func seedCharacterAndTwoLocations(
	t *testing.T,
	fix testMoveHookFixture,
	subjectID string,
) (charID, toLocID ulid.ULID) {
	t.Helper()
	charID = ulid.Make()
	fromLocID := ulid.Make()
	toLocID = ulid.Make()

	fix.engine.Grant(subjectID, "write", "character:"+charID.String())

	existingChar := &world.Character{
		ID:         charID,
		Name:       "Hook Test Character",
		LocationID: &fromLocID,
	}

	fix.charRepo.EXPECT().Get(context.Background(), charID).Return(existingChar, nil)
	fix.locRepo.EXPECT().Get(context.Background(), toLocID).Return(&world.Location{ID: toLocID}, nil)
	fix.charRepo.EXPECT().UpdateLocation(context.Background(), charID, &toLocID, mock.Anything).Return(nil, nil)

	return charID, toLocID
}

func TestMoveCharacter_FiresMovementHook(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())

	var captured struct {
		charID, locID ulid.ULID
		ts            time.Time
	}
	var hookFired bool
	hook := movementHookFn(func(_ context.Context, charID, locID ulid.ULID, ts time.Time) error {
		hookFired = true
		captured.charID, captured.locID, captured.ts = charID, locID, ts
		return nil
	})

	fix := newTestServiceWithHook(t, hook)
	charID, toLocID := seedCharacterAndTwoLocations(t, fix, subjectID)

	require.NoError(t, fix.svc.MoveCharacter(ctx, subjectID, charID, toLocID))

	require.True(t, hookFired)
	assert.Equal(t, charID, captured.charID)
	assert.Equal(t, toLocID, captured.locID)
	assert.WithinDuration(t, time.Now(), captured.ts, 5*time.Second)
}

// TestMoveCharacter_HookFailureIsOperationalDegradation is the round-6 R6-6
// regression guard against re-introducing the fail-after-commit anti-pattern
// (05-06 round-5 finding 3). A movement hook that fails AFTER the state+envelope
// commit MUST NOT flip MoveCharacter to a command failure: the move is committed,
// the envelope is emitted, and the command result is success. The session-derived
// location may lag until re-sync, but the command itself does not report failure.
func TestMoveCharacter_HookFailureIsOperationalDegradation(t *testing.T) {
	ctx := context.Background()
	subjectID := access.CharacterSubject(ulid.Make().String())

	hook := movementHookFn(func(_ context.Context, _, _ ulid.ULID, _ time.Time) error {
		return errors.New("session store unavailable")
	})

	fix := newTestServiceWithHook(t, hook)
	charID, toLocID := seedCharacterAndTwoLocations(t, fix, subjectID)

	// A failing post-commit hook does NOT surface a command error.
	require.NoError(t, fix.svc.MoveCharacter(ctx, subjectID, charID, toLocID),
		"a post-commit movement-hook failure must not fail the command (no CHARACTER_MOVE_FAILED after commit)")

	// The move envelope WAS emitted in the committed transaction.
	require.Equal(t, 1, fix.outbox.calls, "the move envelope must be emitted despite the hook failure")
	assert.Equal(t, "character_moved", fix.outbox.lastIntent.Kind)
	assert.Equal(t, charID, fix.outbox.lastIntent.AggregateID)
}
