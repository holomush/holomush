// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// Verifies: INV-SCENE-62
//
// The web live-tally update path depends on scene_publish_* events arriving on
// a participant's event stream so the client (workspaceStore.ingestEvent →
// publishStore) can refetch. This pins that the vote-cast notice is actually
// delivered as an event frame carrying its type, role-agnostically to
// FocusMembership holders, with the scene id on the subject (the routing key
// translate.go stamps into GameEvent.metadata.scene_id for the web client).
var _ = Describe("scene_publish_vote_cast is delivered to a focused scene subscriber", func() {
	var (
		ts         *integrationtest.Server
		ctx        context.Context
		alice, bob *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(suiteT, integrationtest.WithInTreePlugins(), integrationtest.WithPluginCrypto(), integrationtest.WithFocusDelivery())
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

	It("delivers the vote_cast frame (type + scene subject) to a joined participant", func() {
		sceneID := alice.CreateScene(ctx, ts.NewLocation(ctx))
		sceneRef := sceneID.String()

		// Invite Bob and have him join via the real command path. This adds Bob
		// to scene_participants (as 'member') AND calls JoinFocus + AutoFocusOnJoin
		// to wire events.<gameID>.scene.<sceneID>.ic into his live Subscribe filter.
		Expect(alice.SendCommand(ctx, "scene invite "+sceneRef+" "+bob.CharacterID.String())).To(Succeed())
		Expect(bob.SendCommand(ctx, "scene join "+sceneRef)).To(Succeed())

		// End the scene via the command path. handleEnd first calls the plugin's
		// EndScene service (transitions scene to 'ended' state in DB), then calls
		// LeaveFocusByTarget (internal/grpc/focus/leave.go) which sweeps ALL
		// sessions holding a FocusMembership for this scene, removes the JSONB
		// entry, and sends a REMOVE stream update for events.<gameID>.scene.<id>.ic
		// to each subscriber's control channel — unsubscribing Bob before the
		// publish phase.
		//
		// We cannot use ts.SceneServiceClient().EndScene() directly here because
		// the plugin's EndScene handler calls p.hostService.Evaluate(...) which
		// requires a host-issued dispatch token (set only when a command is
		// dispatched through the plugin host's HandleCommand machinery). Direct
		// gRPC calls to the plugin service lack that token and fail with
		// "plugin called host capability without a host-issued dispatch token".
		Expect(alice.SendCommand(ctx, "scene end "+sceneRef)).To(Succeed())

		// Re-add Bob's FocusMembership via the store shortcut. LeaveFocusByTarget
		// removed it from Bob's session JSONB; JoinScene writes it back directly,
		// without touching scene_participants (Bob remains a DB member). This is
		// the same pattern as scene_activity_badge_test.go (lines 70-78) which
		// uses JoinScene + transport cycle to seed memberships for tests that care
		// about event delivery, not join mechanics.
		bob.JoinScene(ctx, sceneID)

		// Cycle Bob's transport so the fresh Subscribe RPC's initial RestoreFocus
		// call re-assembles his JetStream consumer filter from the current JSONB
		// (which now includes the scene membership). Without this, the existing
		// Subscribe goroutine still has the REMOVE-updated filter with no
		// scene.ic entry, and vote_cast never arrives.
		bob.DetachTransport(ctx)
		bob.ReattachTransport(ctx)

		// Set Bob's connection FocusKey to the scene so dispatchDelivery
		// forwards scene events as full EventFrames rather than downgrading them
		// to CONTROL_SIGNAL_SCENE_ACTIVITY badges.
		//
		// dispatchDelivery (internal/grpc/server.go:1287-1316) reads
		// GetConnection().FocusKey for every event whose subject matches
		// isSceneStream. If the connection's FocusKey is nil or points to a
		// different scene, the event is replaced with a SCENE_ACTIVITY badge
		// and the full frame is discarded — this fires for ALL scene events,
		// regardless of sensitivity, before the DEK gate. Setting FocusKey
		// here is the same pattern as scene_activity_badge_test.go SetSceneFocus
		// (line 82). FocusMembership (JoinScene above) must be present first.
		bob.SetSceneFocus(ctx, sceneID)

		// Start the publish attempt. StartScenePublish requires 'ended' state and
		// checks IsParticipant (DB store check, not ABAC engine — INV-SCENE-33)
		// so it works with a direct RPC. Alice is the owner → IsParticipant true.
		startResp, err := ts.SceneServiceClient().StartScenePublish(ctx, &scenev1.StartScenePublishRequest{
			SceneId:           sceneRef,
			CallerCharacterId: alice.CharacterID.String(),
		})
		Expect(err).NotTo(HaveOccurred(), "StartScenePublish must succeed")
		Expect(startResp.GetPublishedSceneId()).NotTo(BeEmpty(), "StartScenePublish must return a published_scene_id")

		// Alice casts her vote. CastPublishSceneVote also self-gates via
		// IsParticipant (store check, no dispatch token needed). This triggers
		// emitVoteCast in the plugin which publishes
		// core-scenes:scene_publish_vote_cast to
		// events.<gameID>.scene.<sceneID>.ic (plugins/core-scenes/publish_events.go).
		_, err = ts.SceneServiceClient().CastPublishSceneVote(ctx, &scenev1.CastPublishSceneVoteRequest{
			CallerCharacterId: alice.CharacterID.String(),
			PublishedSceneId:  startResp.GetPublishedSceneId(),
			Vote:              true,
		})
		Expect(err).NotTo(HaveOccurred(), "CastPublishSceneVote must succeed")

		// Bob (re-subscribed participant) MUST receive the vote_cast notice as an
		// event frame on his scene stream. This is the frame the web client's
		// workspaceStore.ingestEvent → publishStore path consumes to trigger a
		// live tally refetch.
		frame := bob.WaitForEvent(ctx, "core-scenes:scene_publish_vote_cast")
		Expect(frame).NotTo(BeNil(), "bob must receive the vote_cast frame")
		Expect(frame.GetType()).To(Equal("core-scenes:scene_publish_vote_cast"))
		// The subject carries the scene id (translate.go's sceneIDFromSubject
		// extracts it into GameEvent.metadata.scene_id for the web client).
		Expect(frame.GetStream()).To(ContainSubstring(sceneID.String()))
	})
})
