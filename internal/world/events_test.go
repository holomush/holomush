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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
	"github.com/holomush/holomush/pkg/errutil"
)

// mockEventEmitter captures emitted events for testing.
type mockEventEmitter struct {
	calls []eventEmitCall
	err   error // If set, Emit returns this error
}

type eventEmitCall struct {
	Stream    string
	EventType string
	Payload   []byte
}

func (m *mockEventEmitter) Emit(_ context.Context, stream, eventType string, payload []byte) error {
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, eventEmitCall{
		Stream:    stream,
		EventType: eventType,
		Payload:   payload,
	})
	return nil
}

// retryCountingEmitter fails a specified number of times before succeeding.
type retryCountingEmitter struct {
	failCount   int // Number of times to fail before succeeding
	attempts    int // Tracks how many attempts were made
	calls       []eventEmitCall
	failErr     error
	lastPayload []byte
}

func (m *retryCountingEmitter) Emit(_ context.Context, stream, eventType string, payload []byte) error {
	m.attempts++
	m.lastPayload = payload
	if m.attempts <= m.failCount {
		return m.failErr
	}
	m.calls = append(m.calls, eventEmitCall{
		Stream:    stream,
		EventType: eventType,
		Payload:   payload,
	})
	return nil
}

func TestEmitMoveEvent(t *testing.T) {
	ctx := context.Background()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()
	objID := ulid.Make()

	t.Run("nil emitter is a no-op", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(ctx, nil, payload)
		require.NoError(t, err)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.MovePayload{
			EntityType: "", // Invalid: empty entity type
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "entity_type")
		assert.Empty(t, emitter.calls, "should not emit invalid payload")
	})

	t.Run("emits event with correct stream and payload", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(toLocID), call.Stream)
		assert.Equal(t, string(core.EventTypeMove), call.EventType)

		var decoded world.MovePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
	})

	t.Run("returns error when emitter fails after retries", func(t *testing.T) {
		emitErr := errors.New("emit failed")
		emitter := &mockEventEmitter{err: emitErr}
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		assert.ErrorIs(t, err, emitErr)
	})

	t.Run("retries on transient failure and succeeds", func(t *testing.T) {
		transientErr := errors.New("connection reset")
		emitter := &retryCountingEmitter{
			failCount: 2, // Fail twice, succeed on third attempt
			failErr:   transientErr,
		}
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.NoError(t, err, "should succeed after retries")
		assert.Equal(t, 3, emitter.attempts, "should have made 3 attempts (2 failures + 1 success)")
		require.Len(t, emitter.calls, 1, "should have recorded 1 successful call")
	})

	t.Run("gives up after max retries", func(t *testing.T) {
		persistentErr := errors.New("service unavailable")
		emitter := &retryCountingEmitter{
			failCount: 10, // Always fail (more than max retries)
			failErr:   persistentErr,
		}
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.Error(t, err, "should fail after max retries")
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		assert.Equal(t, 4, emitter.attempts, "should have made 4 attempts (initial + 3 retries)")
	})

	t.Run("logs debug message on retry attempts", func(t *testing.T) {
		// Capture log output by temporarily replacing the default logger
		var logBuf bytes.Buffer
		originalLogger := slog.Default()
		testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
		slog.SetDefault(testLogger)
		defer slog.SetDefault(originalLogger)

		transientErr := errors.New("connection reset")
		emitter := &retryCountingEmitter{
			failCount: 2, // Fail twice, succeed on third attempt
			failErr:   transientErr,
		}
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.NoError(t, err)

		// Verify log output contains retry information
		logOutput := logBuf.String()
		assert.True(t, strings.Contains(logOutput, "event emission failed"),
			"should log retry message, got: %s", logOutput)
		assert.True(t, strings.Contains(logOutput, "stream="),
			"should log stream, got: %s", logOutput)
		assert.True(t, strings.Contains(logOutput, "event_type="),
			"should log event_type, got: %s", logOutput)
		assert.True(t, strings.Contains(logOutput, "attempt="),
			"should log attempt number, got: %s", logOutput)
		assert.True(t, strings.Contains(logOutput, "error="),
			"should log error, got: %s", logOutput)
	})

	t.Run("logs error when all retries exhausted", func(t *testing.T) {
		// Capture log output by temporarily replacing the default logger
		var logBuf bytes.Buffer
		originalLogger := slog.Default()
		testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
			Level: slog.LevelError, // Only capture ERROR level logs
		}))
		slog.SetDefault(testLogger)
		defer slog.SetDefault(originalLogger)

		persistentErr := errors.New("service unavailable")
		emitter := &retryCountingEmitter{
			failCount: 10, // Always fail (more than max retries)
			failErr:   persistentErr,
		}
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.Error(t, err)

		// Verify ERROR log was emitted when retries exhausted
		logOutput := logBuf.String()
		assert.Contains(t, logOutput, "level=ERROR",
			"should log at ERROR level, got: %s", logOutput)
		assert.Contains(t, logOutput, "event emission failed after all retries",
			"should log exhaustion message, got: %s", logOutput)
		assert.Contains(t, logOutput, "stream=",
			"should log stream, got: %s", logOutput)
		assert.Contains(t, logOutput, "event_type=",
			"should log event_type, got: %s", logOutput)
		assert.Contains(t, logOutput, "attempts=4",
			"should log total attempts (4), got: %s", logOutput)
		assert.Contains(t, logOutput, "error=",
			"should log final error, got: %s", logOutput)
	})

	t.Run("respects context cancellation during retry", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		emitter := &mockEventEmitter{err: errors.New("transient error")}
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID,
			FromType:   world.ContainmentTypeLocation,
			FromID:     &fromLocID,
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID,
		}

		err := world.EmitMoveEvent(cancelCtx, emitter, payload)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		// Should not have made multiple attempts since context was cancelled
		assert.LessOrEqual(t, len(emitter.calls), 1, "should stop retrying on context cancellation")
	})
}

