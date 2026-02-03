// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// Session represents a character's ongoing presence in the game.
type Session struct {
	CharacterID  ulid.ULID
	Connections  []ulid.ULID          // Active connection IDs
	EventCursors map[string]ulid.ULID // Last seen event per stream
	LastActivity time.Time            // Last time the session had activity
}

// SessionService provides session management operations.
// This interface allows command handlers to work with sessions while enabling
// mocking for tests.
type SessionService interface {
	// ListActiveSessions returns copies of all active sessions.
	ListActiveSessions() []*Session
	// GetSession returns a copy of a character's session, or nil if none exists.
	GetSession(charID ulid.ULID) *Session
	// EndSession completely removes a character's session from the manager.
	EndSession(charID ulid.ULID) error
}

// copySession returns a defensive copy of a session to prevent external modification.
func copySession(s *Session) *Session {
	cursors := make(map[string]ulid.ULID, len(s.EventCursors))
	for k, v := range s.EventCursors {
		cursors[k] = v
	}
	connections := make([]ulid.ULID, len(s.Connections))
	copy(connections, s.Connections)

	return &Session{
		CharacterID:  s.CharacterID,
		Connections:  connections,
		EventCursors: cursors,
		LastActivity: s.LastActivity,
	}
}

// SessionManager manages character sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[ulid.ULID]*Session // keyed by CharacterID
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[ulid.ULID]*Session),
	}
}

// Connect attaches a connection to a character's session.
// Creates the session if it doesn't exist.
// Returns a copy of the session to prevent external modification.
func (sm *SessionManager) Connect(charID, connID ulid.ULID) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[charID]
	if !exists {
		session = &Session{
			CharacterID:  charID,
			Connections:  make([]ulid.ULID, 0, 1),
			EventCursors: make(map[string]ulid.ULID),
		}
		sm.sessions[charID] = session
	}

	session.Connections = append(session.Connections, connID)
	session.LastActivity = time.Now() // Set for both new and reconnecting sessions

	return copySession(session)
}

// Disconnect removes a connection from a character's session.
// The session persists even with zero connections.
func (sm *SessionManager) Disconnect(charID, connID ulid.ULID) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[charID]
	if !exists {
		slog.Debug("disconnect called for non-existent session",
			"char_id", charID.String(),
			"conn_id", connID.String(),
		)
		return
	}

	// Remove connection
	for i, id := range session.Connections {
		if id == connID {
			session.Connections = append(session.Connections[:i], session.Connections[i+1:]...)
			break
		}
	}
}

// UpdateCursor updates the last seen event for a stream.
func (sm *SessionManager) UpdateCursor(charID ulid.ULID, stream string, eventID ulid.ULID) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[charID]
	if !exists {
		slog.Debug("UpdateCursor called for non-existent session",
			"char_id", charID.String(),
			"stream", stream,
			"event_id", eventID.String(),
		)
		return
	}
	session.EventCursors[stream] = eventID
}

// GetSession returns a copy of a character's session, or nil if none exists.
// Returns a copy to prevent external modification of internal state.
func (sm *SessionManager) GetSession(charID ulid.ULID) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[charID]
	if !exists {
		return nil
	}

	return copySession(session)
}

// GetConnections returns all connection IDs for a character.
func (sm *SessionManager) GetConnections(charID ulid.ULID) []ulid.ULID {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[charID]
	if !exists {
		return nil
	}
	result := make([]ulid.ULID, len(session.Connections))
	copy(result, session.Connections)
	return result
}

// EndSession completely removes a character's session from the manager.
// Returns an error if the session does not exist.
func (sm *SessionManager) EndSession(charID ulid.ULID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[charID]; !exists {
		return oops.Code("SESSION_NOT_FOUND").
			With("char_id", charID.String()).
			Errorf("session not found for character %s", charID.String())
	}

	delete(sm.sessions, charID)
	return nil
}

// UpdateActivity refreshes the last activity time for a character's session.
func (sm *SessionManager) UpdateActivity(charID ulid.ULID) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[charID]
	if !exists {
		slog.Debug("UpdateActivity called for non-existent session",
			"char_id", charID.String(),
		)
		return
	}
	session.LastActivity = time.Now()
}

// ListActiveSessions returns copies of all active sessions.
func (sm *SessionManager) ListActiveSessions() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]*Session, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		result = append(result, copySession(session))
	}
	return result
}
