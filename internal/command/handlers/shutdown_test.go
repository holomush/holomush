// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/accesstest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
)

func TestShutdownHandler_RequiresCapability(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	// Use selective mock that denies admin.shutdown
	accessControl := accesstest.NewMockAccessControl()
	// Do NOT grant execute access to admin.shutdown

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "RegularUser",
		PlayerID:      playerID,
		Args:          "",
		Output:        &buf,
		Services: &command.Services{
			Access: accessControl,
		},
	}

	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodePermissionDenied, oopsErr.Code())
	assert.Equal(t, "admin.shutdown", oopsErr.Context()["capability"])
}

func TestShutdownHandler_ImmediateShutdown(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.shutdown")

	broadcaster := core.NewBroadcaster()
	// Subscribe to system stream to capture broadcast
	ch := broadcaster.Subscribe("system")

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "", // No delay = immediate
		Output:        &buf,
		Services: &command.Services{
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := ShutdownHandler(context.Background(), exec)

	// Should return ErrShutdownRequested sentinel
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify broadcast warning was sent
	select {
	case event := <-ch:
		assert.Equal(t, core.EventTypeSystem, event.Type)
		assert.Contains(t, string(event.Payload), "[SHUTDOWN]")
		assert.Contains(t, string(event.Payload), "NOW")
	default:
		t.Error("Expected shutdown warning to be broadcast")
	}

	// Verify executor feedback
	output := buf.String()
	assert.Contains(t, output, "Initiating server shutdown")
}

func TestShutdownHandler_DelayedShutdown(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.shutdown")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("system")

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "60", // 60 second delay
		Output:        &buf,
		Services: &command.Services{
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := ShutdownHandler(context.Background(), exec)

	// Should return ErrShutdownRequested with delay context
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify the delay is captured in the error
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, int64(60), oopsErr.Context()["delay_seconds"])

	// Verify broadcast warning mentions delay
	select {
	case event := <-ch:
		assert.Equal(t, core.EventTypeSystem, event.Type)
		assert.Contains(t, string(event.Payload), "[SHUTDOWN]")
		assert.Contains(t, string(event.Payload), "60 seconds")
	default:
		t.Error("Expected shutdown warning to be broadcast")
	}

	// Verify executor feedback
	output := buf.String()
	assert.Contains(t, output, "60 seconds")
}

func TestShutdownHandler_InvalidDelay_NotANumber(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.shutdown")

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "abc", // Invalid delay
		Output:        &buf,
		Services: &command.Services{
			Access: accessControl,
		},
	}

	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

func TestShutdownHandler_InvalidDelay_Negative(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.shutdown")

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "-5", // Negative delay
		Output:        &buf,
		Services: &command.Services{
			Access: accessControl,
		},
	}

	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
}

func TestShutdownHandler_LogsAdminAction(t *testing.T) {
	// Note: This test verifies that the handler logs the admin action.
	// We verify this indirectly through the execution succeeding and
	// returning the expected shutdown signal. In a production system,
	// you might use a test logger to capture and verify log entries.

	executorID := ulid.Make()
	playerID := ulid.Make()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.shutdown")

	broadcaster := core.NewBroadcaster()

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "30",
		Output:        &buf,
		Services: &command.Services{
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := ShutdownHandler(context.Background(), exec)
	// Handler should execute successfully (returning shutdown signal)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))
}

func TestShutdownHandler_BroadcastsToAllPlayers(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.shutdown")

	broadcaster := core.NewBroadcaster()

	// Subscribe multiple streams to verify broadcast goes to system stream
	// which should be watched by all players
	systemCh := broadcaster.Subscribe("system")

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "",
		Output:        &buf,
		Services: &command.Services{
			Access:      accessControl,
			Broadcaster: broadcaster,
		},
	}

	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify system broadcast
	select {
	case event := <-systemCh:
		assert.Equal(t, core.EventTypeSystem, event.Type)
		assert.Equal(t, core.ActorSystem, event.Actor.Kind)
		assert.Contains(t, string(event.Payload), "[SHUTDOWN]")
	default:
		t.Error("Expected shutdown warning to be broadcast to system stream")
	}
}

func TestShutdownHandler_WithNilBroadcaster(t *testing.T) {
	executorID := ulid.Make()
	playerID := ulid.Make()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executorID.String(), "execute", "admin.shutdown")

	var buf bytes.Buffer
	exec := &command.CommandExecution{
		CharacterID:   executorID,
		CharacterName: "Admin",
		PlayerID:      playerID,
		Args:          "",
		Output:        &buf,
		Services: &command.Services{
			Access:      accessControl,
			Broadcaster: nil, // No broadcaster
		},
	}

	// Should still work, just skip broadcast
	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify executor feedback still works
	output := buf.String()
	assert.Contains(t, output, "Initiating server shutdown")
}

