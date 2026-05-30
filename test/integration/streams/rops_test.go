// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package streams contains integration tests pinning the colon-style subject
// eradication invariants (holomush-rops). They exercise the full in-process
// holomush stack (Postgres testcontainer + embedded NATS JetStream + production
// CoreServer) to prove that dot-style stream subjects round-trip end-to-end:
// the producer subject (world.LocationStream → eventbus.Qualify) and the
// subscriber/history filters the server derives the same way are byte-identical,
// and the location authorization gates behave correctly on the dot form.
package streams

import (
	"context"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/world"
)

// TestINV_ROPS_4_ProducerSubscriberSymmetry pins INV-ROPS-4: an event emitted
// to a location's DOT stream (world.LocationStream → eventbus.Qualify) is
// delivered to a live Subscribe stream whose filter the server derives the same
// way (server.go:923 — world.LocationStream(info.LocationID)). If the producer
// and subscriber subjects ever diverged (e.g. one stayed colon), the event
// would never reach WaitForEvent and this test would hang to ctx timeout.
//
// Sequence note: ConnectAuthed attaches the Subscribe transport at the guest
// start location, so the initial filter is derived there. We MoveTo(locID) then
// detach+reattach so the re-derived filters include locID — mirrors the
// production reconnect flow and the privacy I-PRIV-3 reattach pattern. The
// post-move emit's JetStream timestamp is strictly after the MoveTo-stamped
// LocationArrivedAt floor, so it passes the per-event floor at delivery.
func TestINV_ROPS_4_ProducerSubscriberSymmetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s := integrationtest.Start(t)
	defer s.Stop()

	locID := s.NewLocation(ctx)
	sess := s.ConnectAuthed(ctx, "alice")
	defer sess.Logout(ctx)

	// Co-locate alice at locID and re-derive the Subscribe filters there.
	sess.MoveTo(ctx, locID)
	sess.DetachTransport(ctx)
	sess.ReattachTransport(ctx)

	require.NoError(t,
		sess.EmitDirectEvent(ctx, world.LocationStream(locID), "say", []byte(`{"text":"hi"}`)),
		"emit to dot location stream must publish")

	frame := sess.WaitForEvent(ctx, "say")
	require.NotNil(t, frame, "event emitted to dot location stream must be delivered to the co-located subscriber")
	require.Equal(t, "say", frame.GetType())
}

