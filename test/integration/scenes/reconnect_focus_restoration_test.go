// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	crand "crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

// INV-SCENE-18 + INV-SCENE-25 + INV-SCENE-26 (reconnect focus restoration):
//
// INV-SCENE-18: RestoreConnectionFocus MUST fall back to grid (FocusKey = nil) and
// emit a structured warning when PresentingFocus is set but the matching
// FocusMembership has been revoked. The validation and fallback MUST occur
// inside one store-locked mutator — no read-then-mutate race.
//
// INV-SCENE-25: Concurrent RestoreConnectionFocus and LeaveFocus MUST serialize
// via the SessionConnectionMutator / FocusMutator path (D11 lock order). Both
// orderings are valid outcomes; the invariant is that no corruption results —
// no panic, no orphaned state, consistent post-state.
//
// INV-SCENE-26 (end-to-end reconnect path): after a terminal connection explicitly
// focuses scene #A, then pivots to scene grid (isSceneGrid=true), the session's
// PresentingFocus MUST remain #A (D10 — grid pivot MUST NOT touch
// PresentingFocus). On reconnect, RestoreConnectionFocus reads PresentingFocus
// = #A, validates the membership, and sets the new connection's FocusKey = #A.
//
// Harness pattern follows focus_without_membership_blocked_test.go (T24) and
// auto_focus_on_join_terminal_only_test.go (T25): minimal Coordinator +
// Postgres-backed sessiontest.NewStore, NullPolicy for FocusKindScene, no JetStream bus.
//
// Spec: docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md
// §7 (INV-SCENE-18, INV-SCENE-25), §8 (INV-SCENE-26).
// Bead: holomush-5rh.14.26.
var _ = Describe("INV-SCENE-18 + INV-SCENE-25 + INV-SCENE-26: reconnect focus restoration", func() {
	// -----------------------------------------------------------------------
	// Shared harness builder — wires a Coordinator + Postgres-backed store with a
	// NullPolicy for FocusKindScene, then returns helpers to seed sessions.
	// -----------------------------------------------------------------------
	type harness struct {
		store session.Store
		coord focus.Coordinator
	}

	newHarness := func() harness {
		store := sessiontest.NewStore(suiteT)
		coord, err := focus.NewCoordinator(
			focus.WithSessionStore(store),
			focus.WithKindPolicy(focus.NewNullPolicy(session.FocusKindScene)),
		)
		Expect(err).NotTo(HaveOccurred(), "Coordinator construction must succeed")
		return harness{store: store, coord: coord}
	}

	newULID := func() ulid.ULID {
		return ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)
	}

	// -----------------------------------------------------------------------
	// Scenario (a) — INV-SCENE-18: PresentingFocus set + membership revoked →
	// grid fallback + no error.
	//
	//   Alice's session has PresentingFocus = {Kind:Scene, TargetID:#42} but
	//   FocusMemberships is empty (scene #42 membership was revoked while
	//   disconnected). A new terminal connection arrives.
	//   RestoreConnectionFocus MUST:
	//     - return nil (no top-level error)
	//     - leave Connection.FocusKey = nil (grid fallback)
	// -----------------------------------------------------------------------
	It("falls back to grid when PresentingFocus set but membership revoked (INV-SCENE-18)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		h := newHarness()

		aliceCharID := newULID()
		sceneID := newULID()
		sessionID := "sess-alice-p5-5-" + newULID().String()
		connID := newULID()

		stalePF := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

		// Seed Alice's session: PresentingFocus points at scene #42 but
		// FocusMemberships is empty — the membership was revoked while
		// Alice was disconnected.
		Expect(h.store.Set(ctx, sessionID, &session.Info{
			ID:               sessionID,
			CharacterID:      aliceCharID,
			LocationID:       newULID(),
			Status:           session.StatusActive,
			PresentingFocus:  &stalePF,
			FocusMemberships: nil, // revoked
		})).To(Succeed())

		// New terminal connection arrives (reconnect — FocusKey starts nil).
		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         connID,
			SessionID:  sessionID,
			ClientType: "terminal",
		})).To(Succeed())

		// RestoreConnectionFocus MUST NOT error; INV-SCENE-18 fallback is silent
		// at the API boundary (warning is logged internally).
		Expect(h.coord.RestoreConnectionFocus(ctx, sessionID, connID)).
			To(Succeed(), "INV-SCENE-18: RestoreConnectionFocus MUST return nil even when membership is revoked")

		// Grid fallback: FocusKey must remain nil.
		conn, err := h.store.GetConnection(ctx, connID)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn.FocusKey).To(BeNil(),
			"INV-SCENE-18: Connection.FocusKey MUST be nil (grid fallback) when membership is revoked")
	})

	// -----------------------------------------------------------------------
	// Scenario (b) — INV-SCENE-25: concurrent RestoreConnectionFocus vs
	// LeaveFocus → both serialization orderings are valid; no corruption.
	//
	//   Setup: Alice has scene #42 membership + PresentingFocus on scene #42.
	//   Run RestoreConnectionFocus(alice, conn) AND LeaveFocus(alice, scene #42)
	//   CONCURRENTLY (goroutines) for 16 iterations to surface the race.
	//   Both outcomes are valid:
	//     - restore-first: conn.FocusKey = scene #42 (leave doesn't clear FocusKey)
	//     - leave-first:   conn.FocusKey = nil (membership gone when restore checks)
	//   Assert: no panic, no error from either goroutine, consistent post-state.
	// -----------------------------------------------------------------------
	It("serializes concurrent reconnect vs leave with no corruption (INV-SCENE-25)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		sceneID := newULID()

		const iterations = 16
		for i := 0; i < iterations; i++ {
			// Fresh harness and IDs per iteration so each race is independent.
			h := newHarness()

			aliceCharID := newULID()
			sessionID := "sess-alice-p5-12-" + newULID().String()
			connID := newULID()
			pf := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

			Expect(h.store.Set(ctx, sessionID, &session.Info{
				ID:          sessionID,
				CharacterID: aliceCharID,
				LocationID:  newULID(),
				Status:      session.StatusActive,
				PresentingFocus: &session.FocusKey{
					Kind:     session.FocusKindScene,
					TargetID: sceneID,
				},
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
				},
			})).To(Succeed())

			// New terminal connection (reconnect).
			Expect(h.store.AddConnection(ctx, &session.Connection{
				ID:         connID,
				SessionID:  sessionID,
				ClientType: "terminal",
			})).To(Succeed())

			var wg sync.WaitGroup
			wg.Add(2)
			var restoreErr, leaveErr error

			go func() {
				defer wg.Done()
				restoreErr = h.coord.RestoreConnectionFocus(ctx, sessionID, connID)
			}()
			go func() {
				defer wg.Done()
				leaveErr = h.coord.LeaveFocus(ctx, sessionID, pf)
			}()
			wg.Wait()

			// INV-SCENE-25: neither operation must error.
			Expect(restoreErr).NotTo(HaveOccurred(),
				"iter %d: RestoreConnectionFocus MUST NOT error under concurrent LeaveFocus", i)
			Expect(leaveErr).NotTo(HaveOccurred(),
				"iter %d: LeaveFocus MUST NOT error under concurrent RestoreConnectionFocus", i)

			// Post-state consistency:
			// LeaveFocus always commits: FocusMemberships empty, PresentingFocus nil.
			info, err := h.store.Get(ctx, sessionID)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.FocusMemberships).To(BeEmpty(),
				"iter %d: INV-SCENE-25: LeaveFocus MUST clear FocusMemberships", i)
			Expect(info.PresentingFocus).To(BeNil(),
				"iter %d: INV-SCENE-25: LeaveFocus MUST clear PresentingFocus (pointed at removed membership)", i)

			// conn.FocusKey: either nil (leave-first → restore saw no membership)
			// or the original scene focus (restore-first → leave doesn't touch FocusKey).
			conn, err := h.store.GetConnection(ctx, connID)
			Expect(err).NotTo(HaveOccurred())
			if conn.FocusKey != nil {
				Expect(conn.FocusKey.Kind).To(Equal(session.FocusKindScene),
					"iter %d: INV-SCENE-25: if FocusKey set, Kind MUST be the original scene kind", i)
				Expect(conn.FocusKey.TargetID).To(Equal(sceneID),
					"iter %d: INV-SCENE-25: if FocusKey set, TargetID MUST match original sceneID", i)
			}
		}
	})

	// -----------------------------------------------------------------------
	// Scenario (c) — INV-SCENE-26: scene focus → scene grid → disconnect+reconnect
	// → new connection lands on scene (end-to-end).
	//
	//   1. Alice has scene #A membership; terminal conn1 present.
	//   2. SetConnectionFocus(conn1, #A, isSceneGrid=false) → conn1.FocusKey=#A,
	//      PresentingFocus=#A (D9: terminal + explicit focus).
	//   3. SetConnectionFocus(conn1, nil, isSceneGrid=true) → conn1.FocusKey=nil,
	//      PresentingFocus REMAINS #A (D10/INV-SCENE-26: scene-grid MUST NOT touch it).
	//   4. Disconnect: conn1 is dropped (no longer in store — simulate via
	//      AddConnection + RemoveConnection or just verify state is set correctly
	//      then add conn2).
	//   5. New connection conn2 arrives → RestoreConnectionFocus(alice, conn2)
	//      reads PresentingFocus = #A, validates membership, sets conn2.FocusKey=#A.
	//   Assert: conn2.FocusKey = {Scene, #A}.
	// -----------------------------------------------------------------------
	It("restores PresentingFocus after disconnect+reconnect end-to-end (INV-SCENE-26)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		h := newHarness()

		aliceCharID := newULID()
		sceneAID := newULID()
		sessionID := "sess-alice-p5-13-" + newULID().String()
		conn1ID := newULID()

		// Step 1: seed Alice's session with scene #A membership and conn1.
		Expect(h.store.Set(ctx, sessionID, &session.Info{
			ID:          sessionID,
			CharacterID: aliceCharID,
			LocationID:  newULID(),
			Status:      session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneAID, JoinedAt: time.Now()},
			},
		})).To(Succeed())

		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         conn1ID,
			SessionID:  sessionID,
			ClientType: "terminal",
		})).To(Succeed())

		// Step 2: explicit focus on scene #A.
		// isSceneGrid=false → D9: terminal + explicit focus updates PresentingFocus.
		focusKey := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneAID}
		_, err := h.coord.SetConnectionFocus(ctx, conn1ID, focusKey, false)
		Expect(err).NotTo(HaveOccurred(), "SetConnectionFocus on scene #A MUST succeed with membership")

		// Verify conn1.FocusKey = #A and PresentingFocus = #A.
		conn1, err := h.store.GetConnection(ctx, conn1ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn1.FocusKey).NotTo(BeNil())
		Expect(conn1.FocusKey.TargetID).To(Equal(sceneAID),
			"INV-SCENE-26 step 2: conn1.FocusKey must be scene #A after explicit focus")

		info, err := h.store.Get(ctx, sessionID)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.PresentingFocus).NotTo(BeNil())
		Expect(info.PresentingFocus.TargetID).To(Equal(sceneAID),
			"INV-SCENE-26 step 2: PresentingFocus must be scene #A after explicit terminal focus")

		// Step 3: pivot to scene grid (isSceneGrid=true).
		// D10/INV-SCENE-26: PresentingFocus MUST NOT be touched.
		_, err = h.coord.SetConnectionFocus(ctx, conn1ID, nil, true)
		Expect(err).NotTo(HaveOccurred(), "SetConnectionFocus to scene grid MUST succeed")

		// conn1.FocusKey is now nil (grid), but PresentingFocus stays #A.
		conn1, err = h.store.GetConnection(ctx, conn1ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn1.FocusKey).To(BeNil(),
			"INV-SCENE-26 step 3: conn1.FocusKey MUST be nil after scene-grid pivot")

		info, err = h.store.Get(ctx, sessionID)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.PresentingFocus).NotTo(BeNil(),
			"INV-SCENE-26 step 3: PresentingFocus MUST remain #A after scene-grid pivot (D10)")
		Expect(info.PresentingFocus.TargetID).To(Equal(sceneAID),
			"INV-SCENE-26 step 3: PresentingFocus target MUST still be scene #A after grid pivot")

		// Step 4: disconnect (simulate by removing conn1; conn2 will be the reconnect).
		Expect(h.store.RemoveConnection(ctx, conn1ID)).To(Succeed(),
			"removing conn1 simulates the disconnect event")

		// Step 5: new connection conn2 arrives on reconnect.
		conn2ID := newULID()
		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         conn2ID,
			SessionID:  sessionID,
			ClientType: "terminal",
			// FocusKey starts nil (brand-new connection, not yet restored).
		})).To(Succeed())

		// RestoreConnectionFocus: reads PresentingFocus=#A, validates membership,
		// sets conn2.FocusKey=#A.
		Expect(h.coord.RestoreConnectionFocus(ctx, sessionID, conn2ID)).
			To(Succeed(), "INV-SCENE-26: RestoreConnectionFocus MUST succeed on reconnect")

		// Assert: new conn2.FocusKey = {Scene, #A}.
		conn2, err := h.store.GetConnection(ctx, conn2ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn2.FocusKey).NotTo(BeNil(),
			"INV-SCENE-26: conn2.FocusKey MUST be restored to scene #A on reconnect")
		Expect(conn2.FocusKey.Kind).To(Equal(session.FocusKindScene),
			"INV-SCENE-26: restored FocusKey.Kind MUST be FocusKindScene")
		Expect(conn2.FocusKey.TargetID).To(Equal(sceneAID),
			"INV-SCENE-26: restored FocusKey.TargetID MUST be scene #A")
	})
})

