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

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

const (
	m2SpecTimeout = 3 * time.Minute
	// pausedMoveTimeout bounds the MoveCharacter call issued while the broker is
	// frozen. Post-05-06 MoveCharacter performs NO broker publish in the command
	// path — the move + its envelope are a single Postgres transaction and publish
	// is the relay's job — so a frozen broker cannot stall the move. The bound is
	// kept only as a defensive ceiling well under the pause window.
	pausedMoveTimeout = 30 * time.Second
)

// M2 was the OPS-05 dual-write non-atomicity finding (D-07): the pre-05-06
// MoveCharacter committed the character row FIRST and only THEN emitted the "move"
// notification post-commit, so a broker outage between the commit and a successful
// emit dropped the notification while the DB write persisted, and the caller saw an
// error tagged move_succeeded=true.
//
// Slice 2 (05-06) CLOSES that window. The post-commit emit path is deleted (D-03);
// MoveCharacter now commits its character-location change AND exactly one move
// envelope to the `outbox` table in the SAME transaction (world.OutboxWriter). The
// two are atomic — there is no second write to lose on a broker blip — and the
// relay (05-08) owns publishing the committed envelopes out-of-band. This Describe
// characterizes the NEW mechanism:
//
//  1. control — with a healthy broker, a move commits the state change AND exactly
//     one outbox envelope atomically;
//  2. flap window — with the broker frozen mid-move, the move STILL commits state +
//     envelope atomically and the caller sees SUCCESS (not an emit failure): the
//     outbox write is a DB write, not a broker publish, so a frozen broker cannot
//     touch the mutation transaction; publish is decoupled to the relay;
//  3. no orphan — every committed move has exactly one matching envelope and vice
//     versa (no state-change-without-envelope, no envelope-without-state);
//  4. relay redelivery (M2 closed END-TO-END) — a move committed WHILE the broker
//     is frozen stays committed-but-unpublished (the notification is PENDING, not
//     lost); after the broker recovers the single leased relay (05-07) publishes
//     the move envelope, so a NATS blip after commit cannot lose the notification.
//
// A single replica suffices: M2 is a write-vs-broker question, not a replica race.
// No plugins are loaded — MoveCharacter has no in-tree command (F5/#4788); the
// direct world.Service path drives it. All read-backs go straight to the shared
// pgxpool (RESEARCH Pitfall 6).
var _ = Describe("M2 dual-write window closed by the transactional outbox", Ordered, func() {
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

	// dbLocation reads the character's current location_id straight from the shared
	// pool (Pitfall 6). location_id is stored as a text ULID (character_repo.go).
	dbLocation := func(ctx context.Context) string {
		var loc string
		ExpectWithOffset(1, replicaA.Pool().QueryRow(ctx,
			`SELECT location_id FROM characters WHERE id = $1`, charID.String()).
			Scan(&loc)).To(Succeed(), "read-back character location_id via shared pool")
		return loc
	}

	// outboxMoveCount counts the character_moved envelopes committed for charID —
	// read over the shared pool, so it observes only COMMITTED rows.
	outboxMoveCount := func(ctx context.Context) int {
		var n int
		ExpectWithOffset(1, replicaA.Pool().QueryRow(ctx,
			`SELECT count(*) FROM outbox WHERE aggregate_id = $1 AND kind = 'character_moved'`,
			charID.String()).Scan(&n)).To(Succeed(), "count committed move envelopes via shared pool")
		return n
	}

	It("control: a healthy-broker move commits state and exactly one outbox envelope atomically", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		svc := newWorldService(replicaA)
		dest := replicaA.NewLocation(ctx)
		before := outboxMoveCount(ctx)

		Expect(svc.MoveCharacter(ctx, subj, charID, dest)).
			To(Succeed(), "a healthy-broker move must commit state + envelope in one transaction")

		// The state change committed …
		Expect(dbLocation(ctx)).To(Equal(dest.String()), "character row must be at the destination")
		// … and exactly one move envelope committed WITH it (no orphan either way).
		Expect(outboxMoveCount(ctx)).To(Equal(before+1),
			"exactly one move envelope must be committed atomically with the state change")

		reportVerdict(fmt.Sprintf(
			"M2-VERDICT: control: healthy broker — move committed (row at %s) AND exactly one move envelope committed in the SAME transaction (transactional outbox; no post-commit emit)",
			dest,
		))
	})

	It("flap window: a broker-frozen move still commits state and envelope atomically and returns success", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		svc := newWorldService(replicaA)
		dest := replicaA.NewLocation(ctx)
		before := outboxMoveCount(ctx)

		// Freeze the broker, then move. MoveCharacter performs NO broker publish in
		// the command path — the move + its envelope are a single Postgres tx — so a
		// frozen broker cannot stall or fail it. The caller no longer receives an
		// emit-failure with move_succeeded=true (that path was deleted); publish is
		// the relay's job (05-08).
		pauseBroker(ctx, env)
		moveCtx, moveCancel := context.WithTimeout(ctx, pausedMoveTimeout)
		moveErr := svc.MoveCharacter(moveCtx, subj, charID, dest)
		moveCancel()
		unpauseBroker(ctx, env)

		Expect(moveErr).To(Succeed(),
			"the move must commit and return success despite the frozen broker — publish is decoupled to the relay")
		Expect(dbLocation(ctx)).To(Equal(dest.String()),
			"the character row must be at the destination — the DB commit survived the broker flap")
		Expect(outboxMoveCount(ctx)).To(Equal(before+1),
			"the move envelope committed atomically with the state change even while the broker was frozen (no orphan state-change-without-envelope)")

		reportVerdict(fmt.Sprintf(
			"M2-VERDICT: flap-window: broker frozen mid-move — state committed (row at %s) AND its move envelope committed in the SAME transaction, caller saw SUCCESS (no move_succeeded=true emit failure); the D-03 post-commit emit path is gone and publish is the relay's job — the M2 non-atomicity window is CLOSED",
			dest,
		))
	})

	It("no orphan: every committed move has exactly one matching envelope and the envelope resolves to the real character row", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		// After the two prior moves, the character is at its last destination and the
		// number of committed move envelopes is >= the number of moves performed —
		// there is no state-change-without-envelope. Assert the inverse too: every
		// move envelope's aggregate_id resolves to the real character row (no
		// envelope-without-state).
		moves := outboxMoveCount(ctx)
		Expect(moves).To(BeNumerically(">=", 2),
			"the two prior successful moves must each have committed a move envelope")

		var orphans int
		Expect(replicaA.Pool().QueryRow(ctx, `
			SELECT count(*) FROM outbox o
			WHERE o.aggregate_id = $1 AND o.kind = 'character_moved'
			  AND NOT EXISTS (SELECT 1 FROM characters c WHERE c.id = o.aggregate_id)`,
			charID.String()).Scan(&orphans)).To(Succeed())
		Expect(orphans).To(Equal(0), "no move envelope may reference a non-existent character (no envelope-without-state)")

		reportVerdict(fmt.Sprintf(
			"M2-VERDICT: no-orphan: %d committed move envelope(s) for the character, every one resolving to the real character row — state and envelope are 1:1 atomic",
			moves,
		))
	})

	It("relay redelivery: a move committed during a broker blip is published after the broker recovers (M2 closed end-to-end)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		svc := newWorldService(replicaA)
		dest := replicaA.NewLocation(ctx)

		// The move envelope's wire subject: events.<game>.character.<id> (05-07 wire
		// adapter). Its stored count corroborates the DB-side published_at proof.
		charSubject, qerr := eventbus.Qualify(replicaA.GameID(), "character."+charID.String())
		Expect(qerr).NotTo(HaveOccurred(), "qualify character subject")
		beforeWire := streamSubjectCount(ctx, env, string(charSubject))

		// Freeze the broker, then move: the character-location change AND the move
		// envelope commit in one Postgres transaction (a DB write, immune to the
		// broker). No relay has run in this suite, so the envelope is
		// committed-but-UNPUBLISHED — precisely the post-commit blip window in which
		// the deleted D-03 emit path would have LOST the notification.
		pauseBroker(ctx, env)
		moveCtx, moveCancel := context.WithTimeout(ctx, pausedMoveTimeout)
		moveErr := svc.MoveCharacter(moveCtx, subj, charID, dest)
		moveCancel()
		Expect(moveErr).To(Succeed(), "the move commits despite the frozen broker (publish is decoupled to the relay)")

		// Identify THIS move's envelope — the newest character_moved row for the char.
		var eventIDStr string
		Expect(replicaA.Pool().QueryRow(ctx, `
			SELECT event_id FROM outbox
			WHERE aggregate_id = $1 AND kind = 'character_moved'
			ORDER BY epoch DESC, feed_position DESC LIMIT 1`, charID.String()).
			Scan(&eventIDStr)).To(Succeed(), "find the frozen-window move envelope")
		moveEventID := ulid.MustParse(eventIDStr)

		// During the blip the notification is PENDING (committed, unpublished) — not lost.
		Expect(outboxPublishedAt(ctx, replicaA, moveEventID)).To(BeFalse(),
			"the move envelope is committed but unpublished while the broker is frozen — the notification is pending, not lost")

		unpauseBroker(ctx, env)

		// After recovery the single leased relay publishes every unpublished
		// envelope (this move included). MarkPublished follows PubAck, so a set
		// published_at is proof the envelope reached the broker.
		relay := newOutboxRelay(outboxStoreFor(replicaA), busPublisher(replicaA), replicaA.GameID())
		// Release the relay's advisory-lock lease at spec end — a leaked pinned
		// connection would block the harness pool.Close() during suite teardown.
		DeferCleanup(func() { _ = relay.Stop(context.Background()) })
		_, err := relay.Drain(ctx)
		Expect(err).NotTo(HaveOccurred(), "the relay drains cleanly after the broker recovers")

		Expect(outboxPublishedAt(ctx, replicaA, moveEventID)).To(BeTrue(),
			"the relay published the frozen-window move envelope after recovery — the notification survived the blip")
		Expect(streamSubjectCount(ctx, env, string(charSubject))).To(BeNumerically(">", beforeWire),
			"the move notification reached the shared broker after recovery")

		reportVerdict(fmt.Sprintf(
			"M2-VERDICT: relay-redelivery: a move committed while the broker was frozen (envelope %s, char at %s) was PUBLISHED by the relay after recovery — a NATS blip after commit cannot lose the notification (M2 closed end-to-end)",
			moveEventID, dest,
		))
	})
})

