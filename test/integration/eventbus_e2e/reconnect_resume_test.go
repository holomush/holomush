// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// TestReconnectResume asserts the reconnect-resume contract from spec §8:
//
//   - A subscriber that closes its SessionStream and re-opens with the
//     same sessionID MUST resume at the last acked seq.
//   - No already-acked event is redelivered (no dup).
//   - No event published while the client was disconnected is lost (no
//     loss).
//
// This is the JetStream-era replacement for the legacy per-session cursor
// lock regression test.
func TestReconnectResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	pub := bus.Bus.Publisher()
	sub := bus.Bus.Subscriber()
	require.NotNil(t, pub)
	require.NotNil(t, sub)

	subject := eventbus.Subject("events.main.reconnect.s1")
	sessionID := freshSessionID()

	// Open, consume + ack 3 events.
	testID := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter, PlayerID: "01TESTPLAYER01234567890A", CharacterID: "01TESTCHARACTER0123456A", BindingID: "01TESTBINDING01234567AB"}
	s1, err := sub.OpenSession(ctx, sessionID, testID, []eventbus.Subject{subject})
	require.NoError(t, err)

	const beforeDisconnect = 3
	for i := 0; i < beforeDisconnect; i++ {
		require.NoError(t, pub.Publish(ctx, mintEvent(subject, "scene.pose", `{"k":"pre"}`)))
	}

	for i := 0; i < beforeDisconnect; i++ {
		d, err := s1.Next(ctx)
		require.NoError(t, err)
		// Use sync ack on the last one so the server has confirmed the
		// ack before we close. Server-confirmed ack prevents the cursor
		// race that the reconnect contract promises to close.
		if i == beforeDisconnect-1 {
			syncCtx, syncCancel := context.WithTimeout(ctx, 2*time.Second)
			require.NoError(t, eventbus.AckSyncForTest(syncCtx, d))
			syncCancel()
		} else {
			require.NoError(t, d.Ack())
		}
	}
	// Barrier: AckFloor reaches the last published seq. This is the
	// synchronization primitive the spec's §8 §"Controllable test seams"
	// calls out — no time.Sleep; read server state.
	bus.AwaitAckedSeq(t, "session_"+sessionID, beforeDisconnect, 5*time.Second)

	// Disconnect (close local iterator; server-side durable persists).
	require.NoError(t, s1.Close())

	// Publish additional events while disconnected.
	const whileDisconnected = 2
	for i := 0; i < whileDisconnected; i++ {
		require.NoError(t, pub.Publish(ctx, mintEvent(subject, "scene.pose", `{"k":"post"}`)))
	}

	// Reconnect.
	s2, err := sub.OpenSession(ctx, sessionID, testID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	// Must deliver exactly the whileDisconnected events; no dup, no loss.
	seenIDs := make(map[string]struct{}, whileDisconnected)
	for i := 0; i < whileDisconnected; i++ {
		dctx, dcancel := context.WithTimeout(ctx, 5*time.Second)
		d, err := s2.Next(dctx)
		dcancel()
		require.NoError(t, err, "expected %d deliveries after reconnect, got %d", whileDisconnected, i)
		id := d.Event().ID.String()
		_, dup := seenIDs[id]
		assert.False(t, dup, "duplicate delivery on reconnect: %s", id)
		seenIDs[id] = struct{}{}
		require.NoError(t, d.Ack())
	}
	// Draining more with a short deadline confirms no-loss (by absence).
	probeCtx, probeCancel := context.WithTimeout(ctx, 300*time.Millisecond)
	_, probeErr := s2.Next(probeCtx)
	probeCancel()
	require.Error(t, probeErr, "no further events expected after draining exactly %d", whileDisconnected)
}
