// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"encoding/json"

	"github.com/samber/oops"
)

// EventEmitter publishes world events.
type EventEmitter interface {
	// Emit publishes an event to the given stream.
	Emit(ctx context.Context, stream string, eventType string, payload []byte) error
}

// EmitMoveEvent emits a move event for character or object movement.
// If emitter is nil, this is a no-op.
// Returns a validation error if the payload is invalid.
func EmitMoveEvent(ctx context.Context, emitter EventEmitter, payload MovePayload) error {
	if emitter == nil {
		return nil
	}

	if err := payload.Validate(); err != nil {
		return oops.Code("EVENT_PAYLOAD_INVALID").With("event_type", "move").Wrap(err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("EVENT_MARSHAL_FAILED").With("event_type", "move").Wrap(err)
	}

	// Emit to destination location stream
	stream := "location:" + payload.ToID
	if err := emitter.Emit(ctx, stream, "move", data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", "move").Wrap(err)
	}
	return nil
}

// EmitObjectCreateEvent emits an object creation event.
// If emitter is nil, this is a no-op.
// Returns an error if obj is nil.
func EmitObjectCreateEvent(ctx context.Context, emitter EventEmitter, obj *Object) error {
	if emitter == nil {
		return nil
	}
	if obj == nil {
		return oops.Code("EVENT_PAYLOAD_INVALID").With("event_type", "object_create").Errorf("object cannot be nil")
	}

	payload := map[string]string{
		"object_id":   obj.ID.String(),
		"object_name": obj.Name,
	}
	if obj.LocationID != nil {
		payload["location_id"] = obj.LocationID.String()
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("EVENT_MARSHAL_FAILED").With("event_type", "object_create").Wrap(err)
	}

	stream := "location:*" // Broadcast to all locations
	if obj.LocationID != nil {
		stream = "location:" + obj.LocationID.String()
	}
	if err := emitter.Emit(ctx, stream, "object_create", data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", "object_create").Wrap(err)
	}
	return nil
}

// EmitObjectGiveEvent emits an object give event for transfers between characters.
// If emitter is nil, this is a no-op.
// Returns a validation error if the payload is invalid.
func EmitObjectGiveEvent(ctx context.Context, emitter EventEmitter, payload ObjectGivePayload) error {
	if emitter == nil {
		return nil
	}

	if err := payload.Validate(); err != nil {
		return oops.Code("EVENT_PAYLOAD_INVALID").With("event_type", "object_give").Wrap(err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("EVENT_MARSHAL_FAILED").With("event_type", "object_give").Wrap(err)
	}

	// Emit to the recipient character's stream
	stream := "character:" + payload.ToCharacterID
	if err := emitter.Emit(ctx, stream, "object_give", data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", "object_give").Wrap(err)
	}
	return nil
}
