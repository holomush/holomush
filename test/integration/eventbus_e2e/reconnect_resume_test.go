// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
)

// Reconnect-resume specs — assert the reconnect-resume contract from spec §8:
//
//   - A subscriber that closes its SessionStream and re-opens with the
//     same sessionID MUST resume at the last acked seq.
//   - No already-acked event is redelivered (no dup).
//   - No event published while the client was disconnected is lost (no
//     loss).
//
// This is the JetStream-era replacement for the legacy per-session cursor
// lock regression test.
var _ = Describe("Reconnect resume", func() {
	It("resumes at last acked seq with no dup and no loss", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pub := bus.Bus.Publisher()
		sub := bus.Bus.Subscriber()
		Expect(pub).NotTo(BeNil())
		Expect(sub).NotTo(BeNil())

		subject := eventbus.Subject("events.main.reconnect.s1")
		sessionID := freshSessionID()

		// Open, consume + ack 3 events.
		testID := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter, PlayerID: "01TESTPLAYER01234567890A", CharacterID: "01TESTCHARACTER0123456A", BindingID: "01TESTBINDING01234567AB"}
		s1, err := sub.OpenSession(ctx, sessionID, testID, []eventbus.Subject{subject})
		Expect(err).NotTo(HaveOccurred())

		const beforeDisconnect = 3
		for i := 0; i < beforeDisconnect; i++ {
			Expect(pub.Publish(ctx, mintEvent(subject, "scene.pose", `{"k":"pre"}`))).To(Succeed())
		}

		for i := 0; i < beforeDisconnect; i++ {
			d, nerr := s1.Next(ctx)
			Expect(nerr).NotTo(HaveOccurred())
			// Use sync ack on the last one so the server has confirmed the
			// ack before we close. Server-confirmed ack prevents the cursor
			// race that the reconnect contract promises to close.
			if i == beforeDisconnect-1 {
				syncCtx, syncCancel := context.WithTimeout(ctx, 2*time.Second)
				Expect(eventbus.AckSyncForTest(syncCtx, d)).To(Succeed())
				syncCancel()
			} else {
				Expect(d.Ack()).To(Succeed())
			}
		}
		// Barrier: AckFloor reaches the last published seq. This is the
		// synchronization primitive the spec's §8 §"Controllable test seams"
		// calls out — no time.Sleep; read server state.
		bus.AwaitAckedSeq(suiteT, "session_"+sessionID, beforeDisconnect, 5*time.Second)

		// Disconnect (close local iterator; server-side durable persists).
		Expect(s1.Close()).To(Succeed())

		// Publish additional events while disconnected.
		const whileDisconnected = 2
		for i := 0; i < whileDisconnected; i++ {
			Expect(pub.Publish(ctx, mintEvent(subject, "scene.pose", `{"k":"post"}`))).To(Succeed())
		}

		// Reconnect.
		s2, err := sub.OpenSession(ctx, sessionID, testID, []eventbus.Subject{subject})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = s2.Close() })

		// Must deliver exactly the whileDisconnected events; no dup, no loss.
		seenIDs := make(map[string]struct{}, whileDisconnected)
		for i := 0; i < whileDisconnected; i++ {
			dctx, dcancel := context.WithTimeout(ctx, 5*time.Second)
			d, nerr := s2.Next(dctx)
			dcancel()
			Expect(nerr).NotTo(HaveOccurred(), "expected %d deliveries after reconnect, got %d", whileDisconnected, i)
			id := d.Event().ID.String()
			_, dup := seenIDs[id]
			Expect(dup).To(BeFalse(), "duplicate delivery on reconnect: %s", id)
			seenIDs[id] = struct{}{}
			Expect(d.Ack()).To(Succeed())
		}
		// Draining more with a short deadline confirms no-loss (by absence).
		probeCtx, probeCancel := context.WithTimeout(ctx, 300*time.Millisecond)
		_, probeErr := s2.Next(probeCtx)
		probeCancel()
		Expect(probeErr).To(HaveOccurred(), "no further events expected after draining exactly %d", whileDisconnected)
	})
})
