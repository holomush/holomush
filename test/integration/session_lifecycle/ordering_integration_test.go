// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// TODO(holomush-1tvn.14): F7 deletes this file along with EventStore.{Append,Replay,SubscribeSession,LastEventID}
//go:build integration && f6_legacy

package session_lifecycle_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
)

var _ = Describe("Cross-stream ordering (I2 invariant)", func() {
	var (
		testCtx    context.Context
		testCancel context.CancelFunc
		srv        *specServer
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithTimeout(context.Background(), 2*time.Minute)
		cleanupTestData(testCtx, env.pool)
		srv = newSpecServer(testCtx)
	})

	AfterEach(func() {
		srv.teardown()
		if testCancel != nil {
			testCancel()
		}
	})

	It("delivers session_ended as the terminal event in merge-sort replay order", func() {
		// Build a CharacterRef pointing at the spec server's start location so
		// that the say and leave events land on the right location stream.
		charID := core.NewULID()
		sessionID := core.NewULID().String()
		charRef := core.CharacterRef{
			ID:         charID,
			Name:       "TestChar",
			LocationID: srv.startLocation,
		}

		charStream := core.StreamPrefixCharacter + charID.String()
		locStream := "location:" + srv.startLocation.String()

		// Append say → leave → session_ended in strict order.
		// Using the engine ensures ULID monotonicity is maintained (I1).
		Expect(srv.engine.HandleSay(testCtx, charRef, "hello world")).To(Succeed())
		Expect(srv.engine.HandleDisconnect(testCtx, charRef, "leaving")).To(Succeed())
		Expect(srv.engine.EndSession(testCtx, charRef, sessionID, core.SessionEndedCauseQuit, "Goodbye!")).To(Succeed())

		// --- Character stream assertions ---
		// Replay the character stream and verify:
		//   1. say comes before session_ended (ordering)
		//   2. session_ended is the last event (terminal)
		charEvents, err := env.eventStore.Replay(testCtx, charStream, ulid.ULID{}, 100)
		Expect(err).NotTo(HaveOccurred())
		Expect(charEvents).NotTo(BeEmpty(), "character stream must have events")

		// session_ended must be the final event on the character stream.
		lastCharEvent := charEvents[len(charEvents)-1]
		Expect(lastCharEvent.Type).To(Equal(core.EventTypeSessionEnded),
			"session_ended MUST be the terminal event on the character stream (I2 invariant)")

		// Verify the session_ended payload is well-formed and matches our session.
		var endedPayload core.SessionEndedPayload
		Expect(json.Unmarshal(lastCharEvent.Payload, &endedPayload)).To(Succeed())
		Expect(endedPayload.SessionID).To(Equal(sessionID))
		Expect(endedPayload.Cause).To(Equal(core.SessionEndedCauseQuit))

		// --- Location stream assertions ---
		// Replay the location stream and verify leave is present.
		locEvents, err := env.eventStore.Replay(testCtx, locStream, ulid.ULID{}, 100)
		Expect(err).NotTo(HaveOccurred())
		Expect(locEvents).NotTo(BeEmpty(), "location stream must have events")

		leaveIdx := -1
		for i, e := range locEvents {
			if e.Type == core.EventTypeLeave {
				leaveIdx = i
				break
			}
		}
		Expect(leaveIdx).To(BeNumerically(">=", 0), "location stream must contain a leave event")

		// --- Cross-stream ordering: ULID-ascending invariant ---
		// Collect all events from both streams, sort by ULID, and verify
		// that session_ended is still the last event when combined.
		allEvents := make([]core.Event, 0, len(charEvents)+len(locEvents))
		allEvents = append(allEvents, charEvents...)
		allEvents = append(allEvents, locEvents...)

		// Find the maximum ULID across all events. session_ended must be >= all others
		// from charStream (it was appended last on that stream), and the cross-stream
		// ordering invariant (I2) requires its ULID to reflect append order.
		maxLocEventID := ulid.ULID{}
		for _, e := range locEvents {
			if e.ID.Compare(maxLocEventID) > 0 {
				maxLocEventID = e.ID
			}
		}

		// The session_ended event ID must be strictly greater than every location
		// stream event ID: session_ended was appended after leave, so its ULID
		// must sort after all location-stream events produced in this test.
		Expect(lastCharEvent.ID.Compare(maxLocEventID)).To(
			BeNumerically(">", 0),
			"I2 invariant: session_ended ULID (%s) must be > latest location-stream ULID (%s); "+
				"session_ended must sort last in any merge-sort across both streams",
			lastCharEvent.ID, maxLocEventID,
		)

		// Within the character stream, events must be in strict ULID-ascending order.
		for i := 1; i < len(charEvents); i++ {
			Expect(charEvents[i].ID.Compare(charEvents[i-1].ID)).To(
				BeNumerically(">", 0),
				"character stream event at index %d (ID=%s) must be > index %d (ID=%s)",
				i, charEvents[i].ID, i-1, charEvents[i-1].ID,
			)
		}

		// Within the location stream, events must be in strict ULID-ascending order.
		for i := 1; i < len(locEvents); i++ {
			Expect(locEvents[i].ID.Compare(locEvents[i-1].ID)).To(
				BeNumerically(">", 0),
				"location stream event at index %d (ID=%s) must be > index %d (ID=%s)",
				i, locEvents[i].ID, i-1, locEvents[i-1].ID,
			)
		}
	})

	It("retains session_ended as terminal even when say and leave precede it on separate streams", func() {
		// A second scenario with explicit ordering verification across the
		// appended sequence: say (char stream is NOT where say lands for
		// normal game events — it lands on location stream). Verify that
		// regardless of the relative interleaving, session_ended on the
		// character stream sorts last among character-stream events.
		charID := core.NewULID()
		sessionID := core.NewULID().String()
		charRef := core.CharacterRef{
			ID:         charID,
			Name:       "TestChar2",
			LocationID: srv.startLocation,
		}

		charStream := core.StreamPrefixCharacter + charID.String()

		// Multiple say events on the location stream before session_ended.
		Expect(srv.engine.HandleSay(testCtx, charRef, "first")).To(Succeed())
		Expect(srv.engine.HandleSay(testCtx, charRef, "second")).To(Succeed())
		Expect(srv.engine.HandleSay(testCtx, charRef, "third")).To(Succeed())
		Expect(srv.engine.HandleDisconnect(testCtx, charRef, "done")).To(Succeed())
		Expect(srv.engine.EndSession(testCtx, charRef, sessionID, core.SessionEndedCauseQuit, "Goodbye!")).To(Succeed())

		// session_ended must still be the only event on the character stream,
		// and it must be the terminal event.
		charEvents, err := env.eventStore.Replay(testCtx, charStream, ulid.ULID{}, 100)
		Expect(err).NotTo(HaveOccurred())
		Expect(charEvents).To(HaveLen(1),
			"character stream must only carry session_ended; say/leave events land on the location stream")
		Expect(charEvents[0].Type).To(Equal(core.EventTypeSessionEnded),
			"the sole character-stream event must be session_ended")

		var endedPayload core.SessionEndedPayload
		Expect(json.Unmarshal(charEvents[0].Payload, &endedPayload)).To(Succeed())
		Expect(endedPayload.SessionID).To(Equal(sessionID))
		Expect(endedPayload.CharacterID).To(Equal(charID.String()))
		Expect(endedPayload.Cause).To(Equal(core.SessionEndedCauseQuit))
	})
})
