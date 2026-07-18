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
)

// Note: Capability checks are performed by the dispatcher, not the handler.
// See TestDispatcher_PermissionDenied in dispatcher_test.go for capability tests.

// broadcastCall records one Broadcast(ctx, subject, message) invocation.
type broadcastCall struct {
	subject string
	message string
}

// fakeBroadcaster is a minimal command.SystemBroadcaster fake that records
// every Broadcast call for assertion. The event construction it stands in
// for (payload shape, actor stamp, event type) is proven once, at the
// builder level, by internal/sysbroadcast's tests — this package only needs
// to prove ShutdownHandler calls Broadcast with the right subject/message.
type fakeBroadcaster struct {
	calls []broadcastCall
	err   error
}

func (f *fakeBroadcaster) Broadcast(_ context.Context, subject, message string) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, broadcastCall{subject: subject, message: message})
	return nil
}

var _ command.SystemBroadcaster = (*fakeBroadcaster)(nil)

func newShutdownExec(t *testing.T, args string, broadcaster command.SystemBroadcaster) (*command.CommandExecution, *bytes.Buffer) {
	t.Helper()

	var buf bytes.Buffer
	svc := command.NewTestServices(command.ServicesConfig{
		Engine:      policytest.AllowAllEngine(),
		Broadcaster: broadcaster,
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
	fb := &fakeBroadcaster{}
	exec, buf := newShutdownExec(t, "", fb)

	err := ShutdownHandler(ctx, exec)

	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	require.Len(t, fb.calls, 1)
	assert.Equal(t, core.SystemBroadcastSubject, fb.calls[0].subject)
	assert.Contains(t, fb.calls[0].message, "[SHUTDOWN]")
	assert.Contains(t, fb.calls[0].message, "NOW")
	assert.Contains(t, buf.String(), "Initiating server shutdown")
}

func TestShutdownHandlerDelayedShutdown(t *testing.T) {
	ctx := context.Background()
	fb := &fakeBroadcaster{}
	exec, buf := newShutdownExec(t, "60", fb)

	err := ShutdownHandler(ctx, exec)

	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, int64(60), oopsErr.Context()["delay_seconds"])

	require.Len(t, fb.calls, 1)
	assert.Contains(t, fb.calls[0].message, "60 seconds")
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
	fb := &fakeBroadcaster{}
	exec, _ := newShutdownExec(t, "", fb)

	err := ShutdownHandler(ctx, exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))

	require.Len(t, fb.calls, 1)
	assert.Equal(t, core.SystemBroadcastSubject, fb.calls[0].subject)
	assert.Contains(t, fb.calls[0].message, "[SHUTDOWN]")
}

func TestShutdownHandlerWithNilEventsIsNoOp(t *testing.T) {
	exec, buf := newShutdownExec(t, "", nil)

	err := ShutdownHandler(context.Background(), exec)
	require.Error(t, err)
	assert.True(t, errors.Is(err, command.ErrShutdownRequested))
	assert.Contains(t, buf.String(), "Initiating server shutdown")
}
