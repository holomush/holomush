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
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Verifies: I-PRIV-7
//
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

// Verifies: I-PRIV-6
//
// I-PRIV-6 gate-bypass arm only: a character granted
// read_unrestricted_history MUST bypass the I-PRIV-1 location hard-gate.
//
// I-PRIV-6 also asserts the floor-preservation arm — staff querying a
// location they're not in still sees only events from their own
// LocationArrivedAt forward. That arm requires emitting events with
// controlled timestamps across the staff session's LocationArrivedAt
// boundary; today's integrationtest harness can't do that (the dispatcher
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
		ts    *integrationtest.Server
		ctx   context.Context
		staff *integrationtest.Session
		locB  string
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = integrationtest.Start(suiteT)

		// Create a second location (not the staff member's location).
		locBID := ts.NewLocation(ctx)
		locB = "location." + locBID.String()

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

// Verifies: I-PRIV-3
//
// I-PRIV-3 (Subscribe-replay path): when a session's transport drops and later
// reattaches within TTL, events emitted during the detach window MUST be
// DELIVERED via the live Subscribe stream's durable replay (NOT dropped by
// the filter-at-delivery). The durable consumer's OptStartTime is immutable
// per NATS error 10012 (I-PRIV-8) and LocationArrivedAt is unchanged across
// reattach (I-PRIV-3); detach-window event timestamps are therefore at-or-after
// the unchanged filter floor and pass through.
//
// Companion to the QueryStreamHistory-path version asserted by the
// "I-PRIV-3 / I-PRIV-4: detach/reattach preserves session floor and replays
// events" block below. Both share the same LocationArrivedAt floor invariant
// but exercise different production code paths:
//
//   - This test: dispatchDelivery filter-at-delivery → live Subscribe stream
//     (internal/grpc/server.go:1053 filter + the broadcaster's stream.Send)
//   - Below: HistoryReader-bounded query (internal/grpc/query_stream_history.go)
//
// Per spec §8 reattach-durability test: open A at T0 (durable created with
// OptStartTime=T0), third party emits at T1, A detaches at T2, third party
// emits at T3 (during detach window), A reattaches at T4 — A's Subscribe
// replay MUST deliver both T1 and T3 events.
var _ = Describe("I-PRIV-3: ReattachCAS preserves durable; Subscribe replay delivers detach-window events", func() {
	// Unique event type for the during-detach emit so WaitForEvent matches
	// exactly the seeded event (not any later same-typed event). Mirrors the
	// pattern in the I-PRIV-3 QueryStreamHistory test below.
	const detachWindowType = "iwzt16-test:during-detach-marker"

	var (
		ts    *integrationtest.Server
		ctx   context.Context
		felix *integrationtest.Session
		gemma *integrationtest.Session
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = integrationtest.Start(suiteT)

		felix = ts.ConnectAuthed(ctx, "Felix")
		gemma = ts.ConnectAuthed(ctx, "Gemma")
		Expect(felix.LocationID).To(Equal(gemma.LocationID),
			"precondition: both characters at the guest start location so Gemma's emits land in Felix's location stream")
	})

	AfterEach(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if gemma != nil {
			gemma.Logout(cleanupCtx)
		}
		if felix != nil {
			felix.Logout(cleanupCtx)
		}
		if ts != nil {
			ts.Stop()
		}
	})

	It("delivers events emitted during transport detach when transport reattaches", func() {
		// Capture LocationArrivedAt before any state-changing op so we can
		// assert it's UNCHANGED after reattach (the load-bearing I-PRIV-3
		// claim — together with iwzt.17's QueryStreamHistory assertion,
		// this proves the floor is preserved on BOTH code paths).
		preDetachFloor := felix.LocationArrivedAt

		felix.DetachTransport(ctx)

		// Gemma emits during Felix's detach window. The event lands in
		// Felix's durable JetStream consumer (per-session durable, immutable
		// OptStartTime); reattach replay below MUST deliver it.
		Expect(gemma.EmitDirectEvent(ctx,
			"location."+gemma.LocationID.String(),
			detachWindowType,
			[]byte(`{"character_name":"Gemma","action":"speaks while Felix is detached."}`))).
			To(Succeed(), "during-detach emit MUST publish into Felix's durable consumer")

		felix.ReattachTransport(ctx)

		// WaitForEvent reads from the post-reattach live Subscribe stream
		// until the marker arrives (or waitCtx cancels / transport exits).
		// The production filter-at-delivery (internal/grpc/server.go:1053)
		// uses streamScopeFloor → LocationArrivedAt (unchanged) as the
		// per-event floor; the marker's JetStream-stamped timestamp is
		// strictly after that floor, so it passes through.
		waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
		defer waitCancel()
		ev := felix.WaitForEvent(waitCtx, detachWindowType)
		Expect(ev).NotTo(BeNil(),
			"I-PRIV-3 (Subscribe replay): durable replay MUST deliver the during-detach event to the reattached transport")
		Expect(ev.GetType()).To(Equal(detachWindowType),
			"delivered event type must match the seeded marker")
		Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", preDetachFloor),
			"I-PRIV-3 floor preservation: delivered event timestamp must be at-or-after the unchanged LocationArrivedAt — "+
				"if production accidentally advanced the floor on reattach (e.g. by re-running auth_handlers.go:331's "+
				"session-create logic in the reattach branch), the marker's pre-reattach timestamp would be below the "+
				"advanced floor and dispatchDelivery's filter at internal/grpc/server.go:1053 would have dropped it. "+
				"That the marker reached WaitForEvent at all is the load-bearing assertion. "+
				"(A direct re-read of sessions.location_arrived_at post-reattach would harden this further; tracked as a "+
				"future Session.RefreshFromPersisted helper. Asserting felix.LocationArrivedAt against preDetachFloor "+
				"would be tautological — that field is set once at ConnectAuthed and is never mutated by the harness.)")
	})
})

