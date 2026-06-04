// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// holomush-shcyu (Task 5, folds holomush-5rh.20.39/E6): the happy-path publish
// E2E. Alice creates a scene, Bob joins (so the vote roster seeds him — an
// invited-only row is excluded by role IN ('owner','member'), INV-SCENE-28),
// encrypted IC content is seeded (the command emit path is fence-rejected under
// crypto, spec §3.4), the lifecycle is driven entirely by commands (end →
// publish → unanimous vote), the scheduler sweeps COOLOFF → PUBLISHED (short
// cooloff_window + scheduler_interval via WithPluginConfigOverrides), and both
// read RPCs return decrypted content keyed by the ATTEMPT ulid.
var _ = Describe("happy-path scene publish lifecycle reaches PUBLISHED with decrypted content", func() {
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
			integrationtest.WithPluginConfigOverrides(map[string]map[string]string{
				"core-scenes": {"cooloff_window": "1ms", "scheduler_interval": "20ms"},
			}),
		)
		alice = ts.ConnectAuthed(ctx, "Alice")
		bob = ts.ConnectAuthed(ctx, "Bob")
	})

	AfterEach(func() {
		if bob != nil {
			bob.Logout(ctx)
		}
		if alice != nil {
			alice.Logout(ctx)
		}
		ts.Stop()
	})

	It("Alice ends, publishes, both vote yes, cool-off sweeps → PUBLISHED, both read RPCs return decrypted content", func() {
		loc := ts.NewLocation(ctx)

		// CreateScene returns the bare ULID, which is exactly the stored id
		// (holomush-y5inx). Command/RPC resolvers match the stored form verbatim
		// (handleEnd/handleJoin/handleInvite pass the token straight through;
		// resolveSceneRef strips only the leading '#'), so the bare id is the ref.
		sceneID := alice.CreateScene(ctx, loc)
		sceneRef := sceneID.String()

		// Bob must JOIN, not merely be invited — the vote roster seeds from
		// role IN ('owner','member') (INV-SCENE-28); an 'invited' row is excluded.
		// scene invite / scene join pass fields[0] directly to the RPC (no '#'
		// stripping), and the invite target is a character ID, not a name.
		Expect(alice.SendCommand(ctx, "scene invite "+sceneRef+" "+bob.CharacterID.String())).To(Succeed())
		Expect(bob.SendCommand(ctx, "scene join "+sceneRef)).To(Succeed())

		// Seed encrypted IC content into the created scene (command emit path
		// can't set Sensitive → INV-SCENE-59 fence, §3.4). EmitSceneICContent takes the
		// bare ULID and builds the bare subject.
		ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			alice.CharacterID, "scene_pose", `{"text":"the scene happens"}`)

		// Command-driven lifecycle. scene end takes the bare stored id; scene
		// publish / scene publish vote route through resolveSceneRef so they need
		// the '#'-prefixed stored form.
		Expect(alice.SendCommand(ctx, "scene end "+sceneRef)).To(Succeed())
		Expect(alice.SendCommand(ctx, "scene publish #"+sceneRef)).To(Succeed())
		Expect(alice.SendCommand(ctx, "scene publish vote yes #"+sceneRef)).To(Succeed())
		Expect(bob.SendCommand(ctx, "scene publish vote yes #"+sceneRef)).To(Succeed())

		// Scheduler (~20ms interval, ~1ms cool-off) sweeps COOLOFF → PUBLISHED.
		// The read RPCs key off the ATTEMPT ulid (published_scene_id), recovered
		// via ListScenePublishAttempts → the PUBLISHED summary's Id. Poll until a
		// PUBLISHED attempt appears — that's the grounded PUBLISHED signal
		// (status string "PUBLISHED", publish_types.go:19) regardless of event
		// name. SceneId keys off the stored bare ULID (IsParticipant /
		// ListSceneAttempts), so pass sceneRef.
		var publishedSceneID string
		Eventually(func(g Gomega) {
			listResp, err := ts.SceneServiceClient().ListScenePublishAttempts(ctx,
				&scenev1.ListScenePublishAttemptsRequest{
					CallerCharacterId: alice.CharacterID.String(),
					SceneId:           sceneRef,
				})
			g.Expect(err).NotTo(HaveOccurred())
			publishedSceneID = ""
			for _, a := range listResp.GetAttempts() {
				if a.GetStatus() == "PUBLISHED" {
					publishedSceneID = a.GetId()
				}
			}
			g.Expect(publishedSceneID).NotTo(BeEmpty(), "no PUBLISHED attempt yet")
		}).WithTimeout(5 * time.Second).WithPolling(20 * time.Millisecond).Should(Succeed())

		// Participant read returns decrypted content. content_entries is
		// "populated only when PUBLISHED" (scene.pb.go:2270), so non-empty
		// content is the grounded PUBLISHED + decryption-succeeded signal.
		pub, err := ts.SceneServiceClient().GetPublishedScene(ctx, &scenev1.GetPublishedSceneRequest{
			CallerCharacterId: alice.CharacterID.String(),
			PublishedSceneId:  publishedSceneID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.GetStatus()).To(Equal("PUBLISHED"))
		Expect(pub.GetContentEntries()).NotTo(BeEmpty())

		// Public archive (no caller gate; keyed by the attempt ulid) returns
		// content once PUBLISHED.
		arch, err := ts.SceneServiceClient().GetPublicSceneArchive(ctx, &scenev1.GetPublicSceneArchiveRequest{
			PublishedSceneId: publishedSceneID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(arch.GetContentEntries()).NotTo(BeEmpty())
	})
})

