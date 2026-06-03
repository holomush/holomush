// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	crand "crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

// INV-SCENE-17 + INV-SCENE-24 (AutoFocusOnJoin filter + skip rules):
//
// INV-SCENE-17: AutoFocusOnJoin MUST only auto-focus terminal/telnet connections.
// comms_hub and any other non-terminal client types MUST be excluded from the
// FocusedConnectionIDs result. TotalConnectionCount still counts ALL connections
// regardless of type.
//
// INV-SCENE-24: AutoFocusOnJoin MUST skip connections that are already explicitly
// focused on a different target (D8 skip-rule). Such connections land in
// SkippedConnectionIDs, not FocusedConnectionIDs.
//
// These specs exercise AutoFocusOnJoin against a live Coordinator wired with a
// Postgres-backed session store (sessiontest.NewStore). No JetStream bus is
// required: the invariants live entirely in the session-store mutation path,
// not in the eventbus.
//
// Harness pattern follows focus_without_membership_blocked_test.go (T24).
//
// Spec: docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md §6.2, INV-SCENE-17, INV-SCENE-24.
// Bead: holomush-5rh.14.25.
var _ = Describe("INV-SCENE-17 + INV-SCENE-24: AutoFocusOnJoin terminal filter and skip rules", func() {
	// -----------------------------------------------------------------------
	// Shared harness builder — wires a Coordinator + Postgres-backed store with a
	// NullPolicy for FocusKindScene, then returns a helper to seed sessions.
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
	// Scenario (a) — INV-SCENE-17: comms_hub filtered, terminal focused.
	//
	//   Alice has 2 connections: 1 terminal + 1 comms_hub.
	//   After JoinFocus(alice, scene), AutoFocusOnJoin(alice, scene) MUST:
	//     - FocusedConnectionIDs = [terminalConnID] (only the terminal)
	//     - TotalConnectionCount = 2 (both connections counted)
	//     - comms_hub absent from FocusedConnectionIDs, SkippedConnectionIDs,
	//       and FailedConnectionIDs
	// -----------------------------------------------------------------------
	It("filters out comms_hub connections (INV-SCENE-17)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		h := newHarness()

		aliceCharID := newULID()
		sceneID := newULID()
		sessionID := "sess-alice-p5-4-" + newULID().String()
		terminalConnID := newULID()
		commsHubConnID := newULID()

		// Seed Alice's session with scene membership already present so
		// AutoFocusOnJoin can succeed for the terminal connection.
		Expect(h.store.Set(ctx, sessionID, &session.Info{
			ID:          sessionID,
			CharacterID: aliceCharID,
			LocationID:  newULID(),
			Status:      session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID},
			},
		})).To(Succeed())

		// Add terminal connection.
		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         terminalConnID,
			SessionID:  sessionID,
			ClientType: "terminal",
		})).To(Succeed())

		// Add comms_hub connection.
		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         commsHubConnID,
			SessionID:  sessionID,
			ClientType: "comms_hub",
		})).To(Succeed())

		resp, err := h.coord.AutoFocusOnJoin(ctx, aliceCharID, sceneID)
		Expect(err).NotTo(HaveOccurred(), "AutoFocusOnJoin must not return a store-level error")

		// INV-SCENE-17: only the terminal connection must be focused.
		// Use HaveLen + ContainElement to be order-agnostic (ordering is not guaranteed).
		Expect(resp.FocusedConnectionIDs).To(HaveLen(1),
			"INV-SCENE-17: FocusedConnectionIDs MUST have exactly one entry")
		Expect(resp.FocusedConnectionIDs).To(ContainElement(terminalConnID),
			"INV-SCENE-17: FocusedConnectionIDs MUST contain only the terminal connection")

		// TotalConnectionCount counts ALL connections regardless of client type.
		Expect(resp.TotalConnectionCount).To(BeEquivalentTo(2),
			"TotalConnectionCount MUST count all connections including comms_hub")

		// comms_hub must not appear anywhere in the result buckets.
		Expect(resp.SkippedConnectionIDs).NotTo(ContainElement(commsHubConnID),
			"comms_hub MUST NOT appear in SkippedConnectionIDs")
		for _, f := range resp.FailedConnectionIDs {
			Expect(f.ConnectionID).NotTo(Equal(commsHubConnID),
				"comms_hub MUST NOT appear in FailedConnectionIDs")
		}
	})

	// -----------------------------------------------------------------------
	// Scenario (b) — INV-SCENE-24: skip-already-focused.
	//
	//   Alice has 2 terminal connections:
	//     conn1: already explicitly focused on scene #99 (different scene)
	//     conn2: unfocused (FocusKey == nil)
	//   AutoFocusOnJoin(alice, scene #42) MUST:
	//     - FocusedConnectionIDs = [conn2]
	//     - SkippedConnectionIDs = [conn1]
	// -----------------------------------------------------------------------
	It("skips connections already focused elsewhere (INV-SCENE-24)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		h := newHarness()

		aliceCharID := newULID()
		targetSceneID := newULID() // scene #42 — the one we're joining
		otherSceneID := newULID()  // scene #99 — conn1 is already focused here
		sessionID := "sess-alice-p5-11-" + newULID().String()
		conn1ID := newULID() // already focused on otherScene
		conn2ID := newULID() // unfocused

		otherFocusKey := &session.FocusKey{Kind: session.FocusKindScene, TargetID: otherSceneID}

		// Seed Alice's session with membership for both scenes so the
		// pre-existing focus on otherScene is coherent and so targetScene
		// membership allows conn2 to be focused.
		Expect(h.store.Set(ctx, sessionID, &session.Info{
			ID:          sessionID,
			CharacterID: aliceCharID,
			LocationID:  newULID(),
			Status:      session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: targetSceneID},
				{Kind: session.FocusKindScene, TargetID: otherSceneID},
			},
		})).To(Succeed())

		// conn1: already explicitly focused on otherScene.
		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         conn1ID,
			SessionID:  sessionID,
			ClientType: "terminal",
			FocusKey:   otherFocusKey,
		})).To(Succeed())

		// conn2: unfocused.
		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         conn2ID,
			SessionID:  sessionID,
			ClientType: "terminal",
		})).To(Succeed())

		resp, err := h.coord.AutoFocusOnJoin(ctx, aliceCharID, targetSceneID)
		Expect(err).NotTo(HaveOccurred(), "AutoFocusOnJoin must not return a store-level error")

		// INV-SCENE-24: conn2 (unfocused) gets focused; conn1 (already elsewhere) skipped.
		// Use HaveLen + ContainElement to be order-agnostic (ordering is not guaranteed).
		Expect(resp.FocusedConnectionIDs).To(HaveLen(1),
			"INV-SCENE-24: FocusedConnectionIDs MUST have exactly one entry")
		Expect(resp.FocusedConnectionIDs).To(ContainElement(conn2ID),
			"INV-SCENE-24: FocusedConnectionIDs MUST contain only unfocused conn2")
		Expect(resp.SkippedConnectionIDs).To(HaveLen(1),
			"INV-SCENE-24: SkippedConnectionIDs MUST have exactly one entry")
		Expect(resp.SkippedConnectionIDs).To(ContainElement(conn1ID),
			"INV-SCENE-24: SkippedConnectionIDs MUST contain conn1 (focused elsewhere)")
		Expect(resp.FailedConnectionIDs).To(BeEmpty(),
			"INV-SCENE-24: no connection should fail — only focused or skipped")
	})

	// -----------------------------------------------------------------------
	// Scenario (c) — FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT:
	// AutoFocus before JoinFocus → all terminal conns in FailedConnectionIDs.
	//
	//   Alice has 1 terminal connection and NO scene membership.
	//   AutoFocusOnJoin(alice, scene) MUST:
	//     - FailedConnectionIDs = [{terminalConnID, "membership_absent"}]
	//     - FocusedConnectionIDs empty
	//     - SkippedConnectionIDs empty
	// -----------------------------------------------------------------------
	It("fails connections without scene membership (FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		h := newHarness()

		aliceCharID := newULID()
		sceneID := newULID()
		sessionID := "sess-alice-no-membership-" + newULID().String()
		terminalConnID := newULID()

		// Seed Alice's session with NO scene membership.
		Expect(h.store.Set(ctx, sessionID, &session.Info{
			ID:               sessionID,
			CharacterID:      aliceCharID,
			LocationID:       newULID(),
			Status:           session.StatusActive,
			FocusMemberships: nil, // no memberships — membership gate must fire
		})).To(Succeed())

		Expect(h.store.AddConnection(ctx, &session.Connection{
			ID:         terminalConnID,
			SessionID:  sessionID,
			ClientType: "terminal",
		})).To(Succeed())

		resp, err := h.coord.AutoFocusOnJoin(ctx, aliceCharID, sceneID)
		Expect(err).NotTo(HaveOccurred(),
			"AutoFocusOnJoin MUST NOT return a top-level error for per-connection membership failures; failures are carried in FailedConnectionIDs")

		// The terminal connection must appear in FailedConnectionIDs with
		// reason "membership_absent" (proto: FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT).
		Expect(resp.FailedConnectionIDs).To(HaveLen(1),
			"MEMBERSHIP_ABSENT: FailedConnectionIDs must contain the terminal connection")
		Expect(resp.FailedConnectionIDs[0].ConnectionID).To(Equal(terminalConnID),
			"MEMBERSHIP_ABSENT: FailedConnectionID.ConnectionID must match the terminal conn")
		Expect(resp.FailedConnectionIDs[0].Reason).To(Equal("membership_absent"),
			"MEMBERSHIP_ABSENT: FailedConnectionID.Reason must be \"membership_absent\"")

		Expect(resp.FocusedConnectionIDs).To(BeEmpty(),
			"MEMBERSHIP_ABSENT: FocusedConnectionIDs must be empty when membership absent")
		Expect(resp.SkippedConnectionIDs).To(BeEmpty(),
			"MEMBERSHIP_ABSENT: SkippedConnectionIDs must be empty when membership absent")
	})
})
