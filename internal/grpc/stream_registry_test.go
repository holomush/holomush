// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
