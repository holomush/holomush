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

// INV-FS-3: Lua-runtime focus-delta parity.
//
// Proves that the gopher-lua VM path into holomush.auto_focus_on_join (the
// Lua hostfunc registered at internal/plugin/lua/stdlib_focus.go) delivers
// a live scene IC event to the joiner's Subscribe stream, end-to-end. This
// is the full-fidelity lock for plugin-runtime symmetry: the SAME
// subscribe/emit/await assertion used by the binary `scene join` keystone
// (scene_command_join_delivery_test.go, holomush-y5inx.9) fires here, but
// with the focus delta triggered by the Lua `luafocusjoin` command rather
// than the binary scene-join gRPC path.
//
// Bead: holomush-66228
// Spec: docs/superpowers/plans/2026-05-28-focus-delta-coordinator-unification.md §Task 13
var _ = Describe("INV-FS-3: Lua-runtime auto_focus_on_join parity — live IC delivery", func() {
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
		// WithExtraPluginDir stages testdata/lua/focus_join (the Lua fixture
		// that calls holomush.auto_focus_on_join) into the plugin load path
		// alongside the in-tree plugins. WithFocusDelivery wires the real
		// focus.Coordinator + SessionStreamRegistry so the Lua hostfunc's
		// auto_focus_on_join call reaches the live Subscribe filter set.
		// WithPluginCrypto is required for EmitSceneICContent + the live
		// crypto-decode path that delivers scene IC frames to the joiner.
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithFocusDelivery(),
			integrationtest.WithExtraPluginDir("testdata/lua/focus_join"),
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

	It("delivers a post-join scene_pose to the joiner after the Lua luafocusjoin command fires auto_focus_on_join (INV-FS-3)", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// Seed a FocusMembership for the joiner so AutoFocusOnJoin does not
		// fail with FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT. The `scene join`
		// binary command does both join+auto-focus internally; the Lua fixture
		// only calls auto_focus_on_join, so the membership must pre-exist.
		// JoinScene adds a FocusMembership{Kind: Scene, TargetID: sceneID}
		// directly to the session store — observationally equivalent to the
		// production JoinFocus path for the membership gate.
		joiner.JoinScene(ctx, sceneID)

		// Run the Lua `luafocusjoin <charID> <sceneID>` command. This fires
		// the test-focus-join plugin's on_command(ctx) handler which calls
		// holomush.auto_focus_on_join(char_id, scene_id) — the Lua hostfunc
		// exercising the gopher-lua VM path into the real focus.Coordinator.
		// On success the coordinator updates the joiner's live Subscribe
		// filter set to include the scene IC stream (mirroring the binary
		// `scene join` path tested in scene_command_join_delivery_test.go).
		err := joiner.SendCommand(ctx, "luafocusjoin "+joiner.CharacterID.String()+" "+sceneID.String())
		Expect(err).NotTo(HaveOccurred(),
			"INV-FS-3: `luafocusjoin` must succeed — the Lua hostfunc should "+
				"reach the focus coordinator and open the scene IC subscription")

		// Emit a real sensitive scene_pose into the scene AFTER the focus join
		// so the only delivery path is the subscription auto_focus_on_join
		// opened via the Lua VM. If the Lua runtime path does not wire the
		// subscription, WaitForEvent will time out.
		const poseJSON = `{"text":"lua focus parity pose for INV-FS-3"}`
		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			owner.CharacterID, "scene_pose", poseJSON)
		Expect(emitted.SubjectStr).To(ContainSubstring(sceneID.String()),
			"emitted subject must carry the bare scene ULID")

		// Authoritative delivery assertion (INV-FS-3): the joiner's live
		// Subscribe stream receives a scene_pose frame. Pre-fix (without the
		// Lua hostfunc or coordinator wiring) this would time out because no
		// subscription was ever added. Post-fix the frame arrives once the
		// Lua auto_focus_on_join call succeeds and registers the subscription.
		frame := joiner.WaitForEvent(ctx, "scene_pose")
		Expect(frame).NotTo(BeNil(),
			"INV-FS-3: post-join pose MUST be delivered via the Lua-runtime "+
				"auto_focus_on_join path — the scene IC subscription must be "+
				"wired into the joiner's live Subscribe filter set")
		Expect(frame.GetType()).To(Equal("scene_pose"))
		// The joiner is a non-DEK-participant, so the delivered frame MUST be
		// metadata-only (no plaintext payload). Mirrors the binary assertion in
		// scene_command_join_delivery_test.go: fail-closed, no plaintext leak.
		Expect(frame.GetMetadataOnly()).To(BeTrue(),
			"INV-FS-3: non-DEK-participant joiner MUST receive a metadata-only "+
				"frame (fail-closed: no plaintext payload leak)")
	})
})
