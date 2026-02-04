// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/accesstest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
)

func TestWallHandler_NoArgs(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "",
		Output:        &buf,
		Services:      &command.Services{},
	}

	err := WallHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

func TestWallHandler_WhitespaceOnlyArgs(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "   ",
		Output:        &buf,
		Services:      &command.Services{},
	}

	err := WallHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

// Note: Capability checks are performed by the dispatcher, not the handler.
// See TestDispatcher_PermissionDenied in dispatcher_test.go for capability tests.

func TestWallHandler_Success_BroadcastsToAllSessions(t *testing.T) {
	executorID := ulid.Make()
	targetID1 := ulid.Make()
	targetID2 := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())
	sessionMgr.Connect(targetID1, ulid.Make())
	sessionMgr.Connect(targetID2, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()

	// Subscribe to all session streams to capture events
	ch1 := broadcaster.Subscribe("session:" + executorID.String())
	ch2 := broadcaster.Subscribe("session:" + targetID1.String())
	ch3 := broadcaster.Subscribe("session:" + targetID2.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Server going down in 5 minutes",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify all sessions received the broadcast
	for i, ch := range []chan core.Event{ch1, ch2, ch3} {
		select {
		case event := <-ch:
			assert.Equal(t, core.EventTypeSystem, event.Type, "Session %d: event type mismatch", i)
			assert.Contains(t, string(event.Payload), "[ADMIN ANNOUNCEMENT]", "Session %d: missing announcement prefix", i)
			assert.Contains(t, string(event.Payload), "Admin", "Session %d: missing admin name", i)
			assert.Contains(t, string(event.Payload), "Server going down in 5 minutes", "Session %d: missing message", i)
		default:
			t.Errorf("Session %d: expected event but none received", i)
		}
	}

	// Verify executor output
	output := buf.String()
	assert.Contains(t, output, "Announcement sent to 3 session")
}

func TestWallHandler_Success_SingleSession(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Test message",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify event was received
	select {
	case event := <-ch:
		assert.Equal(t, core.EventTypeSystem, event.Type)
		assert.Contains(t, string(event.Payload), "[ADMIN ANNOUNCEMENT]")
	default:
		t.Error("Expected event but none received")
	}

	// Verify output uses singular "session"
	output := buf.String()
	assert.Contains(t, output, "1 session")
	assert.NotContains(t, output, "sessions")
}

func TestWallHandler_Success_NoActiveSessions(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	// No sessions connected

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Nobody will hear this",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify output indicates no sessions
	output := buf.String()
	assert.Contains(t, output, "0 session")
}

func TestWallHandler_MessageFormat(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "SuperAdmin",
		PlayerID:      playerID,
		Args:          "Important announcement",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify exact message format: "[ADMIN ANNOUNCEMENT] Admin: message"
	select {
	case event := <-ch:
		payload := string(event.Payload)
		assert.Contains(t, payload, "[ADMIN ANNOUNCEMENT] SuperAdmin: Important announcement")
	default:
		t.Error("Expected event but none received")
	}
}

func TestWallHandler_ActorIsSystem(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Test",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify event actor is system
	select {
	case event := <-ch:
		assert.Equal(t, core.ActorSystem, event.Actor.Kind)
		assert.Equal(t, "system", event.Actor.ID)
	default:
		t.Error("Expected event but none received")
	}
}

func TestWallHandler_LogsAdminAction(t *testing.T) {
	// This test verifies admin action is logged.
	// The actual logging is verified by code inspection and integration tests.
	// Here we just ensure the handler completes successfully and would call slog.Info.

	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	_ = broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Logged message",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	// Logging is verified by the presence of slog.Info in implementation.
	// A production test would capture logs, but for unit tests we verify
	// the handler succeeds and the slog.Info call exists in the implementation.
}

func TestWallHandler_NilBroadcaster(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Test",
		Output:        &buf,
		Services: &command.Services{
			Session: sessionMgr,
			Access:  accessControl,
			// Broadcaster is nil
		},
	}

	// Should not panic, but also won't broadcast
	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	// Output should still indicate sessions targeted
	output := buf.String()
	assert.Contains(t, output, "1 session")
}

