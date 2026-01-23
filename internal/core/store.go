// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"errors"
	"sync"

	"github.com/oklog/ulid/v2"
)

// ErrStreamEmpty is returned when a stream has no events.
var ErrStreamEmpty = errors.New("stream is empty")

// EventStore persists and retrieves events.
type EventStore interface {
	// Append persists an event to a stream.
	Append(ctx context.Context, event Event) error

	// Replay returns up to limit events from a stream, starting after afterID.
	// If afterID is zero ULID, starts from beginning.
	Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)

	// LastEventID returns the most recent event ID for a stream.
	LastEventID(ctx context.Context, stream string) (ulid.ULID, error)

	// Subscribe starts listening for new events on the given stream.
	// Returns a channel of event IDs and an error channel.
	// The caller should use Replay() to fetch full events by ID.
	// Channels are closed when context is cancelled.
	Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error)
}

// MemoryEventStore is an in-memory EventStore for testing.
type MemoryEventStore struct {
	mu      sync.RWMutex
	streams map[string][]Event
}

// NewMemoryEventStore creates a new in-memory event store.
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		streams: make(map[string][]Event),
	}
}

// Append persists an event to the in-memory store.
func (s *MemoryEventStore) Append(_ context.Context, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[event.Stream] = append(s.streams[event.Stream], event)
	return nil
}

// Replay returns events from a stream starting after the given ID.
func (s *MemoryEventStore) Replay(_ context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.streams[stream]
	if len(events) == 0 {
		return nil, nil
	}

	// Find start index
	startIdx := 0
	if afterID.Compare(ulid.ULID{}) != 0 {
		for i, e := range events {
			if e.ID == afterID {
				startIdx = i + 1
				break
			}
		}
	}

	// Slice with limit
	endIdx := min(startIdx+limit, len(events))

	result := make([]Event, endIdx-startIdx)
	copy(result, events[startIdx:endIdx])
	return result, nil
}

// LastEventID returns the most recent event ID for a stream.
func (s *MemoryEventStore) LastEventID(_ context.Context, stream string) (ulid.ULID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.streams[stream]
	if len(events) == 0 {
		return ulid.ULID{}, ErrStreamEmpty
	}
	return events[len(events)-1].ID, nil
}

// Subscribe returns closed channels for the in-memory store.
// This is a stub implementation for testing - real subscriptions use PostgreSQL LISTEN/NOTIFY.
func (s *MemoryEventStore) Subscribe(ctx context.Context, _ string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error) {
	events := make(chan ulid.ULID)
	errs := make(chan error)

	// Close channels when context is cancelled
	go func() {
		<-ctx.Done()
		close(events)
		close(errs)
	}()

	return events, errs, nil
}
