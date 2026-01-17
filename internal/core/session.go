package core

import (
	"log/slog"
	"sync"

	"github.com/oklog/ulid/v2"
)

// Session represents a character's ongoing presence in the game.
type Session struct {
	CharacterID  ulid.ULID
	Connections  []ulid.ULID          // Active connection IDs
	EventCursors map[string]ulid.ULID // Last seen event per stream
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