func TestEmitObjectCreateEvent(t *testing.T) {
	ctx := context.Background()
	locID := ulid.Make()
	objID := ulid.Make()

	t.Run("nil emitter is a no-op", func(t *testing.T) {
		obj := &world.Object{
			ID:   objID,
			Name: "Test Object",
		}

		err := world.EmitObjectCreateEvent(ctx, nil, obj)
		require.NoError(t, err)
	})

	t.Run("emits event with location stream when in location", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		obj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &locID,
		}

		err := world.EmitObjectCreateEvent(ctx, emitter, obj)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(locID), call.Stream)
		assert.Equal(t, string(core.EventTypeObjectCreate), call.EventType)

		var decoded map[string]string
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, objID.String(), decoded["object_id"])
		assert.Equal(t, "Test Object", decoded["object_name"])
		assert.Equal(t, locID.String(), decoded["location_id"])
	})

	t.Run("emits to broadcast stream when not in location", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		charID := ulid.Make()
		obj := &world.Object{
			ID:                objID,
			Name:              "Test Object",
			HeldByCharacterID: &charID,
		}

		err := world.EmitObjectCreateEvent(ctx, emitter, obj)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.BroadcastLocationStream(), call.Stream)
		assert.Equal(t, string(core.EventTypeObjectCreate), call.EventType)
	})

	t.Run("returns error when emitter fails", func(t *testing.T) {
		emitErr := errors.New("emit failed")
		emitter := &mockEventEmitter{err: emitErr}
		obj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &locID,
		}

		err := world.EmitObjectCreateEvent(ctx, emitter, obj)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		assert.ErrorIs(t, err, emitErr)
	})

	t.Run("returns error when object is nil", func(t *testing.T) {
		emitter := &mockEventEmitter{}

		err := world.EmitObjectCreateEvent(ctx, emitter, nil)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_PAYLOAD_INVALID")
		assert.Contains(t, err.Error(), "nil")
		assert.Empty(t, emitter.calls, "should not emit when object is nil")
	})
}

