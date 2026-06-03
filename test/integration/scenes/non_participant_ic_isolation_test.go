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

// INV-SCENE-6: Non-participants in the same physical location MUST NOT receive
// scene IC events. The substrate enforces this by the focus-subscription model:
// joining a scene opens a per-session subscription to the scene IC subject;
// a session that is co-located but NOT a scene participant never has that
// subscription and so the JetStream subject filter never delivers the event.
//
// This spec authentically exercises that boundary against a real embedded
// JetStream bus:
//
//   - Alice (participant) opens a session subscribed to BOTH the location
//     subject AND the scene IC subject — the same subscription set the live
//     command path would have established via JoinFocus.
//   - Bob (non-participant) opens a session subscribed to ONLY the location
//     subject — the same subscription set a co-located onlooker would have
//     who never ran JoinFocus.
//   - A scene_pose is published on the scene IC subject AND a location
//     ambient event is published on the location subject (the ambient event
//     is the positive-control delivery proving Bob's subscriber is healthy).
//   - Alice's stream MUST receive the scene_pose (positive control on the
//     scene IC path).
//   - Bob's stream MUST receive the location ambient event (positive control
//     on the location path) but MUST NOT receive any scene IC event (the
//     invariant assertion).
//
// Closes audit-finding holomush-ac50.
//
// Spec: docs/superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md §2.
var _ = Describe("INV-SCENE-6: non-participant scene IC isolation", func() {
	It("non-participant in same location MUST NOT receive scene IC events", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pub := bus.Bus.Publisher()
		sub := bus.Bus.Subscriber()
		Expect(pub).NotTo(BeNil())
		Expect(sub).NotTo(BeNil())

		// Fixed location + scene ULIDs make the subjects deterministic across
		// re-runs while keeping them parse-clean (scene_access.go's
		// streamToFocusKey requires a valid ULID in the scene-id slot).
		locID := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)
		sceneID := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)

		locSubject := eventbus.Subject("events.main.location." + locID.String())
		sceneICSubject := eventbus.Subject("events.main.scene." + sceneID.String() + ".ic")

		// Alice — participant. Subscribed to BOTH location and scene IC,
		// modelling the post-JoinFocus subscription set.
		aliceID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    "01TESTPLAYERALICE00000001",
			CharacterID: "01TESTCHARALICE0000000001",
			BindingID:   "01TESTBINDINGALICE000001Z",
		}
		alice, err := sub.OpenSession(
			ctx,
			freshSessionID(),
			aliceID,
			[]eventbus.Subject{locSubject, sceneICSubject},
			time.Time{},
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = alice.Close() })

		// Bob — non-participant co-located with Alice. Subscribed ONLY to
		// the location subject; he never ran JoinFocus, so he has no
		// subscription to the scene IC stream.
		bobID := eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    "01TESTPLAYERBOB00000000001",
			CharacterID: "01TESTCHARBOB000000000001",
			BindingID:   "01TESTBINDINGBOB000000001",
		}
		bob, err := sub.OpenSession(
			ctx,
			freshSessionID(),
			bobID,
			[]eventbus.Subject{locSubject},
			time.Time{},
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = bob.Close() })

		// Publish a scene_pose IC event Alice should see; Bob must not.
		poseEvent := mintEvent(sceneICSubject, "scene_pose", `{"text":"smiles at the room"}`)
		Expect(pub.Publish(ctx, poseEvent)).To(Succeed())

		// Publish a location ambient event as Bob's positive-control delivery
		// — proves Bob's subscriber is healthy and the absence of the scene
		// event below is genuine isolation, not a dead session.
		ambientEvent := mintEvent(locSubject, "location_event", `{"text":"a breeze rustles"}`)
		Expect(pub.Publish(ctx, ambientEvent)).To(Succeed())

		// Alice MUST receive the scene_pose IC event.
		// She may receive it before or after the ambient (subject ordering
		// is per-subject in JetStream, not global), so drain up to 2 events
		// and assert the pose ID is among them.
		aliceSeen := drainIDs(ctx, alice, 2, 5*time.Second)
		Expect(aliceSeen).To(HaveKey(poseEvent.ID.String()),
			"INV-SCENE-6 positive control: participant Alice MUST receive scene_pose IC event")

		// Bob MUST receive the ambient (positive control proving his
		// subscriber is alive on the location subject).
		bobFirstCtx, bobFirstCancel := context.WithTimeout(ctx, 5*time.Second)
		bobFirst, err := bob.Next(bobFirstCtx)
		bobFirstCancel()
		Expect(err).NotTo(HaveOccurred(),
			"positive control: non-participant Bob MUST receive location ambient (proves subscriber healthy)")
		Expect(bobFirst.Event().ID).To(Equal(ambientEvent.ID),
			"Bob's only delivery MUST be the location ambient — never the scene IC event")
		Expect(bobFirst.Event().Subject).To(Equal(locSubject),
			"Bob's delivery subject MUST be the location subject, never the scene IC subject")
		Expect(bobFirst.Ack()).To(Succeed())

		// INV-SCENE-6 assertion: Bob has NO further events. Draining with a
		// bounded deadline proves absence-by-timeout. We probe twice as
		// belt-and-braces (e.g., if JetStream were buggy and delayed
		// delivery, the second probe gives it another window to leak).
		for i := 0; i < 2; i++ {
			probeCtx, probeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
			leak, leakErr := bob.Next(probeCtx)
			probeCancel()
			if leakErr == nil {
				// Re-deliver as a clean failure message naming exactly what leaked.
				Fail("INV-SCENE-6 violation: non-participant Bob received an event with subject " +
					string(leak.Event().Subject) + " type " + string(leak.Event().Type) +
					" — non-participants MUST NOT see scene IC events")
			}
			// Expected path: context deadline → no leak. Assert the
			// timeout shape explicitly so a real subscription/stream
			// fault doesn't masquerade as a successful absence probe.
			Expect(errors.Is(leakErr, context.DeadlineExceeded)).To(BeTrue(),
				"probe %d: expected context.DeadlineExceeded (no leak), got %v", i+1, leakErr)
		}
	})
})

// drainIDs reads up to max events from s within budget, acks each, and
// returns the set of observed event IDs. Used to assert presence of a
// specific event without depending on cross-subject delivery ordering.
func drainIDs(ctx context.Context, s eventbus.SessionStream, max int, budget time.Duration) map[string]struct{} {
	seen := make(map[string]struct{}, max)
	deadline, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	for i := 0; i < max; i++ {
		d, err := s.Next(deadline)
		if err != nil {
			return seen
		}
		seen[d.Event().ID.String()] = struct{}{}
		_ = d.Ack()
	}
	return seen
}

// freshSessionID mints a ULID-shaped session identifier. Different sessions
// per spec keep the suite parallel-safe.
func freshSessionID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader).String()
}

// mintEvent builds a well-formed Event on the given subject. Payload is
// small JSON by default so tests don't thrash on bytes. Mirrors the
// eventbus_e2e suite's mintEvent for consistency.
func mintEvent(subject eventbus.Subject, etype eventbus.Type, body string) eventbus.Event {
	return eventbus.Event{
		ID:        ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader),
		Subject:   subject,
		Type:      etype,
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(body),
	}
}
