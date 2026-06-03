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

// INV-SCENE-49 / E9 (holomush-5rh.20.42): the history-scope temporal floor
// excludes a publish event emitted BEFORE a late participant joined a REAL
// scene. The early participant (joined first) sees it; the late participant
// does not. Floors at FocusMembership.JoinedAt via streamScopeFloor.
//
// Two orthogonal mechanisms are exercised simultaneously:
//
//  1. FLOOR — keyed on FocusMembership.JoinedAt, set by the JoinScene
//     store-shortcut helper. The JoinScene path is correct here: this test
//     exercises the floor, not the subscription wiring.
//
//  2. DECRYPTION — keyed on DEK participation, set by SeedSceneDEKParticipants.
//     Both early and late are seeded together in one GetOrCreate call BEFORE any
//     event is emitted, so both receive a decrypted (non-metadata-only) frame.
//     GetOrCreate only applies initial participants on first mint; seeding with
//     both sessions up front is the correct way to grant both decrypt access
//     while keeping the floor (JoinedAt) as the only differentiator.
//
// Spec: docs/superpowers/plans/2026-05-28-scene-bare-ulid-identity.md §Task 6.
// Bead: holomush-y5inx.6, unblocks holomush-5rh.20.42.
var _ = Describe("INV-SCENE-49 / E9: publish-event history-scope floor", func() {
	It("hides a pre-join scene_publish_started from a late joiner of a real scene", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)

		ts := integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
		)
		DeferCleanup(ts.Stop)

		// Create owner + scene.
		owner := ts.ConnectAuthed(ctx, "Owner")
		DeferCleanup(func() { owner.Logout(ctx) })
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		sceneStream := "events." + ts.GameID() + ".scene." + sceneID.String() + ".ic"

		// Connect early and late joiners.
		early := ts.ConnectAuthed(ctx, "Early")
		DeferCleanup(func() { early.Logout(ctx) })
		late := ts.ConnectAuthed(ctx, "Late")
		DeferCleanup(func() { late.Logout(ctx) })

		// Seed DEK with BOTH participants in a single GetOrCreate call BEFORE
		// any emit. GetOrCreate only applies initial participants on first mint,
		// so both early and late must be present now to decrypt later.
		// DEK participation is orthogonal to the floor: seeding up front is
		// correct; only JoinedAt controls which events each session sees.
		ts.SeedSceneDEKParticipants(ctx, sceneID, early, late)

		// Early joins BEFORE the publish-started event; her JoinedAt predates it.
		early.JoinScene(ctx, sceneID)

		// Emit the publish-started event BEFORE the late joiner joins.
		// scene_publish_started has sensitivity: never in the manifest, so it
		// must be emitted as Sensitive=false (EmitScenePlaintextContent).
		// EmitSceneICContent would be rejected by the INV-SCENE-58 fence for this type.
		ts.EmitScenePlaintextContent(ctx, "core-scenes", sceneID,
			owner.CharacterID, "scene_publish_started", `{"attempt":"first"}`)

		// Late joins AFTER the publish-started event; her JoinedAt becomes the floor.
		lateJoinedAt := late.JoinScene(ctx, sceneID)

		// Emit a second event AFTER the late join so the late joiner has at
		// least one visible event — proves the floor is a floor, not a blanket deny.
		ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			owner.CharacterID, "scene_pose", `{"text":"after late joined"}`)

		// ASSERTION 1 — early joiner sees BOTH events (joined before either).
		// The early participant sees the pre-join publish event AND the later pose,
		// and both are DECRYPTED (MetadataOnly == false) because early is a DEK
		// participant.
		Eventually(func(g Gomega) {
			evs, err := early.QueryStreamHistory(ctx, sceneStream)
			g.Expect(err).NotTo(HaveOccurred(),
				"early joiner QueryStreamHistory must not error")
			g.Expect(len(evs)).To(BeNumerically(">=", 2),
				"early joiner must see the pre-join publish event and the later pose")
			for _, e := range evs {
				g.Expect(e.GetMetadataOnly()).To(BeFalse(),
					"INV-SCENE-49: early participant (DEK member) must receive DECRYPTED frames")
			}
		}).WithTimeout(20 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

		// ASSERTION 2 — late joiner sees ONLY post-join events; every returned
		// event's timestamp is >= her JoinedAt. The pre-join scene_publish_started
		// is absent. Visible events are DECRYPTED (MetadataOnly == false) because
		// late is also a DEK participant.
		Eventually(func(g Gomega) {
			evs, err := late.QueryStreamHistory(ctx, sceneStream)
			g.Expect(err).NotTo(HaveOccurred(),
				"late joiner QueryStreamHistory must not error")
			g.Expect(evs).NotTo(BeEmpty(),
				"late joiner must see the post-join pose (vacuous-pass guard)")
			for _, e := range evs {
				g.Expect(e.GetTimestamp().AsTime()).To(BeTemporally(">=", lateJoinedAt),
					"INV-SCENE-49: event %q at %s leaked before late joiner's JoinedAt %s",
					e.GetType(), e.GetTimestamp().AsTime(), lateJoinedAt)
				g.Expect(e.GetMetadataOnly()).To(BeFalse(),
					"INV-SCENE-49: late participant (DEK member) must receive DECRYPTED frames")
			}
		}).WithTimeout(20 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
	})
})
