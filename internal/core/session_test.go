// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"
)

func TestSessionManager_Connect(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	session := sm.Connect(charID, connID)
	if session == nil {
		t.Fatal("Expected session, got nil")
	}
	if session.CharacterID != charID {
		t.Errorf("CharacterID mismatch")
	}
	if len(session.Connections) != 1 {
		t.Errorf("Expected 1 connection, got %d", len(session.Connections))
	}
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
	if session2.CharacterID != session1.CharacterID {
		t.Error("Should be same session")
	}
	if session2.EventCursors["location:test"] != eventID {
		t.Error("Cursor should be preserved")
	}
}

func TestSessionManager_MultipleConnections(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	conn1 := NewULID()
	conn2 := NewULID()

	sm.Connect(charID, conn1)
	session := sm.Connect(charID, conn2)

	if len(session.Connections) != 2 {
		t.Errorf("Expected 2 connections, got %d", len(session.Connections))
	}
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
	if len(session2.Connections) != 1 {
		t.Errorf("Expected 1 connection (internal unchanged), got %d", len(session2.Connections))
	}
	if _, exists := session2.EventCursors["modified"]; exists {
		t.Error("Internal EventCursors should not contain 'modified' key")
	}

	// Verify GetSession also returns defensive copy
	session2.Connections = append(session2.Connections, NewULID())
	session3 := sm.GetSession(charID)
	if len(session3.Connections) != 1 {
		t.Errorf("Expected 1 connection after modifying GetSession result, got %d", len(session3.Connections))
	}
}

func TestSessionManager_GetConnections_DefensiveCopy(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()
	connID := NewULID()

	sm.Connect(charID, connID)

	// Get connections and modify the returned slice
	conns := sm.GetConnections(charID)
	if len(conns) != 1 {
		t.Fatalf("Expected 1 connection initially, got %d", len(conns))
	}
	conns = append(conns, NewULID()) // Modify the returned slice

	// Internal state should be unchanged despite modification
	conns2 := sm.GetConnections(charID)
	if len(conns2) != 1 {
		t.Errorf("Expected 1 connection (internal unchanged), got %d", len(conns2))
	}
	// Verify we actually modified the first slice
	if len(conns) != 2 {
		t.Errorf("Expected modified slice to have 2 connections, got %d", len(conns))
	}
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
	if len(session.Connections) != 1 {
		t.Errorf("Expected 1 connection, got %d", len(session.Connections))
	}
	if session.Connections[0] != connID {
		t.Error("Original connection should still exist")
	}
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
	if session != nil {
		t.Error("Expected nil for non-existent session")
	}
}

func TestSessionManager_GetConnections_NonExistent(t *testing.T) {
	sm := NewSessionManager()

	charID := NewULID()

	conns := sm.GetConnections(charID)
	if conns != nil {
		t.Error("Expected nil for non-existent session")
	}
}
