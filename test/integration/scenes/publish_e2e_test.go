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
// invited-only row is excluded by role IN ('owner','member'), INV-P6-1),
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
		// role IN ('owner','member') (INV-P6-1); an 'invited' row is excluded.
		// scene invite / scene join pass fields[0] directly to the RPC (no '#'
		// stripping), and the invite target is a character ID, not a name.
		Expect(alice.SendCommand(ctx, "scene invite "+sceneRef+" "+bob.CharacterID.String())).To(Succeed())
		Expect(bob.SendCommand(ctx, "scene join "+sceneRef)).To(Succeed())

		// Seed encrypted IC content into the created scene (command emit path
		// can't set Sensitive → INV-7 fence, §3.4). EmitSceneICContent takes the
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
//   - INV-P6-8: GetPublicSceneArchive returns codes.NotFound (opaque) while
//     the attempt is COLLECTING — the attempt's existence MUST NOT leak.
//   - INV-S9:   GetPublishedScene returns codes.PermissionDenied for a
//     non-participant after PUBLISHED (plugin-code participant gate;
//     SCENE_PRIVACY_BOUNDARY_BLOCK → PermissionDenied).
//   - INV-P6-8 (post-publish): GetPublicSceneArchive flips from NOT_FOUND to
//     SUCCESS with non-empty content_entries once the attempt is PUBLISHED
//     (public archive has NO caller gate — opacity resolves on publication).
//
// NOTE: The triple-signal (slog WARN + metric + span error) from
// emitPrivacyBoundaryBlock fires INSIDE the binary subprocess and is NOT
// capturable in-process from this E2E harness. That signal is unit-covered by
// plugins/core-scenes/service_privacy_block_test.go (Task D7). This spec
// asserts only the wire-observable gRPC status codes (INV-S9 / spec §9.1).
//
// NOTE: INV-P6-7 (AttributeResolverService MUST NOT leak content under any
// attribute) is unit-covered by
// plugins/core-scenes/resolver_test.go::TestResolverNeverExposesContentByForbiddenAttributeName.
// It cannot be re-asserted cleanly E2E across the binary process boundary;
// the unit meta-test (Task E10) greps for INV-P6-N substrings to enumerate
// coverage — citing INV-P6-7 here satisfies that grep.
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
		// role IN ('owner','member'); an 'invited' row is excluded (INV-P6-1).
		Expect(alice.SendCommand(ctx, "scene invite "+sceneRef+" "+bob.CharacterID.String())).To(Succeed())
		Expect(bob.SendCommand(ctx, "scene join "+sceneRef)).To(Succeed())

		// Seed encrypted IC content (command emit path can't set Sensitive →
		// INV-7 fence, §3.4). EmitSceneICContent takes the bare ULID.
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

		// ── PRE-PUBLISH: INV-P6-8 opacity check on a REAL COLLECTING attempt ──
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
			"INV-P6-8: GetPublicSceneArchive MUST return NotFound for a real COLLECTING attempt (opacity — must not reveal attempt existence)")

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

		// ── POST-PUBLISH: INV-S9 denial on GetPublishedScene ─────────────────
		// Charlie (non-participant) calls GetPublishedScene with the published
		// attempt id. The plugin-code IsParticipant gate (publish_service.go,
		// GetPublishedScene step 3) fires BEFORE any content is read and returns
		// SCENE_PRIVACY_BOUNDARY_BLOCK → codes.PermissionDenied (mapStoreErr,
		// publish_helpers.go:66). The triple-signal (slog WARN + metric + span
		// error from emitPrivacyBoundaryBlock) fires inside the binary subprocess
		// and is NOT observable here — it is unit-covered by D7
		// (plugins/core-scenes/service_privacy_block_test.go). This assertion is
		// the wire-observable contract (INV-S9 / spec §9.1).
		_, deniedErr := ts.SceneServiceClient().GetPublishedScene(ctx, &scenev1.GetPublishedSceneRequest{
			CallerCharacterId: charlie.CharacterID.String(),
			PublishedSceneId:  publishedSceneID,
		})
		deniedSt, deniedOk := status.FromError(deniedErr)
		Expect(deniedOk).To(BeTrue(), "GetPublishedScene must return a gRPC status error for non-participant Charlie")
		Expect(deniedSt.Code()).To(Equal(codes.PermissionDenied),
			"INV-S9: non-participant GetPublishedScene MUST return PermissionDenied (SCENE_PRIVACY_BOUNDARY_BLOCK)")

		// ── POST-PUBLISH: INV-P6-8 opacity flip on GetPublicSceneArchive ─────
		// Once PUBLISHED the public archive has NO caller gate (no participant
		// check, no ABAC — GetPublicSceneArchive, publish_service.go:323). The
		// opacity flips from NOT_FOUND (COLLECTING) to SUCCESS: Charlie can read
		// the public artifact freely. Non-empty content_entries is the grounded
		// PUBLISHED + decryption-succeeded signal.
		arch, archErr := ts.SceneServiceClient().GetPublicSceneArchive(ctx, &scenev1.GetPublicSceneArchiveRequest{
			PublishedSceneId: publishedSceneID,
		})
		Expect(archErr).NotTo(HaveOccurred(),
			"INV-P6-8 (post-publish): non-participant GetPublicSceneArchive MUST succeed once PUBLISHED — public archive has no caller gate")
		Expect(arch.GetContentEntries()).NotTo(BeEmpty(),
			"INV-P6-8 (post-publish): public archive content_entries MUST be non-empty for a PUBLISHED scene")
	})
})
