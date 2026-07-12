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

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world"
)

const m12SpecTimeout = 3 * time.Minute

// M12 is the OPS-05 decision input (D-06): the world model is direct-write CRUD
// in Postgres — every world repo Update is a full-row UPDATE with NO version
// guard (internal/world/postgres/location_repo.go:73 rewrites name, description,
// type and shadows_id from the caller's in-memory copy) and the tables carry no
// version column (000001_baseline.up.sql). The read-modify-write lives in
// internal/property/entity_mutator.go (GetLocation → mutate ONE field →
// UpdateLocation(full object)). Under two replicas racing a write to the same
// row, one replica's stale full-row write silently clobbers a field another
// replica already committed, and neither call ever surfaces a conflict.
//
// This Describe reproduces that lost update in three graduated forms:
//
//  1. deterministic mechanism proof — explicit interleave, 1 round, always
//     reproduces (this IS the D-06 corruption verdict's mechanism);
//  2. command-fidelity race — two real `describe here` commands (one per
//     replica) race; both report success while N writes are silently superseded;
//  3. hybrid cross-field race — a command races a service-side rename; counts a
//     k/N lost-update rate over the natural (uncontrolled) timing window.
//
// All read-backs go straight to the shared pgxpool (SELECT the row), never
// through sessions or subscriber frames (RESEARCH Pitfall 6).
var _ = Describe("M12 last-write-wins", Ordered, func() {
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
		// spec (#2) can drive `describe here` on EITHER replica. Replica A creates
		// the shared database; B joins it via A.ConnStr(). Plan 01 proved the
		// two-replica shared-DB boot is clean (plugin migrations are idempotent on
		// the already-migrated schema; guest seeding uses fresh ULIDs), so the
		// assumption-A4 "boot B without plugins" fallback was NOT needed here.
		replicaA = startReplica(suiteT, env.URL, "", integrationtest.WithInTreePlugins())
		replicaB = startReplica(suiteT, env.URL, replicaA.ConnStr(), integrationtest.WithInTreePlugins())

		// Two independent world.Service write paths over the ONE shared pool —
		// each funnels into the same unguarded full-row UPDATE.
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
			"M12-VERDICT: setup: two replicas booted over one broker + one shared DB with in-tree plugins; A4 dual-plugin fallback NOT needed (locID=%s)", locID,
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

	It("deterministic interleave: a stale full-row write silently reverts a committed rename", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m12SpecTimeout)
		DeferCleanup(cancel)

		// Both replicas read the SAME row and hold independent in-memory copies —
		// entity_mutator's GetLocation step, done twice with explicit interleave
		// control. repo.Get constructs a fresh struct per call, so locA and locB
		// are independent (a mutation to one does not touch the other).
		locA, err := svcA.GetLocation(ctx, subjA, locID)
		Expect(err).NotTo(HaveOccurred(), "svcA.GetLocation")
		locB, err := svcB.GetLocation(ctx, subjB, locID)
		Expect(err).NotTo(HaveOccurred(), "svcB.GetLocation")

		originalName := locB.Name
		Expect(originalName).NotTo(BeEmpty(), "location must have a starting name")

		const committedName = "name-A-committed"
		const committedDesc = "desc-B-committed"
		Expect(originalName).NotTo(Equal(committedName), "test fixture invariant")

		// A commits a rename using A's copy → DB now: name=committedName.
		locA.Name = committedName
		Expect(svcA.UpdateLocation(ctx, subjA, locA)).
			To(Succeed(), "A's rename must commit with no error")

		// B commits a description change using B's STALE copy — whose Name still
		// holds the ORIGINAL. The unguarded full-row UPDATE rewrites name back to
		// that stale value, silently destroying A's committed rename. B's call
		// ALSO returns nil: no conflict is ever surfaced.
		locB.Description = committedDesc
		Expect(svcB.UpdateLocation(ctx, subjB, locB)).
			To(Succeed(), "B's stale write must ALSO succeed — no conflict surfaced (this is the corruption)")

		gotName, gotDesc := readBack(ctx)

		Expect(gotDesc).To(Equal(committedDesc), "B's description change landed")
		Expect(gotName).To(Equal(originalName),
			"M12 lost update: A's committed rename %q was silently reverted to the original %q by B's stale full-row write",
			committedName, originalName)
		Expect(gotName).NotTo(Equal(committedName), "A's committed rename must be GONE from the row")

		reportVerdict(fmt.Sprintf(
			"M12-VERDICT: deterministic-interleave: reproduced deterministically (A's rename %q->%q silently reverted to %q by B's stale full-row UPDATE; both UpdateLocation calls returned nil)",
			originalName, committedName, gotName,
		))
	})

	It("concurrent describe commands both succeed with no conflict signal", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m12SpecTimeout)
		DeferCleanup(cancel)

		const rounds = 50
		supersededWrites := 0

		for n := 0; n < rounds; n++ {
			alpha := fmt.Sprintf("alpha-%d", n)
			beta := fmt.Sprintf("beta-%d", n)

			// Start-gun: both goroutines block until the channel closes, then race
			// the two real command dispatches. No sleeps anywhere (RESEARCH
			// Pattern 3) — the WaitGroup joins both before we read back.
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

			Expect(errA).NotTo(HaveOccurred(), "round %d: replica A `describe here` must report success", n)
			Expect(errB).NotTo(HaveOccurred(), "round %d: replica B `describe here` must report success", n)

			_, gotDesc := readBack(ctx)
			Expect(gotDesc).To(Or(Equal(alpha), Equal(beta)),
				"round %d: the surviving description must be exactly one of the two racing writes (got %q)", n, gotDesc)

			// Exactly one of the two committed writes survives; the other was
			// silently superseded with no conflict ever raised.
			supersededWrites++
		}

		reportVerdict(fmt.Sprintf(
			"M12-VERDICT: concurrent-describe: both-succeed-no-conflict N=%d (every round: both commands returned success, one write silently superseded, zero conflicts surfaced; %d writes lost)",
			rounds, supersededWrites,
		))
	})

	It("cross-field race: a concurrent command resurrects a stale field", func() {
		ctx, cancel := context.WithTimeout(context.Background(), m12SpecTimeout)
		DeferCleanup(cancel)

		const (
			maxRounds  = 100
			targetLost = 5
		)
		lost := 0
		ranRounds := 0

		for n := 0; n < maxRounds && lost < targetLost; n++ {
			ranRounds++
			descText := fmt.Sprintf("text-%d", n)
			renameTo := fmt.Sprintf("name-%d", n)

			// Race a real `describe here` command (replica A, full-row UPDATE of
			// description with a stale name) against a service-side rename on
			// replica B (Get-fresh → set Name → full-row UPDATE with a stale
			// description). Whichever full-row write lands last with a stale copy
			// resurrects the OTHER field's prior value.
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

			Expect(errA).NotTo(HaveOccurred(), "round %d: A `describe here` must succeed", n)
			Expect(errB).NotTo(HaveOccurred(), "round %d: B rename must succeed", n)

			gotName, gotDesc := readBack(ctx)
			// A lost update = the row does NOT reflect BOTH concurrent writes: a
			// stale full-row UPDATE resurrected a superseded field.
			if gotName != renameTo || gotDesc != descText {
				lost++
			}
		}

		// k/N is the natural-window width, NOT a refutation lever: 0/N does NOT
		// refute M12 — spec 1's deterministic proof stands. A zero here would only
		// mean the uncontrolled timing window never interleaved, which the message
		// must state explicitly.
		result := fmt.Sprintf("k=%d of N=%d natural-window races lost", lost, ranRounds)
		if lost == 0 {
			result += " (window never interleaved; NOT a refutation — the deterministic proof in spec 1 stands)"
		}
		reportVerdict("M12-VERDICT: cross-field-race: " + result)
	})
})