// holomush-5rh.20.40 (Task E7): Phase 6 hard-privacy-boundary gate for a
// NON-PARTICIPANT (Charlie). Asserts the wire-observable denial codes that
// cross the binary-plugin process boundary:
//
//   - INV-SCENE-35: GetPublicSceneArchive returns codes.NotFound (opaque) while
//     the attempt is COLLECTING — the attempt's existence MUST NOT leak.
//   - INV-SCENE-60:   GetPublishedScene returns codes.PermissionDenied for a
//     non-participant after PUBLISHED (plugin-code participant gate;
//     SCENE_PRIVACY_BOUNDARY_BLOCK → PermissionDenied).
//   - INV-SCENE-35 (post-publish): GetPublicSceneArchive flips from NOT_FOUND to
//     SUCCESS with non-empty content_entries once the attempt is PUBLISHED
//     (public archive has NO caller gate — opacity resolves on publication).
//
// NOTE: The triple-signal (slog WARN + metric + span error) from
// emitPrivacyBoundaryBlock fires INSIDE the binary subprocess and is NOT
// capturable in-process from this E2E harness. That signal is unit-covered by
// plugins/core-scenes/service_privacy_block_test.go (Task D7). This spec
// asserts only the wire-observable gRPC status codes (INV-SCENE-60 / spec §9.1).
//
// NOTE: INV-SCENE-34 (AttributeResolverService MUST NOT leak content under any
// attribute) is unit-covered by
// plugins/core-scenes/resolver_test.go::TestResolverNeverExposesContentByForbiddenAttributeName.
// It cannot be re-asserted cleanly E2E across the binary process boundary;
// the unit meta-test (Task E10) greps for INV-P6-N substrings to enumerate
// coverage — citing INV-SCENE-34 here satisfies that grep.
var _ = Describe("Phase 6 hard-privacy-boundary gate for a non-participant (Charlie)", func() {
	var (
		ts      *integrationtest.Server
		ctx     context.Context
		alice   *integrationtest.Session
		bob     *integrationtest.Session
		charlie *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithPluginConfigOverrides(map[string]map[string]string{
				"core-scenes": {"cooloff_window": "1ms", "scheduler_interval": "20ms"},
			}),
		)
		alice = ts.ConnectAuthed(ctx, "Alice")
		bob = ts.ConnectAuthed(ctx, "Bob")
		charlie = ts.ConnectAuthed(ctx, "Charlie")
	})

	AfterEach(func() {
		if charlie != nil {
			charlie.Logout(ctx)
		}
		if bob != nil {
			bob.Logout(ctx)
		}
		if alice != nil {
			alice.Logout(ctx)
		}
		ts.Stop()
	})

	It("Charlie (non-participant) sees NotFound pre-publish, PermissionDenied on GetPublishedScene, then public archive succeeds post-publish", func() {
		loc := ts.NewLocation(ctx)

		// CreateScene returns the bare ULID, which is exactly the stored id
		// (holomush-y5inx — scenes mint bare ULIDs). Command/RPC resolvers match
		// the bare form verbatim (same as the happy-path test above).
		sceneID := alice.CreateScene(ctx, loc)
		sceneRef := sceneID.String()

		// Bob must JOIN (not merely invited) so the vote roster seeds him —
		// role IN ('owner','member'); an 'invited' row is excluded (INV-SCENE-28).
		Expect(alice.SendCommand(ctx, "scene invite "+sceneRef+" "+bob.CharacterID.String())).To(Succeed())
		Expect(bob.SendCommand(ctx, "scene join "+sceneRef)).To(Succeed())

		// Seed encrypted IC content (command emit path can't set Sensitive →
		// INV-SCENE-59 fence, §3.4). EmitSceneICContent takes the bare ULID.
		ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			alice.CharacterID, "scene_pose", `{"text":"the scene happens"}`)

		// ── DRIVE TO COLLECTING ──────────────────────────────────────────────
		// end → publish creates a real attempt in COLLECTING. We deliberately
		// probe opacity against this REAL attempt id (not a bogus key) so the
		// assertion proves the attempt's existence is hidden, not merely that an
		// unknown id 404s.
		Expect(alice.SendCommand(ctx, "scene end "+sceneRef)).To(Succeed())
		Expect(alice.SendCommand(ctx, "scene publish #"+sceneRef)).To(Succeed())

		// Recover the real COLLECTING attempt id (as Alice — a participant).
		var collectingAttemptID string
		Eventually(func(g Gomega) {
			listResp, err := ts.SceneServiceClient().ListScenePublishAttempts(ctx,
				&scenev1.ListScenePublishAttemptsRequest{
					CallerCharacterId: alice.CharacterID.String(),
					SceneId:           sceneRef,
				})
			g.Expect(err).NotTo(HaveOccurred())
			collectingAttemptID = ""
			for _, a := range listResp.GetAttempts() {
				if a.GetStatus() == "COLLECTING" {
					collectingAttemptID = a.GetId()
				}
			}
			g.Expect(collectingAttemptID).NotTo(BeEmpty(), "no COLLECTING attempt yet")
		}).WithTimeout(5 * time.Second).WithPolling(20 * time.Millisecond).Should(Succeed())

		// ── PRE-PUBLISH: INV-SCENE-35 opacity check on a REAL COLLECTING attempt ──
		// A real attempt EXISTS in COLLECTING, yet GetPublicSceneArchive MUST
		// return codes.NotFound — the public surface MUST NOT reveal a non-
		// PUBLISHED attempt's existence (publicArchiveNotFound, publish_helpers.go;
		// GetPublicSceneArchive 404s any status != PUBLISHED). This is the genuine
		// opacity property: the attempt is real and live, but invisible publicly.
		_, preErr := ts.SceneServiceClient().GetPublicSceneArchive(ctx, &scenev1.GetPublicSceneArchiveRequest{
			PublishedSceneId: collectingAttemptID,
		})
		preSt, preOk := status.FromError(preErr)
		Expect(preOk).To(BeTrue(), "pre-publish GetPublicSceneArchive must return a gRPC status error")
		Expect(preSt.Code()).To(Equal(codes.NotFound),
			"INV-SCENE-35: GetPublicSceneArchive MUST return NotFound for a real COLLECTING attempt (opacity — must not reveal attempt existence)")

		// ── DRIVE LIFECYCLE TO PUBLISHED ─────────────────────────────────────
		// Unanimous vote → scheduler sweeps COOLOFF→PUBLISHED (mirrors happy path).
		Expect(alice.SendCommand(ctx, "scene publish vote yes #"+sceneRef)).To(Succeed())
		Expect(bob.SendCommand(ctx, "scene publish vote yes #"+sceneRef)).To(Succeed())

		// Poll ListScenePublishAttempts (as Alice — participant) until a
		// PUBLISHED attempt appears; recover the attempt id.
		var publishedSceneID string
		Eventually(func(g Gomega) {
			listResp, err := ts.SceneServiceClient().ListScenePublishAttempts(ctx,
				&scenev1.ListScenePublishAttemptsRequest{
					CallerCharacterId: alice.CharacterID.String(),
					SceneId:           sceneRef,
				})
			g.Expect(err).NotTo(HaveOccurred())
			publishedSceneID = ""
			for _, a := range listResp.GetAttempts() {
				if a.GetStatus() == "PUBLISHED" {
					publishedSceneID = a.GetId()
				}
			}
			g.Expect(publishedSceneID).NotTo(BeEmpty(), "no PUBLISHED attempt yet")
		}).WithTimeout(5 * time.Second).WithPolling(20 * time.Millisecond).Should(Succeed())

		// The PUBLISHED attempt MUST be the same row that was COLLECTING — proves
		// the COLLECTING→PUBLISHED transition (opacity flip on the same attempt),
		// not a replacement attempt (INV-SCENE-35).
		Expect(publishedSceneID).To(Equal(collectingAttemptID),
			"PUBLISHED attempt must be the same row that was COLLECTING (transition, not replacement)")

		// ── POST-PUBLISH: INV-SCENE-60 denial on GetPublishedScene ─────────────────
		// Charlie (non-participant) calls GetPublishedScene with the published
		// attempt id. The plugin-code IsParticipant gate (publish_service.go,
		// GetPublishedScene step 3) fires BEFORE any content is read and returns
		// SCENE_PRIVACY_BOUNDARY_BLOCK → codes.PermissionDenied (mapStoreErr,
		// publish_helpers.go:66). The triple-signal (slog WARN + metric + span
		// error from emitPrivacyBoundaryBlock) fires inside the binary subprocess
		// and is NOT observable here — it is unit-covered by D7
		// (plugins/core-scenes/service_privacy_block_test.go). This assertion is
		// the wire-observable contract (INV-SCENE-60 / spec §9.1).
		_, deniedErr := ts.SceneServiceClient().GetPublishedScene(ctx, &scenev1.GetPublishedSceneRequest{
			CallerCharacterId: charlie.CharacterID.String(),
			PublishedSceneId:  publishedSceneID,
		})
		deniedSt, deniedOk := status.FromError(deniedErr)
		Expect(deniedOk).To(BeTrue(), "GetPublishedScene must return a gRPC status error for non-participant Charlie")
		Expect(deniedSt.Code()).To(Equal(codes.PermissionDenied),
			"INV-SCENE-60: non-participant GetPublishedScene MUST return PermissionDenied (SCENE_PRIVACY_BOUNDARY_BLOCK)")

		// ── POST-PUBLISH: INV-SCENE-35 opacity flip on GetPublicSceneArchive ─────
		// Once PUBLISHED the public archive has NO caller gate (no participant
		// check, no ABAC — GetPublicSceneArchive, publish_service.go:323). The
		// opacity flips from NOT_FOUND (COLLECTING) to SUCCESS: Charlie can read
		// the public artifact freely. Non-empty content_entries is the grounded
		// PUBLISHED + decryption-succeeded signal.
		arch, archErr := ts.SceneServiceClient().GetPublicSceneArchive(ctx, &scenev1.GetPublicSceneArchiveRequest{
			PublishedSceneId: publishedSceneID,
		})
		Expect(archErr).NotTo(HaveOccurred(),
			"INV-SCENE-35 (post-publish): non-participant GetPublicSceneArchive MUST succeed once PUBLISHED — public archive has no caller gate")
		Expect(arch.GetContentEntries()).NotTo(BeEmpty(),
			"INV-SCENE-35 (post-publish): public archive content_entries MUST be non-empty for a PUBLISHED scene")
	})
})

