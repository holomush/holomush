// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionManager_Connect(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	session := sm.Connect(charID, connID)
	require.NotNil(t, session, "Expected session, got nil")
	assert.Equal(t, charID, session.CharacterID)
	assert.Len(t, session.Connections, 1)
}

func TestSessionManager_Reconnect(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	conn1 := NewULID()
	conn2 := NewULID()

	// First connection
	session1 := sm.Connect(charID, conn1)

	// Update cursor
	eventID := NewULID()
	sm.UpdateCursor(charID, "location:test", eventID)

	// Disconnect
	sm.Disconnect(charID, conn1)

	// Reconnect with new connection
	session2 := sm.Connect(charID, conn2)

	// Should be same session with preserved cursor
	assert.Equal(t, session1.CharacterID, session2.CharacterID, "Should be same session")
	assert.Equal(t, eventID, session2.EventCursors["location:test"], "Cursor should be preserved")
}

func TestSessionManager_MultipleConnections(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	conn1 := NewULID()
	conn2 := NewULID()

	sm.Connect(charID, conn1)
	session := sm.Connect(charID, conn2)

	assert.Len(t, session.Connections, 2)
}

func TestSessionManager_DefensiveCopy(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	// Get session via Connect
	session1 := sm.Connect(charID, connID)

	// Modify the returned session
	session1.Connections = append(session1.Connections, NewULID())
	session1.EventCursors["modified"] = NewULID()

	// Get session again via GetSession
	session2 := sm.GetSession(charID)

	// Internal state should be unchanged
	assert.Len(t, session2.Connections, 1, "Expected 1 connection (internal unchanged)")
	assert.NotContains(t, session2.EventCursors, "modified", "Internal EventCursors should not contain 'modified' key")

	// Verify GetSession also returns defensive copy
	session2.Connections = append(session2.Connections, NewULID())
	session3 := sm.GetSession(charID)
	assert.Len(t, session3.Connections, 1, "Expected 1 connection after modifying GetSession result")
}

func TestSessionManager_GetConnections_DefensiveCopy(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	sm.Connect(charID, connID)

	// Get connections and modify the returned slice
	conns := sm.GetConnections(charID)
	require.Len(t, conns, 1, "Expected 1 connection initially")
	conns = append(conns, NewULID()) // Modify the returned slice

	// Internal state should be unchanged despite modification
	conns2 := sm.GetConnections(charID)
	assert.Len(t, conns2, 1, "Expected 1 connection (internal unchanged)")
	// Verify we actually modified the first slice
	assert.Len(t, conns, 2, "Expected modified slice to have 2 connections")
}

func TestSessionManager_Disconnect_NonExistentSession(_ *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	// Disconnect from non-existent session should not panic
	sm.Disconnect(charID, connID)
	// No assertion - just verify no panic occurs
}

func TestSessionManager_Disconnect_NonExistentConnection(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()
	otherConnID := NewULID()

	// Create session with one connection
	sm.Connect(charID, connID)

	// Disconnect a different connection that doesn't exist
	sm.Disconnect(charID, otherConnID)

	// Original connection should still be there
	session := sm.GetSession(charID)
	assert.Len(t, session.Connections, 1)
	assert.Equal(t, connID, session.Connections[0], "Original connection should still exist")
}

func TestSessionManager_UpdateCursor_NonExistentSession(_ *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	eventID := NewULID()

	// UpdateCursor for non-existent session should not panic
	sm.UpdateCursor(charID, "location:test", eventID)
	// No assertion - just verify no panic occurs
}

func TestSessionManager_GetSession_NonExistent(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()

	session := sm.GetSession(charID)
	assert.Nil(t, session, "Expected nil for non-existent session")
}

func TestSessionManager_GetConnections_NonExistent(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()

	conns := sm.GetConnections(charID)
	assert.Nil(t, conns, "Expected nil for non-existent session")
}