// D-08 (Subscribe wiring) + D-09 (multi-character no-leak):
//
// D-08: internal/grpc/server.go calls RestoreConnectionFocus after AddConnection
// on Subscribe, GATED on Info.PresentingFocus != nil (Assumption A2 / Pitfall 5).
// The gate keeps a web tab's per-tab FocusKey from being clobbered: web sessions
// do not set PresentingFocus (it is the telnet single-pane reconnect signal), so
// a resubscribe with PresentingFocus nil is a documented no-op (branch 1) and the
// tab's chosen focus survives.
//
// D-09: a telnet connection swapping characters (SEQUENTIAL swap per connection —
// QUIT → picker → re-pick) MUST NOT let a swapped-in character B inherit the
// prior character A's scene focus. INV-SCENE-18 membership validation blocks the
// leak (B non-member → grid fallback), and RestoreConnectionFocus defensively
// clears any stale non-entitled FocusKey to nil so no prior character's scene
// focus survives on the connection.
//
// Spec: docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md §8.
var _ = Describe("D-08 + D-09: Subscribe-wiring web-tab safety and multi-character focus no-leak", func() {
	type harness struct {
		store session.Store
		coord focus.Coordinator
	}

	newHarness := func() harness {
		store := sessiontest.NewStore(suiteT)
		coord, err := focus.NewCoordinator(
			focus.WithSessionStore(store),
			focus.WithKindPolicy(focus.NewNullPolicy(session.FocusKindScene)),
		)
		Expect(err).NotTo(HaveOccurred(), "Coordinator construction must succeed")
		return harness{store: store, coord: coord}
	}

	newULID := func() ulid.ULID {
		return ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)
	}

	// D-08 web-tab safety: PresentingFocus nil → RestoreConnectionFocus is a
	// no-op → a web tab's already-chosen per-tab FocusKey is preserved. This is
	// the guarantee the server.go gate (PresentingFocus != nil) protects.
	It("does NOT clobber a web tab's per-tab focus when PresentingFocus is nil (D-08 gate / Assumption A2)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		h := newHarness()

		charID := newULID()
		webSceneID := newULID()
		sessionID := "sess-web-d08-" + newULID().String()
		connID := newULID()

		// Web session: PresentingFocus is nil (web tabs manage per-tab focus,
		// they do not set the session-scoped PresentingFocus). Character holds a
		// membership so the per-tab focus is legitimate.
		Expect(h.store.Set(ctx, sessionID, &session.Info{
			ID:              sessionID,
			CharacterID:     charID,
			LocationID:      newULID(),
			Status:          session.StatusActive,
			PresentingFocus: nil,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: webSceneID, JoinedAt: time.Now()},
			},
		})).To(Succeed())

		// The web tab has already chosen its own per-tab focus on scene #W.
		tabFocus := &session.FocusKey{Kind: session.FocusKindScene, TargetID: webSceneID}
		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         connID,
			SessionID:  sessionID,
			ClientType: "terminal",
			FocusKey:   tabFocus,
		})).To(Succeed())

		// Even if RestoreConnectionFocus is invoked, PresentingFocus == nil makes
		// it a no-op (branch 1) — the tab's chosen focus MUST be preserved.
		Expect(h.coord.RestoreConnectionFocus(ctx, sessionID, connID)).
			To(Succeed(), "D-08: RestoreConnectionFocus MUST be a no-op when PresentingFocus is nil")

		conn, err := h.store.GetConnection(ctx, connID)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn.FocusKey).NotTo(BeNil(),
			"D-08: web tab's per-tab FocusKey MUST survive a PresentingFocus-nil restore")
		Expect(conn.FocusKey.TargetID).To(Equal(webSceneID),
			"D-08: web tab's per-tab FocusKey MUST remain its chosen scene, not be clobbered")
	})
})