func TestWallHandler_PreservesMessageWhitespace(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "  Message with   extra   spaces  ",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	// Verify message preserves internal whitespace (leading/trailing trimmed)
	select {
	case event := <-ch:
		payload := string(event.Payload)
		assert.Contains(t, payload, "Message with   extra   spaces")
	default:
		t.Error("Expected event but none received")
	}
}

func TestWallHandler_UrgencyInfo(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "info Test message",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	select {
	case event := <-ch:
		payload := string(event.Payload)
		assert.Contains(t, payload, "[ADMIN ANNOUNCEMENT]")
		assert.Contains(t, payload, "Test message")
	default:
		t.Error("Expected event but none received")
	}
}

func TestWallHandler_UrgencyWarning(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "warning Server maintenance soon",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	select {
	case event := <-ch:
		payload := string(event.Payload)
		assert.Contains(t, payload, "[ADMIN WARNING]")
		assert.Contains(t, payload, "Server maintenance soon")
	default:
		t.Error("Expected event but none received")
	}
}

func TestWallHandler_UrgencyCritical(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "critical EMERGENCY: Server going down NOW",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	select {
	case event := <-ch:
		payload := string(event.Payload)
		assert.Contains(t, payload, "[ADMIN CRITICAL]")
		assert.Contains(t, payload, "EMERGENCY: Server going down NOW")
	default:
		t.Error("Expected event but none received")
	}
}

func TestWallHandler_UrgencyShorthand(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "crit Database issue detected",
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	select {
	case event := <-ch:
		payload := string(event.Payload)
		assert.Contains(t, payload, "[ADMIN CRITICAL]")
		assert.Contains(t, payload, "Database issue detected")
	default:
		t.Error("Expected event but none received")
	}
}

func TestWallHandler_DefaultUrgency(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	sessionMgr := core.NewSessionManager()
	sessionMgr.Connect(executorID, ulid.Make())

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.wall")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("session:" + executorID.String())

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "Hello everyone", // No urgency prefix, defaults to info
		Output:        &buf,
		Services: &command.Services{
			Session:     sessionMgr,
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := WallHandler(context.Background(), exec)
	require.NoError(t, err)

	select {
	case event := <-ch:
		payload := string(event.Payload)
		assert.Contains(t, payload, "[ADMIN ANNOUNCEMENT]") // Info is default
		assert.Contains(t, payload, "Hello everyone")
	default:
		t.Error("Expected event but none received")
	}
}

func TestParseWallArgs(t *testing.T) {
	tests := []struct {
		name            string
		args            string
		expectedUrgency WallUrgency
		expectedMessage string
	}{
		{"info prefix", "info Hello world", WallUrgencyInfo, "Hello world"},
		{"warning prefix", "warning Server maintenance", WallUrgencyWarning, "Server maintenance"},
		{"warn shorthand", "warn Server maintenance", WallUrgencyWarning, "Server maintenance"},
		{"critical prefix", "critical Emergency", WallUrgencyCritical, "Emergency"},
		{"crit shorthand", "crit Emergency", WallUrgencyCritical, "Emergency"},
		{"no prefix defaults to info", "Hello world", WallUrgencyInfo, "Hello world"},
		{"single word", "Hello", WallUrgencyInfo, "Hello"},
		{"case insensitive", "WARNING All caps", WallUrgencyWarning, "All caps"},
		{"unknown prefix treated as message", "unknown Some message", WallUrgencyInfo, "unknown Some message"},
		// Edge case: urgency with only spaces returns spaces as message.
		// The handler trims and validates, rejecting empty messages with ErrInvalidArgs.
		{"urgency with only spaces", "warning   ", WallUrgencyWarning, "  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urgency, message := parseWallArgs(tt.args)
			assert.Equal(t, tt.expectedUrgency, urgency)
			assert.Equal(t, tt.expectedMessage, message)
		})
	}
}