func TestEmitObjectGiveEvent(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	fromCharID := ulid.Make()
	toCharID := ulid.Make()

	t.Run("nil emitter is a no-op", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Sword",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
		}

		err := world.EmitObjectGiveEvent(ctx, nil, payload)
		require.NoError(t, err)
	})

	t.Run("emits event to character stream", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Sword",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
		}

		err := world.EmitObjectGiveEvent(ctx, emitter, payload)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.CharacterStream(toCharID), call.Stream)
		assert.Equal(t, string(core.EventTypeObjectGive), call.EventType)

		var decoded world.ObjectGivePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.ObjectGivePayload{
			// ObjectID is zero value - invalid
		}

		err := world.EmitObjectGiveEvent(ctx, emitter, payload)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_PAYLOAD_INVALID")
	})

	t.Run("returns error when emitter fails", func(t *testing.T) {
		emitErr := errors.New("emit failed")
		emitter := &mockEventEmitter{err: emitErr}
		payload := world.ObjectGivePayload{
			ObjectID:        objID,
			ObjectName:      "Sword",
			FromCharacterID: fromCharID,
			ToCharacterID:   toCharID,
		}

		err := world.EmitObjectGiveEvent(ctx, emitter, payload)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		assert.ErrorIs(t, err, emitErr)
	})
}

func TestEmitExamineEvent(t *testing.T) {
	ctx := context.Background()
	charID := ulid.Make()
	targetID := ulid.Make()
	locID := ulid.Make()

	t.Run("nil emitter is a no-op", func(t *testing.T) {
		payload := world.ExaminePayload{
			CharacterID: charID,
			TargetType:  world.TargetTypeObject,
			TargetID:    targetID,
			TargetName:  "Chest",
			LocationID:  locID,
		}

		err := world.EmitExamineEvent(ctx, nil, payload)
		require.NoError(t, err)
	})

	t.Run("emits event to location stream", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.ExaminePayload{
			CharacterID: charID,
			TargetType:  world.TargetTypeObject,
			TargetID:    targetID,
			TargetName:  "Chest",
			LocationID:  locID,
		}

		err := world.EmitExamineEvent(ctx, emitter, payload)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(locID), call.Stream)
		assert.Equal(t, string(core.EventTypeObjectExamine), call.EventType)

		var decoded world.ExaminePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
	})

	t.Run("emits event for examining location", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.ExaminePayload{
			CharacterID: charID,
			TargetType:  world.TargetTypeLocation,
			TargetID:    locID,
			TargetName:  "Town Square",
			LocationID:  locID,
		}

		err := world.EmitExamineEvent(ctx, emitter, payload)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		var decoded world.ExaminePayload
		err = json.Unmarshal(emitter.calls[0].Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, world.TargetTypeLocation, decoded.TargetType)
	})

	t.Run("emits event for examining character", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		otherCharID := ulid.Make()
		payload := world.ExaminePayload{
			CharacterID: charID,
			TargetType:  world.TargetTypeCharacter,
			TargetID:    otherCharID,
			TargetName:  "Mysterious Stranger",
			LocationID:  locID,
		}

		err := world.EmitExamineEvent(ctx, emitter, payload)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		var decoded world.ExaminePayload
		err = json.Unmarshal(emitter.calls[0].Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, world.TargetTypeCharacter, decoded.TargetType)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.ExaminePayload{
			// CharacterID is zero value - invalid
		}

		err := world.EmitExamineEvent(ctx, emitter, payload)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_PAYLOAD_INVALID")
	})

	t.Run("returns error when emitter fails", func(t *testing.T) {
		emitErr := errors.New("emit failed")
		emitter := &mockEventEmitter{err: emitErr}
		payload := world.ExaminePayload{
			CharacterID: charID,
			TargetType:  world.TargetTypeObject,
			TargetID:    targetID,
			TargetName:  "Chest",
			LocationID:  locID,
		}

		err := world.EmitExamineEvent(ctx, emitter, payload)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		assert.ErrorIs(t, err, emitErr)
	})
}

