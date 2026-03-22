// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package session

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// MemStore is an in-memory implementation of Store for testing.
type MemStore struct {
	mu          sync.RWMutex
	sessions    map[string]*Info
	connections map[ulid.ULID]*Connection // keyed by connection ID
	watchers    map[string][]chan Event
}

// NewMemStore creates a new in-memory session store.
func NewMemStore() *MemStore {
	return &MemStore{
		sessions:    make(map[string]*Info),
		connections: make(map[ulid.ULID]*Connection),
		watchers:    make(map[string][]chan Event),
	}
}

// compile-time check that MemStore implements Store.
var _ Store = (*MemStore)(nil)

// Get retrieves a session by ID.
func (m *MemStore) Get(_ context.Context, id string) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.sessions[id]
	if !ok {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	return copyInfo(info), nil
}

// Set creates or updates a session.
func (m *MemStore) Set(_ context.Context, id string, info *Info) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[id] = copyInfo(info)
	return nil
}

// Delete removes a session and its associated connections.
// It notifies any active WatchSession watchers with the given reason.
func (m *MemStore) Delete(_ context.Context, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ch := range m.watchers[id] {
		select {
		case ch <- Event{Type: Destroyed, Message: reason}:
		default:
		}
		close(ch)
	}
	delete(m.watchers, id)

	delete(m.sessions, id)
	for connID, conn := range m.connections {
		if conn.SessionID == id {
			delete(m.connections, connID)
		}
	}
	return nil
}

// WatchSession returns a channel that receives an Event when
// the session is destroyed.
func (m *MemStore) WatchSession(_ context.Context, sessionID string) (<-chan Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan Event, 1)
	m.watchers[sessionID] = append(m.watchers[sessionID], ch)
	return ch, nil
}

// FindByCharacter returns the active or detached session for a character.
func (m *MemStore) FindByCharacter(_ context.Context, characterID ulid.ULID) (*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, info := range m.sessions {
		if info.CharacterID == characterID &&
			(info.Status == StatusActive || info.Status == StatusDetached) {
			return copyInfo(info), nil
		}
	}
	return nil, oops.Code("SESSION_NOT_FOUND").
		With("character_id", characterID.String()).
		Errorf("no active or detached session for character")
}

// ListByPlayer returns all non-expired sessions. MemStore does not track
// player-to-character relationships, so this returns all non-expired sessions.
// TODO: filter by playerID when player-character relationship table exists.
func (m *MemStore) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*Info, error) {
	// MemStore does not track player-to-character relationships.
	// This is a stub that returns all non-expired sessions.
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Info
	for _, info := range m.sessions {
		if info.Status != StatusExpired {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

// ListExpired returns all detached sessions past their expiry time.
func (m *MemStore) ListExpired(_ context.Context) ([]*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusDetached && info.ExpiresAt != nil && now.After(*info.ExpiresAt) {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

// UpdateStatus transitions a session's status.
func (m *MemStore) UpdateStatus(_ context.Context, id string, status Status,
	detachedAt *time.Time, expiresAt *time.Time,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.Status = status
	info.DetachedAt = detachedAt
	info.ExpiresAt = expiresAt
	info.UpdatedAt = time.Now()
	return nil
}

// ReattachCAS atomically transitions a detached session to active.
// Returns true if the transition succeeded, false if the session was not detached.
func (m *MemStore) ReattachCAS(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return false, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	if info.Status != StatusDetached {
		return false, nil
	}
	info.Status = StatusActive
	info.DetachedAt = nil
	info.ExpiresAt = nil
	info.UpdatedAt = time.Now()
	return true, nil
}

// UpdateCursors updates the event cursors for a session.
func (m *MemStore) UpdateCursors(_ context.Context, id string, cursors map[string]ulid.ULID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	if info.EventCursors == nil {
		info.EventCursors = make(map[string]ulid.ULID)
	}
	for k, v := range cursors {
		info.EventCursors[k] = v
	}
	return nil
}

// AppendCommand adds a command to the session's history, enforcing the cap.
func (m *MemStore) AppendCommand(_ context.Context, id, command string, maxHistory int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.CommandHistory = append(info.CommandHistory, command)
	if len(info.CommandHistory) > maxHistory {
		info.CommandHistory = info.CommandHistory[len(info.CommandHistory)-maxHistory:]
	}
	return nil
}

// GetCommandHistory returns the session's command history.
func (m *MemStore) GetCommandHistory(_ context.Context, id string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.sessions[id]
	if !ok {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	result := make([]string, len(info.CommandHistory))
	copy(result, info.CommandHistory)
	return result, nil
}

// validClientTypes is the set of allowed client_type values for MemStore.
var validClientTypes = map[string]bool{
	"terminal":  true,
	"comms_hub": true,
	"telnet":    true,
}

// AddConnection registers a new connection to a session.
func (m *MemStore) AddConnection(_ context.Context, conn *Connection) error {
	if !validClientTypes[conn.ClientType] {
		return oops.With("operation", "add connection").
			With("client_type", conn.ClientType).
			Errorf("invalid client_type %q: must be one of terminal, comms_hub, telnet", conn.ClientType)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cp := *conn
	cp.Streams = make([]string, len(conn.Streams))
	copy(cp.Streams, conn.Streams)
	m.connections[conn.ID] = &cp
	return nil
}

// RemoveConnection removes a connection from a session.
func (m *MemStore) RemoveConnection(_ context.Context, connectionID ulid.ULID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.connections, connectionID)
	return nil
}

// CountConnections returns the number of active connections for a session.
func (m *MemStore) CountConnections(_ context.Context, sessionID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, conn := range m.connections {
		if conn.SessionID == sessionID {
			count++
		}
	}
	return count, nil
}

// CountConnectionsByType returns the number of active connections of a specific client type for a session.
func (m *MemStore) CountConnectionsByType(_ context.Context, sessionID, clientType string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, conn := range m.connections {
		if conn.SessionID == sessionID && conn.ClientType == clientType {
			count++
		}
	}
	return count, nil
}

// UpdateGridPresent sets the grid_present flag on a session.
func (m *MemStore) UpdateGridPresent(_ context.Context, id string, present bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").
			With("session_id", id).
			Errorf("session not found")
	}
	info.GridPresent = present
	info.UpdatedAt = time.Now()
	return nil
}

// ListActiveByLocation returns active sessions whose LocationID matches.
func (m *MemStore) ListActiveByLocation(_ context.Context, locationID ulid.ULID) ([]*Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Info
	for _, info := range m.sessions {
		if info.Status == StatusActive && info.LocationID == locationID {
			result = append(result, copyInfo(info))
		}
	}
	return result, nil
}

// copyInfo returns a defensive copy of an Info to prevent external modification.
func copyInfo(info *Info) *Info {
	cursors := make(map[string]ulid.ULID, len(info.EventCursors))
	for k, v := range info.EventCursors {
		cursors[k] = v
	}
	history := make([]string, len(info.CommandHistory))
	copy(history, info.CommandHistory)

	cp := *info
	cp.EventCursors = cursors
	cp.CommandHistory = history
	return &cp
}
