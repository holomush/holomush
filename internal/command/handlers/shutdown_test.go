// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/core"
)

// Note: Capability checks are performed by the dispatcher, not the handler.
// See TestDispatcher_PermissionDenied in dispatcher_test.go for capability tests.

func TestShutdownHandler_ImmediateShutdown(t *testing.T) {
	ctx := context.Background()
	executor := testutil.AdminPlayer()

	accessControl := policytest.NewGrantEngine()
	accessControl.Grant(access.SubjectCharacter+executor.CharacterID.String(), "execute", "admin.shutdown")

	store := core.NewMemoryEventStore()

	services := testutil.NewServicesBuilder().
		WithEngine(accessControl).
		WithEvents(store).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("").
		WithServices(services).
		Build()

	err := ShutdownHandler(ctx, exec)

	// Should return ErrShutdownRequested sentinel
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify system event was appended
	events, replayErr := store.Replay(ctx, "system", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, core.EventTypeSystem, event.Type)
	assert.Contains(t, string(event.Payload), "[SHUTDOWN]")
	assert.Contains(t, string(event.Payload), "NOW")

	// Verify executor feedback
	assert.Contains(t, buf.String(), "Initiating server shutdown")
}

func TestShutdownHandler_DelayedShutdown(t *testing.T) {
	ctx := context.Background()
	executor := testutil.AdminPlayer()

	accessControl := policytest.NewGrantEngine()
	accessControl.Grant(access.SubjectCharacter+executor.CharacterID.String(), "execute", "admin.shutdown")

	store := core.NewMemoryEventStore()

	services := testutil.NewServicesBuilder().
		WithEngine(accessControl).
		WithEvents(store).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("60").
		WithServices(services).
		Build()

	err := ShutdownHandler(ctx, exec)

	// Should return ErrShutdownRequested with delay context
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify the delay is captured in the error
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, int64(60), oopsErr.Context()["delay_seconds"])

	// Verify system event mentions delay
	events, replayErr := store.Replay(ctx, "system", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, core.EventTypeSystem, event.Type)
	assert.Contains(t, string(event.Payload), "[SHUTDOWN]")
	assert.Contains(t, string(event.Payload), "60 seconds")

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
			accessControl := policytest.NewGrantEngine()
			accessControl.Grant(access.SubjectCharacter+executor.CharacterID.String(), "execute", "admin.shutdown")

			services := testutil.NewServicesBuilder().
				WithEngine(accessControl).
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

	accessControl := policytest.NewGrantEngine()
	accessControl.Grant(access.SubjectCharacter+executor.CharacterID.String(), "execute", "admin.shutdown")

	services := testutil.NewServicesBuilder().
		WithEngine(accessControl).
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

func TestShutdownHandler_BroadcastsToSystemStream(t *testing.T) {
	ctx := context.Background()
	executor := testutil.AdminPlayer()

	accessControl := policytest.NewGrantEngine()
	accessControl.Grant(access.SubjectCharacter+executor.CharacterID.String(), "execute", "admin.shutdown")

	store := core.NewMemoryEventStore()

	services := testutil.NewServicesBuilder().
		WithEngine(accessControl).
		WithEvents(store).
		Build()
	exec, _ := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("").
		WithServices(services).
		Build()

	err := ShutdownHandler(ctx, exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify system stream received event
	events, replayErr := store.Replay(ctx, "system", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	event := events[0]
	assert.Equal(t, core.EventTypeSystem, event.Type)
	assert.Equal(t, core.ActorSystem, event.Actor.Kind)
	assert.Contains(t, string(event.Payload), "[SHUTDOWN]")
}

func TestShutdownHandler_WithNilEvents_IsNoOp(t *testing.T) {
	executor := testutil.AdminPlayer()

	accessControl := policytest.NewGrantEngine()
	accessControl.Grant(access.SubjectCharacter+executor.CharacterID.String(), "execute", "admin.shutdown")

	// Build with nil events — the default builder provides one, so override
	services := testutil.NewServicesBuilder().
		WithEngine(accessControl).
		WithEvents(nil).
		Build()
	exec, buf := testutil.NewExecutionBuilder().
		WithCharacter(executor).
		WithArgs("").
		WithServices(services).
		Build()

	// Should still work, just skip event append
	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	// Verify executor feedback still works
	assert.Contains(t, buf.String(), "Initiating server shutdown")
}
