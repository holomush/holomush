// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

// mockEventEmitter captures emitted events for testing.
type mockEventEmitter struct {
	calls []eventEmitCall
}

type eventEmitCall struct {
	Stream    string
	EventType string
	Payload   []byte
}

func (m *mockEventEmitter) Emit(_ context.Context, stream, eventType string, payload []byte) error {
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
}