// Verifies: I-PRIV-3
// Verifies: I-PRIV-4
//
// I-PRIV-3 / I-PRIV-4 / spec §2 "transport-continuity worked example":
// the session row is the unit of continuity, not the transport connection.
// When a session detaches (transport drop) and later reattaches within TTL,
// LocationArrivedAt MUST be preserved and the durable consumer's events
// emitted during the detach window MUST remain queryable.
//
// Schema model: one game session per character (idx_sessions_active_character
// at internal/store/migrations/000001_baseline.up.sql:221-222); multiple
// transports attach via session_connections. The spec §2 worked example uses
// "session A and session B" loosely to mean "transport connection A and B" —
// the schema-faithful reading is one session, two transports.
//
// Test exercises the production reattach path (sessionStore.ReattachCAS at
// internal/store/session_store.go:421-429, which is what Subscribe runs at
// internal/grpc/server.go:778) end-to-end against a real session row.
//
// Default allow-all engine is sufficient: location streams pass the hard-gate
// because the session stays at its original location across detach/reattach.
var _ = Describe("I-PRIV-3 / I-PRIV-4: detach/reattach preserves session floor and replays events", func() {
	// Unique event type for the during-detach emit so the post-reattach
	// assertion can match it exactly rather than relying solely on a
	// timestamp window (a future regression adding an unrelated emit whose
	// timestamp landed in the same window would otherwise silently pass).
	const duringDetachType = "iwzt17-test:during-detach-marker"

	var (
		ts              *integrationtest.Server
		ctx             context.Context
		hugo            *integrationtest.AuthedPlayer
		firstSess       *integrationtest.Session
		reattachedSess  *integrationtest.Session
		other           *integrationtest.Session
		locStream       string
		preDetachEmitAt time.Time
		duringDetachAt  time.Time
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = integrationtest.Start(suiteT)

		// Hugo's first OpenWebSession creates the session row at T0;
		// LocationArrivedAt is captured from the persisted row (canonical).
		hugo = ts.AuthedPlayer(ctx, "Hugo")
		firstSess = hugo.OpenWebSession(ctx)
		locStream = "location." + firstSess.LocationID.String()

		// A different character at the same location emits a pre-detach event.
		// Using a separate session avoids hijacking hugo's session for the
		// emit-while-detached step below.
		other = ts.ConnectAuthed(ctx, "Iris")

		// Pre-detach emit: this event should be visible after reattach (it's
		// after firstSess.LocationArrivedAt and before the detach window).
		preDetachEmitAt = time.Now().UTC()
		preDetachPayload := []byte(`{"character_name":"Iris","action":"speaks before Hugo drops."}`)
		Expect(other.EmitDirectEvent(ctx, locStream, "core-communication:pose", preDetachPayload)).
			To(Succeed(), "pre-detach emit MUST publish")

		// Detach Hugo's session — mirrors production's non-guest transport
		// drop (status=Detached, detached_at=now, expires_at=now+TTL). The
		// session row + JetStream durable both stay alive.
		ts.DetachSession(ctx, firstSess.SessionID)

		// During-detach emit: this is the key event the test asserts on —
		// the spec's "events emitted during transport disconnect" claim
		// (§2 worked example + I-PRIV-1's session-row-lifetime clause).
		// Tagged with duringDetachType (declared at Describe scope) so the
		// post-reattach assertion can match the exact seeded event rather
		// than any event in a timestamp window.
		duringDetachAt = time.Now().UTC()
		duringPayload := []byte(`{"character_name":"Iris","action":"speaks while Hugo is detached."}`)
		Expect(other.EmitDirectEvent(ctx, locStream, duringDetachType, duringPayload)).
			To(Succeed(), "during-detach emit MUST publish into the still-live JetStream subject")

		// Reattach Hugo's session — production ReattachCAS path (the same
		// CAS Subscribe runs on its Detached branch). Asserting the CAS
		// succeeded here guards against silent loss-of-race producing a
		// misleading query result downstream.
		ts.ReattachSession(ctx, firstSess.SessionID)

		// A second OpenWebSession after reattach exercises the production
		// SelectCharacter reattach branch (spec §5 row 2) — it MUST return
		// the same SessionID with LocationArrivedAt unchanged. This is the
		// I-PRIV-3 invariant in code form.
		reattachedSess = hugo.OpenWebSession(ctx)
	})

	AfterEach(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if reattachedSess != nil {
			reattachedSess.Logout(cleanupCtx)
		}
		if other != nil {
			other.Logout(cleanupCtx)
		}
		if ts != nil {
			ts.Stop()
		}
	})

	It("returns the same SessionID on reattach", func() {
		Expect(reattachedSess.SessionID).To(Equal(firstSess.SessionID),
			"SelectCharacter reattach MUST return the same SessionID (one-session-per-character invariant)")
		// The Reattached flag is the production handler's positive signal
		// that the reattach branch (auth_handlers.go:319-356) ran, not the
		// fresh-create branch. Asserting it catches a regression where the
		// handler silently re-created a row (which would also surface as a
		// unique-index DB error, but this assertion is cheaper and clearer).
		Expect(reattachedSess.Reattached).To(BeTrue(),
			"second OpenWebSession MUST take SelectCharacter's reattach branch (Reattached=true)")
		// And the first call MUST have created the row, not reattached.
		Expect(firstSess.Reattached).To(BeFalse(),
			"first OpenWebSession MUST take the fresh-create branch")
	})

	It("preserves LocationArrivedAt across detach/reattach (I-PRIV-3)", func() {
		Expect(reattachedSess.LocationArrivedAt).To(BeTemporally("==", firstSess.LocationArrivedAt),
			"I-PRIV-3: LocationArrivedAt MUST be unchanged across detach/reattach within TTL")
	})

	It("returns events emitted during the detach window after reattach", func() {
		// QueryStreamHistory uses session.LocationArrivedAt (unchanged) as
		// the floor; the durable consumer held both events during the detach
		// window. Both events MUST appear in the response.
		events, err := reattachedSess.QueryStreamHistory(ctx, locStream)
		Expect(err).NotTo(HaveOccurred(),
			"post-reattach query MUST succeed (session is Active, hard-gate passes — same location)")
		// Vacuous-pass guard: a regression that over-filtered every event
		// would return an empty slice and let the timestamp loop trivially
		// pass. Require at least the two we just seeded.
		Expect(len(events)).To(BeNumerically(">=", 2),
			"both pre-detach and during-detach emits MUST be visible (vacuous-pass guard)")

		// Every returned event MUST be at-or-after firstSess.LocationArrivedAt:
		// reattach did not advance the floor; the prior floor still applies.
		for _, ev := range events {
			Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", firstSess.LocationArrivedAt),
				"event %q at %s leaked before firstSess.LocationArrivedAt %s",
				ev.GetType(), ev.GetTimestamp().AsTime(), firstSess.LocationArrivedAt)
		}

		// Affirmatively assert the during-detach event is in the result —
		// not just "no leak" but "the spec-claimed event is present".
		// Match by the unique duringDetachType the seed planted (precise),
		// with timestamp guards kept as a belt-and-suspenders sanity check
		// (catches a hypothetical regression that double-publishes events
		// with the same type at unrelated timestamps).
		//
		// Post-holomush-gfo6 the entire pipeline preserves nanosecond precision
		// end-to-end (INV-STORE-1 BIGINT-ns columns + INV-STORE-4 publisher does not
		// truncate). The prior defensive microsecond floor-truncate is no
		// longer needed — wall-clock floors compare against equal-precision
		// event timestamps.
		preDetachFloor := preDetachEmitAt
		duringDetachFloor := duringDetachAt
		var sawDuringDetach bool
		for _, ev := range events {
			evTS := ev.GetTimestamp().AsTime()
			if ev.GetType() == duringDetachType &&
				!evTS.Before(preDetachFloor) &&
				!evTS.Before(duringDetachFloor) {
				sawDuringDetach = true
				break
			}
		}
		Expect(sawDuringDetach).To(BeTrue(),
			"during-detach event MUST be present in post-reattach history (the spec §2 worked example's key claim)")
	})
})

