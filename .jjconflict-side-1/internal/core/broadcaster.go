// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"log/slog"
	"sync"
)

// Broadcaster distributes events to subscribers.
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[string][]chan Event
}

// NewBroadcaster creates a new broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs: make(map[string][]chan Event),
	}
}

// Subscribe creates a channel for receiving events on a stream.
func (b *Broadcaster) Subscribe(stream string) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, 100)
	b.subs[stream] = append(b.subs[stream], ch)
	return ch
}

// Unsubscribe removes a channel from a stream.
func (b *Broadcaster) Unsubscribe(stream string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subs[stream]
	for i, sub := range subs {
		if sub == ch {
			b.subs[stream] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Broadcast sends an event to all subscribers of its stream.
func (b *Broadcaster) Broadcast(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subs[event.Stream] {
		select {
		case ch <- event:
		default:
			// Known limitation: User has already been told their message was sent
			// before we attempt delivery to subscribers. If a subscriber's buffer
			// is full, they will miss this event. Future improvement: implement
			// delivery acknowledgment before confirming to sender.
			slog.Warn("event dropped: subscriber buffer full",
				"stream", event.Stream,
				"event_id", event.ID.String(),
				"event_type", event.Type,
			)
		}
	}
}
