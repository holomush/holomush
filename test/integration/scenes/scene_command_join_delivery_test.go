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

// Keystone (holomush-y5inx.9): a REAL `scene join` command, dispatched through
// the harness's CoreServer, must reach JoinFocus → AutoFocusOnJoin →
// per-connection subscription delivery, so an IC pose emitted into the scene
// AFTER the join is delivered on the joiner's live Subscribe stream.
//
// PRE-FIX GAP: the integrationtest harness wired only WithSubscriber and NO
// focus.Coordinator / StreamRegistry. The plugin host-service JoinFocus RPC
// (internal/plugin/goplugin/host_service.go:113) short-circuits with "focus
// coordinator not configured" BEFORE protoToFocusKey, so the real `scene join`
// path could never register the scene-stream subscription. WaitForEvent then
// times out — the joiner never receives the post-join pose.
//
// POST-FIX: WithFocusDelivery() wires a real focus.Coordinator + SessionStreamRegistry
// (+ ConfigureFocusDeps) into the harness, gated exactly like WithPluginCrypto
// (.8) so non-focus suites keep current wiring. The `scene join` command then
// drives AutoFocusOnJoin → StreamSenderAdapter → the connection's control
// channel, adding the scene IC stream to the live Subscribe filter set; the
// next EmitSceneICContent pose is delivered.
//
// This is a DELIVERY (subscription-open) assertion: a metadata-only frame whose
// Type == "scene_pose" is sufficient — the joiner need not be a DEK participant
// to prove the subscription reached the live loop (toProtoSubscribeResponse
// stamps Type from the event regardless of metadata_only,
// internal/grpc/server.go:664).
var _ = Describe("holomush-y5inx.9: real scene join command delivers post-join IC pose", func() {
	var (
		ts     *integrationtest.Server
		ctx    context.Context
		owner  *integrationtest.Session
		joiner *integrationtest.Session
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
		owner = ts.ConnectAuthed(ctx, "Owner")
		joiner = ts.ConnectAuthed(ctx, "Joiner")
	})

	AfterEach(func() {
		if joiner != nil {
			joiner.Logout(ctx)
		}
		if owner != nil {
			owner.Logout(ctx)
		}
		ts.Stop()
	})

	It("delivers a post-join scene_pose to a joiner who ran the real `scene join` command", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// REAL command path (NOT the JoinScene store-shortcut): this is the
		// surface the keystone exists to exercise. Pre-fix this either fails
		// (focus coordinator not configured surfaced as a CommandError) or
		// succeeds without wiring the subscription; post-fix it wires the
		// scene IC stream onto the joiner's live Subscribe loop.
		err := joiner.SendCommand(ctx, "scene join "+sceneID.String())
		Expect(err).NotTo(HaveOccurred(),
			"real `scene join` must succeed once the focus coordinator is wired "+
				"(pre-fix: COMMAND_REJECTED — focus coordinator not configured)")

		// Emit a real sensitive scene_pose into the scene AFTER the join, so the
		// only way the joiner can receive it is via the subscription
		// AutoFocusOnJoin added. The owner is the actor (a scene participant).
		const poseJSON = `{"text":"a post-join pose for the y5inx.9 delivery keystone"}`
		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			owner.CharacterID, "scene_pose", poseJSON)
		Expect(emitted.SubjectStr).To(ContainSubstring(sceneID.String()),
			"emitted subject must carry the bare scene ULID")

		// The authoritative delivery assertion: the joiner's live Subscribe
		// stream receives a scene_pose frame. Pre-fix this times out (no
		// subscription was ever added). A frame whose Type == "scene_pose"
		// proves the scene IC stream reached the live loop.
		frame := joiner.WaitForEvent(ctx, "core-scenes:scene_pose")
		Expect(frame).NotTo(BeNil(),
			"holomush-y5inx.9: the post-join pose MUST be delivered to the joiner's "+
				"live Subscribe stream once the focus coordinator + stream registry are wired")
		Expect(frame.GetType()).To(Equal("core-scenes:scene_pose"))
		// The joiner is a non-DEK-participant, so the delivered frame MUST be
		// metadata-only (no plaintext payload). This pins the live-path fail-closed
		// (no-plaintext-leak) invariant — defense-in-depth per abac-reviewer.
		// toProtoSubscribeResponse stamps Type regardless of metadata_only
		// (internal/grpc/server.go:664), so Type and MetadataOnly are independent
		// assertions.
		Expect(frame.GetMetadataOnly()).To(BeTrue(),
			"holomush-y5inx.9: non-DEK-participant joiner MUST receive metadata-only frame "+
				"(fail-closed: no plaintext payload leak)")
	})
})
