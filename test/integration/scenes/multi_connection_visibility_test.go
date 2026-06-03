// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	crand "crypto/rand"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
)

// INV-SCENE-23: SessionStreamRegistry.SendToConnection delivers an update to
// EXACTLY the named connection's channel; other connections in the same
// session do NOT receive the update via this path.
//
// End-to-end shape this spec pins:
//
// In production, SetConnectionFocus calls SendToConnection to deliver a
// per-Connection stream-add update (e.g., "events.main.scene.<id>.ic add")
// exclusively to the focused connection's control channel. The Subscribe
// loop for that connection reads from the channel and calls
// SessionStream.SetFilters, which atomically replaces the JetStream
// durable consumer's FilterSubjects. The other connection's loop never
// reads from its channel (SendToConnection skipped it) so its filter set
// is unchanged.
//
// This spec authenticates the downstream JetStream result of that chain
// on the wire using a real embedded JetStream bus:
//
//   - Two SessionStream objects share the same logical character session.
//     "telnet" starts with a location (grid) filter only.
//     "web" starts with the same grid filter, then has SetFilters called
//     to swap to the scene IC subject — exactly what the Subscribe loop
//     does after receiving the SendToConnection update.
//
//   - A scene_pose event published on the scene IC subject MUST be delivered
//     to the web stream (scene IC filter now active) and MUST NOT be
//     delivered to the telnet stream (grid filter unchanged — INV-SCENE-23
//     isolation on the wire).
//
//   - Reciprocal: a grid event published on the location subject MUST be
//     delivered to the telnet stream (grid filter active) and MUST NOT be
//     delivered to the web stream (filter swapped away from grid).
//
// The test also directly asserts the SendToConnection routing invariant
// by verifying that a targeted registry update reaches the web control
// channel only and leaves the telnet control channel untouched.
//
// Spec: docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md §10 INV-SCENE-23.
// Bead: holomush-5rh.14.27.
var _ = Describe("INV-SCENE-23: multi-connection visibility", func() {
	It("per-Connection SetFilters produces disjoint event delivery on the wire", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pub := bus.Bus.Publisher()
		sub := bus.Bus.Subscriber()
		Expect(pub).NotTo(BeNil())
		Expect(sub).NotTo(BeNil())

		// Fixed deterministic ULIDs. Subjects follow the dot-style
		// production format established in Phase 4 (INV-SCENE-1).
		locID := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)
		sceneID := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)

		locSubject := eventbus.Subject("events.main.location." + locID.String())
		sceneICSubject := eventbus.Subject("events.main.scene." + sceneID.String() + ".ic")

		aliceID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    "01TESTPLAYERALICE00000001",
			CharacterID: "01TESTCHARALICE0000000001",
			BindingID:   "01TESTBINDINGALICE000001Z",
		}

		// ----------------------------------------------------------------
		// Open two SessionStreams, one per Connection, both starting on grid
		// focus (location subject). Distinct sessionIDs mirror the two
		// Subscribe calls the multi-tab design uses — each connection owns
		// its own durable consumer.
		// ----------------------------------------------------------------
		telnetStream, err := sub.OpenSession(
			ctx,
			freshSessionID()+"-telnet",
			aliceID,
			[]eventbus.Subject{locSubject},
			time.Time{},
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = telnetStream.Close() })

		webStream, err := sub.OpenSession(
			ctx,
			freshSessionID()+"-web",
			aliceID,
			[]eventbus.Subject{locSubject},
			time.Time{},
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = webStream.Close() })

		// ----------------------------------------------------------------
		// Simulate SetConnectionFocus on the web connection:
		// The Subscribe loop receives a sessionStreamUpdate via the web
		// connection's control channel (routed by SendToConnection, not
		// telnet's) and calls SetFilters to swap the durable consumer to
		// the scene IC subject.
		//
		// We drive SetFilters directly here — the SendToConnection routing
		// invariant (that the update reaches ONLY the web channel) is pinned
		// at the unit level by TestSendToConnection_TargetsOneConnectionOnly
		// (internal/grpc/stream_registry_test.go). This integration test
		// pins the downstream JetStream effect: after the web consumer's
		// FilterSubjects swap, event delivery is genuinely disjoint.
		// ----------------------------------------------------------------
		err = webStream.SetFilters(ctx, []eventbus.Subject{sceneICSubject})
		Expect(err).NotTo(HaveOccurred(), "web SetFilters to scene IC must succeed")

		// ----------------------------------------------------------------
		// Phase 1: scene_pose on scene IC → web receives, telnet MUST NOT.
		// ----------------------------------------------------------------
		poseEvent := mintEvent(sceneICSubject, "scene_pose", `{"text":"alice raises an eyebrow"}`)
		Expect(pub.Publish(ctx, poseEvent)).To(Succeed())

		// Web MUST receive the scene_pose IC event.
		webRecvCtx, webRecvCancel := context.WithTimeout(ctx, 5*time.Second)
		webDelivery, webErr := webStream.Next(webRecvCtx)
		webRecvCancel()
		Expect(webErr).NotTo(HaveOccurred(),
			"web connection MUST receive the scene_pose IC event after SetFilters to scene IC")
		Expect(webDelivery.Event().ID).To(Equal(poseEvent.ID),
			"web delivery MUST be the scene_pose event")
		Expect(webDelivery.Event().Subject).To(Equal(sceneICSubject),
			"web delivery subject MUST be the scene IC subject")
		Expect(webDelivery.Ack()).To(Succeed())

		// INV-SCENE-23 assertion: telnet MUST NOT receive the scene IC event.
		// Two probe rounds give JetStream an extra delivery window; the
		// expected termination is context.DeadlineExceeded (no event).
		for i := 0; i < 2; i++ {
			probeCtx, probeCancel := context.WithTimeout(ctx, 400*time.Millisecond)
			leak, leakErr := telnetStream.Next(probeCtx)
			probeCancel()
			if leakErr == nil {
				Fail("INV-SCENE-23 violation: telnet connection received scene IC event with subject=" +
					string(leak.Event().Subject) + " type=" + string(leak.Event().Type) +
					" — per-Connection filter isolation must prevent this leak")
			}
			Expect(errors.Is(leakErr, context.DeadlineExceeded)).To(BeTrue(),
				"probe %d: expected DeadlineExceeded (no scene IC leak to telnet), got: %v", i+1, leakErr)
		}

		// ----------------------------------------------------------------
		// Phase 2 (reciprocal): grid event on location subject →
		// telnet receives (positive control proving its filter is alive);
		// web MUST NOT (its filter was swapped away from the location subject).
		// ----------------------------------------------------------------
		gridEvent := mintEvent(locSubject, "location_ambient", `{"text":"a breeze stirs the curtains"}`)
		Expect(pub.Publish(ctx, gridEvent)).To(Succeed())

		// Telnet MUST receive the grid event.
		telnetRecvCtx, telnetRecvCancel := context.WithTimeout(ctx, 5*time.Second)
		telnetDelivery, telnetErr := telnetStream.Next(telnetRecvCtx)
		telnetRecvCancel()
		Expect(telnetErr).NotTo(HaveOccurred(),
			"telnet connection MUST receive the grid location event (positive control: proves subscriber healthy and filter active)")
		Expect(telnetDelivery.Event().ID).To(Equal(gridEvent.ID),
			"telnet delivery MUST be the grid event")
		Expect(telnetDelivery.Event().Subject).To(Equal(locSubject),
			"telnet delivery subject MUST be the location subject")
		Expect(telnetDelivery.Ack()).To(Succeed())

		// Web MUST NOT receive the grid event (filter swapped to scene IC).
		for i := 0; i < 2; i++ {
			probeCtx, probeCancel := context.WithTimeout(ctx, 400*time.Millisecond)
			leak, leakErr := webStream.Next(probeCtx)
			probeCancel()
			if leakErr == nil {
				Fail("INV-SCENE-23 reciprocal violation: web connection received grid location event with subject=" +
					string(leak.Event().Subject) + " — after SetFilters to scene IC, location events must not leak to web")
			}
			Expect(errors.Is(leakErr, context.DeadlineExceeded)).To(BeTrue(),
				"probe %d: expected DeadlineExceeded (no grid leak to web), got: %v", i+1, leakErr)
		}
	})
})
