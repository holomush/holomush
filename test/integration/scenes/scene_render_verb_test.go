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

// Regression spec (holomush-r0kup): core-scenes IC content resolves in the verb
// registry from its OWN manifest verbs: block — the test-harness scene-verb
// seeding (registerSceneEmitVerbs) is gone. So the production
// RenderingPublisher-backed plugin emitter does not reject scene emits with
// EMIT_UNKNOWN_VERB.
//
// ROOT CAUSE (pre-fix): core-scenes shipped NO verbs: block. Binary plugins
// register rendering verbs ONLY from the manifest verbs: block
// (manager.go:1138); core-scenes' types lived only in crypto.emits +
// RegisterEmitTypes (which feed the INV-PLUGIN-32 set-equality check, NOT the
// verb registry). RenderingPublisher.Publish hard-fails EMIT_UNKNOWN_VERB
// (internal/eventbus/rendering_publisher.go) for any type missing from the verb
// registry. In production every scene emit failed; it only appeared to work in
// tests because the harness explicitly seeded scene verbs.
//
// FIX: core-scenes ships a verbs: block of qualified core-scenes:<verb> types,
// and the harness emit helpers qualify the wire type (mirroring the real
// plugin) so they resolve against the manifest-sourced registry. The seeding is
// removed; the SAME verbRegistry the plugin loader populates from manifests
// (harness.go) backs the RenderingPublisher here (WithPluginCrypto REQUIRES
// WithInTreePlugins).
//
// EmitSceneICContent require.NoErrors the publish internally, so an
// EMIT_UNKNOWN_VERB would fail this spec. A non-empty returned subject proves
// the qualified wire type resolved against a manifest-sourced verb.
var _ = Describe("holomush-r0kup: core-scenes renders via its own manifest verbs", func() {
	var (
		ts    *integrationtest.Server
		ctx   context.Context
		owner *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		// WithPluginCrypto wires the RenderingPublisher-backed plugin emitter and
		// REQUIRES WithInTreePlugins, which loads core-scenes and registers its
		// manifest verbs into the shared verb registry — no seeding.
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
		)
		owner = ts.ConnectAuthed(ctx, "Owner")
	})

	AfterEach(func() {
		if owner != nil {
			owner.Logout(ctx)
		}
		ts.Stop()
	})

	It("publishes an IC pose without EMIT_UNKNOWN_VERB using manifest-sourced verbs", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// Pass the BARE verb — the harness helper qualifies it to
		// core-scenes:scene_pose (mirroring the real plugin). Without the
		// manifest verbs: block the manifest-sourced registry would have no
		// entry, RenderingPublisher would reject with EMIT_UNKNOWN_VERB, and
		// EmitSceneICContent's internal require.NoError would fail the spec.
		const poseJSON = `{"text":"r0kup regression: manifest-sourced scene verb"}`
		emitted := ts.EmitSceneICContent(ctx, "core-scenes", sceneID,
			owner.CharacterID, "scene_pose", poseJSON)
		Expect(emitted.SubjectStr).NotTo(BeEmpty(),
			"core-scenes:scene_pose MUST resolve in the manifest verb registry and publish")
		Expect(emitted.SubjectStr).To(ContainSubstring(sceneID.String()),
			"emitted subject must carry the bare scene ULID")
	})
})
