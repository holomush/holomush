// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// MemoryEventStore is an in-memory EventStore for testing.
type MemoryEventStore struct {
	mu          sync.RWMutex
	streams     map[string][]Event
	subs        map[string][]chan ulid.ULID
	sessionSubs []*memorySubscription
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
	for _, ss := range s.sessionSubs {
		ss.notify(event.Stream, event.ID)
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
		found := false
		for i, e := range events {
			if e.ID == afterID {
				startIdx = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, nil
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

// maxReplayTailCount is the server-side cap for ReplayTail count parameter.
const maxReplayTailCount = 500

// ReplayTail returns the most recent count events on stream, ascending by ID.
// Events with timestamps before notBefore are excluded. Count is capped at 500.
func (s *MemoryEventStore) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if count > maxReplayTailCount {
		count = maxReplayTailCount
	}
	if count <= 0 {
		return nil, nil
	}

	events := s.streams[stream]
	if len(events) == 0 {
		return nil, nil
	}

	// Filter by notBefore if set, scanning from the end.
	var eligible []Event
	if !notBefore.IsZero() {
		for i := len(events) - 1; i >= 0 && len(eligible) < count; i-- {
			if !events[i].Timestamp.Before(notBefore) {
				eligible = append(eligible, events[i])
			}
		}
	} else {
		start := len(events) - count
		if start < 0 {
			start = 0
		}
		eligible = make([]Event, len(events)-start)
		copy(eligible, events[start:])
		return eligible, nil
	}

	// Reverse eligible to get ascending order.
	for i, j := 0, len(eligible)-1; i < j; i, j = i+1, j-1 {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	}
	return eligible, nil
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

// SubscribeSession creates a new session-wide subscription. Append-order
// delivery across all added streams is guaranteed (memory equivalent of I-14).
// The subscription is automatically cleaned up when ctx is cancelled.
func (s *MemoryEventStore) SubscribeSession(ctx context.Context) (Subscription, error) {
	ms := &memorySubscription{
		streams: make(map[string]struct{}),
		notifCh: make(chan StreamNotification, 256),
		errCh:   make(chan error, 1),
		store:   s,
	}
	s.mu.Lock()
	s.sessionSubs = append(s.sessionSubs, ms)
	s.mu.Unlock()

	// Clean up when the caller's context is cancelled.
	go func() {
		<-ctx.Done()
		ms.Close() //nolint:errcheck // best-effort cleanup on context cancellation
	}()

	return ms, nil
}

func (s *MemoryEventStore) removeSessionSub(ms *memorySubscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.sessionSubs {
		if sub == ms {
			s.sessionSubs = append(s.sessionSubs[:i], s.sessionSubs[i+1:]...)
			return
		}
	}
}

// memorySubscription implements Subscription for MemoryEventStore.
// It maintains a set of subscribed streams and receives notifications
// from the store's Append method. Delivery order matches append order
// across all streams (in-memory equivalent of I-14).
type memorySubscription struct {
	mu      sync.RWMutex
	streams map[string]struct{}
	notifCh chan StreamNotification
	errCh   chan error
	closed  bool
	store   *MemoryEventStore
}

// AddStream adds a stream to the subscription. Idempotent.
func (ms *memorySubscription) AddStream(_ context.Context, stream string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.closed {
		return errors.New("subscription closed")
	}
	ms.streams[stream] = struct{}{}
	return nil
}

// RemoveStream removes a stream from the subscription.
func (ms *memorySubscription) RemoveStream(_ context.Context, stream string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	delete(ms.streams, stream)
	return nil
}

// Notifications returns the channel of stream notifications.
func (ms *memorySubscription) Notifications() <-chan StreamNotification {
	return ms.notifCh
}

// Errors returns the error channel.
func (ms *memorySubscription) Errors() <-chan error {
	return ms.errCh
}

// Close unregisters the subscription from the store and closes channels.
func (ms *memorySubscription) Close() error {
	ms.mu.Lock()
	if ms.closed {
		ms.mu.Unlock()
		return nil
	}
	ms.closed = true
	ms.mu.Unlock()

	ms.store.removeSessionSub(ms)
	close(ms.notifCh)
	close(ms.errCh)
	return nil
}

// notify is called by the store under its lock for each appended event.
func (ms *memorySubscription) notify(stream string, eventID ulid.ULID) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if ms.closed {
		return
	}
	if _, ok := ms.streams[stream]; !ok {
		return
	}
	select {
	case ms.notifCh <- StreamNotification{Stream: stream, EventID: eventID}:
	default:
	}
}
