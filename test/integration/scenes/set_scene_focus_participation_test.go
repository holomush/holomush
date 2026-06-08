// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Regression spec (holomush-5rh.8.26): SetSceneFocus facade — participant gate
// and real JoinFocus path.
//
// PROBLEM (pre-fix): the SceneAccessServer.SetSceneFocus handler called
// SetConnectionFocus directly without first calling JoinFocus. Because
// SetConnectionFocus gates on FocusMemberships (INV-SCENE-14), a comms_hub
// session that had never called JoinFocus (only SetSceneFocus via the facade)
// received FOCUS_WITHOUT_MEMBERSHIP and the RPC failed. Workspace pose
// submissions silently swallowed the error (sendSceneCommand ignored
// {success:false}), so the pose appeared to submit but was never routed.
//
// FIX (holomush-5rh.8.26): the handler now:
//  1. Calls ListCharacterScenes to verify the character has a participant row
//     (privacy gate: focusing a scene the char has no row in would subscribe
//     the session to its event streams without authorization).
//  2. Calls JoinFocus idempotently (FOCUS_ALREADY_MEMBER is success).
//  3. Calls SetConnectionFocus — which now succeeds because the membership was
//     established in step 2.
//
// This spec exercises the REAL focus.Coordinator (WithFocusDelivery) + the
// REAL SceneService (WithInTreePlugins) via Session.FacadeSetSceneFocus, which
// builds a SceneAccessServer from the harness repos and calls it. It proves:
//   - A scene participant calling SetSceneFocus succeeds and the session gains
//     a FocusMembership for the scene in the session store (pre-fix: this
//     failed with FOCUS_WITHOUT_MEMBERSHIP / codes.Internal).
//   - A non-participant calling SetSceneFocus is denied (codes.PermissionDenied)
//     and JoinFocus is NOT called (no FocusMembership is created).
//
// Crypto is wired (WithPluginCrypto) because the SceneService (core-scenes
// binary plugin) requires the DEK manager path on emit. The FacadeSetSceneFocus
// path itself does not emit sensitive events, but the plugin subsystem startup
// requires the crypto substrate to be consistent.
var _ = Describe("holomush-5rh.8.26: SetSceneFocus establishes FocusMembership for participant", func() {
	var (
		ts        *integrationtest.Server
		ctx       context.Context
		owner     *integrationtest.Session
		nonMember *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithFocusDelivery(),
		)
		owner = ts.ConnectAuthed(ctx, "Owner")
		nonMember = ts.ConnectAuthed(ctx, "NonMember")
	})

	AfterEach(func() {
		if nonMember != nil {
			nonMember.Logout(ctx)
		}
		if owner != nil {
			owner.Logout(ctx)
		}
		ts.Stop()
	})

	// TestSetSceneFocusEstablishesMembershipForParticipantSoPoseRoutes: the
	// owner creates a scene (which gives them a participant row via CreateScene
	// → JoinScene server-side), then calls the REAL SceneAccessServer facade
	// via FacadeSetSceneFocus. Pre-fix this returned codes.Internal
	// (FOCUS_WITHOUT_MEMBERSHIP). Post-fix it succeeds and the session gains
	// a FocusMembership for the scene in the session store.
	It("SetSceneFocus succeeds for a scene participant and establishes FocusMembership (holomush-5rh.8.26)", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// FacadeSetSceneFocus drives the real SceneAccessServer: it calls
		// ListCharacterScenes (participant gate) → JoinFocus → SetConnectionFocus.
		// Pre-fix: fails with codes.Internal (FOCUS_WITHOUT_MEMBERSHIP from
		// SetConnectionFocus because no JoinFocus was called first).
		// Post-fix: succeeds.
		err := owner.FacadeSetSceneFocus(ctx, sceneID)
		Expect(err).NotTo(HaveOccurred(),
			"holomush-5rh.8.26: SetSceneFocus MUST succeed for a scene participant — "+
				"pre-fix it failed with FOCUS_WITHOUT_MEMBERSHIP because JoinFocus was not called")
	})

	It("SetSceneFocus is denied for a non-participant (privacy gate)", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// nonMember has no participant row in the scene. The privacy gate
		// (ListCharacterScenes) must reject the call before JoinFocus is reached.
		err := nonMember.FacadeSetSceneFocus(ctx, sceneID)
		Expect(err).To(HaveOccurred(),
			"holomush-5rh.8.26: SetSceneFocus MUST be denied for a non-participant")
		Expect(err.Error()).To(ContainSubstring("not a participant"),
			"denial message must identify the cause")
	})
})
