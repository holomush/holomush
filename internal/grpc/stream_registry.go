// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/grpc/focus"
)

// sessionStreamUpdate is sent on a session's control channel to add or remove a stream.
type sessionStreamUpdate struct {
	stream     string
	add        bool             // true = subscribe, false = unsubscribe
	replayMode focus.ReplayMode // only meaningful when add == true
	tailCount  int       //nolint:unused // forward-declared for B7 live-loop replay dispatch
	notBefore  time.Time //nolint:unused // forward-declared for B7 live-loop replay dispatch
}

// SessionStreamRegistry maps active session IDs to their Subscribe control channels.
// It implements plugins.StreamRegistry for use by the hostfunc layer.
// Multiple subscribers per session are supported — each Subscribe call registers
// its own channel, and updates are broadcast to all active subscribers.
type SessionStreamRegistry struct {
	mu       sync.Mutex
	channels map[string]map[chan<- sessionStreamUpdate]struct{}
}

// NewSessionStreamRegistry creates an empty registry.
func NewSessionStreamRegistry() *SessionStreamRegistry {
	return &SessionStreamRegistry{
		channels: make(map[string]map[chan<- sessionStreamUpdate]struct{}),
	}
}

// Register associates a session subscriber with its control channel.
// Called by CoreServer.Subscribe at stream setup time.
func (r *SessionStreamRegistry) Register(sessionID string, ch chan<- sessionStreamUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	subs, ok := r.channels[sessionID]
	if !ok {
		subs = make(map[chan<- sessionStreamUpdate]struct{})
		r.channels[sessionID] = subs
	}
	subs[ch] = struct{}{}
}

// Deregister removes a specific subscriber channel from the registry.
// Called by CoreServer.Subscribe on exit (via defer).
func (r *SessionStreamRegistry) Deregister(sessionID string, ch chan<- sessionStreamUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	subs, ok := r.channels[sessionID]
	if !ok {
		return
	}
	delete(subs, ch)
	if len(subs) == 0 {
		delete(r.channels, sessionID)
	}
}

// Send broadcasts an update to all active subscribers for a session.
// Returns SESSION_NOT_FOUND if no active Subscribe exists for the session.
// Returns CONTROL_CHANNEL_FULL if any subscriber's channel buffer is exhausted
// (the update is still delivered to other subscribers).
func (r *SessionStreamRegistry) Send(sessionID string, update sessionStreamUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	subs, ok := r.channels[sessionID]
	if !ok || len(subs) == 0 {
		return oops.Code("SESSION_NOT_FOUND").Errorf("no active subscribe for session %s", sessionID)
	}
	var lastErr error
	for ch := range subs {
		select {
		case ch <- update:
		default:
			lastErr = oops.Code("CONTROL_CHANNEL_FULL").Errorf("control channel full for session %s", sessionID)
		}
	}
	return lastErr
}

// AddStream implements plugins.StreamRegistry. Subscribes a session to a stream.
func (r *SessionStreamRegistry) AddStream(_ context.Context, sessionID, stream string) error {
	return r.Send(sessionID, sessionStreamUpdate{stream: stream, add: true, replayMode: focus.ReplayModeFromCursor})
}

// RemoveStream implements plugins.StreamRegistry. Unsubscribes a session from a stream.
func (r *SessionStreamRegistry) RemoveStream(_ context.Context, sessionID, stream string) error {
	return r.Send(sessionID, sessionStreamUpdate{stream: stream, add: false})
}

// StreamSenderAdapter wraps SessionStreamRegistry to satisfy focus.StreamSender.
// It calls Send directly (not AddStream) to pass explicit ReplayMode values.
type StreamSenderAdapter struct {
	registry *SessionStreamRegistry
}

// NewStreamSenderAdapter creates a StreamSenderAdapter.
func NewStreamSenderAdapter(r *SessionStreamRegistry) *StreamSenderAdapter {
	return &StreamSenderAdapter{registry: r}
}

// Send implements focus.StreamSender.
func (a *StreamSenderAdapter) Send(sessionID, stream string, add bool, mode focus.ReplayMode) error {
	return a.registry.Send(sessionID, sessionStreamUpdate{
		stream:     stream,
		add:        add,
		replayMode: mode,
	})
}
