// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package resilience_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world"
)

const m12SpecTimeout = 3 * time.Minute

// M12 was the OPS-05 last-write-wins finding (#4798): the world model is
// direct-write CRUD in Postgres, and — before Phase 5 — every world repo Update
// was an UNGUARDED full-row UPDATE with no version column, so two replicas
// racing a write to the same row silently clobbered each other and neither call
// ever surfaced a conflict. Plan 05-01 added `version INTEGER NOT NULL DEFAULT
// 1` to all four world tables; plans 05-02/05-03 made every repo Update/Delete a
// version-predicated CAS (`... WHERE id = $1 AND version = $expected`) whose
// zero-row result is classified into the typed world.ErrConcurrentEdit
// (WORLD_CONCURRENT_EDIT). Reads populate struct.Version and the read-modify-write
// path (internal/property/entity_mutator.go: GetLocation → mutate ONE field →
// UpdateLocation(full object), and world.Service.UpdateCharacterDescription)
// threads that read version into the guarded write. Plan 05-04 is where the
// guard closes the loop end-to-end.
//
// This Describe is the standing D-05 regression gate that proves the guard
// closes last-write-wins. It now asserts the SURFACED CONFLICT reality (it
// previously asserted "both writers return nil, one write silently lost"):
//
//  1. deterministic interleave (location, service level) — both replicas read
//     the same version, one commits, the stale writer is REJECTED with
//     WORLD_CONCURRENT_EDIT and the committed rename SURVIVES (no silent revert);
//  2. command-fidelity race — two real `describe here` commands (one per
//     replica) race; at most one commits, the loser surfaces a conflict (never a
//     silent overwrite), and the surviving value is always a genuine committed
//     write;
//  3. cross-field race — a real `describe here` command races a service-side
//     rename; a stale full-row write is rejected rather than silently
//     resurrecting the other field's superseded value;
//  4. per-aggregate guard (object, service level) — the guard rejects the stale
//     writer for a NON-location aggregate too.
//
// All read-backs go straight to the shared pgxpool (SELECT the row), never
// through sessions or subscriber frames (RESEARCH Pitfall 6).
var _ = Describe("M12 last-write-wins closed by the version guard", Ordered, func() {
	var (
		env      *natstest.NATSEnv
		replicaA *integrationtest.Server
		replicaB *integrationtest.Server
		svcA     *world.Service
		svcB     *world.Service
		sessA    *integrationtest.Session
		sessB    *integrationtest.Session
		locID    ulid.ULID
		subjA    string
		subjB    string
	)

	BeforeAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		DeferCleanup(cancel)

		env = startExternalNATS(ctx)

		// Both replicas boot WITH the in-tree plugins so the command-fidelity
		// specs can drive `describe here` on EITHER replica. Replica A creates
		// the shared database; B joins it via A.ConnStr(). Plan 01 proved the
		// two-replica shared-DB boot is clean (plugin migrations are idempotent on
		// the already-migrated schema; guest seeding uses fresh ULIDs), so the
		// assumption-A4 "boot B without plugins" fallback was NOT needed here.
		replicaA = startReplica(suiteT, env.URL, "", integrationtest.WithInTreePlugins())
		replicaB = startReplica(suiteT, env.URL, replicaA.ConnStr(), integrationtest.WithInTreePlugins())

		// Two independent world.Service write paths over the ONE shared database —
		// each funnels into the same version-predicated guarded CAS Update.
		svcA = newWorldService(replicaA)
		svcB = newWorldService(replicaB)

		// One shared location; one guest session per replica, both focused on it.
		locID = replicaA.NewLocation(ctx)
		sessA = replicaA.ConnectGuest(ctx)
		sessB = replicaB.ConnectGuest(ctx)
		sessA.MoveTo(ctx, locID)
		sessB.MoveTo(ctx, locID)

		// Production subject shape for the direct service-write path: the property
		// registry stamps a character subject. The allow-all default engine (no
		// WithRealABAC on these replicas) accepts any string, so no policy seeding
		// is required.
		subjA = "character:" + sessA.CharacterID.String()
		subjB = "character:" + sessB.CharacterID.String()

		reportVerdict(fmt.Sprintf(
			"M12-VERDICT: setup: two replicas booted over one broker + one shared DB with in-tree plugins; version guard active (locID=%s)", locID,
		))
	})

	// readBack reads name+description straight from the shared pool (Pitfall 6:
	// never through sessions or subscriber frames).
	readBack := func(ctx context.Context) (name, desc string) {
		ExpectWithOffset(1, replicaA.Pool().QueryRow(ctx,
			`SELECT name, description FROM locations WHERE id = $1`, locID.String()).
			Scan(&name, &desc)).To(Succeed(), "read-back SELECT via shared pool")
		return name, desc
	}

	It("deterministic interleave: the stale writer is rejected with WORLD_CONCURRENT_EDIT and the committed rename survives", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m12SpecTimeout)
		DeferCleanup(cancel)

		// Both replicas read the SAME row and hold independent in-memory copies
		// carrying the SAME read version — entity_mutator's GetLocation step, done
		// twice with explicit interleave control. repo.Get constructs a fresh
		// struct per call (each with the stored version), so locA and locB are
		// independent.
		locA, err := svcA.GetLocation(ctx, subjA, locID)
		Expect(err).NotTo(HaveOccurred(), "svcA.GetLocation")
		locB, err := svcB.GetLocation(ctx, subjB, locID)
		Expect(err).NotTo(HaveOccurred(), "svcB.GetLocation")

		originalName := locB.Name
		originalDesc := locB.Description
		Expect(originalName).NotTo(BeEmpty(), "location must have a starting name")

		const committedName = "name-A-committed"
		const committedDesc = "desc-B-rejected"
		Expect(originalName).NotTo(Equal(committedName), "test fixture invariant")
		Expect(locA.Version).To(Equal(locB.Version), "both copies hold the SAME read version — the guard's precondition")

		// A commits a rename using A's copy → the CAS matches (version == read)
		// and the stored version advances by one.
		locA.Name = committedName
		Expect(svcA.UpdateLocation(ctx, subjA, locA)).
			To(Succeed(), "A's rename must commit (its read version still matches)")

		// B commits a description change using B's STALE copy — whose version now
		// trails the stored row. The guarded CAS matches zero rows and the
		// zero-row classifier surfaces the typed conflict. B's write is REJECTED,
		// NOT silently applied: A's committed rename is not clobbered.
		locB.Description = committedDesc
		errB := svcB.UpdateLocation(ctx, subjB, locB)
		Expect(errB).To(HaveOccurred(), "B's stale write MUST be rejected — this is the guard closing M12")
		Expect(errB).To(MatchError(world.ErrConcurrentEdit),
			"B's stale write must surface the typed conflict")
		oopsErr, ok := oops.AsOops(errB)
		Expect(ok).To(BeTrue(), "conflict must be an oops error")
		Expect(oopsErr.Code()).To(Equal(world.CodeConcurrentEdit),
			"the surfaced code is WORLD_CONCURRENT_EDIT (D-02: propagated unchanged)")

		gotName, gotDesc := readBack(ctx)
		Expect(gotName).To(Equal(committedName),
			"A's committed rename %q must SURVIVE (no stale full-row revert)", committedName)
		Expect(gotDesc).To(Equal(originalDesc),
			"B's rejected write must NOT have landed its description change")
		Expect(gotDesc).NotTo(Equal(committedDesc), "the rejected value must be absent from the row")

		reportVerdict(fmt.Sprintf(
			"M12-VERDICT: deterministic-interleave: guard closed last-write-wins — B's stale write surfaced WORLD_CONCURRENT_EDIT; A's rename %q->%q survived (B rejected, no silent revert)",
			originalName, committedName,
		))
	})

	It("concurrent describe commands: no silent overwrite; any command-level conflict is surfaced", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m12SpecTimeout)
		DeferCleanup(cancel)

		const rounds = 50
		conflicts := 0

		for n := 0; n < rounds; n++ {
			alpha := fmt.Sprintf("alpha-%d", n)
			beta := fmt.Sprintf("beta-%d", n)

			// Start-gun: both goroutines block until the channel closes, then race
			// the two real command dispatches. No sleeps anywhere (RESEARCH
			// Pattern 3); the WaitGroup joins both before we read back.
			start := make(chan struct{})
			var wg sync.WaitGroup
			var errA, errB error
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				errA = sessA.SendCommand(ctx, "describe here "+alpha)
			}()
			go func() {
				defer wg.Done()
				<-start
				errB = sessB.SendCommand(ctx, "describe here "+beta)
			}()
			close(start)
			wg.Wait()

			// The guard NEVER loses both writes: at least one command commits.
			Expect(errA == nil || errB == nil).To(BeTrue(),
				"round %d: at least one `describe here` must commit (errA=%v, errB=%v)", n, errA, errB)

			_, gotDesc := readBack(ctx)

			switch {
			case errA != nil && errB == nil:
				// A raced-and-lost: its describe was rejected (the only failure
				// mode here is the surfaced conflict — allow-all engine, valid
				// input, existing row), so B's write is the survivor.
				conflicts++
				Expect(gotDesc).To(Equal(beta),
					"round %d: A was rejected, so B's write must be the survivor (no silent overwrite)", n)
			case errB != nil && errA == nil:
				conflicts++
				Expect(gotDesc).To(Equal(alpha),
					"round %d: B was rejected, so A's write must be the survivor (no silent overwrite)", n)
			default:
				// Both committed (the dispatches serialized): both writes applied
				// in order, the later one survives — still no silent revert.
				Expect(gotDesc).To(Or(Equal(alpha), Equal(beta)),
					"round %d: the survivor must be exactly one of the two racing writes (got %q)", n, gotDesc)
			}
		}

		// The command dispatch through HandleCommand tends to SERIALIZE the two
		// describes (each command's read→guarded-write completes as a unit
		// relative to the other), so conflicts at this layer are a natural-window
		// count and 0 is common — that does NOT refute the guard: every round's
		// survivor was a genuine committed write with zero silent overwrites, and
		// specs 1 and 4 prove WORLD_CONCURRENT_EDIT is surfaced deterministically
		// at the service level (where the interleave is controlled). Any command
		// that DID race-and-lose surfaced the conflict (the loser's survivor
		// assertion above), never a silent last-write-wins.
		result := fmt.Sprintf("N=%d rounds, zero silent overwrites; %d rounds surfaced WORLD_CONCURRENT_EDIT to the losing command", rounds, conflicts)
		if conflicts == 0 {
			result += " (dispatch serialized the races; NOT a refutation — specs 1 and 4 prove the surfaced conflict deterministically)"
		}
		reportVerdict("M12-VERDICT: concurrent-describe: " + result)
	})

	It("cross-field race: a stale writer is rejected, never silently resurrecting a superseded field", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m12SpecTimeout)
		DeferCleanup(cancel)

		const (
			maxRounds       = 100
			targetConflicts = 5
		)
		conflicts := 0
		ranRounds := 0

		for n := 0; n < maxRounds && conflicts < targetConflicts; n++ {
			ranRounds++
			descText := fmt.Sprintf("text-%d", n)
			renameTo := fmt.Sprintf("name-%d", n)

			// Race a real `describe here` command (replica A, guarded full-row
			// UPDATE of description) against a service-side rename on replica B
			// (Get-fresh → set Name → guarded full-row UPDATE). Whichever writer
			// races-and-loses holds a stale version and is REJECTED — it can no
			// longer resurrect the other field's prior value.
			start := make(chan struct{})
			var wg sync.WaitGroup
			var errA, errB error
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				errA = sessA.SendCommand(ctx, "describe here "+descText)
			}()
			go func() {
				defer wg.Done()
				<-start
				loc, getErr := svcB.GetLocation(ctx, subjB, locID)
				if getErr != nil {
					errB = getErr
					return
				}
				loc.Name = renameTo
				errB = svcB.UpdateLocation(ctx, subjB, loc)
			}()
			close(start)
			wg.Wait()

			// Different fields: if the dispatches serialize, BOTH land; if they
			// truly interleave, exactly one is rejected. Both-rejected is
			// impossible.
			Expect(errA == nil || errB == nil).To(BeTrue(),
				"round %d: at least one write must commit (errA=%v, errB=%v)", n, errA, errB)

			gotName, gotDesc := readBack(ctx)

			if errB != nil {
				// B (service) raced-and-lost → surfaced conflict, rename not landed.
				conflicts++
				Expect(errB).To(MatchError(world.ErrConcurrentEdit),
					"round %d: B's stale rename must surface WORLD_CONCURRENT_EDIT", n)
				Expect(gotName).NotTo(Equal(renameTo),
					"round %d: B was rejected — its rename must NOT have landed", n)
			}
			if errA != nil {
				// A (command) raced-and-lost → its describe was rejected.
				conflicts++
				Expect(gotDesc).NotTo(Equal(descText),
					"round %d: A was rejected — its describe must NOT have landed", n)
			}
			if errA == nil && errB == nil {
				// Serialized: both writes applied, no silent loss on either field.
				Expect(gotName).To(Equal(renameTo), "round %d: serialized rename must land", n)
				Expect(gotDesc).To(Equal(descText), "round %d: serialized describe must land", n)
			}
		}

		// conflicts is the natural-window width, NOT a refutation lever: 0 does NOT
		// refute — the deterministic proofs in specs 1 and 4 stand. A zero here
		// would only mean the uncontrolled timing window never interleaved.
		result := fmt.Sprintf("%d of N=%d natural-window races surfaced WORLD_CONCURRENT_EDIT to the stale writer (zero silent field-resurrections)", conflicts, ranRounds)
		if conflicts == 0 {
			result += " (window never interleaved; NOT a refutation — the deterministic proofs in specs 1 and 4 stand)"
		}
		reportVerdict("M12-VERDICT: cross-field-race: " + result)
	})

	It("per-aggregate guard: an object stale writer is also rejected with WORLD_CONCURRENT_EDIT", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m12SpecTimeout)
		DeferCleanup(cancel)

		// A shared object in the location, so the guard is exercised on a
		// NON-location aggregate (the CAS + zero-row classifier are shared across
		// all four aggregates; this pins that characters/objects surface the
		// conflict too, not only locations).
		obj, err := world.NewObjectWithID(ulid.Make(), "orb", world.InLocation(locID))
		Expect(err).NotTo(HaveOccurred(), "construct object")
		Expect(svcA.CreateObject(ctx, subjA, obj)).To(Succeed(), "create shared object")
		objID := obj.ID

		objA, err := svcA.GetObject(ctx, subjA, objID)
		Expect(err).NotTo(HaveOccurred(), "svcA.GetObject")
		objB, err := svcB.GetObject(ctx, subjB, objID)
		Expect(err).NotTo(HaveOccurred(), "svcB.GetObject")
		Expect(objA.Version).To(Equal(objB.Version), "both copies hold the SAME read version")

		const renamed = "orb-A-renamed"
		const rejectedDesc = "desc-B-rejected"

		// A commits a rename → the stored version advances.
		objA.Name = renamed
		Expect(svcA.UpdateObject(ctx, subjA, objA)).To(Succeed(), "A's object rename must commit")

		// B commits a description change on its STALE copy → rejected.
		objB.Description = rejectedDesc
		errB := svcB.UpdateObject(ctx, subjB, objB)
		Expect(errB).To(HaveOccurred(), "B's stale object write MUST be rejected")
		Expect(errB).To(MatchError(world.ErrConcurrentEdit), "object stale write surfaces the typed conflict")
		oopsErr, ok := oops.AsOops(errB)
		Expect(ok).To(BeTrue(), "conflict must be an oops error")
		Expect(oopsErr.Code()).To(Equal(world.CodeConcurrentEdit), "surfaced code is WORLD_CONCURRENT_EDIT")

		var gotName, gotDesc string
		Expect(replicaA.Pool().QueryRow(ctx,
			`SELECT name, description FROM objects WHERE id = $1`, objID.String()).
			Scan(&gotName, &gotDesc)).To(Succeed(), "object read-back via shared pool")
		Expect(gotName).To(Equal(renamed), "A's committed object rename must survive")
		Expect(gotDesc).NotTo(Equal(rejectedDesc), "B's rejected object write must NOT have landed")

		reportVerdict(fmt.Sprintf(
			"M12-VERDICT: per-aggregate-object: guard rejected B's stale object write with WORLD_CONCURRENT_EDIT; A's rename %q survived (objID=%s)",
			renamed, objID,
		))
	})
})
