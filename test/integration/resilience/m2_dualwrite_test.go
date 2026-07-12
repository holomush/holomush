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

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
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
//     versa (no state-change-without-envelope, no envelope-without-state).
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
})
