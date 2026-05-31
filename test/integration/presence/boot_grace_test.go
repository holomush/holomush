// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package presence_test integration spec for holomush-rsoe6.14:
// boot-grace suppression (I-LIVE-4 / G2).
package presence_test

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Verifies (against the design invariant catalog §297-309):
//   - I-LIVE-4 (boot grace): the lease sweep MUST NOT reap any connection
//     within the boot-grace window after core process start.
//
// Spec scenario: G2 — a core restart does not interrupt a live client. A
// surviving gateway holds the client socket across the restart and re-asserts
// liveness; the freshly-constructed reaper (bootAt=now) MUST NOT sweep the
// connection even though its last_seen_at is backdated past the lease TTL.
//
// The backdated last_seen_at is deliberate: without boot-grace the connection
// WOULD be swept (proved by I-LIVE-2 in lease_reaper_test.go). The protection
// is real — a trivially non-lapsed connection would pass regardless of whether
// boot-grace is working.
var _ = Describe("Boot-grace suppression (I-LIVE-4 / G2)", func() {
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
		if ts != nil {
			ts.Stop()
		}
		if cancel != nil {
			cancel()
		}
	})

	It("does not detach or phase out a session with a lapsed-looking lease while inside the boot-grace window", func() {
		pool := ts.Pool()
		sessStore := ts.SessionStore()

		// Phase 1 — precondition: guest is visible in presence (active session,
		// grid_present=true, terminal connection registered by Subscribe).
		Eventually(func(g Gomega) {
			resp, err := guest.ListFocusPresence(ctx)
			g.Expect(err).NotTo(HaveOccurred(),
				"ListFocusPresence MUST succeed for an active guest session")
			g.Expect(entryNames(resp.GetEntries())).To(ContainElement(guest.CharacterName),
				"guest MUST appear in presence while active and grid_present=true")
		}, 2*time.Second, 50*time.Millisecond).Should(Succeed())

		// Phase 2 — backdate last_seen_at to 2 hours ago so the connection
		// LOOKS lapsed to the sweep (cutoff = Now - LeaseTTL = now - 30m;
		// 2h ago is well past the cutoff). Without boot-grace this would be
		// swept by I-LIVE-2; boot-grace must suppress it (I-LIVE-4).
		_, err := pool.Exec(ctx,
			`UPDATE session_connections SET last_seen_at = $1 WHERE session_id = $2`,
			pgnanos.From(time.Now().Add(-2*time.Hour)), guest.SessionID)
		Expect(err).NotTo(HaveOccurred(),
			"backdate last_seen_at to simulate a lapsed-but-boot-grace-protected connection (I-LIVE-4 proof requires a non-trivially-lapsed lease)")

		// Phase 3 — construct a FRESH reaper: bootAt is stamped to now() at
		// construction, so Now()-bootAt ≈ 0, which is < BootGrace (1h).
		// The condition `Now().Sub(bootAt) >= BootGrace` is false for every
		// tick during this test, so reapLapsedConnections is never called.
		now := time.Now()
		var (
			callbackMu   sync.Mutex
			detached     []*session.Info
			expired      []*session.Info
			gridPhaseOut []*session.Info
		)
		recordDetached := func(info *session.Info) {
			callbackMu.Lock()
			detached = append(detached, info)
			callbackMu.Unlock()
		}
		recordExpired := func(info *session.Info) {
			callbackMu.Lock()
			expired = append(expired, info)
			callbackMu.Unlock()
		}
		recordGridPhaseOut := func(info *session.Info) {
			callbackMu.Lock()
			gridPhaseOut = append(gridPhaseOut, info)
			callbackMu.Unlock()
		}

		reaper := session.NewReaper(sessStore, session.ReaperConfig{
			Interval:          50 * time.Millisecond,
			LeaseTTL:          30 * time.Minute,
			BootGrace:         time.Hour, // Now is frozen at construction, so Now()-bootAt stays ≈0 < BootGrace for every tick
			Now:               func() time.Time { return now },
			OnSessionDetached: recordDetached,
			OnExpired:         recordExpired,
			OnGridPhaseOut:    recordGridPhaseOut,
		})

		reaperCtx, reaperCancel := context.WithTimeout(ctx, 3*time.Second)
		defer reaperCancel() // safety net; idempotent with the join-triggered cancel below
		reaperDone := make(chan struct{})
		go func() {
			defer close(reaperDone)
			reaper.Run(reaperCtx)
		}()

		// Phase 4 — assert boot-grace suppression.
		//
		// Consistently over ~600ms (12 ticks at 50ms) proves the reaper
		// fires multiple sweep attempts and all are no-ops under boot-grace.
		//
		// a) Session row is STILL active (not detached / expired).
		Consistently(func(g Gomega) {
			info, getErr := sessStore.Get(ctx, guest.SessionID)
			g.Expect(getErr).NotTo(HaveOccurred(),
				"session MUST still exist during boot-grace window (I-LIVE-4)")
			g.Expect(info.Status).To(Equal(session.StatusActive),
				"I-LIVE-4: session MUST remain active while inside the boot-grace window; "+
					"lease sweep MUST NOT reap a lapsed-looking connection during boot-grace (G2)")
		}, 600*time.Millisecond, 50*time.Millisecond).Should(Succeed())

		// b) Presence STILL contains the character (grid_present not cleared).
		Consistently(func(g Gomega) {
			resp, presErr := guest.ListFocusPresence(ctx)
			g.Expect(presErr).NotTo(HaveOccurred())
			g.Expect(entryNames(resp.GetEntries())).To(ContainElement(guest.CharacterName),
				"I-LIVE-4/I-PRES-1: guest MUST remain visible in presence throughout boot-grace window; "+
					"grid_present MUST NOT be cleared by a suppressed sweep")
		}, 600*time.Millisecond, 50*time.Millisecond).Should(Succeed())

		// c) No callbacks fired.
		callbackMu.Lock()
		nDetached := len(detached)
		nExpired := len(expired)
		nGridPhaseOut := len(gridPhaseOut)
		callbackMu.Unlock()

		Expect(nDetached).To(Equal(0),
			"I-LIVE-4: OnSessionDetached MUST NOT fire within the boot-grace window")
		Expect(nExpired).To(Equal(0),
			"I-LIVE-4: OnExpired MUST NOT fire within the boot-grace window")
		Expect(nGridPhaseOut).To(Equal(0),
			"I-LIVE-4: OnGridPhaseOut MUST NOT fire within the boot-grace window")

		// Shut the reaper down cleanly and wait for it to exit.
		reaperCancel()
		<-reaperDone
	})
})
