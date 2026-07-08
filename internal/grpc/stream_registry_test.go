// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
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

// TestAddStreamWithModeAcceptsLiveOnly proves HIGH-2: a mid-session LIVE_ONLY add
// is accepted (no REPLAY_MODE_NOT_SUPPORTED) and carries the mode as an add
// update. The no-history-flood guarantee is structural (SetFilters preserves the
// live consumer's start policy on filter rotation) and covered by the subscriber
// SetFilters tests; here we lock that the registry accepts and forwards the mode.
func TestAddStreamWithModeAcceptsLiveOnly(t *testing.T) {
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 4)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	err := reg.AddStreamWithMode(context.Background(), "sess-1", "channel.x", session.ReplayModeLiveOnly)
	require.NoError(t, err)

	update := <-ch
	assert.Equal(t, "channel.x", update.stream)
	assert.True(t, update.add)
	assert.Equal(t, focus.ReplayModeLiveOnly, update.replayMode)
}

func TestAddStreamWithModeRejectsUnsupportedModes(t *testing.T) {
	// Post-F3 FROM_CURSOR (scenes) and LIVE_ONLY (channels, HIGH-2) are honoured;
	// BoundedTail remains unsupported and must fail explicitly rather than
	// silently downgrade.
	reg := NewSessionStreamRegistry()
	ch := make(chan sessionStreamUpdate, 4)
	reg.Register("sess-1", ch)
	defer reg.Deregister("sess-1", ch)

	err := reg.AddStreamWithMode(context.Background(), "sess-1", "channel.x", session.ReplayModeBoundedTail)
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

func TestSendToConnection_TargetsOneConnectionOnly(t *testing.T) {
	t.Parallel()
	// INV-SCENE-23: SendToConnection delivers update to EXACTLY the named
	// connection's channel; other connections in the same session do
	// NOT receive the update via this path.
	r := NewSessionStreamRegistry()
	sessionID := "sess-stc"
	connA := ulid.Make()
	connB := ulid.Make()
	chA := make(chan sessionStreamUpdate, 1)
	chB := make(chan sessionStreamUpdate, 1)

	r.RegisterConnection(sessionID, connA, chA)
	r.RegisterConnection(sessionID, connB, chB)

	err := r.SendToConnection(sessionID, connA, sessionStreamUpdate{stream: "events.main.scene.X.ic", add: true})
	require.NoError(t, err)

	select {
	case upd := <-chA:
		assert.Equal(t, "events.main.scene.X.ic", upd.stream)
		assert.True(t, upd.add)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected delivery to connA's channel")
	}

	select {
	case upd := <-chB:
		t.Fatalf("INV-SCENE-23 violated: connB received SendToConnection meant for connA: %+v", upd)
	case <-time.After(50 * time.Millisecond):
		// good — connB did NOT receive
	}
}

func TestSendToConnection_ReturnsConnectionNotRegistered(t *testing.T) {
	t.Parallel()
	r := NewSessionStreamRegistry()
	err := r.SendToConnection("sess-x", ulid.Make(), sessionStreamUpdate{stream: "s", add: true})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONNECTION_NOT_REGISTERED")
}

func TestSend_StillBroadcastsForSessionWideCallers(t *testing.T) {
	t.Parallel()
	// Regression: existing Send (session-wide broadcast) MUST be
	// unchanged by Phase 5 additions.
	r := NewSessionStreamRegistry()
	sessionID := "sess-broadcast"
	ch1 := make(chan sessionStreamUpdate, 1)
	ch2 := make(chan sessionStreamUpdate, 1)
	r.Register(sessionID, ch1)
	r.Register(sessionID, ch2)

	require.NoError(t, r.Send(sessionID, sessionStreamUpdate{stream: "ambient", add: true}))

	for _, ch := range []chan sessionStreamUpdate{ch1, ch2} {
		select {
		case upd := <-ch:
			assert.Equal(t, "ambient", upd.stream)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Send broadcast regression: subscriber missed the update")
		}
	}
}

// TestSessionStreamRegistryDeregisterConnectionGuardsByChannelIdentity
// pins the reconnect-race fix from CodeRabbit PR #4191 round 6: if a
// reconnect re-registers the same (sessionID, connectionID) key before
// the old goroutine's deferred DeregisterConnection fires, the stale
// defer MUST NOT clobber the new mapping — or the live SendToConnection
// would surface CONNECTION_NOT_REGISTERED on the active stream.
func TestSessionStreamRegistryDeregisterConnectionGuardsByChannelIdentity(t *testing.T) {
	r := NewSessionStreamRegistry()
	sessionID := "sess-reconnect-race"
	connID := ulid.Make()

	oldCh := make(chan sessionStreamUpdate, 1)
	newCh := make(chan sessionStreamUpdate, 1)

	// Initial registration.
	r.RegisterConnection(sessionID, connID, oldCh)
	// Reconnect: same key, new channel.
	r.RegisterConnection(sessionID, connID, newCh)
	// Stale defer fires now (carries oldCh). It MUST NOT delete newCh's mapping.
	r.DeregisterConnection(sessionID, connID, oldCh)

	// SendToConnection should still reach newCh — proves the mapping survives.
	require.NoError(t, r.SendToConnection(sessionID, connID, sessionStreamUpdate{stream: "live", add: true}))

	select {
	case upd := <-newCh:
		assert.Equal(t, "live", upd.stream)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stale DeregisterConnection clobbered the live mapping")
	}

	// Now the new owner properly deregisters with its own channel.
	r.DeregisterConnection(sessionID, connID, newCh)
	err := r.SendToConnection(sessionID, connID, sessionStreamUpdate{stream: "ghost", add: true})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONNECTION_NOT_REGISTERED")
}