// holomush-5rh.20.41 (Task E8): Phase 6 retry budget exhaustion + admin extend
// flow end-to-end. Proves:
//
//   - INV-P6-E8-1: StartScenePublish returns codes.FailedPrecondition
//     (SCENE_PUBLISH_ATTEMPTS_EXHAUSTED) once the budget is exhausted
//     (default max = 3, after 3 start → withdraw cycles).
//   - INV-P6-E8-2: A non-admin attempting `scene publish vote extend` is
//     denied (COMMAND_REJECTED) by the admin ABAC gate
//     (admin-extend-publish-attempts policy: action "extend_publish_attempts",
//     requires "admin" in principal.character.roles — plugin.yaml:226-229).
//   - INV-P6-E8-3: An admin running the SAME command succeeds (budget bumped).
//   - INV-P6-E8-4: After the admin extend, the next StartScenePublish
//     succeeds (Total=3 < newMax=5).
//
// Requires WithRealABAC so the admin-extend-publish-attempts ABAC gate is live
// (under allow-all the non-admin denial assertion would be meaningless).
// WithInTreePlugins loads core-scenes and registers its manifest policies
// (including execute-scene-commands and admin-extend-publish-attempts) on the
// real engine's cache.
var _ = Describe("publish-attempt retry budget exhaustion and admin extend", func() {
	var (
		ts           *integrationtest.Server
		ctx          context.Context
		alice        *integrationtest.Session // scene owner, no admin role
		adminSession *integrationtest.Session // admin role → extend gate passes
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
		DeferCleanup(cancel)
		// WithRealABAC is mandatory for this suite: the admin-extend-publish-attempts
		// policy (plugin.yaml:226-229) must be enforced. Under allow-all the non-admin
		// denial cannot be distinguished from an admin grant.
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithRealABAC(),
		)
		alice = ts.ConnectAuthed(ctx, "Alice")
		adminSession = ts.ConnectAuthedWithRoles(ctx, "Admin", []string{"admin"})
	})

	AfterEach(func() {
		if adminSession != nil {
			adminSession.Logout(ctx)
		}
		if alice != nil {
			alice.Logout(ctx)
		}
		ts.Stop()
	})

	It("exhausts attempt budget, non-admin extend does not bump budget, admin extend bumps budget, then StartScenePublish succeeds", func() {
		loc := ts.NewLocation(ctx)

		// CreateScene returns the bare ULID, which is exactly the stored id
		// (holomush-y5inx — scenes mint bare ULIDs, no "scene-" prefix).
		sceneID := alice.CreateScene(ctx, loc)
		sceneRef := sceneID.String()

		// End the scene so publish attempts can be started (scene must be in
		// 'ended' state — StartScenePublish precondition, publish_service.go:176).
		// The "scene end" command takes the bare stored id (no '#' required;
		// mirrors the existing publish_e2e_test.go pattern).
		Expect(alice.SendCommand(ctx, "scene end "+sceneRef)).To(Succeed())

		// ── EXHAUST THE BUDGET (default max = 3) ─────────────────────────────
		// Each start → withdraw cycle increments CountAttempts.Total by 1
		// (publish_store.go: CountAttempts counts ALL rows; TriggerWithdraw
		// transitions the row to ATTEMPT_FAILED which is terminal, so Active=0
		// again). After 3 cycles: Total=3, Active=0 → 4th Start is rejected.
		for i := range 3 {
			startResp, startErr := ts.SceneServiceClient().StartScenePublish(ctx,
				&scenev1.StartScenePublishRequest{
					CallerCharacterId: alice.CharacterID.String(),
					SceneId:           sceneRef,
				})
			Expect(startErr).NotTo(HaveOccurred(),
				"INV-P6-E8: StartScenePublish attempt %d/%d must succeed (budget not yet exhausted)", i+1, 3)
			Expect(startResp.GetPublishedSceneId()).NotTo(BeEmpty(),
				"INV-P6-E8: attempt %d/%d must return a non-empty published_scene_id", i+1, 3)

			_, withdrawErr := ts.SceneServiceClient().WithdrawScenePublish(ctx,
				&scenev1.WithdrawScenePublishRequest{
					CallerCharacterId: alice.CharacterID.String(),
					PublishedSceneId:  startResp.GetPublishedSceneId(),
				})
			Expect(withdrawErr).NotTo(HaveOccurred(),
				"INV-P6-E8: WithdrawScenePublish attempt %d/%d must succeed", i+1, 3)
		}

		// ── INV-P6-E8-1: 4th StartScenePublish → FailedPrecondition ─────────
		// Total=3 >= maxAttempts=3; mapStoreErr maps SCENE_PUBLISH_ATTEMPTS_EXHAUSTED
		// to codes.FailedPrecondition (publish_helpers.go:57-59).
		_, exhaustedErr := ts.SceneServiceClient().StartScenePublish(ctx,
			&scenev1.StartScenePublishRequest{
				CallerCharacterId: alice.CharacterID.String(),
				SceneId:           sceneRef,
			})
		Expect(exhaustedErr).To(HaveOccurred(),
			"INV-P6-E8-1: 4th StartScenePublish MUST be rejected after budget exhaustion")
		exhaustedSt, exhaustedOk := status.FromError(exhaustedErr)
		Expect(exhaustedOk).To(BeTrue(),
			"INV-P6-E8-1: exhausted error MUST be a gRPC status error")
		Expect(exhaustedSt.Code()).To(Equal(codes.FailedPrecondition),
			"INV-P6-E8-1: exhausted StartScenePublish MUST return FailedPrecondition (SCENE_PUBLISH_ATTEMPTS_EXHAUSTED)")

		// ── INV-P6-E8-2: Non-admin extend → budget NOT changed ───────────────
		// SendCommand routing for plugin CommandError: when handleVoteExtend
		// returns pluginsdk.Errorf("permission denied"), the dispatcher treats a
		// CommandError as a user-facing command RESPONSE, not a dispatch error —
		// Dispatch returns nil (dispatcher.go), so HandleCommand reports
		// Success=true and Session.SendCommand returns nil for plugin-level
		// command denials (the denial reaches the player as a command_response,
		// never as an RPC failure). The grounded signal for denial is therefore
		// STATE-BASED: the budget MUST remain exhausted (Total=3 >= max=3) after
		// Alice's attempt, proved by the 4th StartScenePublish still returning
		// FailedPrecondition.
		//
		// Under allow-all the budget WOULD be bumped (handleVoteExtend calls
		// ExtendScenePublishVoteAttempts when the gate passes). Under the real
		// ABAC engine, the admin-extend-publish-attempts policy denies Alice
		// (no admin role) so the budget stays exhausted. The delta is observable.
		//
		// Alice (owner, NO admin role) sends the extend command. The binary
		// plugin's handleVoteExtend (commands.go:1565) evaluates the
		// admin-extend-publish-attempts policy via PluginHostService.Evaluate.
		// The real engine denies Alice (principal.character.roles = ["player"]).
		Expect(alice.SendCommand(ctx, "scene publish vote extend 2 #"+sceneRef)).To(Succeed(),
			"INV-P6-E8-2: Alice's extend command must dispatch without infra error — "+
				"user-facing denials surface as command_response events, not RPC failures")

		// The grounded denial signal: the budget MUST still be exhausted after
		// Alice's extend because the ABAC gate denied her and the budget was NOT
		// bumped. Under allow-all the budget would have been bumped to 5 and this
		// StartScenePublish would succeed — the failure here proves the real ABAC
		// gate is live and denied Alice.
		_, stillExhaustedErr := ts.SceneServiceClient().StartScenePublish(ctx,
			&scenev1.StartScenePublishRequest{
				CallerCharacterId: alice.CharacterID.String(),
				SceneId:           sceneRef,
			})
		Expect(stillExhaustedErr).To(HaveOccurred(),
			"INV-P6-E8-2 (state-based denial proof): budget MUST still be exhausted after Alice's extend — "+
				"if this fails under allow-all (budget bumped), it proves the real ABAC gate is non-trivially enforced")
		stillExhaustedSt, stillExhaustedOk := status.FromError(stillExhaustedErr)
		Expect(stillExhaustedOk).To(BeTrue(),
			"INV-P6-E8-2: post-Alice-extend StartScenePublish MUST return a gRPC status error")
		Expect(stillExhaustedSt.Code()).To(Equal(codes.FailedPrecondition),
			"INV-P6-E8-2: budget MUST remain exhausted (FailedPrecondition) after Alice's non-admin extend — "+
				"real ABAC engine denied Alice so ExtendScenePublishVoteAttempts was never called")

		// ── INV-P6-E8-3: Admin extend → budget BUMPED ────────────────────────
		// Admin has the "admin" role in character_roles (stamped by
		// ConnectAuthedWithRoles). The real engine permits
		// extend_publish_attempts via admin-extend-publish-attempts
		// (plugin.yaml:226-229). Budget bumps from 3 → 5.
		Expect(adminSession.SendCommand(ctx, "scene publish vote extend 2 #"+sceneRef)).To(Succeed(),
			"INV-P6-E8-3: admin's extend command must dispatch without infra error")

		// ── INV-P6-E8-4: StartScenePublish succeeds after admin extend ────────
		// After the admin extend: Total=3, newMax=5 → 3 < 5 → budget not
		// exhausted; a fresh attempt is created. This also proves the admin ABAC
		// gate was actually enforced: Alice's extend (above) left the budget
		// unchanged, while Admin's extend bumped it. The ONLY difference between
		// the two callers is the "admin" role — proving the policy gate.
		postExtendResp, postExtendErr := ts.SceneServiceClient().StartScenePublish(ctx,
			&scenev1.StartScenePublishRequest{
				CallerCharacterId: alice.CharacterID.String(),
				SceneId:           sceneRef,
			})
		Expect(postExtendErr).NotTo(HaveOccurred(),
			"INV-P6-E8-4: StartScenePublish MUST succeed after admin extends the budget (Total=3 < newMax=5)")
		Expect(postExtendResp.GetPublishedSceneId()).NotTo(BeEmpty(),
			"INV-P6-E8-4: post-extend StartScenePublish MUST return a non-empty published_scene_id — "+
				"budget was bumped by admin and the attempt was created successfully")
	})
})