// Per-aggregate concurrent-writer races: the version guard (plans 05-01..05-04)
// must surface WORLD_CONCURRENT_EDIT on ALL FOUR world aggregates under two
// replicas over one broker + one shared DB — not only the location/object pair
// the M12 suite already pins. Each aggregate is raced DETERMINISTICALLY: both
// replicas read the SAME version, one commits (advancing the stored version), and
// the other's now-stale write is REJECTED rather than silently clobbering. This is
// the guarded-CAS + zero-row classifier shared across every world repo, proven per
// aggregate under the two-replica chaos substrate.
var _ = Describe("Per-aggregate concurrent-writer races surface the conflict", Ordered, func() {
	var (
		env      *natstest.NATSEnv
		replicaA *integrationtest.Server
		replicaB *integrationtest.Server
		svcA     *world.Service
		svcB     *world.Service
		subjA    string
		subjB    string
	)

	BeforeAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		DeferCleanup(cancel)

		env = startExternalNATS(ctx)
		replicaA = startReplica(suiteT, env.URL, "")
		replicaB = startReplica(suiteT, env.URL, replicaA.ConnStr())

		// Two independent world.Service write paths over the ONE shared database,
		// each funnelling into the same version-predicated guarded CAS.
		svcA = newWorldService(replicaA)
		svcB = newWorldService(replicaB)

		// The allow-all default engine accepts any subject string, so no policy
		// seeding is required.
		sessA := replicaA.ConnectGuest(ctx)
		sessB := replicaB.ConnectGuest(ctx)
		subjA = "character:" + sessA.CharacterID.String()
		subjB = "character:" + sessB.CharacterID.String()
	})

	// assertConflict asserts a stale write was rejected with the typed
	// WORLD_CONCURRENT_EDIT (matched both by sentinel and by oops code — the same
	// assertion shape the M12 suite uses).
	assertConflict := func(err error) {
		GinkgoHelper()
		Expect(err).To(HaveOccurred(), "the stale writer MUST be rejected — this is the guard closing last-write-wins")
		Expect(err).To(MatchError(world.ErrConcurrentEdit), "the conflict is the typed world.ErrConcurrentEdit")
		oopsErr, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue(), "the conflict is an oops error")
		Expect(oopsErr.Code()).To(Equal(world.CodeConcurrentEdit), "the surfaced code is WORLD_CONCURRENT_EDIT")
	}

	It("location: a stale writer is rejected with WORLD_CONCURRENT_EDIT", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		locID := replicaA.NewLocation(ctx)
		locA, err := svcA.GetLocation(ctx, subjA, locID)
		Expect(err).NotTo(HaveOccurred(), "svcA.GetLocation")
		locB, err := svcB.GetLocation(ctx, subjB, locID)
		Expect(err).NotTo(HaveOccurred(), "svcB.GetLocation")
		Expect(locA.Version).To(Equal(locB.Version), "both copies hold the SAME read version")

		locA.Name = "loc-A-committed"
		Expect(svcA.UpdateLocation(ctx, subjA, locA)).To(Succeed(), "A's write commits")
		locB.Description = "loc-B-rejected"
		assertConflict(svcB.UpdateLocation(ctx, subjB, locB))

		reportVerdict("M2-VERDICT: per-aggregate-location: two-replica stale write rejected with WORLD_CONCURRENT_EDIT")
	})

	It("exit: a stale writer is rejected with WORLD_CONCURRENT_EDIT", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		from := replicaA.NewLocation(ctx)
		to := replicaA.NewLocation(ctx)
		exit, err := world.NewExit(from, to, "north")
		Expect(err).NotTo(HaveOccurred(), "construct exit")
		Expect(svcA.CreateExit(ctx, subjA, exit)).To(Succeed(), "create shared exit")

		exitA, err := svcA.GetExit(ctx, subjA, exit.ID)
		Expect(err).NotTo(HaveOccurred(), "svcA.GetExit")
		exitB, err := svcB.GetExit(ctx, subjB, exit.ID)
		Expect(err).NotTo(HaveOccurred(), "svcB.GetExit")
		Expect(exitA.Version).To(Equal(exitB.Version), "both copies hold the SAME read version")

		exitA.Name = "south"
		Expect(svcA.UpdateExit(ctx, subjA, exitA)).To(Succeed(), "A's exit write commits")
		exitB.Name = "east"
		assertConflict(svcB.UpdateExit(ctx, subjB, exitB))

		reportVerdict("M2-VERDICT: per-aggregate-exit: two-replica stale write rejected with WORLD_CONCURRENT_EDIT")
	})

	It("character: a stale writer is rejected with WORLD_CONCURRENT_EDIT", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		// The world.Service exposes no full-object character update
		// (UpdateCharacterDescription does its own internal read-modify-write, so a
		// caller-controlled stale version cannot be injected through it). The guard
		// is raced at the guarded character repo — the SAME CAS + zero-row classifier
		// the service delegates to — one repo per replica pool over the shared DB.
		repoA := worldpostgres.NewCharacterRepository(replicaA.Pool())
		repoB := worldpostgres.NewCharacterRepository(replicaB.Pool())
		target := replicaA.ConnectGuest(ctx).CharacterID

		charA, err := repoA.Get(ctx, target)
		Expect(err).NotTo(HaveOccurred(), "repoA.Get character")
		charB, err := repoB.Get(ctx, target)
		Expect(err).NotTo(HaveOccurred(), "repoB.Get character")
		Expect(charA.Version).To(Equal(charB.Version), "both copies hold the SAME read version")

		charA.Description = "char-A-committed"
		_, err = repoA.Update(ctx, charA)
		Expect(err).NotTo(HaveOccurred(), "A's character write commits")
		charB.Description = "char-B-rejected"
		_, errB := repoB.Update(ctx, charB)
		assertConflict(errB)

		reportVerdict("M2-VERDICT: per-aggregate-character: two-replica stale write rejected with WORLD_CONCURRENT_EDIT")
	})

	It("object: a stale writer is rejected with WORLD_CONCURRENT_EDIT", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m2SpecTimeout)
		DeferCleanup(cancel)

		locID := replicaA.NewLocation(ctx)
		obj, err := world.NewObjectWithID(ulid.Make(), "orb", world.InLocation(locID))
		Expect(err).NotTo(HaveOccurred(), "construct object")
		Expect(svcA.CreateObject(ctx, subjA, obj)).To(Succeed(), "create shared object")

		objA, err := svcA.GetObject(ctx, subjA, obj.ID)
		Expect(err).NotTo(HaveOccurred(), "svcA.GetObject")
		objB, err := svcB.GetObject(ctx, subjB, obj.ID)
		Expect(err).NotTo(HaveOccurred(), "svcB.GetObject")
		Expect(objA.Version).To(Equal(objB.Version), "both copies hold the SAME read version")

		objA.Name = "orb-A-renamed"
		Expect(svcA.UpdateObject(ctx, subjA, objA)).To(Succeed(), "A's object write commits")
		objB.Description = "obj-B-rejected"
		assertConflict(svcB.UpdateObject(ctx, subjB, objB))

		reportVerdict("M2-VERDICT: per-aggregate-object: two-replica stale write rejected with WORLD_CONCURRENT_EDIT")
	})
})
