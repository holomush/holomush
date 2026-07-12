// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package resilience_test

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world"
)

const (
	m2SpecTimeout = 3 * time.Minute
	// pausedMoveTimeout bounds the MoveCharacter call issued while the broker is
	// frozen. The post-commit emit's publish blocks against the paused broker up
	// to min(ctx, DupeWindow=30m) = this ctx; the retry loop (events.go
	// emitWithRetry: 3 retries, exp from 50ms) exhausts and the whole call returns
	// an emit-failure error. 30s comfortably exceeds the ~350ms retry window while
	// keeping the total pause well under 60s.
	pausedMoveTimeout = 30 * time.Second
	// deliveryGrace is the bounded window over which, after unpause, we OBSERVE
	// whether the flap-window move notification lands on the stream (lost vs late
	// out-of-band delivery). A bounded poll, never a sleep-assert.
	deliveryGrace = 6 * time.Second
)

// M2 is the OPS-05 dual-write non-atomicity finding (D-07). world.Service.
// MoveCharacter commits the character row FIRST (service.go:803 UpdateLocation)
// and only THEN emits the "move" notification post-commit (service.go:841
// EmitMoveEvent). The two are NOT atomic: a broker outage between the commit and
// a successful emit drops the notification while the DB write persists. On emit
// failure the caller receives an error tagged move_succeeded=true — the move
// "succeeded" (row moved) but no consumer was ever told.
//
// This Describe characterizes that window (D-07 requires proving the race window
// EXISTS on a broker flap; a deterministic lost move is NOT required) in three
// graduated forms:
//
//  1. control — with a healthy broker, a move commits AND its notification
//     reaches the stream (baseline: the emit leg works when the broker is up);
//  2. flap window — with the broker frozen mid-move, the DB commit survives and
//     the caller sees an emit-failure error tagged move_succeeded=true, while the
//     notification's actual delivery is DECOUPLED from that result: it may be lost
//     OR delivered late out-of-band (a frozen broker buffers the publish bytes in
//     the client TCP send buffer and flushes them on unpause). This decoupling IS
//     the D-07 window — the caller cannot know whether the notification landed. Per
//     D-07 the goal is to CHARACTERIZE that the window exists, NOT to force a
//     deterministic loss, so the spec asserts the deterministic facts (commit +
//     move_succeeded error) and records the observed delivery outcome;
//  3. production shape — with NO emitter wired (the exact production construction
//     per internal/world/setup/subsystem.go:66-77), every move reports emit
//     failure while the DB commits — the M2 notification leg is dead code in
//     production wiring today.
//
// A single replica suffices: M2 is a write-vs-broker race, not a replica race.
// No plugins are loaded — MoveCharacter has no in-tree command (F5/#4788); the
// direct world.Service path drives it. All read-backs go straight to the shared
// pgxpool (RESEARCH Pitfall 6). Stream presence/absence is asserted over an
// INDEPENDENT connection (eventsStream) via GetLastMsgForSubject, never through a
// replica's cached view or a connection-state callback (RESEARCH Pitfall 5).
var _ = Describe("M2 dual-write window", Ordered, func() {
	var (
		env      *natstest.NATSEnv
		replicaA *integrationtest.Server
		sess     *integrationtest.Session
		charID   ulid.ULID
		subj     string
	)

	BeforeAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		DeferCleanup(cancel)

		env = startExternalNATS(ctx)
		// Single replica; it creates the fresh per-test database (connStr="").
		replicaA = startReplica(suiteT, env.URL, "")

		// One guest whose character is the mover; its start location is the origin.
		sess = replicaA.ConnectGuest(ctx)
		charID = sess.CharacterID
		// The allow-all default engine (no WithRealABAC on the resilience replica)
		// accepts any subject string, so no policy seeding is required.
		subj = "character:" + charID.String()
	})

	// moveSubjectFor returns the fully-qualified EVENTS subject the move
	// notification for a destination location lands on — exactly the subject
	// events.go emits to (LocationStream(dest) qualified with the game id).
	moveSubjectFor := func(dest ulid.ULID) string {
		sub, err := eventbus.Qualify(replicaA.GameID(), world.LocationStream(dest))
		Expect(err).NotTo(HaveOccurred(), "qualify destination location subject")
		return string(sub)
	}

	// expectMoveEmitFailure asserts err is a MoveCharacter emit-failure. Two facts
	// matter for the D-07 verdict, and both are asserted:
	//
	//   - the caller error carries move_succeeded=true in its oops context. This is
	//     the load-bearing signal: MoveCharacter's outer wrap
	//     (CHARACTER_MOVE_EVENT_FAILED, service.go:844) sets move_succeeded=true so
	//     the caller can tell the DB row already committed and MUST NOT be retried.
	//     oops.Context() merges the whole chain, so this is present regardless of
	//     which code surfaces.
	//   - oops.Code() equals wantCode. Per samber/oops@v1.22 (error.go:230-249),
	//     Code() returns the DEEPEST non-nil code in the chain — NOT the outermost.
	//     The outer CHARACTER_MOVE_EVENT_FAILED is only categorization; the machine
	//     code a caller reads is the deepest one the emit path set: EVENT_EMIT_FAILED
	//     when a wired emitter's publish fails against a frozen broker,
	//     EVENT_EMITTER_MISSING when no emitter is wired at all.
	expectMoveEmitFailure := func(err error, wantCode string) {
		GinkgoHelper()
		Expect(err).To(HaveOccurred(), "move must report an emit failure")
		oopsErr, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue(), "expected an oops error, got %T", err)
		Expect(oopsErr.Context()).To(HaveKeyWithValue("move_succeeded", true),
			"the error context must carry move_succeeded=true — the DB row already committed")
		Expect(oopsErr.Code()).To(Equal(wantCode),
			"deepest oops code must categorize the emit-path failure")
	}

	// dbLocation reads the character's current location_id straight from the shared
	// pool (Pitfall 6). location_id is stored as a text ULID (character_repo.go:112).
	dbLocation := func(ctx context.Context) string {
		var loc string
		ExpectWithOffset(1, replicaA.Pool().QueryRow(ctx,
			`SELECT location_id FROM characters WHERE id = $1`, charID.String()).
			Scan(&loc)).To(Succeed(), "read-back character location_id via shared pool")
		return loc
	}

	It("control: with a healthy broker a move commits and its notification reaches the stream", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		svc := newEmittingWorldService(replicaA)
		stream := eventsStream(ctx, env)
		dest := replicaA.NewLocation(ctx)
		destSub := moveSubjectFor(dest)

		// Healthy broker: the post-commit emit succeeds, so MoveCharacter returns nil.
		Expect(svc.MoveCharacter(ctx, subj, charID, dest)).
			To(Succeed(), "with a healthy broker the move must commit AND emit cleanly")

		// The DB row moved.
		Expect(dbLocation(ctx)).To(Equal(dest.String()), "character row must be at the destination")

		// The move notification reached the stream: a message exists on the
		// destination location subject (bounded poll over the independent conn).
		Eventually(func() error {
			_, err := stream.GetLastMsgForSubject(ctx, destSub)
			return err
		}, 20*time.Second, 100*time.Millisecond).
			Should(Succeed(), "the move notification must reach the destination location subject %s", destSub)

		msg, err := stream.GetLastMsgForSubject(ctx, destSub)
		Expect(err).NotTo(HaveOccurred(), "read the move message back")
		Expect(msg.Header.Get(eventbus.HeaderEventType)).To(Equal(string(core.EventTypeMove)),
			"the delivered stream message must carry the move event type")

		reportVerdict(fmt.Sprintf(
			"M2-VERDICT: control: healthy broker — move committed (row at %s) AND the move notification reached the stream subject %s (event type %q)",
			dest, destSub, core.EventTypeMove,
		))
	})

	It("flap window: the DB commit survives while the notification's delivery is decoupled from the caller's result", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		svc := newEmittingWorldService(replicaA)
		dest := replicaA.NewLocation(ctx)
		destSub := moveSubjectFor(dest)

		// Freeze the broker, then move. UpdateLocation (a Postgres write) commits
		// BEFORE the post-commit emit hits the frozen broker; the emit's publish
		// blocks and the retry loop exhausts, so MoveCharacter returns an emit
		// failure. The move ctx is bounded (pausedMoveTimeout) to keep the pause short.
		pauseBroker(ctx, env)
		moveCtx, moveCancel := context.WithTimeout(ctx, pausedMoveTimeout)
		moveErr := svc.MoveCharacter(moveCtx, subj, charID, dest)
		moveCancel()
		unpauseBroker(ctx, env)

		// Leg 1 + 2: the caller sees a NON-nil emit-failure error tagged
		// move_succeeded=true. The wired emitter's publish blocked against the frozen
		// broker and the retry loop exhausted, so the deepest code is EVENT_EMIT_FAILED
		// (events.go:149), wrapped by the outer CHARACTER_MOVE_EVENT_FAILED categorization.
		expectMoveEmitFailure(moveErr, "EVENT_EMIT_FAILED")

		// Leg 3: the DB row IS at the destination — the commit persisted despite the
		// dropped notification.
		Expect(dbLocation(ctx)).To(Equal(dest.String()),
			"the character row must be at the destination — the DB commit survived the broker flap")

		// Leg 4: the notification's delivery is DECOUPLED from the caller's result and
		// is timing-dependent — the caller already saw an emit failure, yet the frozen
		// broker may have buffered the publish bytes in the client TCP send buffer and
		// flush them on unpause, delivering the move LATE and out-of-band. D-07 requires
		// characterizing that the window EXISTS (legs 1-3, deterministic), NOT forcing a
		// deterministic loss — so here we OBSERVE the post-unpause delivery outcome over a
		// bounded grace window and RECORD it, without gating on a timing-dependent result.
		// Consistently runs the full window; the inner check records delivery and the
		// matcher always holds, so there is no sleep and no flaky presence/absence gate.
		stream := eventsStream(ctx, env)
		notificationLanded := false
		Consistently(func() bool {
			if _, err := stream.GetLastMsgForSubject(ctx, destSub); err == nil {
				notificationLanded = true
			}
			return true
		}, deliveryGrace, 250*time.Millisecond).Should(BeTrue())

		deliveryOutcome := "permanently lost (never reached the stream within the grace window after unpause)"
		if notificationLanded {
			deliveryOutcome = "delivered LATE and out-of-band after the caller already saw emit failure (frozen-broker TCP buffer flushed on unpause)"
		}

		reportVerdict(fmt.Sprintf(
			"M2-VERDICT: flap-window: DB commit survived (row at %s) + caller error EVENT_EMIT_FAILED (outer CHARACTER_MOVE_EVENT_FAILED) with move_succeeded=true, while the notification delivery was DECOUPLED from that result: %s — the D-07 non-atomicity window is real (the caller cannot know whether the notification landed)",
			dest, deliveryOutcome,
		))
	})

	It("production shape: with no emitter wired, every move reports emit failure while the DB commits", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		// newWorldService is the emitter-LESS construction — byte-for-byte the
		// production wiring at internal/world/setup/subsystem.go:66-77, where no
		// EventEmitter is ever passed. The broker is HEALTHY; the failure is purely
		// the missing emitter.
		svc := newWorldService(replicaA)
		dest := replicaA.NewLocation(ctx)

		moveErr := svc.MoveCharacter(ctx, subj, charID, dest)

		// Same non-atomicity signature as the flap window: the DB row committed and
		// the caller is told move_succeeded=true, but the notification leg is dead
		// code — the emitter is nil, so EmitMoveEvent returns EVENT_EMITTER_MISSING
		// (events.go:127) as the deepest code, wrapped by the outer
		// CHARACTER_MOVE_EVENT_FAILED categorization.
		expectMoveEmitFailure(moveErr, "EVENT_EMITTER_MISSING")
		Expect(dbLocation(ctx)).To(Equal(dest.String()),
			"the character row must be at the destination — the DB commit is unconditional")

		reportVerdict(fmt.Sprintf(
			"M2-VERDICT: production-shape: with NO emitter wired (production construction), the move committed (row at %s) but reported EVENT_EMITTER_MISSING (outer CHARACTER_MOVE_EVENT_FAILED) with move_succeeded=true — the M2 notification leg is dead code in production today",
			dest,
		))
	})
})
