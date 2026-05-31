// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package presence_test integration spec for holomush-rsoe6:
// reattach restores grid_present (I-LIVE-3).
package presence_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Verifies (against the design invariant catalog §297-309):
//   - I-LIVE-3 (derivation): grid_present is DERIVED from live connection count.
//     A session that detaches (grid_present=false) and reattaches via Subscribe
//     MUST have grid_present flipped back to true, so ListFocusPresence includes
//     the reattached character again.
//
// Spec: docs/superpowers/specs/2026-05-30-session-liveness-and-gateway-survival-design.md §303
//
// Scenario: a connected guest is visible in presence. The transport detaches
// (grid_present cleared, session StatusDetached). The transport reattaches via
// the real Subscribe path (registerSubscribeConnection → AddConnection →
// recomputeSessionLiveness). After reattach, ListFocusPresence MUST include the
// character and the session row MUST have grid_present=true and status=active.
//
// The fix being covered: internal/grpc/server.go registerSubscribeConnection now
// calls recomputeSessionLiveness after AddConnection, mirroring the sibling
// registerConnection path. Without the fix, reattach leaves grid_present=false.
var _ = Describe("Reattach restores grid_present (I-LIVE-3)", func() {
	var (
		ts     *integrationtest.Server
		ctx    context.Context
		cancel context.CancelFunc
		guest  *integrationtest.Session
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		ts = integrationtest.Start(suiteT)
		guest = ts.ConnectGuest(ctx)
	})

	AfterEach(func() {
		if ts != nil {
			ts.Stop()
		}
		if cancel != nil {
			cancel()
		}
	})

	It("restores grid_present and presence entry after transport detach and reattach via Subscribe", func() {
		sessStore := ts.SessionStore()

		// Phase 1 — precondition: guest appears in presence (active session,
		// grid_present=true, terminal connection registered by Subscribe).
		Eventually(func(g Gomega) {
			resp, err := guest.ListFocusPresence(ctx)
			g.Expect(err).NotTo(HaveOccurred(),
				"ListFocusPresence MUST succeed for an active guest session")
			g.Expect(entryNames(resp.GetEntries())).To(ContainElement(guest.CharacterName),
				"guest MUST appear in presence while active and grid_present=true")
		}, 2*time.Second, 50*time.Millisecond).Should(Succeed())

		// Phase 2 — detach the transport: cancels the Subscribe stream and calls
		// the production Disconnect RPC, transitioning the session to
		// StatusDetached with grid_present=false.
		guest.DetachTransport(ctx)

		// Assert the session is detached with grid_present cleared.
		sessInfo, err := sessStore.Get(ctx, guest.SessionID)
		Expect(err).NotTo(HaveOccurred(), "session row MUST still exist after detach")
		Expect(sessInfo.Status).To(Equal(session.StatusDetached),
			"session MUST be StatusDetached immediately after DetachTransport")
		Expect(sessInfo.GridPresent).To(BeFalse(),
			"I-LIVE-3: grid_present MUST be false when no live connection exists (detached)")

		// Assert the character is NOT in presence (grid_present drives inclusion).
		Eventually(func(g Gomega) {
			resp, presErr := guest.ListFocusPresence(ctx)
			g.Expect(presErr).NotTo(HaveOccurred())
			g.Expect(entryNames(resp.GetEntries())).NotTo(ContainElement(guest.CharacterName),
				"I-LIVE-3/I-PRES-1: detached guest MUST NOT appear in presence (grid_present=false)")
		}, 2*time.Second, 50*time.Millisecond).Should(Succeed())

		// Phase 3 — reattach: routes through the real coreServer.Subscribe path →
		// registerSubscribeConnection → AddConnection → recomputeSessionLiveness.
		// attach() blocks until REPLAY_COMPLETE, so the recompute is guaranteed
		// to have run by the time ReattachTransport returns.
		guest.ReattachTransport(ctx)

		// Phase 4 — assert grid_present restored and session active.
		// recomputeSessionLiveness is synchronous within Subscribe before
		// REPLAY_COMPLETE, so an Eventually with a short window is sufficient.
		Eventually(func(g Gomega) {
			info, getErr := sessStore.Get(ctx, guest.SessionID)
			g.Expect(getErr).NotTo(HaveOccurred(), "session row MUST exist after reattach")
			g.Expect(info.Status).To(Equal(session.StatusActive),
				"I-LIVE-3: session MUST be StatusActive after reattach (ReattachCAS flips detached→active)")
			g.Expect(info.GridPresent).To(BeTrue(),
				"I-LIVE-3: grid_present MUST be true after reattach (≥1 live connection via Subscribe)")
		}, 2*time.Second, 50*time.Millisecond).Should(Succeed())

		// Assert the character is back in presence.
		Eventually(func(g Gomega) {
			resp, presErr := guest.ListFocusPresence(ctx)
			g.Expect(presErr).NotTo(HaveOccurred())
			g.Expect(entryNames(resp.GetEntries())).To(ContainElement(guest.CharacterName),
				"I-LIVE-3/I-PRES-1: reattached guest MUST appear in presence (grid_present restored to true)")
		}, 2*time.Second, 50*time.Millisecond).Should(Succeed())
	})
})
