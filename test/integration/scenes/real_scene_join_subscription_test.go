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

// Regression spec (holomush-y5inx.5): INV-SCENE-48 — joining a scene via the
// REAL `scene join` command with a BARE-ULID scene ID opens a focus
// subscription, so an IC pose emitted into the scene AFTER the join is
// DELIVERED to the joiner's live Subscribe stream.
//
// ROOT CAUSE (pre-fix): protoToFocusKey in
// internal/plugin/goplugin/host_service.go received the join key as the
// PREFIXED string "scene-<ULID>" — the scene's stored primary key, which
// newSceneID() minted pre-fix as "scene-" + ULID. ulid.Parse rejects that
// prefix, so the parse failed, the subscription was never registered, and
// WaitForEvent timed out — no pose ever arrived on the joiner's stream.
//
// FIX (holomush-y5inx Task 1 / ADR holomush-vy0rt): scene IDs are now minted
// BARE — newSceneID() returns a bare ULID (`return id.String()`), so the stored
// scene id is bare and `scene join` passes a bare ULID that protoToFocusKey's
// ulid.Parse accepts UNMODIFIED. NO boundary-stripping is involved: ADR
// holomush-vy0rt explicitly REJECTED the per-boundary TrimPrefix/strip approach
// (Option B). With the bare id parsing cleanly, AutoFocusOnJoin registers the
// scene IC stream on the joiner's live Subscribe loop.
//
// This is a DELIVERY assertion: a metadata-only frame whose Type == "scene_pose"
// proves the subscription opened and the live loop received the event.
// toProtoSubscribeResponse stamps Type regardless of metadata_only
// (internal/grpc/server.go:664), so Type and MetadataOnly are independent.
var _ = Describe("INV-SCENE-48: bare-ULID scene join opens a live subscription", func() {
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

	It("delivers a post-join scene_pose to the joiner after running `scene join <bare-ULID>` (INV-SCENE-48)", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// REAL command path (NOT the JoinScene store-shortcut, which bypasses
		// protoToFocusKey entirely). The regression being pinned is specifically
		// that `scene join <bare-ULID>` reaches protoToFocusKey with the
		// prefixed key "scene-<ULID>", which ulid.Parse rejects pre-fix.
		// Post-fix the prefix is stripped before parsing, so the subscription
		// is registered and AutoFocusOnJoin wires the scene IC stream into the
		// joiner's live Subscribe filter set.
		err := joiner.SendCommand(ctx, "scene join "+sceneID.String())
		Expect(err).NotTo(HaveOccurred(),
			"INV-SCENE-48: `scene join <bare-ULID>` must succeed — pre-fix: "+
				"ulid.Parse of the prefixed key failed, returning COMMAND_REJECTED")

		// Emit a real IC pose AFTER the join so the only delivery path is the
		// subscription AutoFocusOnJoin opened. If protoToFocusKey still fails
		// to parse, no subscription exists and WaitForEvent times out.
		const poseJSON = `{"text":"regression pose for INV-SCENE-48: bare-ULID parse fix"}`
		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			owner.CharacterID, "scene_pose", poseJSON)
		Expect(emitted.SubjectStr).To(ContainSubstring(sceneID.String()),
			"emitted subject must carry the bare scene ULID")

		// Authoritative delivery assertion (INV-SCENE-48): the joiner's live
		// Subscribe stream receives a scene_pose frame. Pre-fix this always
		// timed out because the subscription was never registered. Post-fix
		// the frame arrives once AutoFocusOnJoin succeeds.
		frame := joiner.WaitForEvent(ctx, "scene_pose")
		Expect(frame).NotTo(BeNil(),
			"INV-SCENE-48: post-join pose MUST be delivered — pre-fix: "+
				"protoToFocusKey could not parse the prefixed join key, "+
				"so AutoFocusOnJoin never registered the subscription")
		Expect(frame.GetType()).To(Equal("scene_pose"))
		// The joiner is not a DEK participant, so the frame MUST be
		// metadata-only — no plaintext payload leak (fail-closed).
		Expect(frame.GetMetadataOnly()).To(BeTrue(),
			"INV-SCENE-48: non-DEK-participant joiner MUST receive a "+
				"metadata-only frame (fail-closed: no plaintext leak)")
	})
})
