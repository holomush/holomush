// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/sethvargo/go-retry"

	"github.com/holomush/holomush/internal/core"
)

// Stream prefixes for event streams.
const (
	StreamPrefixLocation  = "location:"
	StreamPrefixCharacter = "character:"
)

// LocationStream returns the stream name for a location.
func LocationStream(id ulid.ULID) string {
	return StreamPrefixLocation + id.String()
}

// CharacterStream returns the stream name for a character.
func CharacterStream(id ulid.ULID) string {
	return StreamPrefixCharacter + id.String()
}

// BroadcastLocationStream returns the stream name for broadcasting to all locations.
func BroadcastLocationStream() string {
	return StreamPrefixLocation + "*"
}

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
	attempt := 0
	if err := retry.Do(ctx, backoff, func(ctx context.Context) error {
		attempt++
		if err := emitter.Emit(ctx, stream, eventType, data); err != nil {
			slog.Debug("event emission failed, will retry",
				"stream", stream,
				"event_type", eventType,
				"attempt", attempt,
				"error", err)
			// Mark error as retryable
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		// Log at ERROR level when all retries are exhausted for production visibility
		slog.Error("event emission failed after all retries",
			"stream", stream,
			"event_type", eventType,
			"attempts", attempt,
			"error", err)
		return oops.Wrapf(err, "emit event after retries")
	}
	return nil
}

// EmitMoveEvent emits a move event for character or object movement.
// If emitter is nil, this is a no-op.
// Returns a validation error if the payload is invalid.
func EmitMoveEvent(ctx context.Context, emitter EventEmitter, payload MovePayload) error {
	if emitter == nil {
		slog.Debug("event emitter nil, skipping event",
			"event_type", core.EventTypeMove,
			"entity_type", payload.EntityType,
			"entity_id", payload.EntityID.String(),
			"to_id", payload.ToID.String())
		return nil
	}

	eventType := string(core.EventTypeMove)
	if err := payload.Validate(); err != nil {
		return oops.Code("EVENT_PAYLOAD_INVALID").With("event_type", eventType).Wrap(err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("EVENT_MARSHAL_FAILED").With("event_type", eventType).Wrap(err)
	}

	// Emit to destination stream with retry
	stream := LocationStream(payload.ToID)
	if err := emitWithRetry(ctx, emitter, stream, eventType, data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", eventType).Wrap(err)
	}
	return nil
}

// EmitObjectCreateEvent emits an object creation event.
// If emitter is nil, this is a no-op.
// Returns an error if obj is nil.
func EmitObjectCreateEvent(ctx context.Context, emitter EventEmitter, obj *Object) error {
	if emitter == nil {
		objectID := "nil"
		if obj != nil {
			objectID = obj.ID.String()
		}
		slog.Debug("event emitter nil, skipping event",
			"event_type", core.EventTypeObjectCreate,
			"object_id", objectID)
		return nil
	}

	eventType := string(core.EventTypeObjectCreate)
	if obj == nil {
		return oops.Code("EVENT_PAYLOAD_INVALID").With("event_type", eventType).Errorf("object cannot be nil")
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
		return oops.Code("EVENT_MARSHAL_FAILED").With("event_type", eventType).Wrap(err)
	}

	stream := BroadcastLocationStream() // Broadcast to all locations
	if obj.LocationID != nil {
		stream = LocationStream(*obj.LocationID)
	}
	if err := emitWithRetry(ctx, emitter, stream, eventType, data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", eventType).Wrap(err)
	}
	return nil
}

// EmitExamineEvent emits an examine/look event when a character looks at something.
// If emitter is nil, this is a no-op.
// Returns a validation error if the payload is invalid.
func EmitExamineEvent(ctx context.Context, emitter EventEmitter, payload ExaminePayload) error {
	if emitter == nil {
		slog.Debug("event emitter nil, skipping event",
			"event_type", core.EventTypeObjectExamine,
			"character_id", payload.CharacterID.String(),
			"target_type", payload.TargetType,
			"target_id", payload.TargetID.String())
		return nil
	}

	eventType := string(core.EventTypeObjectExamine)
	if err := payload.Validate(); err != nil {
		return oops.Code("EVENT_PAYLOAD_INVALID").With("event_type", eventType).Wrap(err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("EVENT_MARSHAL_FAILED").With("event_type", eventType).Wrap(err)
	}

	// Emit to the location stream where the examine is happening
	stream := LocationStream(payload.LocationID)
	if err := emitWithRetry(ctx, emitter, stream, eventType, data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", eventType).Wrap(err)
	}
	return nil
}

// EmitObjectGiveEvent emits an object give event for transfers between characters.
// If emitter is nil, this is a no-op.
// Returns a validation error if the payload is invalid.
func EmitObjectGiveEvent(ctx context.Context, emitter EventEmitter, payload ObjectGivePayload) error {
	if emitter == nil {
		slog.Debug("event emitter nil, skipping event",
			"event_type", core.EventTypeObjectGive,
			"object_id", payload.ObjectID.String(),
			"from_character_id", payload.FromCharacterID.String(),
			"to_character_id", payload.ToCharacterID.String())
		return nil
	}

	eventType := string(core.EventTypeObjectGive)
	if err := payload.Validate(); err != nil {
		return oops.Code("EVENT_PAYLOAD_INVALID").With("event_type", eventType).Wrap(err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("EVENT_MARSHAL_FAILED").With("event_type", eventType).Wrap(err)
	}

	// Emit to the recipient character's stream with retry
	stream := CharacterStream(payload.ToCharacterID)
	if err := emitWithRetry(ctx, emitter, stream, eventType, data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", eventType).Wrap(err)
	}
	return nil
}
