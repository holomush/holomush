// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package privacy_test contains integration tests for holomush-iwzt
// history-scope privacy invariants.
//
// Test IDs follow the I-PRIV-N convention from the design spec at
// docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md.
package privacy_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/privacytest"
)

// I-PRIV-7 placeholder: no plugin currently declares history_scope: custom.
// The full scenario will exercise a plugin whose history_scope semantics
// diverge from grid/scene; until that plugin lands, the test is skipped
// to record the invariant requirement explicitly. Replace Skip with the
// real assertion when a custom-scope plugin adopts the field.
var _ = Describe("I-PRIV-7: plugin-owned history_scope semantics", func() {
	It("exercises a plugin that declared custom history_scope (placeholder)", func() {
		Skip("no plugin currently declares history_scope: custom — re-enable when a plugin adopts this field")
	})
})

// I-PRIV-6 gate-bypass arm only: a character granted
// read_unrestricted_history MUST bypass the I-PRIV-1 location hard-gate.
//
// I-PRIV-6 also asserts the floor-preservation arm — staff querying a
// location they're not in still sees only events from their own
// LocationArrivedAt forward. That arm requires emitting events with
// controlled timestamps across the staff session's LocationArrivedAt
// boundary; today's privacytest harness can't do that (the dispatcher
// is wired with an empty command registry, so SendCommand("say ...")
// has nothing to invoke, and direct emit helpers that bypass the
// command layer don't yet exist). The gate-bypass half is exercised
// here; the floor-preservation half is tracked separately.
//
// Per ADR wxty. The harness uses allowAllPolicyEngine which grants
// read_unrestricted_history for all requests, exercising the
// staffOverride → gate-bypass code path end-to-end.
var _ = Describe("I-PRIV-6 (gate-bypass arm): staff override bypasses the location hard-gate", func() {
	var (
		ts    *privacytest.Server
		ctx   context.Context
		staff *privacytest.Session
		locB  string
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = privacytest.Start(suiteT)

		// Create a second location (not the staff member's location).
		locBID := ts.NewLocation(ctx)
		locB = "location:" + locBID.String()

		// Staff session is at the guest start location (locA), which differs from locB.
		// ConnectAuthedWithRoles stamps "staff" into character_roles and opens a game session.
		staff = ts.ConnectAuthedWithRoles(ctx, "StaffMira", []string{"staff"})
	})

	AfterEach(func() {
		if staff != nil {
			staff.Logout(ctx)
		}
		ts.Stop()
	})

	It("returns success (not STREAM_ACCESS_DENIED) when staff queries a location they are not in", func() {
		// Staff is at the guest start location (locA); locB is a different
		// location they're not in. The harness's allowAllPolicyEngine
		// permits read_unrestricted_history for every principal, so
		// staffOverride returns true and the I-PRIV-1 location hard-gate
		// (session.LocationID == requested-location) is bypassed.
		// LocB has no events, so the response is an empty (non-nil) slice.
		// This asserts ONLY the gate-bypass — the floor-preservation arm
		// of I-PRIV-6 is tracked separately (no harness event-emit yet).
		events, err := staff.QueryStreamHistory(ctx, locB)
		Expect(err).NotTo(HaveOccurred(),
			"staff with read_unrestricted_history MUST bypass the location hard-gate (I-PRIV-6 gate-bypass arm)")
		Expect(events).NotTo(BeNil(),
			"response events must be a non-nil slice (empty is fine; locB has no history)")
		Expect(events).To(BeEmpty(),
			"locB has no events; response should be empty")
	})
})

// I-PRIV-1: a fresh guest connecting to a location MUST NOT see any event
// whose timestamp predates their session's SessionCreatedAt. This is the
// regression guard for the Phase 2 QueryStreamHistory restructure (hard-gate
// + scope floor) landed via holomush-iwzt.8.
var _ = Describe("I-PRIV-1: new guest sees no pre-arrival location history", func() {
	var (
		ts     *privacytest.Server
		ctx    context.Context
		guestA *privacytest.Session
		guestB *privacytest.Session
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = privacytest.Start(suiteT)
	})

	AfterEach(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if guestB != nil {
			guestB.Logout(cleanupCtx)
		}
		// guestA already logged out as part of the scenario.
		if ts != nil {
			ts.Stop()
		}
	})

	It("returns only events emitted after guest B's session created_at", func() {
		// Guest A connects, emits a pose into the location stream, disconnects.
		guestA = ts.ConnectGuest(ctx)
		locStream := "location:" + guestA.LocationID.String()
		payload := []byte(`{"character_name":"` + guestA.CharacterName + `","action":"waves a greeting."}`)
		Expect(guestA.EmitDirectEvent(ctx, locStream, "core-communication:pose", payload)).
			To(Succeed(), "harness emit MUST succeed for the seed event")
		guestA.Logout(ctx)

		// Brief gap so guest B's SessionCreatedAt is strictly later than
		// guest A's emit timestamp. The embedded bus publish is synchronous,
		// but the wall-clock advance ensures unambiguous ordering when
		// sub-millisecond co-occurrence could tie timestamps.
		time.Sleep(50 * time.Millisecond)

		// Guest B connects (fresh) into the same location.
		guestB = ts.ConnectGuest(ctx)
		Expect(guestB.LocationID).To(Equal(guestA.LocationID),
			"preconditions: both guests must land at the shared guest start location")

		events, err := guestB.QueryStreamHistory(ctx, locStream)
		Expect(err).NotTo(HaveOccurred(),
			"I-PRIV-1: same-location query MUST succeed for guest B (no hard-gate denial)")

		for _, ev := range events {
			// Floor is at guestB.SessionCreatedAt — events with earlier
			// timestamps are I-PRIV-1 leaks.
			Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", guestB.SessionCreatedAt),
				"event %q at %s leaked before guest B SessionCreatedAt %s",
				ev.GetType(), ev.GetTimestamp().AsTime(), guestB.SessionCreatedAt)
		}
	})
})
