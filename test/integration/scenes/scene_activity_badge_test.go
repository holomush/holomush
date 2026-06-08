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
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Verifies: INV-SCENE-62
// integration (WithInTreePlugins + WithPluginCrypto + WithFocusDelivery):
// char member of scenes A+B, connection focused on A; emit in B → connection
// receives CONTROL_SIGNAL_SCENE_ACTIVITY{scene_id: B}, and NO EventFrame for B;
// emit in A → normal EventFrame; a NON-member's connection receives nothing
// for either.
var _ = Describe("INV-SCENE-62: scene_activity badge downgrade for non-focused member connections", func() {
	var (
		ts    *integrationtest.Server
		ctx   context.Context
		alice *integrationtest.Session
		bob   *integrationtest.Session
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
		// Connect both characters. ConnectAuthed immediately attaches the
		// Subscribe transport; JoinScene is called AFTER that, so we
		// DetachTransport + ReattachTransport to fold the membership into
		// the initial filter that RestoreFocus assembles.
		alice = ts.ConnectAuthed(ctx, "Alice")
		bob = ts.ConnectAuthed(ctx, "Bob")
	})

	AfterEach(func() {
		if alice != nil {
			alice.Logout(ctx)
		}
		if bob != nil {
			bob.Logout(ctx)
		}
		ts.Stop()
	})

	It("downgrades non-focused scene events to SCENE_ACTIVITY badges and forwards focused scene events normally", func() {
		loc := ts.NewLocation(ctx)

		// Alice creates two scenes and joins both.
		sceneA := alice.CreateScene(ctx, loc)
		Expect(sceneA).NotTo(BeZero(), "CreateScene A must return a non-zero bare ULID")
		sceneB := alice.CreateScene(ctx, loc)
		Expect(sceneB).NotTo(BeZero(), "CreateScene B must return a non-zero bare ULID")

		alice.JoinScene(ctx, sceneA)
		alice.JoinScene(ctx, sceneB)

		// Cycle Alice's transport so Subscribe picks up the new memberships
		// in its initial RestoreFocus filter assembly. Without this, the
		// JetStream consumer has no scene A/B filter subjects and scene
		// events are never delivered to Alice's loop at all.
		alice.DetachTransport(ctx)
		alice.ReattachTransport(ctx)

		// Set Alice's connection focus to scene A. dispatchDelivery reads
		// this via GetConnection.FocusKey when routing scene events.
		alice.SetSceneFocus(ctx, sceneA)

		// Bob is not a member of either scene. His Subscribe filter only
		// carries his location stream, so scene events from A or B never
		// reach his loop (no badge, no event).

		// Seed Alice as a DEK participant for both scenes so the encrypt/decrypt
		// fence (INV-PLUGIN-30) is satisfied. scene_pose is always sensitive per
		// the core-scenes manifest's crypto.emits, so Sensitive=true is required.
		// The badge downgrade fires before the decrypt path, so DEK seeding here
		// is needed only for the scene A forward path (where Alice is focused and
		// receives the full EventFrame).
		ts.SeedSceneDEKParticipant(ctx, sceneA, alice)
		ts.SeedSceneDEKParticipant(ctx, sceneB, alice)

		// Emit a sensitive event to scene B. Alice is a member but NOT focused
		// on B → dispatchDelivery must downgrade it to a SCENE_ACTIVITY badge
		// before the event content reaches the subscriber.
		ts.EmitSceneICContent(ctx, "core-scenes", sceneB,
			alice.CharacterID, "core-scenes:scene_pose",
			`{"text":"badge test pose in scene B"}`)

		// Alice's stream: SCENE_ACTIVITY badge for scene B (NOT an EventFrame).
		badge := alice.WaitForSceneActivityBadge(ctx, sceneB.String())
		Expect(badge).NotTo(BeNil(), "Alice must receive SCENE_ACTIVITY badge for scene B")
		Expect(badge.GetSignal()).To(Equal(corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY),
			"badge signal must be CONTROL_SIGNAL_SCENE_ACTIVITY (INV-SCENE-62)")
		Expect(badge.GetSceneId()).To(Equal(sceneB.String()),
			"badge scene_id must be scene B's bare ULID (INV-SCENE-62)")

		// Emit a sensitive event to scene A. Alice IS focused on A →
		// dispatchDelivery must forward it as a normal EventFrame.
		ts.EmitSceneICContent(ctx, "core-scenes", sceneA,
			alice.CharacterID, "core-scenes:scene_pose",
			`{"text":"forward test pose in scene A"}`)

		frame := alice.WaitForEvent(ctx, "core-scenes:scene_pose")
		Expect(frame).NotTo(BeNil(), "Alice must receive a normal EventFrame for scene A (INV-SCENE-62)")
		Expect(frame.GetType()).To(Equal("core-scenes:scene_pose"),
			"EventFrame type must be core-scenes:scene_pose")
		// Content-leak guard (INV-SCENE-62): matching by type alone is
		// insufficient — both scenes emit scene_pose, so a leaked scene-B
		// content frame would be returned first by the FIFO WaitForEvent and
		// pass a type-only check. The frame's stream subject is the
		// discriminator: it MUST carry scene A's ULID and MUST NOT carry scene
		// B's (a downgraded scene never reaches the event channel as content).
		Expect(frame.GetStream()).To(ContainSubstring(sceneA.String()),
			"the forwarded EventFrame MUST be scene A's (its focused scene)")
		Expect(frame.GetStream()).NotTo(ContainSubstring(sceneB.String()),
			"scene B content MUST NOT leak onto Alice's event channel — it was downgraded to a badge")

		// Bob: no badge, no event (non-member; scene subjects not in his filter).
		// Bob's Subscribe consumer was set up with his location filter only;
		// scene A/B subjects were never added, so no frames arrive on his stream.
		// Assert directly that his badge channel is empty after the emit settled.
		Expect(bob.SceneActivityBadgeCount()).To(Equal(0),
			"Bob (non-member) must not receive any SCENE_ACTIVITY badge (INV-SCENE-62)")
	})
})
