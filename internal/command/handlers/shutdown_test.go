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

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/core/coretest"
	"github.com/holomush/holomush/internal/eventvocab"
)

// Note: Capability checks are performed by the dispatcher, not the handler.
// See TestDispatcher_PermissionDenied in dispatcher_test.go for capability tests.

func newShutdownExec(t *testing.T, args string, store core.EventAppender) (*command.CommandExecution, *bytes.Buffer) {
	t.Helper()

	var buf bytes.Buffer
	svc := command.NewTestServices(command.ServicesConfig{
		Engine: policytest.AllowAllEngine(),
		Events: store,
	})
	exec := command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:   ulid.Make(),
		CharacterName: "Admin",
		PlayerID:      ulid.Make(),
		Args:          args,
		Output:        &buf,
		Services:      svc,
	})
	return exec, &buf
}

func TestShutdownHandlerImmediateShutdown(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewMemoryEventStore()
	exec, buf := newShutdownExec(t, "", store)

	err := ShutdownHandler(ctx, exec)

	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	events, replayErr := store.Replay(ctx, "system", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	assert.Equal(t, eventvocab.EventTypeSystem, events[0].Type)
	assert.Contains(t, string(events[0].Payload), "[SHUTDOWN]")
	assert.Contains(t, string(events[0].Payload), "NOW")
	assert.Contains(t, buf.String(), "Initiating server shutdown")
}

func TestShutdownHandlerDelayedShutdown(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewMemoryEventStore()
	exec, buf := newShutdownExec(t, "60", store)

	err := ShutdownHandler(ctx, exec)

	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, int64(60), oopsErr.Context()["delay_seconds"])

	events, replayErr := store.Replay(ctx, "system", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	assert.Contains(t, string(events[0].Payload), "60 seconds")
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
			exec, _ := newShutdownExec(t, tt.args, nil)

			err := ShutdownHandler(context.Background(), exec)
			require.Error(t, err)

			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, command.CodeInvalidArgs, oopsErr.Code())
		})
	}
}

func TestShutdownHandlerBroadcastsToSystemStream(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewMemoryEventStore()
	exec, _ := newShutdownExec(t, "", store)

	err := ShutdownHandler(ctx, exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	events, replayErr := store.Replay(ctx, "system", ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	require.Len(t, events, 1)
	assert.Equal(t, eventvocab.EventTypeSystem, events[0].Type)
	assert.Equal(t, core.ActorSystem, events[0].Actor.Kind)
	assert.Contains(t, string(events[0].Payload), "[SHUTDOWN]")
}

func TestShutdownHandlerWithNilEventsIsNoOp(t *testing.T) {
	exec, buf := newShutdownExec(t, "", nil)

	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))
	assert.Contains(t, buf.String(), "Initiating server shutdown")
}
