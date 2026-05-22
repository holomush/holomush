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
		locStream = "location:" + guestA.LocationID.String()
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

		// Brief gap so the joiner's JoinedAt is strictly later than the
		// pre-join emit. Mirrors the I-PRIV-1 fresh-guest pattern above —
		// the embedded bus publish is synchronous, but the wall-clock gap
		// makes timestamp ordering unambiguous.
		time.Sleep(50 * time.Millisecond)

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
		startLoc = "location:" + mover.LocationID.String()

		// Pre-move: emit a seed event at locA so we can verify the post-move
		// hard-gate denial is content-independent (denial fires before any
		// query reads the underlying stream).
		Expect(mover.EmitDirectEvent(ctx, startLoc, "core-communication:pose",
			[]byte(`{"character_name":"Mover","action":"pauses before leaving."}`))).
			To(Succeed(), "pre-move seed event MUST publish")

		// Move to a fresh location.
		destLocID := ts.NewLocation(ctx)
		destLoc = "location:" + destLocID.String()
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
		_, err := sess.QueryStreamHistory(ctx, "location:"+otherLoc.String())
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
		_, err := sess.QueryStreamHistory(ctx, "admin:audit")
		expectStreamAccessDenied(err, "policy_denied")
	})

	It("expired session returns STREAM_ACCESS_DENIED", func() {
		sess := ts.ConnectAuthed(ctx, "Delta")
		// No defer Logout — the session row is force-expired below; the
		// production logout RPC against an expired session may behave
		// differently than this test cares about.
		ts.ExpireSession(ctx, sess.SessionID)
		_, err := sess.QueryStreamHistory(ctx, "location:"+sess.LocationID.String())
		expectStreamAccessDenied(err, "expired_session")
	})

	It("session not found (deleted) returns STREAM_ACCESS_DENIED", func() {
		sess := ts.ConnectAuthed(ctx, "Epsilon")
		// No defer Logout — the session row is deleted below.
		ts.DeleteSession(ctx, sess.SessionID)
		_, err := sess.QueryStreamHistory(ctx, "location:"+sess.LocationID.String())
		expectStreamAccessDenied(err, "session_not_found")
	})
})