// TestINV_ROPS_7_LateJoinerFloorAndLocationGate pins INV-ROPS-7: a session that
// arrives at a location AFTER an event was emitted there cannot read that
// pre-arrival event — the scope floor (streamScopeFloor → LocationArrivedAt for
// location streams) excludes it. The read itself is authorized by the LOCATION
// HARD-GATE (I-PRIV-1: session.LocationID == requested-stream location), which
// is the gate for location streams; the I-17 membership gate applies only to
// PRIVATE streams (character/scene), not location streams.
//
// DenyAllEngine is used so staffOverride returns false and the hard-gate is the
// load-bearing authorization path (otherwise the allow-all default would bypass
// it via read_unrestricted_history).
func TestINV_ROPS_7_LateJoinerFloorAndLocationGate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// DenyAll: staffOverride=false → the location hard-gate is exercised, not bypassed.
	s := integrationtest.Start(t, integrationtest.WithPolicyEngine(policytest.DenyAllEngine()))
	defer s.Stop()

	locID := s.NewLocation(ctx)

	// Early arrival: emitter moves to locID, then emits a pre-join event.
	emitter := s.ConnectAuthed(ctx, "early")
	defer emitter.Logout(ctx)
	emitter.MoveTo(ctx, locID)
	require.NoError(t,
		emitter.EmitDirectEvent(ctx, world.LocationStream(locID), "say", []byte(`{"text":"pre"}`)),
		"pre-join emit must publish")

	// Brief gap so the late joiner's LocationArrivedAt is strictly after the
	// pre-join event's JetStream timestamp (floor is wall-clock comparison).
	time.Sleep(50 * time.Millisecond)

	// Late arrival: joiner moves to locID AFTER the pre-join event. MoveTo stamps
	// a fresh LocationArrivedAt (the scope floor).
	late := s.ConnectAuthed(ctx, "late")
	defer late.Logout(ctx)
	late.MoveTo(ctx, locID)

	// The read is permitted by the location hard-gate (late IS at locID), but the
	// floor excludes the pre-join frame.
	frames, err := late.QueryStreamHistory(ctx, world.LocationStream(locID))
	require.NoError(t, err, "co-located read must pass the location hard-gate (I-PRIV-1)")
	for _, f := range frames {
		require.False(t,
			f.GetTimestamp().AsTime().Before(late.LocationArrivedAt),
			"INV-ROPS-7: frame %q at %s leaked before late joiner's LocationArrivedAt %s (scope floor)",
			f.GetType(), f.GetTimestamp().AsTime(), late.LocationArrivedAt)
	}

	// Positive control: an event emitted AFTER the late joiner arrives MUST be
	// visible — guards against a vacuous pass where the floor over-filters
	// everything to empty.
	require.NoError(t,
		emitter.EmitDirectEvent(ctx, world.LocationStream(locID), "post-join", []byte(`{"text":"post"}`)),
		"post-join emit must publish")
	postFrames, err := late.QueryStreamHistory(ctx, world.LocationStream(locID))
	require.NoError(t, err)
	var sawPost bool
	for _, f := range postFrames {
		if f.GetType() == "post-join" {
			sawPost = true
		}
	}
	require.True(t, sawPost, "INV-ROPS-7 vacuous-pass guard: a post-arrival event MUST be readable by the late joiner")

	// TODO(rops): also assert the read traversed the location HARD-GATE path
	// (not ABAC fall-through) by inspecting the server's "stream access denied by
	// ABAC" / empty-policy_id log line. The integration harness exposes no
	// decision/slog capture surface today (only emit + history + WaitForEvent),
	// so the gate-path tell cannot be asserted from a test. The DenyAllEngine
	// configuration + a successful co-located read is the strongest available
	// proxy: under DenyAll, an ABAC fall-through would have DENIED this read, so
	// success proves the hard-gate (not ABAC) authorized it.
}

// TestINV_ROPS_8_LocationSeedAuthorization pins INV-ROPS-8: with the real
// seeded ABAC engine, the dot location stream's authorization is location-bound.
// A co-located character may read its location stream (hard-gate permit); a
// non-co-located character is denied (hard-gate STREAM_ACCESS_DENIED).
//
// Note on the gate: for LOCATION streams, QueryStreamHistory authorizes via the
// I-PRIV-1 location hard-gate (session.LocationID == stream location), with
// staffOverride consulting the real engine's read_unrestricted_history grant.
// A roleless ConnectAuthed character has no such grant, so staffOverride=false
// and the hard-gate is the deciding path. (EmitDirectEvent publishes straight
// to the bus and is NOT a deny surface — authorization lives on the read path,
// so the deny assertion is on QueryStreamHistory, per the production gates.)
func TestINV_ROPS_8_LocationSeedAuthorization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s := integrationtest.Start(t, integrationtest.WithRealABAC())
	defer s.Stop()

	locID := s.NewLocation(ctx)

	// Resident is co-located at locID.
	resident := s.ConnectAuthed(ctx, "resident")
	defer resident.Logout(ctx)
	resident.MoveTo(ctx, locID)

	require.NoError(t,
		resident.EmitDirectEvent(ctx, world.LocationStream(locID), "say", []byte(`{"text":"hi"}`)),
		"resident emit to its location stream must publish")

	// Permit: co-located read succeeds under the real seeded engine.
	_, err := resident.QueryStreamHistory(ctx, world.LocationStream(locID))
	require.NoError(t, err, "INV-ROPS-8: co-located character MUST read its own dot location stream under real ABAC")

	// Deny: an outsider at a DIFFERENT location is rejected reading locID's stream.
	otherLoc := s.NewLocation(ctx)
	outsider := s.ConnectAuthed(ctx, "outsider")
	defer outsider.Logout(ctx)
	outsider.MoveTo(ctx, otherLoc)

	_, err = outsider.QueryStreamHistory(ctx, world.LocationStream(locID))
	require.Error(t, err, "INV-ROPS-8: non-co-located read MUST be denied by the location hard-gate")
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "denial must surface as an oops error")
	require.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"INV-ROPS-8: non-co-located denial MUST collapse to STREAM_ACCESS_DENIED")
}
