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
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
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

func TestEmitMoveEvent(t *testing.T) {
	ctx := context.Background()
	fromLocID := ulid.Make()
	toLocID := ulid.Make()
	objID := ulid.Make()

	t.Run("nil emitter is a no-op", func(t *testing.T) {
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID.String(),
			FromType:   world.ContainmentTypeLocation,
			FromID:     fromLocID.String(),
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID.String(),
		}

		err := world.EmitMoveEvent(ctx, nil, payload)
		require.NoError(t, err)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.MovePayload{
			EntityType: "", // Invalid: empty entity type
			EntityID:   objID.String(),
			FromType:   world.ContainmentTypeLocation,
			FromID:     fromLocID.String(),
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID.String(),
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
			EntityID:   objID.String(),
			FromType:   world.ContainmentTypeLocation,
			FromID:     fromLocID.String(),
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID.String(),
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, "location:"+toLocID.String(), call.Stream)
		assert.Equal(t, "move", call.EventType)

		var decoded world.MovePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
	})

	t.Run("returns error when emitter fails", func(t *testing.T) {
		emitErr := errors.New("emit failed")
		emitter := &mockEventEmitter{err: emitErr}
		payload := world.MovePayload{
			EntityType: world.EntityTypeObject,
			EntityID:   objID.String(),
			FromType:   world.ContainmentTypeLocation,
			FromID:     fromLocID.String(),
			ToType:     world.ContainmentTypeLocation,
			ToID:       toLocID.String(),
		}

		err := world.EmitMoveEvent(ctx, emitter, payload)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_EMIT_FAILED")
		assert.ErrorIs(t, err, emitErr)
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
		assert.Equal(t, "location:"+locID.String(), call.Stream)
		assert.Equal(t, "object_create", call.EventType)

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
		assert.Equal(t, "location:*", call.Stream)
		assert.Equal(t, "object_create", call.EventType)
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

	t.Run("nil emitter is a no-op", func(t *testing.T) {
		payload := world.ObjectGivePayload{
			ObjectID:        "obj-123",
			ObjectName:      "Sword",
			FromCharacterID: "char-1",
			ToCharacterID:   "char-2",
		}

		err := world.EmitObjectGiveEvent(ctx, nil, payload)
		require.NoError(t, err)
	})

	t.Run("emits event to character stream", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.ObjectGivePayload{
			ObjectID:        "obj-123",
			ObjectName:      "Sword",
			FromCharacterID: "char-1",
			ToCharacterID:   "char-2",
		}

		err := world.EmitObjectGiveEvent(ctx, emitter, payload)
		require.NoError(t, err)

		require.Len(t, emitter.calls, 1)
		call := emitter.calls[0]
		assert.Equal(t, "character:char-2", call.Stream)
		assert.Equal(t, "object_give", call.EventType)

		var decoded world.ObjectGivePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		emitter := &mockEventEmitter{}
		payload := world.ObjectGivePayload{
			ObjectID: "", // Invalid - empty
		}

		err := world.EmitObjectGiveEvent(ctx, emitter, payload)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "EVENT_PAYLOAD_INVALID")
	})

	t.Run("returns error when emitter fails", func(t *testing.T) {
		emitErr := errors.New("emit failed")
		emitter := &mockEventEmitter{err: emitErr}
		payload := world.ObjectGivePayload{
			ObjectID:        "obj-123",
			ObjectName:      "Sword",
			FromCharacterID: "char-1",
			ToCharacterID:   "char-2",
		}

		err := world.EmitObjectGiveEvent(ctx, emitter, payload)
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
		assert.Equal(t, "location:"+toLocID.String(), call.Stream)
		assert.Equal(t, "move", call.EventType)

		var decoded world.MovePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, world.EntityTypeObject, decoded.EntityType)
		assert.Equal(t, objID.String(), decoded.EntityID)
		assert.Equal(t, world.ContainmentTypeLocation, decoded.FromType)
		assert.Equal(t, fromLocID.String(), decoded.FromID)
		assert.Equal(t, world.ContainmentTypeLocation, decoded.ToType)
		assert.Equal(t, toLocID.String(), decoded.ToID)
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
		assert.Equal(t, "location:"+toLocID.String(), call.Stream)
		assert.Equal(t, "move", call.EventType)

		var decoded world.MovePayload
		err = json.Unmarshal(call.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, world.EntityTypeObject, decoded.EntityType)
		assert.Equal(t, objID.String(), decoded.EntityID)
		assert.Equal(t, world.ContainmentTypeNone, decoded.FromType)
		assert.Equal(t, "", decoded.FromID)
		assert.Equal(t, world.ContainmentTypeLocation, decoded.ToType)
		assert.Equal(t, toLocID.String(), decoded.ToID)
	})

	t.Run("succeeds even when event emitter fails", func(t *testing.T) {
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

		// Operation should succeed despite event emission failure (fire-and-forget)
		err := svc.MoveObject(ctx, subjectID, objID, to)
		require.NoError(t, err)
		// When mockEventEmitter.err is set, it returns error before recording to calls slice
		require.Len(t, emitter.calls, 0, "mock returns error before recording call")
	})
}
