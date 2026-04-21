// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestSessionStreamRegistrySendDeliversToRegisteredSession(t *testing.T) {
	r := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1)
	r.Register("sess-1", ch)

	err := r.Send("sess-1", sessionStreamUpdate{stream: "channel:abc", add: true})
	require.NoError(t, err)

	update := <-ch
	assert.Equal(t, "channel:abc", update.stream)
	assert.True(t, update.add)
}

func TestSessionStreamRegistrySendReturnsNotFoundForUnknownSession(t *testing.T) {
	r := NewSessionStreamRegistry()
	err := r.Send("missing", sessionStreamUpdate{stream: "channel:abc", add: true})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestSessionStreamRegistrySendReturnsNotFoundAfterDeregister(t *testing.T) {
	r := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1)
	r.Register("sess-1", ch)
	r.Deregister("sess-1", ch)

	err := r.Send("sess-1", sessionStreamUpdate{stream: "channel:abc", add: true})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestSessionStreamRegistrySendReturnsChannelFullWhenBufferExhausted(t *testing.T) {
	r := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1) // buffer of 1
	r.Register("sess-1", ch)

	// Fill the buffer
	err := r.Send("sess-1", sessionStreamUpdate{stream: "channel:abc", add: true})
	require.NoError(t, err)

	// Second send to full buffer should return error immediately
	err = r.Send("sess-1", sessionStreamUpdate{stream: "channel:def", add: true})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONTROL_CHANNEL_FULL")
}

func TestSessionStreamRegistryAddStreamDelegatesToSend(t *testing.T) {
	r := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1)
	r.Register("sess-1", ch)

	err := r.AddStream(context.Background(), "sess-1", "channel:abc")
	require.NoError(t, err)
	update := <-ch
	assert.Equal(t, "channel:abc", update.stream)
	assert.True(t, update.add)
}

func TestSessionStreamRegistryRemoveStreamDelegatesToSend(t *testing.T) {
	r := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1)
	r.Register("sess-1", ch)

	err := r.RemoveStream(context.Background(), "sess-1", "channel:abc")
	require.NoError(t, err)
	update := <-ch
	assert.Equal(t, "channel:abc", update.stream)
	assert.False(t, update.add)
}

func TestStreamSenderAdapterSendPassesFromCursorToRegistry(t *testing.T) {
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	adapter := NewStreamSenderAdapter(reg)
	err := adapter.Send("sess-1", "scene:abc:ic", true, focus.ReplayModeFromCursor)
	require.NoError(t, err)

	update := <-ch
	assert.Equal(t, "scene:abc:ic", update.stream)
	assert.True(t, update.add)
	assert.Equal(t, focus.ReplayModeFromCursor, update.replayMode)
}

func TestStreamSenderAdapterSendRejectsUnsupportedModeOnAdd(t *testing.T) {
	// Adapter must enforce the same replay-mode contract as the registry.
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	adapter := NewStreamSenderAdapter(reg)
	err := adapter.Send("sess-1", "scene:abc:ic", true, focus.ReplayModeBoundedTail)
	errutil.AssertErrorCode(t, err, "REPLAY_MODE_NOT_SUPPORTED")
}

func TestStreamSenderAdapterSendReturnsErrorForMissingSession(t *testing.T) {
	reg := NewSessionStreamRegistry()
	adapter := NewStreamSenderAdapter(reg)
	err := adapter.Send("nonexistent", "stream", true, focus.ReplayModeFromCursor)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestAddStreamDefaultsToFromCursor(t *testing.T) {
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	err := reg.AddStream(context.Background(), "sess-1", "character:abc")
	require.NoError(t, err)

	update := <-ch
	assert.Equal(t, focus.ReplayModeFromCursor, update.replayMode)
}

func TestAddStreamWithModeAcceptsFromCursor(t *testing.T) {
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 4)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	err := reg.AddStreamWithMode(context.Background(), "sess-1", "channel:x", session.ReplayModeFromCursor)
	require.NoError(t, err)

	update := <-ch
	assert.Equal(t, "channel:x", update.stream)
	assert.True(t, update.add)
	assert.Equal(t, focus.ReplayModeFromCursor, update.replayMode)
}

func TestAddStreamWithModeRejectsUnsupportedModes(t *testing.T) {
	// Post-F3 only ReplayModeFromCursor is honoured — BoundedTail /
	// LiveOnly must fail explicitly rather than silently downgrade.
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 4)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	err := reg.AddStreamWithMode(context.Background(), "sess-1", "channel:x", session.ReplayModeLiveOnly)
	errutil.AssertErrorCode(t, err, "REPLAY_MODE_NOT_SUPPORTED")

	err = reg.AddStreamWithMode(context.Background(), "sess-1", "channel:x", session.ReplayModeBoundedTail)
	errutil.AssertErrorCode(t, err, "REPLAY_MODE_NOT_SUPPORTED")
}

func TestSendCarriesReplayMode(t *testing.T) {
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 1)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	err := reg.Send("sess-1", sessionStreamUpdate{
		stream:     "scene:abc:ic",
		add:        true,
		replayMode: focus.ReplayModeBoundedTail,
	})
	require.NoError(t, err)

	update := <-ch
	assert.Equal(t, "scene:abc:ic", update.stream)
	assert.True(t, update.add)
	assert.Equal(t, focus.ReplayModeBoundedTail, update.replayMode)
}