func TestSessionManager_EndSession(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	// Create session
	sm.Connect(charID, connID)
	require.NotNil(t, sm.GetSession(charID), "Session should exist before EndSession")

	// End session
	err := sm.EndSession(charID)
	require.NoError(t, err)

	// Verify session is gone
	assert.Nil(t, sm.GetSession(charID), "Session should not exist after EndSession")
}

func TestSessionManager_EndSession_NonExistent(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()

	// End non-existent session should return error
	err := sm.EndSession(charID)
	require.Error(t, err)

	// Verify error code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "Expected oops error")
	assert.Equal(t, "SESSION_NOT_FOUND", oopsErr.Code())
}

func TestSessionManager_EndSession_MultipleConnections(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	conn1 := NewULID()
	conn2 := NewULID()

	// Create session with multiple connections
	sm.Connect(charID, conn1)
	sm.Connect(charID, conn2)
	session := sm.GetSession(charID)
	require.Len(t, session.Connections, 2, "Should have 2 connections before EndSession")

	// End session
	err := sm.EndSession(charID)
	require.NoError(t, err)

	// Verify session is completely gone
	assert.Nil(t, sm.GetSession(charID), "Session should not exist after EndSession")
	assert.Nil(t, sm.GetConnections(charID), "Connections should not exist after EndSession")
}

func TestSessionManager_Connect_SetsLastActivity(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	session := sm.Connect(charID, connID)
	require.NotNil(t, session)

	// LastActivity should be set and recent
	assert.False(t, session.LastActivity.IsZero(), "LastActivity should be set on connect")
}

func TestSessionManager_UpdateActivity(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	// Create session
	session1 := sm.Connect(charID, connID)
	originalActivity := session1.LastActivity

	// Update activity
	sm.UpdateActivity(charID)

	// Get session and verify activity was updated
	session2 := sm.GetSession(charID)
	assert.True(t, session2.LastActivity.After(originalActivity) || session2.LastActivity.Equal(originalActivity),
		"LastActivity should be updated or equal after UpdateActivity")
}

func TestSessionManager_UpdateActivity_NonExistentSession(_ *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()

	// UpdateActivity for non-existent session should not panic
	sm.UpdateActivity(charID)
	// No assertion - just verify no panic occurs
}

func TestSessionManager_ListActiveSessions_Empty(t *testing.T) {
	sm := NewSessionManager()

	sessions := sm.ListActiveSessions()
	assert.Empty(t, sessions, "Expected empty list for no sessions")
}

func TestSessionManager_ListActiveSessions_MultipleSessions(t *testing.T) {
	sm := NewSessionManager()

	char1 := NewULID()
	char2 := NewULID()
	char3 := NewULID()
	conn1 := NewULID()
	conn2 := NewULID()
	conn3 := NewULID()

	sm.Connect(char1, conn1)
	sm.Connect(char2, conn2)
	sm.Connect(char3, conn3)

	sessions := sm.ListActiveSessions()
	assert.Len(t, sessions, 3, "Expected 3 active sessions")

	// Verify all character IDs are present
	charIDs := make(map[ulid.ULID]bool)
	for _, s := range sessions {
		charIDs[s.CharacterID] = true
	}
	assert.True(t, charIDs[char1], "char1 should be in sessions")
	assert.True(t, charIDs[char2], "char2 should be in sessions")
	assert.True(t, charIDs[char3], "char3 should be in sessions")
}

func TestSessionManager_ListActiveSessions_DefensiveCopy(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	sm.Connect(charID, connID)

	// Get sessions and modify returned slice
	sessions := sm.ListActiveSessions()
	require.Len(t, sessions, 1)
	sessions[0].Connections = append(sessions[0].Connections, NewULID())

	// Internal state should be unchanged
	sessions2 := sm.ListActiveSessions()
	assert.Len(t, sessions2[0].Connections, 1, "Internal session connections should be unchanged")
}
