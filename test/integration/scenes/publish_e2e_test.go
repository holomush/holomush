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
