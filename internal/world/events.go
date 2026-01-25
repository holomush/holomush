// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"encoding/json"
	"errors"
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
//
// # Design Note: Raw Interface vs Typed Events
//
// This interface uses raw string/[]byte parameters rather than a typed Event
// interface (e.g., Event.Stream(), Event.Type(), Event.Payload()). This is
// intentional for several reasons:
//
//  1. Type safety at call sites: The EmitX helper functions (EmitMoveEvent,
//     EmitExamineEvent, etc.) provide compile-time type safety where it matters.
//     Service code calls these typed helpers, not the raw Emit method directly.
//
//  2. Implementation flexibility: The raw interface allows adapters (like
//     EventStoreAdapter) to construct events with implementation-specific details
//     (IDs, timestamps, actors) without being constrained by an Event interface.
//
//  3. Minimal abstraction: Events are stored as JSON blobs. Adding an Event
//     interface would be an abstraction over []byte that provides no additional
//     safety beyond what the EmitX helpers already provide.
//
// If callers bypass EmitX helpers and call Emit directly, they take on
// responsibility for correct stream/eventType/payload construction.
type EventEmitter interface {
	// Emit publishes an event to the given stream.
	Emit(ctx context.Context, stream string, eventType string, payload []byte) error
}

// emitWithRetry wraps an emit call with retry logic using exponential backoff.
// Uses exponential backoff starting at 50ms, up to 3 retries (4 total attempts).
// entityType and entityID provide context for debugging (e.g., "character", "01ABC...").
func emitWithRetry(ctx context.Context, emitter EventEmitter, stream, eventType, entityType, entityID string, data []byte) error {
	// Create new backoff for each call - backoffs are stateful and track retry count
	backoff := retry.WithMaxRetries(3, retry.NewExponential(50*time.Millisecond))
	attempt := 0
	if err := retry.Do(ctx, backoff, func(ctx context.Context) error {
		attempt++
		if err := emitter.Emit(ctx, stream, eventType, data); err != nil {
			slog.Debug("event emission failed, will retry",
				"stream", stream,
				"event_type", eventType,
				"entity_type", entityType,
				"entity_id", entityID,
				"attempt", attempt,
				"error", err)
			// Mark error as retryable
			return retry.RetryableError(err)
		}
		return nil
	}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// Log at WARN level for context cancellation (expected during shutdown)
			slog.Warn("event emission cancelled",
				"stream", stream,
				"event_type", eventType,
				"entity_type", entityType,
				"entity_id", entityID,
				"attempts", attempt,
				"reason", err)
		} else {
			// Log at ERROR level when all retries are exhausted for production visibility
			slog.Error("event emission failed after all retries",
				"stream", stream,
				"event_type", eventType,
				"entity_type", entityType,
				"entity_id", entityID,
				"attempts", attempt,
				"error", err)
		}
		return oops.Wrapf(err, "emit event after retries")
	}
	return nil
}

// EmitMoveEvent emits a move event for character or object movement.
// Retries up to 3 times with exponential backoff (4 total attempts) before
// returning EVENT_EMIT_FAILED.
// Returns ErrNoEventEmitter if emitter is nil (indicates misconfiguration).
// Returns a validation error if the payload is invalid.
func EmitMoveEvent(ctx context.Context, emitter EventEmitter, payload MovePayload) error {
	if emitter == nil {
		return oops.Code("EVENT_EMITTER_MISSING").
			With("event_type", core.EventTypeMove).
			With("entity_type", payload.EntityType).
			With("entity_id", payload.EntityID.String()).
			Wrap(ErrNoEventEmitter)
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
	entityType := string(payload.EntityType)
	entityID := payload.EntityID.String()
	if err := emitWithRetry(ctx, emitter, stream, eventType, entityType, entityID, data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", eventType).Wrap(err)
	}
	return nil
}

// EmitObjectCreateEvent emits an object creation event.
// Retries up to 3 times with exponential backoff (4 total attempts) before
// returning EVENT_EMIT_FAILED.
// Returns ErrNoEventEmitter if emitter is nil (indicates misconfiguration).
// Returns an error if obj is nil.
func EmitObjectCreateEvent(ctx context.Context, emitter EventEmitter, obj *Object) error {
	if emitter == nil {
		objectID := "nil"
		if obj != nil {
			objectID = obj.ID.String()
		}
		return oops.Code("EVENT_EMITTER_MISSING").
			With("event_type", core.EventTypeObjectCreate).
			With("object_id", objectID).
			Wrap(ErrNoEventEmitter)
	}

	eventType := string(core.EventTypeObjectCreate)
	if obj == nil {
		return oops.Code("EVENT_PAYLOAD_INVALID").With("event_type", eventType).Errorf("object cannot be nil")
	}

	payload := map[string]string{
		"object_id":   obj.ID.String(),
		"object_name": obj.Name,
	}
	if obj.LocationID() != nil {
		payload["location_id"] = obj.LocationID().String()
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return oops.Code("EVENT_MARSHAL_FAILED").With("event_type", eventType).Wrap(err)
	}

	stream := BroadcastLocationStream() // Broadcast to all locations
	if obj.LocationID() != nil {
		stream = LocationStream(*obj.LocationID())
	}
	if err := emitWithRetry(ctx, emitter, stream, eventType, "object", obj.ID.String(), data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", eventType).Wrap(err)
	}
	return nil
}

// EmitExamineEvent emits an examine/look event when a character looks at something.
// Retries up to 3 times with exponential backoff (4 total attempts) before
// returning EVENT_EMIT_FAILED.
// Returns ErrNoEventEmitter if emitter is nil (indicates misconfiguration).
// Returns a validation error if the payload is invalid.
func EmitExamineEvent(ctx context.Context, emitter EventEmitter, payload ExaminePayload) error {
	if emitter == nil {
		return oops.Code("EVENT_EMITTER_MISSING").
			With("event_type", core.EventTypeObjectExamine).
			With("character_id", payload.CharacterID.String()).
			With("target_type", payload.TargetType).
			With("target_id", payload.TargetID.String()).
			Wrap(ErrNoEventEmitter)
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
	targetType := string(payload.TargetType)
	targetID := payload.TargetID.String()
	if err := emitWithRetry(ctx, emitter, stream, eventType, targetType, targetID, data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", eventType).Wrap(err)
	}
	return nil
}

// EmitObjectGiveEvent emits an object give event for transfers between characters.
// Retries up to 3 times with exponential backoff (4 total attempts) before
// returning EVENT_EMIT_FAILED.
// Returns ErrNoEventEmitter if emitter is nil (indicates misconfiguration).
// Returns a validation error if the payload is invalid.
func EmitObjectGiveEvent(ctx context.Context, emitter EventEmitter, payload ObjectGivePayload) error {
	if emitter == nil {
		return oops.Code("EVENT_EMITTER_MISSING").
			With("event_type", core.EventTypeObjectGive).
			With("object_id", payload.ObjectID.String()).
			With("from_character_id", payload.FromCharacterID.String()).
			With("to_character_id", payload.ToCharacterID.String()).
			Wrap(ErrNoEventEmitter)
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
	if err := emitWithRetry(ctx, emitter, stream, eventType, "object", payload.ObjectID.String(), data); err != nil {
		return oops.Code("EVENT_EMIT_FAILED").With("stream", stream).With("event_type", eventType).Wrap(err)
	}
	return nil
}
