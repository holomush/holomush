// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package presence_test integration spec for holomush-rsoe6.13:
// lease-driven liveness end-to-end (deadlock resolution).
package presence_test

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Verifies: I-LIVE-2
// Verifies: I-LIVE-3
// Verifies: I-LIVENESS-PRES-1
//
// Verifies (against the design invariant catalog, §297-309):
//   - I-LIVE-2 (expiry): a connection whose last_seen_at is older than the
//     lease cutoff is swept.
//   - I-LIVE-3 (derivation): with zero live connections the session is no
//     longer active and grid_present is cleared, so presence empties.
//   - INV-PRESENCE-1: the presence roster reflects grid_present.
//
// End-to-end goal (the deadlock resolution motivating this epic): the lease
// sweep → session detach → session expiry chain unblocks the guest reaper,
// which then collects the now-idle guest. Guest collection itself is not a
// named I-LIVE invariant — it is the cross-system outcome the chain enables.
//
// Scenario: a connected guest is visible in presence. Its connection lease
// lapses; the session reaper sweeps the lapsed connection, detaches the
// session, and clears grid_present (emptying presence). The session then
// expires and is deleted. With no active/detached session remaining, the idle
// guest becomes eligible for the guest reaper, which collects it.
var _ = Describe("Lease-driven liveness", func() {
	var (
		ts     *integrationtest.Server
		ctx    context.Context
		cancel context.CancelFunc
		guest  *integrationtest.Session
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		ts = integrationtest.Start(suiteT)
		guest = ts.ConnectGuest(ctx)
	})

	AfterEach(func() {
		// ts.Stop() releases embedded NATS and Postgres testcontainer resources.
		if ts != nil {
			ts.Stop()
		}
		if cancel != nil {
			cancel()
		}
	})

	It("clears presence and unblocks the guest reaper when a connection lease lapses", func() {
		pool := ts.Pool()
		sessStore := ts.SessionStore()

		// Phase 1 — precondition: guest appears in presence (active session,
		// grid_present=true, terminal connection registered by Subscribe).
		Eventually(func(g Gomega) {
			resp, err := guest.ListFocusPresence(ctx)
			g.Expect(err).NotTo(HaveOccurred(),
				"ListFocusPresence MUST succeed for an active guest session")
			g.Expect(entryNames(resp.GetEntries())).To(ContainElement(guest.CharacterName),
				"guest MUST appear in presence while active and grid_present=true")
		}, 2*time.Second, 50*time.Millisecond).Should(Succeed())

		// Simulate a lapsed gateway: backdate last_seen_at on all connections for
		// this session to 2 hours ago. The reaper's cutoff = Now() - LeaseTTL
		// (30 minutes), so 2h ago is well past the cutoff.
		_, err := pool.Exec(ctx,
			`UPDATE session_connections SET last_seen_at = $1 WHERE session_id = $2`,
			pgnanos.From(time.Now().Add(-2*time.Hour)), guest.SessionID)
		Expect(err).NotTo(HaveOccurred(), "backdate last_seen_at to simulate lapsed connection")

		// Phase 2 — Run session reaper (lease sweep): Now set to current time
		// with LeaseTTL=30m. Cutoff = now - 30m; connection last seen 2h ago →
		// swept. recomputeAfterSweep finds no connections → detaches session +
		// clears grid_present. BootGrace=0 so the sweep fires immediately.
		now := time.Now()
		sessReaper := session.NewReaper(sessStore, session.ReaperConfig{
			Interval:  50 * time.Millisecond,
			LeaseTTL:  30 * time.Minute,
			BootGrace: 0,
			Now:       func() time.Time { return now },
		})
		reaperCtx, reaperCancel := context.WithTimeout(ctx, 3*time.Second)
		// Safety net: guarantee teardown even if an Eventually below fails
		// (Ginkgo's runtime.Goexit would skip the inline cancel). CancelFunc is
		// idempotent, so the inline reaperCancel() used for phase sequencing is
		// harmless to call twice.
		defer reaperCancel()
		reaper1Done := make(chan struct{})
		go func() {
			defer close(reaper1Done)
			sessReaper.Run(reaperCtx)
		}()

		// Phase 3 — presence MUST empty after the lapse sweep.
		Eventually(func(g Gomega) {
			presResp, presErr := guest.ListFocusPresence(ctx)
			g.Expect(presErr).NotTo(HaveOccurred())
			g.Expect(entryNames(presResp.GetEntries())).NotTo(ContainElement(guest.CharacterName),
				"I-LIVE-3/INV-PRESENCE-1: guest MUST NOT appear in presence after the lease-lapse sweep "+
					"(zero live connections ⇒ grid_present cleared)")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Stop the first reaper once presence is empty.
		reaperCancel()
		<-reaper1Done

		// Verify the session is now detached.
		sessInfo, getErr := sessStore.Get(ctx, guest.SessionID)
		Expect(getErr).NotTo(HaveOccurred(), "session row MUST still exist (detached, not yet expired)")
		Expect(sessInfo.Status).To(Equal(session.StatusDetached),
			"session MUST be detached after last connection swept")

		// Phase 4 — expire the session: push expires_at into the past so the DB's
		// now() picks it up. ListExpired selects "status='detached' AND
		// expires_at < (DB now)".
		_, err = pool.Exec(ctx,
			`UPDATE sessions SET expires_at = $1 WHERE id = $2 AND status = 'detached'`,
			pgnanos.From(time.Now().Add(-time.Second)), guest.SessionID)
		Expect(err).NotTo(HaveOccurred(), "push session expires_at into the past")

		// Run session reaper again: reapExpired will find, mark expired, and
		// delete the session row.
		sessReaper2 := session.NewReaper(sessStore, session.ReaperConfig{
			Interval:  50 * time.Millisecond,
			LeaseTTL:  30 * time.Minute,
			BootGrace: 0,
			Now:       func() time.Time { return time.Now() },
		})
		reaperCtx2, reaperCancel2 := context.WithTimeout(ctx, 3*time.Second)
		defer reaperCancel2() // safety net; idempotent with the inline cancel below
		reaper2Done := make(chan struct{})
		go func() {
			defer close(reaper2Done)
			sessReaper2.Run(reaperCtx2)
		}()

		// Wait until the session row is gone.
		Eventually(func() bool {
			_, e := sessStore.Get(ctx, guest.SessionID)
			return e != nil // SESSION_NOT_FOUND once deleted
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
			"session MUST be deleted after the expiry sweep (TTL expiry; "+
				"precondition for guest-reaper eligibility)")

		reaperCancel2()
		<-reaper2Done

		// Phase 5 — guest reaper collects the now-idle guest.
		// Precondition: ListIdleGuests requires updated_at < idleSince. Back-date
		// the player's updated_at to 2 hours ago so the 30-minute IdleTTL window
		// makes it eligible (idleSince = now - 30m; 2h ago < idleSince).
		Expect(guest.PlayerID).NotTo(Equal(ulid.ULID{}),
			"precondition: guest.PlayerID MUST be non-zero (populated by ConnectGuest)")
		ts.BackdateGuestPlayer(ctx, guest.PlayerID, time.Now().Add(-2*time.Hour))

		// Confirm the guest IS idle before running the reaper.
		playerRepo := authpg.NewPlayerRepository(pool)
		idleBefore, listErr := playerRepo.ListIdleGuests(ctx, time.Now().Add(-30*time.Minute))
		Expect(listErr).NotTo(HaveOccurred())
		var foundBefore bool
		for _, p := range idleBefore {
			if p.ID == guest.PlayerID {
				foundBefore = true
			}
		}
		Expect(foundBefore).To(BeTrue(),
			"guest player MUST appear in ListIdleGuests before guest reaper runs")

		// Run the guest reaper. Use a mutex to protect the reaped slice from the
		// data race between the OnReaped goroutine (writes) and Gomega's Eventually
		// poller (reads).
		var (
			reapedMu sync.Mutex
			reaped   []ulid.ULID
		)
		guestReaper := auth.NewGuestReaper(auth.GuestReaperConfig{
			Interval: 50 * time.Millisecond,
			IdleTTL:  30 * time.Minute,
			OnReaped: func(playerID ulid.ULID) {
				reapedMu.Lock()
				reaped = append(reaped, playerID)
				reapedMu.Unlock()
			},
		}, playerRepo, playerRepo)

		grCtx, grCancel := context.WithTimeout(ctx, 5*time.Second)
		defer grCancel()
		go guestReaper.Run(grCtx)

		// Phase 6 — assert the guest was collected.
		Eventually(func() int {
			reapedMu.Lock()
			defer reapedMu.Unlock()
			return len(reaped)
		}, 5*time.Second, 50*time.Millisecond).Should(Equal(1),
			"deadlock resolution: the idle guest MUST be collected by the guest reaper "+
				"once its session is gone")
		reapedMu.Lock()
		firstReaped := reaped[0]
		reapedMu.Unlock()
		Expect(firstReaped).To(Equal(guest.PlayerID),
			"reaped player ID MUST match the guest's player ID")

		// Confirm the player row is gone from ListIdleGuests.
		idleAfter, listErr2 := playerRepo.ListIdleGuests(ctx, time.Now().Add(time.Hour))
		Expect(listErr2).NotTo(HaveOccurred())
		for _, p := range idleAfter {
			Expect(p.ID).NotTo(Equal(guest.PlayerID),
				"reaped guest player MUST NOT appear in ListIdleGuests after collection")
		}
	})
})
