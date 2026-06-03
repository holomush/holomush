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

// INV-SCENE-47: a participant can read the IC history of a REAL CreateScene-minted
// (bare-ULID) scene via the host QueryStreamHistory path (the "scene log" route),
// receiving the REAL DECRYPTED sensitive pose — with no INVALID_ARGUMENT and no
// EVENTBUS_HISTORY_DECODE_FAILED.
//
// PRE-FIX FAILURE CHAIN (holomush-y5inx):
//   - Task 1 fix: core-scenes started minting bare-ULID scene IDs (e.g.
//     "01JVWXYZ…") instead of the legacy "scene-<ULID>" prefix form. Before
//     that fix, QueryStreamHistory's streamToFocusKey path could not ulid.Parse
//     the prefixed id and errored with INVALID_ARGUMENT. The bare-ULID id parses
//     cleanly, so the membership gate (I-17) and scope floor pass through.
//   - Task 8 fix: the harness history reader was wired with WithPluginCrypto's
//     AuthGuard + DEK manager + codec selector (holomush-y5inx.8). Before that
//     wiring the bare reader hit the zero-key decrypt path and returned
//     EVENTBUS_HISTORY_DECODE_FAILED for any encrypted event.
//
// This regression spec proves BOTH fixes hold end-to-end: CreateScene (mints bare
// ULID), JoinScene (I-17 membership), SeedSceneDEKParticipant (DEK participant
// set), EmitSceneICContent (real encrypted scene_pose), QueryStreamHistory → clean
// decrypt, payload == plaintext.
var _ = Describe("INV-SCENE-47: real scene history readable and decrypted via host", func() {
	const plaintext = `{"text":"a real scene pose for regression INV-SCENE-47"}`

	var (
		ts    *integrationtest.Server
		ctx   context.Context
		alice *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(suiteT, integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto())
		alice = ts.ConnectAuthed(ctx, "Alice")
	})

	AfterEach(func() {
		if alice != nil {
			alice.Logout(ctx)
		}
		ts.Stop()
	})

	It("decrypts a sensitive scene_pose for a participant reading via the scene log route (bare-ULID scene)", func() {
		// CreateScene mints a bare-ULID scene ID (no "scene-" prefix) as of
		// holomush-y5inx Task 1. The old prefix form caused INVALID_ARGUMENT from
		// streamToFocusKey's ulid.Parse; this confirms the bare form parses cleanly.
		loc := ts.NewLocation(ctx)
		sceneID := alice.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// Alice joins the scene (I-17 membership gate). Join BEFORE the emit so
		// the scope floor (her JoinedAt) does not filter the event out.
		alice.JoinScene(ctx, sceneID)

		// Mint the scene DEK with Alice as a participant BEFORE the emit. The
		// publisher's GetOrCreate(ctx, ctxID, nil) on the emit path finds this
		// existing DEK, so Alice's binding_id is in the participant set the
		// hot-tier AuthGuard checks on QueryStreamHistory. Without this, the
		// AuthGuard would downgrade to metadata-only (no error, but no plaintext).
		ts.SeedSceneDEKParticipant(ctx, sceneID, alice)

		// Emit a REAL sensitive scene_pose into the CreateScene-minted scene.
		// EmitSceneICContent passes Sensitive=true, taking the XChaCha20v1 encrypt
		// branch; the ciphertext is published to JetStream and projected to
		// plugin_core_scenes.scene_log by the audit consumer.
		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			alice.CharacterID, "scene_pose", plaintext)
		Expect(emitted.SubjectStr).To(ContainSubstring(sceneID.String()),
			"emitted subject must carry the bare scene ULID")

		// Poll until the event is readable (JetStream ack may be slightly ahead of
		// the durable consumer's projection). This also provides an early signal
		// that the INVALID_ARGUMENT regression (Task 1) and the
		// EVENTBUS_HISTORY_DECODE_FAILED regression (Task 8) do not fire.
		Eventually(func(g Gomega) {
			got, err := alice.QueryStreamHistory(ctx, emitted.SubjectStr)
			g.Expect(err).NotTo(HaveOccurred(),
				"QueryStreamHistory must not error — pre-Task-1: INVALID_ARGUMENT, pre-Task-8: EVENTBUS_HISTORY_DECODE_FAILED")
			g.Expect(got).NotTo(BeEmpty(), "the encrypted scene_pose must be readable back")
		}).Should(Succeed())

		// Final authoritative assertion: the last frame is fully decrypted.
		got, err := alice.QueryStreamHistory(ctx, emitted.SubjectStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeEmpty())
		ev := got[len(got)-1]
		Expect(ev.GetMetadataOnly()).To(BeFalse(),
			"INV-SCENE-47: participant must receive a DECRYPTED (non-metadata-only) frame via the scene log route")
		Expect(string(ev.GetPayload())).To(Equal(plaintext),
			"INV-SCENE-47: decrypted payload must equal the original plaintext JSON")
	})
})
