// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"encoding/json"
	"time"

	"github.com/samber/oops"
	"github.com/sethvargo/go-retry"
)

// EventEmitter publishes world events.
type EventEmitter interface {
	// Emit publishes an event to the given stream.
	Emit(ctx context.Context, stream string, eventType string, payload []byte) error
}

// emitWithRetry wraps an emit call with retry logic using exponential backoff.
// Uses exponential backoff starting at 50ms, max 3 retries.
func emitWithRetry(ctx context.Context, emitter EventEmitter, stream, eventType string, data []byte) error {
	// Create new backoff for each call - backoffs are stateful and track retry count
	backoff := retry.WithMaxRetries(3, retry.NewExponential(50*time.Millisecond))
	if err := retry.Do(ctx, backoff, func(ctx context.Context) error {
		if err := emitter.Emit(ctx, stream, eventType, data); err != nil {
			// Mark error as retryable
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		return oops.Wrapf(err, "emit event after retries")
	}
	return nil
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

	// Emit to destination location stream with retry
	stream := "location:" + payload.ToID.String()
	if err := emitWithRetry(ctx, emitter, stream, "move", data); err != nil {
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
	if err := emitWithRetry(ctx, emitter, stream, "object_create", data); err != nil {
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

	// Emit to the recipient character's stream with retry
	stream := "character:" + payload.ToCharacterID.String()
	if err := emitWithRetry(ctx, emitter, stream, "object_give", data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", "object_give").Wrap(err)
	}
	return nil
}
