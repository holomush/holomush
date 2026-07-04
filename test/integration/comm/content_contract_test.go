// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package comm_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Slice 1 capstone (holomush-kk1ot.11): drives two REAL broadcasts through the
// full stack — a core-communication location pose and a core-scenes scene
// pose, both invoked with an embedded ";" sigil — and asserts the emitted
// wire payload decodes as holomush.comm.v1.CommunicationContent with
// actor_id/actor_display_name and no_space populated. Both verbs were
// migrated to the shared pkg/plugin/comm builder by kk1ot.9
// (core-communication) and kk1ot.7 (core-scenes); this spec is the
// end-to-end proof that the migration holds through the real command
// dispatcher, not just the plugins' own unit tests.
//
// Both cases drive the sigil embedded directly in the args ("pose ;waves",
// "scene pose ;dances") rather than through the top-level ";"/":" alias
// table: this integration harness loads plugin manifests (WithInTreePlugins)
// but does not refresh the command dispatcher's in-memory AliasCache from
// the alias_seeder's persisted rows, so alias-invoked commands
// (";waves"/":waves") 404 as "Unknown command" here even though the alias
// table works in production (unit-covered in internal/command/alias_test.go
// and internal/command/resolve_test.go). The embedded-sigil form exercises
// the IDENTICAL pkg/plugin/comm.ParsePose fallback branch a top-level alias
// would (ParsePose's non-";"/":" invokedAs branch strips a leading ";"/":"
// from the raw text), so no_space coverage is equivalent either way — this is
// a harness routing gap, not a gap in what this spec proves about the
// content contract.
//
// The translate.go leg (web GameEvent.metadata["no_space"] + GameEvent.actor)
// is intentionally NOT re-asserted here: internal/web.Handler.translateEvent
// is an unexported method on an unexported-construction-surface type, so it
// cannot be called from this external test package. That leg is already
// covered at the unit tier by kk1ot.6 — see
// internal/web/translate_test.go's CommunicationContent-shaped-payload cases
// (actor_display_name/text/no_space, ~line 620-640), which feed a payload
// shaped exactly like the one this spec proves lands on the wire and assert
// GetActor() and GetMetadata().AsMap()["no_space"].
var _ = Describe("holomush-kk1ot: CommunicationContent wire contract", func() {
	var (
		ts    *integrationtest.Server
		ctx   context.Context
		owner *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		// WithPluginCrypto is required even for the non-sensitive location
		// broadcast: WithInTreePlugins alone leaves the plugin event emitter
		// unconfigured (integrationtest.plugins.go), so ANY command that emits
		// an event needs the crypto-wired publisher WithPluginCrypto installs.
		// Sensitivity:never events (core-communication's say/pose/ooc/emit)
		// simply take the identity (non-encrypt) branch of that publisher.
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

	It("emits a core-communication location pose as CommunicationContent with no_space and actor populated", func() {
		// The embedded ";" sigil (no alias involved — see the harness note
		// above) drives pkg/plugin/comm.ParsePose's fallback branch, which
		// strips the leading ";" and sets no_space=true — the same outcome
		// the ";" alias produces in production.
		Expect(owner.SendCommand(ctx, "pose ;waves")).To(Succeed(),
			"`pose ;<text>` must dispatch to core-communication's pose command")

		var payload commv1.CommunicationContent
		Eventually(func(g Gomega) {
			frames, err := owner.QueryStreamHistory(ctx, "location."+owner.LocationID.String())
			g.Expect(err).NotTo(HaveOccurred(), "QueryStreamHistory must not error for the character's own location")
			found := findFrameByType(frames, "core-communication:pose")
			g.Expect(found).NotTo(BeNil(), "the location broadcast pose must be readable back")
			g.Expect(found.GetMetadataOnly()).To(BeFalse(),
				"pose is crypto.emits sensitivity:never — must not be downgraded to metadata-only")
			g.Expect(protojson.Unmarshal(found.GetPayload(), &payload)).To(Succeed(),
				"the wire payload must decode as holomush.comm.v1.CommunicationContent")
		}).Should(Succeed())

		Expect(payload.GetActorId()).To(Equal(owner.CharacterID.String()),
			"actor_id must carry the invoking character's ULID")
		Expect(payload.GetActorDisplayName()).To(Equal("Owner"),
			"actor_display_name must carry the resolved character name")
		Expect(payload.GetText()).To(Equal("waves"))
		Expect(payload.GetNoSpace()).To(BeTrue(),
			"the embedded \";\" sigil must set no_space (semipose form)")
	})

	It("emits a core-scenes scene pose as CommunicationContent with no_space and actor populated", func() {
		loc := ts.NewLocation(ctx)
		sceneID := owner.CreateScene(ctx, loc)
		Expect(sceneID).NotTo(BeZero(), "CreateScene must return a non-zero bare ULID")

		// I-17 membership gate: JoinScene stamps FocusMemberships so
		// QueryStreamHistory's private-stream gate (and the INV-PRIVACY-6 scope
		// floor) admit this session for the scene's IC stream. Must run BEFORE
		// the emit so the floor (JoinedAt) does not filter the event out.
		owner.JoinScene(ctx, sceneID)
		// core-scenes' pose/say/emit/ooc are crypto.emits sensitivity:always
		// (INV-SCENE-3). Mint the scene DEK with owner as the sole participant
		// BEFORE the emit: the publisher's GetOrCreate only seeds the initial
		// participant set on first mint, so this must precede SendCommand for
		// the AuthGuard to decrypt owner's own read-back instead of downgrading
		// to metadata-only.
		ts.SeedSceneDEKParticipant(ctx, sceneID, owner)

		// core-scenes has no per-subcommand alias table (unlike the top-level
		// pose command), so the semipose form always embeds the ";" sigil
		// directly in the args — pkg/plugin/comm.ParsePose's fallback branch
		// strips it and sets no_space=true (mirrors
		// plugins/core-scenes/commands_emit_test.go).
		Expect(owner.SendCommand(ctx, "scene pose ;dances")).To(Succeed(),
			"`scene pose ;<text>` must dispatch to core-scenes' handleEmit")

		stream := "scene." + sceneID.String() + ".ic"
		var payload commv1.CommunicationContent
		Eventually(func(g Gomega) {
			frames, err := owner.QueryStreamHistory(ctx, stream)
			g.Expect(err).NotTo(HaveOccurred(), "QueryStreamHistory must not error for a scene the owner is a member of")
			found := findFrameByType(frames, "core-scenes:scene_pose")
			g.Expect(found).NotTo(BeNil(), "the scene pose must be readable back")
			g.Expect(found.GetMetadataOnly()).To(BeFalse(),
				"owner is a seeded DEK participant — must receive a decrypted (non-metadata-only) frame")
			g.Expect(protojson.Unmarshal(found.GetPayload(), &payload)).To(Succeed(),
				"the wire payload must decode as holomush.comm.v1.CommunicationContent")
		}).Should(Succeed())

		Expect(payload.GetActorId()).To(Equal(owner.CharacterID.String()),
			"actor_id must be preserved for the replay/snapshot Speaker (decodeReplayEntries/decodeSnapshotEntry)")
		Expect(payload.GetActorDisplayName()).To(Equal("Owner"),
			"actor_display_name must carry the resolved character name")
		Expect(payload.GetText()).To(Equal("dances"))
		Expect(payload.GetNoSpace()).To(BeTrue(),
			"the embedded \";\" sigil must set no_space (semipose form)")
	})
})

// findFrameByType returns the last (most recent) frame in frames whose Type
// matches eventType, or nil if none match. History pages are ascending
// (oldest→newest; internal/grpc/query_stream_history.go), so scanning from
// the end returns the newest match.
func findFrameByType(frames []*corev1.EventFrame, eventType string) *corev1.EventFrame {
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].GetType() == eventType {
			return frames[i]
		}
	}
	return nil
}
