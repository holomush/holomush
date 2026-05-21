// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package presence_test contains integration tests for holomush-5b2j
// presence snapshot semantics.
//
// Test IDs follow the AC-N convention from the epic acceptance criteria
// at bd holomush-5b2j and the design spec at
// docs/superpowers/specs/2026-05-19-presence-snapshot-design.md.
package presence_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/privacytest"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Verifies: I-PRES-1
// Verifies: I-PRES-6
// AC4: A connects, B then connects to the same location, B's ListFocusPresence
// MUST include A within 1s of session open. Proves the snapshot RPC populates
// presence independent of event replay — the architectural pattern that
// unblocks iwzt.15 (Tier 2 history-scope filter).
var _ = Describe("AC4: joiner sees prior presence", func() {
	var (
		ts    *privacytest.Server
		ctx   context.Context
		alice *privacytest.Session
		bob   *privacytest.Session
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = privacytest.Start(suiteT)
		alice = ts.ConnectAuthed(ctx, "Alice")
		// Let alice's connect settle before bob joins. The snapshot RPC reads
		// session state directly so does NOT depend on this delay for
		// correctness — kept for parity with the AC4 acceptance scenario
		// (B joins AFTER A is established).
		time.Sleep(200 * time.Millisecond)
		bob = ts.ConnectAuthed(ctx, "Bob")
	})

	AfterEach(func() {
		// Use a fresh cleanup context independent of the BeforeEach ctx (which
		// could in principle be expired by AfterEach time) and nil-check ts in
		// case privacytest.Start panicked before assigning. Ginkgo runs
		// AfterEach even when BeforeEach failed.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if bob != nil {
			bob.Logout(cleanupCtx)
		}
		if alice != nil {
			alice.Logout(cleanupCtx)
		}
		if ts != nil {
			ts.Stop()
		}
	})

	It("B's ListFocusPresence response includes A AND B within 1s", func() {
		Expect(alice.LocationID).To(Equal(bob.LocationID),
			"preconditions: both sessions must be at the same location for AC4")

		Eventually(func(g Gomega) {
			resp, err := bob.ListFocusPresence(ctx)
			g.Expect(err).NotTo(HaveOccurred(),
				"ListFocusPresence MUST succeed for a session in a same-location query (seed:player-location-list-presence)")
			g.Expect(resp).NotTo(BeNil())
			g.Expect(resp.GetContext()).To(Equal(corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION),
				"context MUST be LOCATION when session has no FocusMemberships")
			g.Expect(resp.GetContextId()).To(Equal(bob.LocationID.String()),
				"context_id MUST be the queried location's ULID")
			g.Expect(entryNames(resp.GetEntries())).To(ConsistOf("Alice", "Bob"),
				"presence MUST include both sessions in the location")
		}, time.Second, 50*time.Millisecond).Should(Succeed())
	})
})

// AC3 / I-PRES-2: the snapshot RPC MUST be exempt from the I-PRIV-1 temporal
// floor (LocationArrivedAt). Manipulate bob's LocationArrivedAt to 1 hour in
// the future — under any temporal-floor-based filter (e.g., iwzt.15 Tier 2),
// alice's arrive event would be filtered out. The snapshot reads sessionStore
// directly and never traverses event delivery, so alice MUST still appear.
//
// Note: Tier 2 filter is currently a structural no-op in production (see
// internal/grpc/scope_floor.go:22 comment) because of a stream-subject
// format mismatch. This test asserts the architectural property the filter
// would gate against — independent of whether the filter itself is active.
// The future-LocationArrivedAt write is currently inert to the handler path
// (ListActiveByLocation has no temporal predicate) but acts as a regression
// tripwire: if anyone adds floor filtering at the snapshot layer, alice
// vanishes from the response and this test fails.
//
// TODO(iwzt.15): once the Tier 2 filter is no longer a structural no-op,
// upgrade this scenario to also exercise an active filter (e.g., via a
// WithTier2FilterActive harness option). Until then, the future-floor
// write + architectural assertion is the strongest available shape.
// Verifies: I-PRES-2
var _ = Describe("AC3 / I-PRES-2: snapshot bypasses I-PRIV-1 temporal floor", func() {
	var (
		ts    *privacytest.Server
		ctx   context.Context
		alice *privacytest.Session
		bob   *privacytest.Session
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = privacytest.Start(suiteT)
		alice = ts.ConnectAuthed(ctx, "Alice")
		bob = ts.ConnectAuthed(ctx, "Bob")
		// Push bob's LocationArrivedAt 1 hour into the future. Any temporal-
		// floor-based event filter would exclude alice's earlier arrive event
		// under this regime. The snapshot RPC must remain unaffected.
		ts.SetLocationArrivedAt(ctx, bob.SessionID, time.Now().Add(time.Hour))
	})

	AfterEach(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if bob != nil {
			bob.Logout(cleanupCtx)
		}
		if alice != nil {
			alice.Logout(cleanupCtx)
		}
		if ts != nil {
			ts.Stop()
		}
	})

	It("alice still appears in bob's ListFocusPresence response", func() {
		Expect(alice.LocationID).To(Equal(bob.LocationID),
			"preconditions: both sessions must be at the same location")

		Eventually(func(g Gomega) {
			resp, err := bob.ListFocusPresence(ctx)
			g.Expect(err).NotTo(HaveOccurred(),
				"ListFocusPresence MUST succeed for a same-location query (seed:player-location-list-presence)")
			g.Expect(resp).NotTo(BeNil())
			g.Expect(resp.GetContext()).To(Equal(corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION))
			g.Expect(entryNames(resp.GetEntries())).To(ContainElement("Alice"),
				"I-PRES-2: snapshot MUST surface alice despite bob's strict LocationArrivedAt floor")
			g.Expect(entryNames(resp.GetEntries())).To(ContainElement("Bob"),
				"I-PRES-6: caller's own session MUST be in the response")
		}, time.Second, 50*time.Millisecond).Should(Succeed())
	})
})

// entryNames extracts character names from PresenceEntry slice for ConsistOf
// matching. Mirrors the plan template's entryNames helper.
func entryNames(entries []*corev1.PresenceEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.GetCharacterName())
	}
	return names
}
