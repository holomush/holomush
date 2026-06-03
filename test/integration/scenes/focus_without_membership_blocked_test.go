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
	"github.com/holomush/holomush/pkg/errutil"
)

// INV-SCENE-14 (FocusMemberships gate): SetConnectionFocus on a scene-kind
// focusKey MUST be rejected with FOCUS_WITHOUT_MEMBERSHIP when the session's
// FocusMemberships does not contain the target scene. After JoinFocus adds the
// membership, the same SetConnectionFocus call MUST succeed.
//
// End-to-end shape this spec pins:
//
// SetConnectionFocus (T14 Coordinator) validates the session's
// FocusMemberships inside the same store-locked mutator it commits the write
// in — no TOCTOU window. The check is:
//
//	if focusKey != nil && focusKey.Kind == FocusKindScene {
//	    if !hasMembership(si.FocusMemberships, focusKey.Kind, focusKey.TargetID) {
//	        return ..., oops.Code("FOCUS_WITHOUT_MEMBERSHIP")...
//	    }
//	}
//
// JoinFocus (T15 Coordinator, join.go) appends the new FocusMembership
// atomically via UpdateFocusMemberships. Once it commits, the guard above
// passes on the retry.
//
// This spec exercises both halves against a live Coordinator wired with a
// Postgres-backed session store (sessiontest.NewStore). No JetStream bus is
// required: the invariant lives entirely in the session-store mutation path,
// not in the eventbus.
//
// Spec: docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md §10, INV-SCENE-14.
// Bead: holomush-5rh.14.24.
var _ = Describe("INV-SCENE-14: focus without membership blocked", func() {
	It("SetConnectionFocus fails without membership, succeeds after JoinFocus", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		// ----------------------------------------------------------------
		// Step 1: wire a Coordinator with a Postgres-backed store.
		//
		// NullPolicy satisfies the KindPolicy interface for FocusKindScene
		// without emitting streams — sufficient for the JoinFocus call to
		// add the membership record.  No StreamSender is needed: we assert
		// the membership-gate error, not stream delivery.
		// ----------------------------------------------------------------
		store := sessiontest.NewStore(suiteT)
		coord, err := focus.NewCoordinator(
			focus.WithSessionStore(store),
			focus.WithKindPolicy(focus.NewNullPolicy(session.FocusKindScene)),
		)
		Expect(err).NotTo(HaveOccurred(), "Coordinator construction must succeed")

		// ----------------------------------------------------------------
		// Step 2: seed Alice's session (no FocusMemberships) and one
		// terminal Connection.  Both IDs are fresh-entropy ULIDs so
		// parallel specs cannot collide.
		// ----------------------------------------------------------------
		aliceCharID := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)
		sceneID := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)
		sessionID := "sess-alice-" + ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader).String()
		connID := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader)

		Expect(store.Set(ctx, sessionID, &session.Info{
			ID:               sessionID,
			CharacterID:      aliceCharID,
			LocationID:       ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader),
			Status:           session.StatusActive,
			FocusMemberships: nil, // no memberships — gate MUST fire
		})).To(Succeed())

		Expect(store.AddConnection(ctx, &session.Connection{
			ID:         connID,
			SessionID:  sessionID,
			ClientType: "terminal",
		})).To(Succeed())

		// ----------------------------------------------------------------
		// Step 3 — INV-SCENE-14 FAIL assertion.
		//
		// SetConnectionFocus with a scene focusKey while FocusMemberships
		// is empty MUST return FOCUS_WITHOUT_MEMBERSHIP.  No state must
		// be written: FocusKey remains nil and PresentingFocus stays nil.
		// ----------------------------------------------------------------
		focusKey := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
		_, setErr := coord.SetConnectionFocus(ctx, connID, focusKey, false)
		Expect(setErr).To(HaveOccurred(),
			"INV-SCENE-14: SetConnectionFocus MUST fail when FocusMemberships is empty")
		errutil.AssertErrorCode(suiteT, setErr, "FOCUS_WITHOUT_MEMBERSHIP")

		// Confirm no state was written (membership-gate runs before any commit).
		conn, getErr := store.GetConnection(ctx, connID)
		Expect(getErr).NotTo(HaveOccurred())
		Expect(conn.FocusKey).To(BeNil(),
			"INV-SCENE-14: FocusKey MUST remain nil after membership-gate rejection")

		info, getErr := store.Get(ctx, sessionID)
		Expect(getErr).NotTo(HaveOccurred())
		Expect(info.PresentingFocus).To(BeNil(),
			"INV-SCENE-14: PresentingFocus MUST remain nil after membership-gate rejection")

		// ----------------------------------------------------------------
		// Step 4 — JoinFocus adds the scene membership.
		//
		// JoinFocus atomically appends a FocusMembership for the scene to
		// Alice's session. After this succeeds, the SetConnectionFocus
		// guard will find the matching entry and allow the write.
		// ----------------------------------------------------------------
		Expect(coord.JoinFocus(ctx, sessionID, session.FocusKey{
			Kind:     session.FocusKindScene,
			TargetID: sceneID,
		})).To(Succeed(), "JoinFocus MUST succeed for an active session with a registered policy")

		// Verify membership landed.
		info, getErr = store.Get(ctx, sessionID)
		Expect(getErr).NotTo(HaveOccurred())
		Expect(info.FocusMemberships).To(HaveLen(1),
			"JoinFocus must have appended exactly one FocusMembership")
		Expect(info.FocusMemberships[0].Kind).To(Equal(session.FocusKindScene))
		Expect(info.FocusMemberships[0].TargetID).To(Equal(sceneID))

		// ----------------------------------------------------------------
		// Step 5 — INV-SCENE-14 PASS assertion.
		//
		// Retry SetConnectionFocus — the membership now exists.  The call
		// MUST succeed and commit Connection.FocusKey and PresentingFocus
		// (D9: terminal + non-grid explicit focus).
		// ----------------------------------------------------------------
		res, setErr := coord.SetConnectionFocus(ctx, connID, focusKey, false)
		Expect(setErr).NotTo(HaveOccurred(),
			"INV-SCENE-14: SetConnectionFocus MUST succeed after JoinFocus adds membership")
		Expect(res.SessionID).To(Equal(sessionID),
			"result SessionID must match the session under test")

		// D7: Connection.FocusKey written.
		conn, getErr = store.GetConnection(ctx, connID)
		Expect(getErr).NotTo(HaveOccurred())
		Expect(conn.FocusKey).NotTo(BeNil(),
			"INV-SCENE-14 post-membership: Connection.FocusKey MUST be set after successful SetConnectionFocus")
		Expect(conn.FocusKey.Kind).To(Equal(session.FocusKindScene))
		Expect(conn.FocusKey.TargetID).To(Equal(sceneID))

		// D9: terminal + non-grid → PresentingFocus updated.
		info, getErr = store.Get(ctx, sessionID)
		Expect(getErr).NotTo(HaveOccurred())
		Expect(info.PresentingFocus).NotTo(BeNil(),
			"D9: terminal explicit focus MUST write PresentingFocus")
		Expect(info.PresentingFocus.Kind).To(Equal(session.FocusKindScene))
		Expect(info.PresentingFocus.TargetID).To(Equal(sceneID))
	})
})
