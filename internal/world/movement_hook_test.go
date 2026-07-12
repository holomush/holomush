// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
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
}

// newTestServiceWithHook builds a world.Service with mock repos, a GrantEngine,
// and installs the given MovementHook.
func newTestServiceWithHook(t *testing.T, hook world.MovementHook) testMoveHookFixture {
	t.Helper()
	engine := policytest.NewGrantEngine()
	charRepo := worldtest.NewMockCharacterRepository(t)
	locRepo := worldtest.NewMockLocationRepository(t)
	emitter := &mockEventEmitter{}
	svc := world.NewService(world.ServiceConfig{
		CharacterRepo: charRepo,
		LocationRepo:  locRepo,
		Engine:        engine,
		EventEmitter:  emitter,
	})
	svc.SetMovementHook(hook)
	return testMoveHookFixture{svc: svc, engine: engine, charRepo: charRepo, locRepo: locRepo}
}

// seedCharacterAndTwoLocations sets up mock expectations for a complete MoveCharacter
// call: a character at fromLoc and a destination toLoc. Returns the IDs.
// The caller must pass subjectID so access can be granted.
func seedCharacterAndTwoLocations(
	t *testing.T,
	fix testMoveHookFixture,
	subjectID string,
) (charID, fromLocID, toLocID ulid.ULID) {
	t.Helper()
	charID = ulid.Make()
	fromLocID = ulid.Make()
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

	return charID, fromLocID, toLocID
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
	charID, _, toLocID := seedCharacterAndTwoLocations(t, fix, subjectID)

	require.NoError(t, fix.svc.MoveCharacter(ctx, subjectID, charID, toLocID))

	require.True(t, hookFired)
	assert.Equal(t, charID, captured.charID)
	assert.Equal(t, toLocID, captured.locID)
	assert.WithinDuration(t, time.Now(), captured.ts, 5*time.Second)
}