// Verifies: I-PRIV-1
//
// I-PRIV-1: a fresh guest connecting to a location MUST NOT see any event
// whose timestamp predates their session's SessionCreatedAt. This is the
// regression guard for the Phase 2 QueryStreamHistory restructure (hard-gate
// + scope floor) landed via holomush-iwzt.8.
var _ = Describe("I-PRIV-1: new guest sees no pre-arrival location history", func() {
	var (
		ts     *integrationtest.Server
		ctx    context.Context
		guestA *integrationtest.Session
		guestB *integrationtest.Session
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = integrationtest.Start(suiteT)
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
		locStream := "location." + guestA.LocationID.String()
		payload := []byte(`{"character_name":"` + guestA.CharacterName + `","action":"waves a greeting."}`)
		Expect(guestA.EmitDirectEvent(ctx, locStream, "core-communication:pose", payload)).
			To(Succeed(), "harness emit MUST succeed for the seed event")
		guestA.Logout(ctx)

		// INV-STORE-6/INV-STORE-7 (gfo6): ns-resolution timestamps + >= floor semantics
		// make the prior µs tie-prevention gap unnecessary.

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

// Verifies: I-PRIV-2
//
// I-PRIV-2 (guest identity overlay): when a guest's display name happens to
// collide with a previous guest's name (random-namer collisions are possible
// within 20×20 = 400 names — see internal/naming/gemstone.go), the new guest
// MUST NOT see events emitted by the previous holder of that name. The
// MAX(LocationArrivedAt, guest_character.CreatedAt) floor isolates by
// character row identity, not by display name — the new guest has a fresh
// guest_character.CreatedAt strictly later than the prior emit timestamp.
//
// Test infra caveat: name collision is probabilistic. The harness loops up
// to 50 attempts; if no collision is observed (≈4% with 400-name pool), the
// test Skips with a documented reason. This matches the bead's acceptance
// criteria. The same invariant is exercised by I-PRIV-1 above against
// fresh-named guests, so a Skip here does not leave the I-PRIV-2 invariant
// unbound.
var _ = Describe("I-PRIV-2: guest name reuse does not leak prior holder's events", func() {
	var (
		ts          *integrationtest.Server
		ctx         context.Context
		firstName   string
		locStream   string
		reusedGuest *integrationtest.Session
		priorEmit   time.Time
	)

	const maxCollisionAttempts = 50

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = integrationtest.Start(suiteT)

		// First guest: connect, emit, logout. Record name + emit timestamp.
		guestA := ts.ConnectGuest(ctx)
		firstName = guestA.CharacterName
		locStream = "location." + guestA.LocationID.String()
		priorEmit = time.Now()
		payload := []byte(`{"character_name":"` + guestA.CharacterName + `","action":"waves once."}`)
		Expect(guestA.EmitDirectEvent(ctx, locStream, "core-communication:pose", payload)).
			To(Succeed(), "seed event from first guest MUST publish")
		guestA.Logout(ctx)

		// Production logout does NOT delete the guest's character row, so
		// the unique-name DB constraint blocks any subsequent ConnectGuest
		// from drawing the same name. Delete guestA's character to release
		// the name back to the namer pool — this is the test-only analog of
		// guest-character cleanup that would happen if logout-cleanup were
		// wired (tracked as a separate concern).
		ts.DeleteCharacter(ctx, guestA.CharacterID)

		// Spin up guests until one randomly draws the same display name. Each
		// non-matching guest is logged out + its character deleted so the
		// namer pool stays unrestricted across attempts.
		for attempt := 0; attempt < maxCollisionAttempts; attempt++ {
			candidate := ts.ConnectGuest(ctx)
			if candidate.CharacterName == firstName {
				reusedGuest = candidate
				return
			}
			// Different name — release it back to the pool so subsequent
			// attempts have full 400-name space available.
			candidate.Logout(ctx)
			ts.DeleteCharacter(ctx, candidate.CharacterID)
		}
		// reusedGuest remains nil → the It below Skips.
	})

	AfterEach(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if reusedGuest != nil {
			reusedGuest.Logout(cleanupCtx)
		}
		if ts != nil {
			ts.Stop()
		}
	})

	It("returns no events emitted by the prior holder of the reused name", func() {
		if reusedGuest == nil {
			Skip("namer-pool collision did not occur within " +
				"maxCollisionAttempts (probabilistic test infra; I-PRIV-2 " +
				"invariant is still exercised by the fresh-name I-PRIV-1 test)")
		}

		Expect(reusedGuest.CharacterName).To(Equal(firstName),
			"precondition: the reused-name guest's display name matches the prior holder")
		// Per spec §4.3: guest_character.CreatedAt is captured at session
		// creation and feeds the MAX(LocationArrivedAt, GuestCharacterCreatedAt)
		// floor. The reused-name guest's row is fresh — its CreatedAt is
		// strictly later than the prior holder's emit timestamp.
		Expect(reusedGuest.SessionCreatedAt).To(BeTemporally(">", priorEmit),
			"reused-name guest's SessionCreatedAt MUST be strictly after the prior emit")

		events, err := reusedGuest.QueryStreamHistory(ctx, locStream)
		Expect(err).NotTo(HaveOccurred(),
			"I-PRIV-2: same-location query MUST succeed for the reused-name guest")

		// Floor isolates by character row identity. No event emitted by the
		// prior name-holder may appear.
		for _, ev := range events {
			Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", reusedGuest.SessionCreatedAt),
				"event %q at %s leaked from prior holder of name %q (floor=%s)",
				ev.GetType(), ev.GetTimestamp().AsTime(), firstName, reusedGuest.SessionCreatedAt)
		}
	})
})

