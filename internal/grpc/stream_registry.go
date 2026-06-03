// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"sync"

	"github.com/oklog/ulid/v2"
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
	// connections maps (sessionID → connectionID → channel) for the
	// Phase 5 per-Connection routing path (D5). Co-exists with channels;
	// session-wide Send still broadcasts via channels.
	connections map[string]map[ulid.ULID]chan<- sessionStreamUpdate
}

// NewSessionStreamRegistry creates an empty registry.
func NewSessionStreamRegistry() *SessionStreamRegistry {
	return &SessionStreamRegistry{
		channels:    make(map[string]map[chan<- sessionStreamUpdate]struct{}),
		connections: make(map[string]map[ulid.ULID]chan<- sessionStreamUpdate),
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

// RegisterConnection associates a (sessionID, connectionID) pair with
// its control channel. Used by CoreServer.Subscribe at stream setup
// time when the request carries an explicit ConnectionId.
func (r *SessionStreamRegistry) RegisterConnection(
	sessionID string, connectionID ulid.ULID, ch chan<- sessionStreamUpdate,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conns, ok := r.connections[sessionID]
	if !ok {
		conns = make(map[ulid.ULID]chan<- sessionStreamUpdate)
		r.connections[sessionID] = conns
	}
	conns[connectionID] = ch
}

// DeregisterConnection removes the (sessionID, connectionID) entry
// from the per-Connection routing map ONLY if the currently-mapped
// channel matches ch. If a reconnect re-registered the same key
// before the old goroutine's defer fires, the stale defer must NOT
// clobber the new mapping or future SendToConnection calls would
// surface CONNECTION_NOT_REGISTERED on the live stream.
// (CodeRabbit PR #4191 round 6)
func (r *SessionStreamRegistry) DeregisterConnection(sessionID string, connectionID ulid.ULID, ch chan<- sessionStreamUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conns, ok := r.connections[sessionID]
	if !ok {
		return
	}
	if cur, ok := conns[connectionID]; !ok || cur != ch {
		// Different channel registered — a reconnect won the race.
		// Leave the new mapping intact.
		return
	}
	delete(conns, connectionID)
	if len(conns) == 0 {
		delete(r.connections, sessionID)
	}
}

// SendToConnection delivers update to EXACTLY the named connection's
// channel. INV-SCENE-23.
// Returns CONNECTION_NOT_REGISTERED if the conn isn't registered;
// CONTROL_CHANNEL_FULL if the buffer is exhausted.
func (r *SessionStreamRegistry) SendToConnection(
	sessionID string, connectionID ulid.ULID, update sessionStreamUpdate,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	conns, ok := r.connections[sessionID]
	if !ok {
		return oops.Code("CONNECTION_NOT_REGISTERED").
			With("session_id", sessionID).
			With("connection_id", connectionID.String()).
			Errorf("no connection registered for session")
	}
	ch, ok := conns[connectionID]
	if !ok {
		return oops.Code("CONNECTION_NOT_REGISTERED").
			With("session_id", sessionID).
			With("connection_id", connectionID.String()).
			Errorf("connection not registered for session")
	}
	select {
	case ch <- update:
		return nil
	default:
		return oops.Code("CONTROL_CHANNEL_FULL").
			With("session_id", sessionID).
			With("connection_id", connectionID.String()).
			Errorf("control channel full")
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

// HasConnection reports whether (sessionID, connectionID) is currently
// registered in the per-Connection routing map.
// Intended for use by tests only.
func (r *SessionStreamRegistry) HasConnection(sessionID string, connectionID ulid.ULID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	conns, ok := r.connections[sessionID]
	if !ok {
		return false
	}
	_, found := conns[connectionID]
	return found
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

// ConnectionSenderAdapter wraps SessionStreamRegistry to satisfy focus.ConnectionSender.
// Used by Phase 5 RPC handlers (SetConnectionFocus, AutoFocusOnJoin) to deliver
// per-Connection stream subscription deltas without importing internal/grpc directly.
type ConnectionSenderAdapter struct {
	registry *SessionStreamRegistry
}

// NewConnectionSenderAdapter creates a ConnectionSenderAdapter.
func NewConnectionSenderAdapter(r *SessionStreamRegistry) *ConnectionSenderAdapter {
	return &ConnectionSenderAdapter{registry: r}
}

// SendToConnection implements focus.ConnectionSender.
// Wraps SessionStreamRegistry.SendToConnection, translating the stream+add pair
// into a sessionStreamUpdate. Returns CONNECTION_NOT_REGISTERED when the
// connection has no active Subscribe goroutine — best-effort; callers log and continue.
func (a *ConnectionSenderAdapter) SendToConnection(sessionID string, connectionID ulid.ULID, stream string, add bool) error {
	return a.registry.SendToConnection(sessionID, connectionID, sessionStreamUpdate{
		stream:     stream,
		add:        add,
		replayMode: focus.ReplayModeFromCursor,
	})
}
