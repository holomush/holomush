// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package core

import (
	"context"
	"sync"

	"github.com/oklog/ulid/v2"
)

// MemoryEventStore is an in-memory EventStore for testing.
type MemoryEventStore struct {
	mu      sync.RWMutex
	streams map[string][]Event
	subs    map[string][]chan ulid.ULID
}

// NewMemoryEventStore creates a new in-memory event store.
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		streams: make(map[string][]Event),
		subs:    make(map[string][]chan ulid.ULID),
	}
}

// Append persists an event to the in-memory store.
func (s *MemoryEventStore) Append(_ context.Context, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[event.Stream] = append(s.streams[event.Stream], event)
	for _, ch := range s.subs[event.Stream] {
		select {
		case ch <- event.ID:
		default:
		}
	}
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

// Subscribe returns channels that receive event IDs when Append is called on the stream.
// Notifications are non-blocking: if the buffer is full, the notification is dropped.
// Both channels are closed when the context is cancelled.
func (s *MemoryEventStore) Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error) {
	events := make(chan ulid.ULID, 100)
	errs := make(chan error, 1)

	s.mu.Lock()
	s.subs[stream] = append(s.subs[stream], events)
	s.mu.Unlock()

	go func() {
		<-ctx.Done()

		s.mu.Lock()
		subs := s.subs[stream]
		for i, ch := range subs {
			if ch == events {
				s.subs[stream] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		s.mu.Unlock()

		close(events)
		close(errs)
	}()

	return events, errs, nil
}