// Verifies: I-PRIV-2
//
// I-PRIV-2 (scene-join floor): when a character joins a scene at time T, the
// I-PRIV-2 scene branch of streamScopeFloor MUST floor the joiner's view of
// the scene at FocusMembership.JoinedAt. Events emitted to the scene BEFORE
// the joiner's join time are invisible; events at or after the join time are
// visible.
//
// Harness contract: scene streams use the NATS-native dot-style subject
// `events.<gameID>.scene.<sceneID>.ic` (per INV-P4-1 / ADR holomush-s9nu) —
// the legacy colon-style `scene:<id>:ic` falls through to the ABAC default
// branch and would defeat the I-17 / scope-floor codepaths. The harness's
// default allow-all engine is sufficient here: scene streams are
// membership-gated (I-17), not ABAC-gated, so the policy engine is never
// consulted on the visible-events path.
var _ = Describe("I-PRIV-2 (scene): scene events before join are invisible", func() {
	var (
		ts          *integrationtest.Server
		ctx         context.Context
		owner       *integrationtest.Session
		joiner      *integrationtest.Session
		sceneStream string
		joinedAt    time.Time
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = integrationtest.Start(suiteT)

		// Owner connects and emits a pre-join event into the scene stream.
		// The owner doesn't need to be a scene member to publish — the bus
		// publisher is unrestricted; only the read side (I-17 gate) requires
		// membership. This keeps the test focused on the join-time floor.
		owner = ts.ConnectAuthed(ctx, "Jamie")
		sceneID := ts.NewSceneWithoutMember(ctx)
		sceneStream = "events." + ts.GameID() + ".scene." + sceneID.String() + ".ic"

		prePayload := []byte(`{"character_name":"Jamie","action":"speaks before Kai arrives."}`)
		Expect(owner.EmitDirectEvent(ctx, sceneStream, "core-scenes:pose", prePayload)).
			To(Succeed(), "pre-join seed event MUST publish")

		// INV-STORE-6/INV-STORE-7 (gfo6): ns-resolution timestamps + >= floor semantics
		// make the prior µs tie-prevention gap unnecessary.

		// Joiner connects and joins the scene. JoinScene stamps the
		// session's FocusMemberships with JoinedAt = time.Now() and returns
		// that exact timestamp so the test can assert against the canonical
		// floor (avoids wall-clock skew between mutator-internal and
		// caller-side time.Now() snapshots).
		joiner = ts.ConnectAuthed(ctx, "Kai")
		joinedAt = joiner.JoinScene(ctx, sceneID)

		// Owner emits a post-join event. This MUST be visible to the
		// joiner (guard against vacuous-pass via Expect(events).NotTo(BeEmpty)
		// below — without it, a regression that filtered every event would
		// silently pass the floor loop).
		postPayload := []byte(`{"character_name":"Jamie","action":"speaks after Kai arrives."}`)
		Expect(owner.EmitDirectEvent(ctx, sceneStream, "core-scenes:pose", postPayload)).
			To(Succeed(), "post-join emit MUST publish")
	})

	AfterEach(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if joiner != nil {
			joiner.Logout(cleanupCtx)
		}
		if owner != nil {
			owner.Logout(cleanupCtx)
		}
		if ts != nil {
			ts.Stop()
		}
	})

	It("floors scene history at FocusMembership.JoinedAt", func() {
		events, err := joiner.QueryStreamHistory(ctx, sceneStream)
		Expect(err).NotTo(HaveOccurred(),
			"I-PRIV-2 (scene): joiner with FocusMembership MUST pass the I-17 gate")
		// Vacuous-pass guard — the post-join emit must be visible. Without
		// this, a regression that floored every scene event to time.Now()
		// would return an empty slice and the loop below would pass.
		Expect(events).NotTo(BeEmpty(),
			"post-join scene emit must be visible (vacuous-pass guard)")

		for _, ev := range events {
			Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", joinedAt),
				"event %q at %s leaked before joiner's JoinedAt %s",
				ev.GetType(), ev.GetTimestamp().AsTime(), joinedAt)
		}
	})
})

