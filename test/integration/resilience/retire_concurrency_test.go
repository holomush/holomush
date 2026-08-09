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

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

// retireSpecTimeout mirrors m12SpecTimeout's in-file budget precedent
// (m12_lastwritewins_test.go:24): a per-spec ceiling well above the observed
// runtime, so a wedged replica fails the spec instead of hanging the suite.
const retireSpecTimeout = 3 * time.Minute

// codeAlreadyRetired is the lifecycle-guard code RetireCharacter returns when the
// row is ALREADY in the retired state (internal/world/service.go:972). It is
// spelled here only so the spec can assert the stale caller NEVER sees it — the
// negative half of the guard-order proof. There is no exported constant for it.
const codeAlreadyRetired = "CHARACTER_ALREADY_RETIRED"

// This Describe points the two-replica resilience harness at Phase 3's new
// character-lifecycle command (ROADMAP success criterion 3, IDENT-10,
// INV-WORLD-7). It retargets m12_lastwritewins_test.go's spec-1 mechanism from
// UpdateLocation to RetireCharacter.
//
// The mechanism transfers exactly, but through a different seam. M12's
// UpdateLocation carries its expected version INSIDE the *world.Location struct,
// so "two copies holding the same read version" (m12_lastwritewins_test.go:142)
// is expressed as two independent Get results. Plan 03-01 gave RetireCharacter a
// CALLER-SUPPLIED expectedVersion parameter instead, so the same "both writers
// hold the same read version" precondition is expressed directly: read the row
// ONCE, then hand the SAME integer to both replicas. That makes the conflict
// deterministic under a fully SEQUENTIAL drive — no interleave hook, no timing
// window, and no neutralization of any production predicate.
//
// The spec asserts a PAIR, and the pair is the point:
//
//   - the stale writer is rejected with WORLD_CONCURRENT_EDIT — the guard closes;
//   - the stale writer is NOT told CHARACTER_ALREADY_RETIRED — the guard ORDER
//     holds. 03-01 pins the version precheck BEFORE the lifecycle-state guard
//     (service.go:953-980) precisely so a stale caller learns that its view is
//     stale, rather than learning the racing writer's outcome. Reporting
//     "already retired" would tell the loser its stale version was authoritative
//     and invite it to proceed on that basis.
//
// Both replicas drive the real world.Service over the ONE shared database. All
// read-backs go straight to the shared pgxpool (SELECT the row), never through
// sessions or subscriber frames (RESEARCH Pitfall 6).
var _ = Describe("IDENT-10: a stale caller-held expected_version is rejected by RetireCharacter", Ordered, func() {
	var (
		env      *natstest.NATSEnv
		replicaA *integrationtest.Server
		replicaB *integrationtest.Server
		svcA     *world.Service
		svcB     *world.Service
		charID   ulid.ULID
		subjA    string
		subjB    string
	)

	BeforeAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		DeferCleanup(cancel)

		env = startExternalNATS(ctx)

		// No in-tree plugins: this Describe drives world.Service directly and
		// never dispatches a command, so it takes the lighter boot that
		// boot_smoke / m2_dualwrite / outbox_faultinjection already use.
		// Replica A creates the shared database; B joins it via A.ConnStr().
		replicaA = startReplica(suiteT, env.URL, "")
		replicaB = startReplica(suiteT, env.URL, replicaA.ConnStr())

		// Two independent world.Service write paths over the ONE shared database —
		// both funnel into the same version-predicated guarded CAS on characters.
		svcA = newWorldService(replicaA)
		svcB = newWorldService(replicaB)

		// The retire TARGET: a guest starter character created through the
		// production CreateGuest path on replica A (status active, version 1).
		// GuestPlayer provisions without opening a game session, which is all this
		// spec needs — the retirement reactor that reacts to the envelope does not
		// land until plan 03-04.
		charID = replicaA.GuestPlayer(ctx).CharacterID

		// Production subject shape: each replica retires as its OWN character
		// subject, so the two calls are genuinely two callers. The allow-all
		// default engine (no WithRealABAC on the resilience replicas) accepts any
		// subject string, so no policy seeding is required — the 03-04 retire/
		// unretire grants are not a precondition here.
		subjA = "character:" + replicaA.GuestPlayer(ctx).CharacterID.String()
		subjB = "character:" + replicaB.GuestPlayer(ctx).CharacterID.String()

		reportVerdict(fmt.Sprintf(
			"M12-VERDICT: setup: two replicas booted over one broker + one shared DB; retire target charID=%s", charID,
		))
	})

	// readCharacter reads status+version straight from the shared pool (Pitfall 6:
	// never through sessions or subscriber frames).
	readCharacter := func(ctx context.Context) (status string, version int) {
		ExpectWithOffset(1, replicaA.Pool().QueryRow(ctx,
			`SELECT status, version FROM characters WHERE id = $1`, charID.String()).
			Scan(&status, &version)).To(Succeed(), "read-back SELECT via shared pool")
		return status, version
	}

	It("rejects the stale replica with WORLD_CONCURRENT_EDIT and never with CHARACTER_ALREADY_RETIRED", func() {
		ctx, cancel := context.WithTimeout(context.Background(), retireSpecTimeout)
		DeferCleanup(cancel)

		// ONE read of the row. The captured integer is the caller-held expected
		// version BOTH replicas will supply — the direct analogue of M12's two
		// struct copies carrying the same read version, and the guard's
		// precondition.
		startStatus, readVersion := readCharacter(ctx)
		Expect(startStatus).To(Equal(string(world.StatusActive)),
			"fixture invariant: the target must start active, or the lifecycle guard rather than the version guard would be under test")
		Expect(readVersion).To(BeNumerically(">=", 1), "fixture invariant: a committed character row carries version >= 1")

		// A commits with the shared read version: the precheck matches, the
		// lifecycle guard admits active->retired, and the CAS advances the stored
		// version by exactly one.
		Expect(svcA.RetireCharacter(ctx, world.HumanCaller(subjA), charID, readVersion)).
			To(Succeed(), "A's retire must commit (its expected version still matches the stored row)")

		midStatus, midVersion := readCharacter(ctx)
		Expect(midStatus).To(Equal(string(world.StatusRetired)), "A's retire must have landed the status change")
		Expect(midVersion).To(Equal(readVersion+1), "a committed retire bumps the version exactly once")

		// B drives the SAME caller-held version — now stale by one. The precheck
		// fires BEFORE the lifecycle guard, so B is told its view is stale rather
		// than told A's outcome.
		errB := svcB.RetireCharacter(ctx, world.HumanCaller(subjB), charID, readVersion)
		Expect(errB).To(HaveOccurred(), "B's stale retire MUST be rejected — this is the guard closing on the new command")
		Expect(errB).To(MatchError(world.ErrConcurrentEdit), "B's stale retire must surface the typed conflict")

		oopsErr, ok := oops.AsOops(errB)
		Expect(ok).To(BeTrue(), "conflict must be an oops error")
		Expect(oopsErr.Code()).To(Equal(world.CodeConcurrentEdit),
			"the surfaced code is WORLD_CONCURRENT_EDIT (D-02: propagated unchanged)")
		// The negative half: the guard ORDER proof. A stale caller must never be
		// handed the racing writer's outcome.
		Expect(oopsErr.Code()).NotTo(Equal(codeAlreadyRetired),
			"guard order (03-01 R1): the version precheck runs BEFORE the lifecycle guard, so a stale caller sees the CONFLICT, never %s", codeAlreadyRetired)
		errutil.AssertErrorCode(suiteT, errB, world.CodeConcurrentEdit)

		// The loser left no trace: no second status write, no second version bump.
		endStatus, endVersion := readCharacter(ctx)
		Expect(endStatus).To(Equal(string(world.StatusRetired)), "A's committed retire must SURVIVE B's rejection")
		Expect(endVersion).To(Equal(midVersion),
			"B was rejected — the stored version must NOT have moved a second time (no silent second write)")

		reportVerdict(fmt.Sprintf(
			"M12-VERDICT: retire-stale-version: guard closed on RetireCharacter — B's stale expected_version=%d surfaced WORLD_CONCURRENT_EDIT (not %s); A's retire committed at version %d->%d (charID=%s)",
			readVersion, codeAlreadyRetired, readVersion, midVersion, charID,
		))
	})
})
