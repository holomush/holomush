// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// TestMultiProtocolFanoutTelnetAndWebSeeSamePose covers spec §8 "Multi-
// protocol fan-out -> Telnet + web in same scene see same pose".
//
// The assertion shape is: two distinct protocol adapters subscribed to
// the same scene subject both receive the same pose event with the same
// ULID and stream seq. The JetStream invariant (all subscribers of a
// subject get the same seq in the same order) already backstops this;
// the test exists to verify the full protocol-translation layer does not
// introduce dedup bugs on the way out.
//
// The Go test harness does not currently stand up the telnet adapter +
// web adapter without docker compose infrastructure. This skeleton uses
// two raw eventbus subscribers as a proxy for "two independent protocol
// adapters"; the real adapters will replace these in the follow-up bead.
//
// Follow-up: holomush-nko7 — multi-protocol fan-out e2e harness.
func TestMultiProtocolFanoutTelnetAndWebSeeSamePose(t *testing.T) {
	t.Skip("TODO(holomush-nko7): telnet + web adapter harness not reachable from Go test — skeleton retained for the follow-up bead")

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	pub := bus.Bus.Publisher()
	subSvc := bus.Bus.Subscriber()

	subject := eventbus.Subject("events.main.scene.01ABC.ic")

	// Two subscribers on the same subject simulate two protocol adapters.
	testID := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter, PlayerID: "01TESTPLAYER01234567890A", CharacterID: "01TESTCHARACTER0123456A", BindingID: "01TESTBINDING01234567AB"}
	s1, err := subSvc.OpenSession(ctx, freshSessionID(), testID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s1.Close() })
	s2, err := subSvc.OpenSession(ctx, freshSessionID(), testID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	evt := mintEvent(subject, "scene.pose", `{"action":"bows"}`)
	require.NoError(t, pub.Publish(ctx, evt))

	// Both must observe the identical ULID.
	d1ctx, c1 := context.WithTimeout(ctx, 5*time.Second)
	d1, err := s1.Next(d1ctx)
	c1()
	require.NoError(t, err)
	d2ctx, c2 := context.WithTimeout(ctx, 5*time.Second)
	d2, err := s2.Next(d2ctx)
	c2()
	require.NoError(t, err)
	require.Equal(t, evt.ID, d1.Event().ID)
	require.Equal(t, evt.ID, d2.Event().ID)

	// TODO(holomush-nko7): replace s1/s2 with real telnet + web adapters
	// and assert the pose flows through the protocol translation correctly
	// (e.g., telnet sees rendered text, web sees the JSON envelope).
	require.NoError(t, d1.Ack())
	require.NoError(t, d2.Ack())
}