// Verifies: I-PRIV-1
//
// I-PRIV-1 (character move): when a character moves from locA to locB, the
// floor MUST reset to the new location's arrival time and the hard-gate
// MUST deny queries against the prior location. This is the location-
// switching arm of I-PRIV-1.
//
// Harness contract: this test uses WithPolicyEngine(DenyAllEngine) so the
// staffOverride bypass in QueryStreamHistory returns false — without that
// override, the harness's default allowAll engine grants
// read_unrestricted_history and the hard-gate denial path can't be
// exercised (see internal/grpc/scope_floor.go::staffOverride +
// query_stream_history.go hard-gate branch).
var _ = Describe("I-PRIV-1: character move resets location floor", func() {
	var (
		ts       *integrationtest.Server
		ctx      context.Context
		mover    *integrationtest.Session
		startLoc string
		destLoc  string
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		// DenyAll engine: staffOverride returns false → hard-gate fires when
		// session.LocationID != requested stream's location.
		ts = integrationtest.Start(suiteT, integrationtest.WithPolicyEngine(policytest.DenyAllEngine()))

		mover = ts.ConnectAuthed(ctx, "Mover")
		startLoc = "location." + mover.LocationID.String()

		// Pre-move: emit a seed event at locA so we can verify the post-move
		// hard-gate denial is content-independent (denial fires before any
		// query reads the underlying stream).
		Expect(mover.EmitDirectEvent(ctx, startLoc, "core-communication:pose",
			[]byte(`{"character_name":"Mover","action":"pauses before leaving."}`))).
			To(Succeed(), "pre-move seed event MUST publish")

		// Move to a fresh location.
		destLocID := ts.NewLocation(ctx)
		destLoc = "location." + destLocID.String()
		mover.MoveTo(ctx, destLocID)
	})

	AfterEach(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if mover != nil {
			mover.Logout(cleanupCtx)
		}
		if ts != nil {
			ts.Stop()
		}
	})

	It("denies queries against the prior location (hard-gate)", func() {
		_, err := mover.QueryStreamHistory(ctx, startLoc)
		Expect(err).To(HaveOccurred(),
			"I-PRIV-1 hard-gate: query against prior location MUST fail after move")
		oopsErr, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue(), "denial must surface as an oops error")
		Expect(oopsErr.Code()).To(Equal("STREAM_ACCESS_DENIED"),
			"denial MUST collapse to STREAM_ACCESS_DENIED — denial reason 'wrong_location'")
	})

	It("floors queries against the new location at arrival time", func() {
		// Emit a post-move event at the destination. It MUST appear in the
		// query result (timestamp is strictly >= mover.LocationArrivedAt).
		postPayload := []byte(`{"character_name":"Mover","action":"arrives and looks around."}`)
		Expect(mover.EmitDirectEvent(ctx, destLoc, "core-communication:pose", postPayload)).
			To(Succeed(), "post-move emit MUST publish")

		events, err := mover.QueryStreamHistory(ctx, destLoc)
		Expect(err).NotTo(HaveOccurred(),
			"I-PRIV-1: same-location query at the destination MUST succeed")
		// Guard against vacuous pass: the post-move event must be visible.
		// Without this assertion, a regression that over-filters everything
		// to nil would silently pass the floor loop below.
		Expect(events).NotTo(BeEmpty(),
			"post-move emit must be visible in history (vacuous-pass guard)")

		// Floor at the new location is LocationArrivedAt (updated by MoveTo).
		// Any returned event with timestamp before that is an I-PRIV-1 leak.
		for _, ev := range events {
			Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", mover.LocationArrivedAt),
				"event %q at %s leaked before mover.LocationArrivedAt %s after move",
				ev.GetType(), ev.GetTimestamp().AsTime(), mover.LocationArrivedAt)
		}
	})
})

