// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// holomush-5rh.25: cross-location roster name resolution and pose author
// name resolution end-to-end.
//
// ROOT CAUSE (pre-fix): GetSceneForViewer returned raw ULID strings in
// ParticipantInfo.CharacterName for participants whose sessions are in a
// different location from the viewing character. The old implementation
// resolved names only from the viewer's session location, so cross-location
// participants fell through with ULID fallbacks. The fix (holomush-5rh.25)
// introduces resolveRosterNames which fetches display names via
// characterNameResolver.Names — a direct DB lookup that is location-agnostic.
//
// Similarly, handleEmit stamped the pose's character_name field from
// req.CharacterName (the dispatcher provides the character's display name from
// session.Info.CharacterName), so the IC payload now carries the display name
// rather than leaving the field empty (no ULID was ever used there — the
// field was simply absent pre-fix).
var _ = Describe("holomush-5rh.25: scene name resolution", func() {
	// Spec 1: GetSceneForViewer roster — cross-location display names.
	Describe("GetSceneForViewer cross-location roster name resolution", func() {
		var (
			ts     *integrationtest.Server
			ctx    context.Context
			owner  *integrationtest.Session
			member *integrationtest.Session
		)

		BeforeEach(func() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
			DeferCleanup(cancel)
			ts = integrationtest.Start(
				suiteT,
				integrationtest.WithInTreePlugins(),
				integrationtest.WithPluginCrypto(),
				integrationtest.WithFocusDelivery(),
			)
			owner = ts.ConnectAuthed(ctx, "SceneOwner")
			member = ts.ConnectAuthed(ctx, "CrossLocMember")
		})

		AfterEach(func() {
			if member != nil {
				member.Logout(ctx)
			}
			if owner != nil {
				owner.Logout(ctx)
			}
			ts.Stop()
		})

		It("returns display names (not ULIDs) for participants in different locations", func() {
			// Place owner and member in DIFFERENT locations — this is the exact
			// cross-location case the pre-fix code could not resolve.
			ownerLoc := ts.NewLocation(ctx)
			memberLoc := ts.NewLocation(ctx)

			owner.MoveTo(ctx, ownerLoc)
			member.MoveTo(ctx, memberLoc)

			// Owner creates the scene (CreateScene also seeds the creator as a
			// participant row in the plugin DB). JoinScene (store shortcut) adds
			// the session-side focus_memberships entry so the owner can view the
			// scene via GetSceneForViewer.
			sceneID := owner.CreateScene(ctx, ownerLoc)
			Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero ULID")

			// Add owner's session-side focus membership (for I-17 scope floor).
			owner.JoinScene(ctx, sceneID)

			// Add member via the REAL `scene join` command — this writes a
			// participant row into the plugin DB (plugin_core_scenes.scene_participants),
			// which is what GetScene reads for the roster. The harness
			// Session.JoinScene store-shortcut only touches focus_memberships JSONB
			// and does NOT write to the plugin DB, so it cannot produce roster entries.
			//
			// `scene join` requires a bare scene ULID (ADR holomush-vy0rt) and
			// WithFocusDelivery so focusClient is non-nil (avoids the
			// "focus client not configured" error path).
			err := member.SendCommand(ctx, "scene join "+sceneID.String())
			Expect(err).NotTo(HaveOccurred(), "member must be able to join the scene via `scene join`")

			// Fetch via the REAL SceneAccessServer facade — the resolver is wired
			// by Session.GetSceneForViewer → Server.NewSceneAccessServer →
			// WithCharacterNameResolver(RepoCharacterNameResolver(worldCharRepo)).
			resp, err := owner.GetSceneForViewer(ctx, sceneID)
			Expect(err).NotTo(HaveOccurred(), "GetSceneForViewer must succeed")
			Expect(resp.GetScene()).NotTo(BeNil(), "response must carry a scene")

			participants := resp.GetScene().GetParticipants()
			Expect(participants).NotTo(BeEmpty(), "scene must have at least one participant")

			// Both participants must carry display names, not ULIDs.
			ulid26RE := `^[0-9A-HJKMNP-TV-Z]{26}$`
			for _, p := range participants {
				Expect(p.GetCharacterName()).NotTo(MatchRegexp(ulid26RE),
					"roster name for character %s must be a display name, not a ULID",
					p.GetCharacterId())
				Expect(p.GetCharacterName()).NotTo(BeEmpty(),
					"roster CharacterName must be populated (not empty)")
			}

			// Stronger: assert the exact seeded names appear.
			names := make([]string, 0, len(participants))
			for _, p := range participants {
				names = append(names, p.GetCharacterName())
			}
			Expect(names).To(ContainElement("SceneOwner"),
				"owner's display name must appear on the roster")
			Expect(names).To(ContainElement("CrossLocMember"),
				"cross-location member's display name must appear on the roster")
		})
	})

	// Spec 2: pose author display name in the IC event payload.
	Describe("scene pose command stamps author display name in IC payload", func() {
		var (
			ts    *integrationtest.Server
			ctx   context.Context
			alice *integrationtest.Session
		)

		BeforeEach(func() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
			DeferCleanup(cancel)
			ts = integrationtest.Start(
				suiteT,
				integrationtest.WithInTreePlugins(),
				integrationtest.WithPluginCrypto(),
				integrationtest.WithFocusDelivery(),
			)
			alice = ts.ConnectAuthed(ctx, "Alice")
		})

		AfterEach(func() {
			if alice != nil {
				alice.Logout(ctx)
			}
			ts.Stop()
		})

		It("decrypted pose payload carries actor_display_name equal to the author's display name (not a ULID)", func() {
			loc := ts.NewLocation(ctx)
			sceneID := alice.CreateScene(ctx, loc)
			Expect(sceneID).NotTo(BeZero())

			// Alice joins the scene so single-membership inference in handleEmit
			// can route the pose to this scene, and so the JoinedAt scope floor
			// does not filter the event out during read-back.
			alice.JoinScene(ctx, sceneID)

			// Seed Alice as a DEK participant BEFORE the emit so the AuthGuard
			// hot-tier permits decryption when QueryStreamHistory reads back.
			ts.SeedSceneDEKParticipant(ctx, sceneID, alice)

			// Emit via the REAL `scene pose` command path (dispatcher →
			// handleEmit). The dispatcher stamps req.CharacterName from
			// session.Info.CharacterName — "Alice" — which the CommunicationContent
			// builder carries as actor_display_name: "Alice".
			const poseText = "stands at the window, watching the rain"
			err := alice.SendCommand(ctx, "scene pose "+poseText)
			Expect(err).NotTo(HaveOccurred(), "scene pose command must succeed")

			// Build the dot-style IC subject for QueryStreamHistory.
			// Pattern: events.<gameID>.scene.<sceneID>.ic
			gameID := ts.GameID()
			icSubject := "events." + gameID + ".scene." + sceneID.String() + ".ic"

			// Read back with eventual consistency (the emit is async through
			// JetStream). Alice is a DEK participant, so the payload is decrypted.
			var decryptedPayload string
			Eventually(func(g Gomega) {
				events, queryErr := alice.QueryStreamHistory(ctx, icSubject)
				g.Expect(queryErr).NotTo(HaveOccurred())
				g.Expect(events).NotTo(BeEmpty(), "pose must appear in history")
				last := events[len(events)-1]
				g.Expect(last.GetMetadataOnly()).To(BeFalse(),
					"Alice is a DEK participant — payload must be decrypted, not metadata-only")
				decryptedPayload = string(last.GetPayload())
			}).Should(Succeed())

			// Parse the JSON payload and assert actor_display_name is the display name.
			var payloadMap map[string]string
			Expect(json.Unmarshal([]byte(decryptedPayload), &payloadMap)).To(Succeed(),
				"decrypted payload must be valid JSON")

			displayName, ok := payloadMap["actor_display_name"]
			Expect(ok).To(BeTrue(),
				"decrypted IC payload must contain an actor_display_name field; got: %s", decryptedPayload)

			Expect(displayName).To(Equal("Alice"),
				"pose author actor_display_name must equal the seeded display name")

			ulid26RE := `^[0-9A-HJKMNP-TV-Z]{26}$`
			Expect(displayName).NotTo(MatchRegexp(ulid26RE),
				"actor_display_name must be a display name, not a ULID; got: %s", displayName)
		})
	})
})
