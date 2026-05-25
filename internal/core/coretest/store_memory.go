// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package coretest provides test-only implementations of internal/core
// interfaces. Production code MUST NOT import this package; the prohibition
// is enforced by the depguard rule in .golangci.yaml (see holomush-1eps2).
package coretest

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
)

// MemoryEventStore is an in-memory event store for unit testing.
// It implements core.EventAppender and provides Replay/ReplayTail/LastEventID
// as test-inspection helpers.
type MemoryEventStore struct {
	mu      sync.RWMutex
	streams map[string][]core.Event
}

// NewMemoryEventStore creates a new in-memory event store.
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		streams: make(map[string][]core.Event),
	}
}

// Compile-time check: MemoryEventStore satisfies core.EventAppender.
var _ core.EventAppender = (*MemoryEventStore)(nil)

// Append persists an event to the in-memory store.
func (s *MemoryEventStore) Append(_ context.Context, event core.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[event.Stream] = append(s.streams[event.Stream], event)
	return nil
}

// Replay returns events from a stream starting after the given ID.
// Test-inspection helper; not part of any production interface.
func (s *MemoryEventStore) Replay(_ context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.streams[stream]
	if len(events) == 0 {
		return nil, nil
	}

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

	endIdx := min(startIdx+limit, len(events))

	result := make([]core.Event, endIdx-startIdx)
	copy(result, events[startIdx:endIdx])
	return result, nil
}

// LastEventID returns the most recent event ID for a stream.
// Test-inspection helper; not part of any production interface.
func (s *MemoryEventStore) LastEventID(_ context.Context, stream string) (ulid.ULID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.streams[stream]
	if len(events) == 0 {
		return ulid.ULID{}, core.ErrStreamEmpty
	}
	return events[len(events)-1].ID, nil
}

// maxReplayTailCount is the server-side cap for ReplayTail count parameter.
const maxReplayTailCount = 501

// ReplayTail returns the most recent count events on stream, ascending by ID.
// Events with timestamps before notBefore are excluded. If beforeID is non-zero,
// events with ID >= beforeID are excluded. Count is capped at maxReplayTailCount.
// Test-inspection helper; not part of any production interface.
func (s *MemoryEventStore) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
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

	var eligible []core.Event
	for i := len(events) - 1; i >= 0 && len(eligible) < count; i-- {
		e := events[i]
		if !beforeID.IsZero() && e.ID.Compare(beforeID) >= 0 {
			continue
		}
		if !notBefore.IsZero() && e.Timestamp.Before(notBefore) {
			continue
		}
		eligible = append(eligible, e)
	}

	for i, j := 0, len(eligible)-1; i < j; i, j = i+1, j-1 {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	}
	return eligible, nil
}