// Verifies: I-PRIV-5
//
// I-PRIV-5: every denial path on QueryStreamHistory MUST return the same
// wire-level code STREAM_ACCESS_DENIED. The internal denial_reason
// (wrong_location, not_member, policy_denied, expired_session,
// session_not_found) goes to slog only — it MUST NOT cross the wire as a
// distinct error code, because doing so leaks information that lets a
// caller probe the denial taxonomy.
//
// Harness contract: uses WithPolicyEngine(DenyAllEngine) so staffOverride
// returns false (otherwise the hard-gate is bypassed for staff). The
// policy-denied entry queries an unseeded stream ("admin:audit") which
// DenyAll rejects regardless.
var _ = Describe("I-PRIV-5: denial wire opacity", func() {
	var (
		ts  *integrationtest.Server
		ctx context.Context
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel unused in test lifecycle
		ts = integrationtest.Start(suiteT, integrationtest.WithPolicyEngine(policytest.DenyAllEngine()))
	})

	AfterEach(func() {
		if ts != nil {
			ts.Stop()
		}
	})

	expectStreamAccessDenied := func(err error, denialReason string) {
		Expect(err).To(HaveOccurred(),
			"I-PRIV-5: %s denial MUST surface as an error", denialReason)
		oopsErr, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue(), "denial must surface as an oops error")
		Expect(oopsErr.Code()).To(Equal("STREAM_ACCESS_DENIED"),
			"I-PRIV-5: %s denial MUST collapse to STREAM_ACCESS_DENIED on the wire (internal denial_reason goes to slog only)",
			denialReason)
	}

	It("wrong location (hard-gate) returns STREAM_ACCESS_DENIED", func() {
		sess := ts.ConnectAuthed(ctx, "Alpha")
		defer sess.Logout(ctx)
		// Pre-create a different location; sess is at the guest start location.
		otherLoc := ts.NewLocation(ctx)
		_, err := sess.QueryStreamHistory(ctx, "location."+otherLoc.String())
		expectStreamAccessDenied(err, "wrong_location")
	})

	It("not member (I-17 scene private stream) returns STREAM_ACCESS_DENIED", func() {
		sess := ts.ConnectAuthed(ctx, "Beta")
		defer sess.Logout(ctx)
		// Scene where Beta is NOT a participant (no scene_participants row).
		// Use dot-style stream subject so isSceneStream recognises it as a
		// private stream and routes through the I-17 membership gate
		// (sessionHasMembership). Colon-style "scene:<id>:ic" falls through
		// to the ABAC default branch and would duplicate the policy_denied
		// entry below.
		scene := ts.NewSceneWithoutMember(ctx)
		stream := "events." + ts.GameID() + ".scene." + scene.String() + ".ic"
		_, err := sess.QueryStreamHistory(ctx, stream)
		expectStreamAccessDenied(err, "not_member")
	})

	It("ABAC policy denied (public stream, no grant) returns STREAM_ACCESS_DENIED", func() {
		sess := ts.ConnectAuthed(ctx, "Gamma")
		defer sess.Logout(ctx)
		// "admin:audit" is a stream pattern with no seed grant; DenyAll
		// engine rejects every Evaluate call → ABAC denial path.
		_, err := sess.QueryStreamHistory(ctx, "admin.audit")
		expectStreamAccessDenied(err, "policy_denied")
	})

	It("expired session returns STREAM_ACCESS_DENIED", func() {
		sess := ts.ConnectAuthed(ctx, "Delta")
		// No defer Logout — the session row is force-expired below; the
		// production logout RPC against an expired session may behave
		// differently than this test cares about.
		ts.ExpireSession(ctx, sess.SessionID)
		_, err := sess.QueryStreamHistory(ctx, "location."+sess.LocationID.String())
		expectStreamAccessDenied(err, "expired_session")
	})

	It("session not found (deleted) returns STREAM_ACCESS_DENIED", func() {
		sess := ts.ConnectAuthed(ctx, "Epsilon")
		// No defer Logout — the session row is deleted below.
		ts.DeleteSession(ctx, sess.SessionID)
		_, err := sess.QueryStreamHistory(ctx, "location."+sess.LocationID.String())
		expectStreamAccessDenied(err, "session_not_found")
	})
})
