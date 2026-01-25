// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
)

// EventAppender defines the minimal interface for appending events to a store.
// This allows EventStoreAdapter to work with store.PostgresEventStore or any mock.
type EventAppender interface {
	Append(ctx context.Context, event core.Event) error
}

// EventStoreAdapter bridges world.EventEmitter to a core.EventStore.
// It generates event IDs and timestamps, and uses a system actor for all events.
type EventStoreAdapter struct {
	store EventAppender
}

// NewEventStoreAdapter creates a new adapter wrapping the provided event store.
func NewEventStoreAdapter(store EventAppender) *EventStoreAdapter {
	return &EventStoreAdapter{store: store}
}

// Emit publishes an event to the given stream.
// Implements world.EventEmitter interface.
func (a *EventStoreAdapter) Emit(ctx context.Context, stream, eventType string, payload []byte) error {
	event := core.Event{
		ID:        core.NewULID(),
		Stream:    stream,
		Type:      core.EventType(eventType),
		Timestamp: time.Now(),
		Actor: core.Actor{
			Kind: core.ActorSystem,
			ID:   "world-service",
		},
		Payload: payload,
	}

	if err := a.store.Append(ctx, event); err != nil {
		return oops.Code("EVENT_STORE_APPEND_FAILED").
			With("stream", stream).
			With("event_type", eventType).
			Wrap(err)
	}

	return nil
}

// Compile-time interface check.
var _ EventEmitter = (*EventStoreAdapter)(nil)