// mockAccessControl is a test mock for AccessControl.
type mockAccessControlForEvents struct {
	allowAll bool
}

func (m *mockAccessControlForEvents) Check(_ context.Context, _, _, _ string) bool {
	return m.allowAll
}

func TestService_MoveObject_EmitsEvent(t *testing.T) {
	ctx := context.Background()
	objID := ulid.Make()
	subjectID := "char:" + ulid.Make().String()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()

	t.Run("emits move event on successful move", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: &mockAccessControlForEvents{allowAll: true},
			EventEmitter:  emitter,
		})

		existingObj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &fromLocID,
		}
		to := world.Containment{LocationID: &toLocID}

		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(nil)

		err := svc.MoveObject(ctx, subjectID, objID, to)
		require.NoError(t, err)

		// Verify event was emitted
		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(toLocID), call.Stream)
		assert.Equal(t, string(core.EventTypeMove), call.EventType)

		var decoded world.MovePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, world.EntityTypeObject, decoded.EntityType)
		assert.Equal(t, objID, decoded.EntityID)
		assert.Equal(t, world.ContainmentTypeLocation, decoded.FromType)
		assert.Equal(t, &fromLocID, decoded.FromID)
		assert.Equal(t, world.ContainmentTypeLocation, decoded.ToType)
		assert.Equal(t, toLocID, decoded.ToID)
	})

	t.Run("works without event emitter configured", func(t *testing.T) {
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: &mockAccessControlForEvents{allowAll: true},
			// No EventEmitter configured
		})

		existingObj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &fromLocID,
		}
		to := world.Containment{LocationID: &toLocID}

		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(nil)

		err := svc.MoveObject(ctx, subjectID, objID, to)
		require.NoError(t, err)
		// No panic or error, just skips event emission
	})

	t.Run("emits event with from_type none for first-time placement", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: &mockAccessControlForEvents{allowAll: true},
			EventEmitter:  emitter,
		})

		// Object with no prior containment (first-time placement)
		existingObj := &world.Object{
			ID:   objID,
			Name: "Unplaced Object",
			// All containment fields nil
		}
		to := world.Containment{LocationID: &toLocID}

		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(nil)

		err := svc.MoveObject(ctx, subjectID, objID, to)
		require.NoError(t, err)

		// Verify event was emitted with from_type "none"
		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, world.LocationStream(toLocID), call.Stream)
		assert.Equal(t, string(core.EventTypeMove), call.EventType)

		var decoded world.MovePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, world.EntityTypeObject, decoded.EntityType)
		assert.Equal(t, objID, decoded.EntityID)
		assert.Equal(t, world.ContainmentTypeNone, decoded.FromType)
		assert.Nil(t, decoded.FromID)
		assert.Equal(t, world.ContainmentTypeLocation, decoded.ToType)
		assert.Equal(t, toLocID, decoded.ToID)
	})

	t.Run("fails when event emitter fails", func(t *testing.T) {
		emitErr := errors.New("event bus unavailable")
		emitter := &mockEventEmitter{err: emitErr}
		mockObjRepo := worldtest.NewMockObjectRepository(t)

		svc := world.NewService(world.ServiceConfig{
			ObjectRepo:    mockObjRepo,
			AccessControl: &mockAccessControlForEvents{allowAll: true},
			EventEmitter:  emitter,
		})

		existingObj := &world.Object{
			ID:         objID,
			Name:       "Test Object",
			LocationID: &fromLocID,
		}
		to := world.Containment{LocationID: &toLocID}

		mockObjRepo.EXPECT().Get(ctx, objID).Return(existingObj, nil)
		mockObjRepo.EXPECT().Move(ctx, objID, to).Return(nil)

		// Event emission failure should fail the operation
		err := svc.MoveObject(ctx, subjectID, objID, to)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "event")
	})
}
