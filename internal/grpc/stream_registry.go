// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"sync"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
)

// sessionStreamUpdate is sent on a session's control channel to add or
// remove a stream. Pre-F3 this carried ReplayMode hints (BoundedTail
// tailCount/notBefore) that the old replay machinery consumed; post-F3
// all replay lives inside JetStream's durable consumer so only the
// stream + add/remove bit remain.
//
// Note: replayMode is still set by callers for forward compatibility
// with F5+, but the Subscribe handler ignores it — SessionStream.SetFilters
// replaces explicit replay logic entirely.
type sessionStreamUpdate struct {
	stream     string
	add        bool             // true = subscribe, false = unsubscribe
	replayMode focus.ReplayMode // advisory post-F3; ignored by Subscribe handler
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

// AddStreamWithMode implements plugins.StreamRegistry. Subscribes with explicit replay mode.
//
// Post-F3 the Subscribe handler uses cursor-mode replay via
// SessionStream.SetFilters exclusively; BoundedTail and LiveOnline are not
// honoured. We reject those modes eagerly (option A of TODO(holomush-6uvc))
// instead of silently downgrading, so plugins requesting a specific replay
// window see an explicit error rather than the wrong behaviour.
func (r *SessionStreamRegistry) AddStreamWithMode(_ context.Context, sessionID, stream string, mode session.ReplayMode) error {
	if mode != focus.ReplayModeFromCursor {
		return oops.Code("REPLAY_MODE_NOT_SUPPORTED").
			With("session_id", sessionID).
			With("stream", stream).
			With("mode", mode).
			Errorf("replay mode %v is not supported post-F3; only ReplayModeFromCursor is honored", mode)
	}
	return r.Send(sessionID, sessionStreamUpdate{stream: stream, add: true, replayMode: mode})
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
//
// Post-F3 only ReplayModeFromCursor is honoured; see AddStreamWithMode for
// the rationale. Add-requests with other modes are rejected here too so
// callers see the mismatch instead of getting silent cursor replay.
func (a *StreamSenderAdapter) Send(sessionID, stream string, add bool, mode focus.ReplayMode) error {
	if add && mode != focus.ReplayModeFromCursor {
		return oops.Code("REPLAY_MODE_NOT_SUPPORTED").
			With("session_id", sessionID).
			With("stream", stream).
			With("mode", mode).
			Errorf("replay mode %v is not supported post-F3; only ReplayModeFromCursor is honored", mode)
	}
	return a.registry.Send(sessionID, sessionStreamUpdate{
		stream:     stream,
		add:        add,
		replayMode: mode,
	})
}
