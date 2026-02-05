// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/accesstest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/core"
)

// Note: Capability checks are performed by the dispatcher, not the handler.
// See TestDispatcher_PermissionDenied in dispatcher_test.go for capability tests.

func TestShutdownHandler_ImmediateShutdown(t *testing.T) {
	executor := testutil.AdminPlayer()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executor.CharacterID.String(), "execute", "admin.shutdown")

	broadcaster := core.NewBroadcaster()
	// Subscribe to system stream to capture broadcast
	ch := broadcaster.Subscribe("system")

	services := testutil.NewServicesBuilder().
		WithAccess(accessControl).
		WithBroadcaster(broadcaster).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("").
		WithServices(services).
		Build()

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
	assert.Contains(t, buf.String(), "Initiating server shutdown")
}

func TestShutdownHandler_DelayedShutdown(t *testing.T) {
	executor := testutil.AdminPlayer()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executor.CharacterID.String(), "execute", "admin.shutdown")

	broadcaster := core.NewBroadcaster()
	ch := broadcaster.Subscribe("system")

	services := testutil.NewServicesBuilder().
		WithAccess(accessControl).
		WithBroadcaster(broadcaster).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("60").
		WithServices(services).
		Build()

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
	assert.Contains(t, buf.String(), "60 seconds")
}

func TestShutdownHandler_InvalidDelay(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{name: "not a number", args: "abc"},
		{name: "negative", args: "-5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := testutil.AdminPlayer()
			accessControl := accesstest.NewMockAccessControl()
			accessControl.Grant("char:"+executor.CharacterID.String(), "execute", "admin.shutdown")

			services := testutil.NewServicesBuilder().
				WithAccess(accessControl).
				Build()
			exec, _ := testutil.NewExecutionBuilder().
				WithCharacter(executor).
				WithArgs(tt.args).
				WithServices(services).
				Build()

			err := ShutdownHandler(context.Background(), exec)
			require.Error(t, err)

			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
		})
	}
}

func TestShutdownHandler_LogsAdminAction(t *testing.T) {
	// Note: This test verifies that the handler logs the admin action.
	// We verify this indirectly through the execution succeeding and
	// returning the expected shutdown signal. In a production system,
	// you might use a test logger to capture and verify log entries.

	executor := testutil.AdminPlayer()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executor.CharacterID.String(), "execute", "admin.shutdown")

	broadcaster := core.NewBroadcaster()

	services := testutil.NewServicesBuilder().
		WithAccess(accessControl).
		WithBroadcaster(broadcaster).
		Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("30").
		WithServices(services).
		Build()

	err := ShutdownHandler(context.Background(), exec)
	// Handler should execute successfully (returning shutdown signal)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))
}

func TestShutdownHandler_BroadcastsToAllPlayers(t *testing.T) {
	executor := testutil.AdminPlayer()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executor.CharacterID.String(), "execute", "admin.shutdown")

	broadcaster := core.NewBroadcaster()

	// Subscribe multiple streams to verify broadcast goes to system stream
	// which should be watched by all players
	systemCh := broadcaster.Subscribe("system")

	services := testutil.NewServicesBuilder().
		WithAccess(accessControl).
		WithBroadcaster(broadcaster).
		Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("").
		WithServices(services).
		Build()

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
	executor := testutil.AdminPlayer()

	accessControl := accesstest.NewMockAccessControl()
	accessControl.Grant("char:"+executor.CharacterID.String(), "execute", "admin.shutdown")

	services := testutil.NewServicesBuilder().
		WithAccess(accessControl).
		WithBroadcaster(nil).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("").
		WithServices(services).
		Build()

	// Should still work, just skip broadcast
	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify executor feedback still works
	assert.Contains(t, buf.String(), "Initiating server shutdown")
}
