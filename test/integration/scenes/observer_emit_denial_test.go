// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// holomush-5rh.8.4 — emit denial integration pin: a REAL role='observer' row is
// excluded from the write-scene-as-participant code path by the structural gate
// in ListScenesForCharacter (store.go):
//
//	SELECT p.scene_id FROM scene_participants p JOIN scenes s ON s.id = p.scene_id
//	WHERE p.character_id = $1 AND p.role IN ('owner', 'member') AND s.state IN ('active', 'paused')
//
// Because observer rows carry role='observer', resolveSingleSceneMembership
// returns "not in any scene", handleEmit returns a CommandError, and the
// dispatcher routes the denial to the character stream as a command_error event
// (not an RPC failure — user-facing denials arrive via command_response/command_error
// events; HandleCommand returns Success=true; SendCommand returns nil).
//
// This spec asserts the denial semantics at the command-dispatch layer:
//   - observer.SendCommand("scene pose hi") returns nil (user-facing denial,
//     not an RPC error — the denial text arrives as a command_error event).
//   - The command_error event payload contains "not currently in any scene",
//     confirming the membership-gate fired rather than ABAC or any other path.
//   - alice.SendCommand("scene pose hi") succeeds (she has role='owner').
//
// The observer row is seeded directly via SQL into plugin_core_scenes.scene_participants
// with role='observer'. This is a structural-exclusion pin: it creates exactly the
// row shape production AddObserver writes without requiring WatchScene (which
// needs a focus client via WithFocusDelivery).
//
// WithRealABAC is intentionally NOT used: the "not in any scene" denial fires
// at resolveSingleSceneMembership (code-level, before ABAC evaluation), so no
// real policy engine is needed.
//
// The membership-gate SQL is also pinned at the unit level by:
//   - TestResolveResourceExcludesObserverFromParticipantsAttribute (resolver_test.go)
//   - TestComputeHasNoRoleAwarenessSoStoreFilterIsLoadBearing (poseorder_test.go)
//   - TestObserverIsNotSeededIntoVoteRosterAndCannotCastVote (publish_vote_tally_test.go)
//
// Verifies: INV-SCENE-61
var _ = Describe("holomush-5rh.8.4: observer denied emit by write-scene-as-participant structural gate", func() {
	var (
		ts       *integrationtest.Server
		ctx      context.Context
		alice    *integrationtest.Session // scene owner (role='owner')
		observer *integrationtest.Session // character with a REAL role='observer' row
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
		DeferCleanup(cancel)

		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
		)
		alice = ts.ConnectAuthed(ctx, "Alice")
		observer = ts.ConnectAuthed(ctx, "Observer")
	})

	AfterEach(func() {
		if observer != nil {
			observer.Logout(ctx)
		}
		if alice != nil {
			alice.Logout(ctx)
		}
		ts.Stop()
	})

	It("observer (role='observer' row) receives 'not in any scene' denial on scene pose; owner succeeds", func() {
		loc := ts.NewLocation(ctx)

		// Alice creates the scene (she gets role='owner' via CreateWithOwner).
		sceneID := alice.CreateScene(ctx, loc)
		sceneRef := sceneID.String()

		// Insert a REAL role='observer' row for the observer character directly
		// via SQL. This is the canonical approach for a structural-exclusion pin:
		// it creates exactly the row shape the production AddObserver writes
		// (scene_participants.role = 'observer'), without going through WatchScene
		// (which requires a focus client via WithFocusDelivery).
		//
		// The table lives in the plugin_core_scenes schema (search_path set by
		// the binary plugin's ServiceConfig.ConnectionString — see main.go).
		// joined_at is stored as epoch-nanoseconds BIGINT (pgnanos.Time),
		// matching the production AddObserver INSERT shape and the
		// mustAddParticipant helper in store_integration_test.go.
		_, insertErr := ts.Pool().Exec(
			ctx,
			`INSERT INTO plugin_core_scenes.scene_participants (scene_id, character_id, role, joined_at)
			 VALUES ($1, $2, 'observer', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
			 ON CONFLICT (scene_id, character_id) DO NOTHING`,
			sceneRef, observer.CharacterID.String(),
		)
		Expect(insertErr).NotTo(HaveOccurred(),
			"direct SQL insert must succeed — seeds the real observer row in plugin_core_scenes.scene_participants")

		// The membership-gate denial assertion:
		//
		// observer.SendCommand returns nil — user-facing denials (pluginsdk.CommandError)
		// are routed as command_error events on the character stream, NOT as
		// RPC failures (HandleCommand returns Success=true; SendCommand returns nil).
		// See server.go:executeViaDispatcher, isUserFacingError, and
		// the publish_e2e_test.go "INV-P6-E8-2" comment for the established idiom.
		Expect(observer.SendCommand(ctx, "scene pose hi")).To(Succeed(),
			"user-facing denial MUST arrive as a command_error event, not an RPC error — "+
				"SendCommand must return nil even when the command is denied")

		// Authoritative denial assertion: the observer's character stream must
		// contain a command_error event whose payload text says "not currently in
		// any scene". This is the user-facing string produced by
		// resolveSingleSceneMembership when ListScenesForCharacter returns empty
		// (observer row excluded by AND p.role IN ('owner', 'member')).
		denialFrame := observer.WaitForEvent(ctx, string(core.EventTypeCommandError))
		var crp core.CommandResponsePayload
		Expect(json.Unmarshal(denialFrame.GetPayload(), &crp)).To(Succeed(),
			"command_error payload must unmarshal as CommandResponsePayload")
		Expect(crp.Text).To(ContainSubstring("not currently in any scene"),
			"denial text MUST confirm the membership-gate fired (role='observer' excluded by "+
				"ListScenesForCharacter's AND p.role IN ('owner', 'member') filter)")

		// Positive control: alice (role='owner') can pose successfully.
		// SendCommand returning nil proves nothing on its own (denials also
		// return nil — see above), so assert the pose actually landed: a
		// scene_pose row appears in the plugin's scene_log audit table for
		// this scene. The audit projection is async, hence Eventually.
		Expect(alice.SendCommand(ctx, "scene pose hi")).To(Succeed())
		Eventually(func() int {
			var n int
			if err := ts.Pool().QueryRow(
				ctx,
				`SELECT COUNT(*) FROM plugin_core_scenes.scene_log
				 WHERE subject LIKE 'events.%.scene.' || $1 || '.ic'
				   AND type = 'core-scenes:scene_pose'`,
				sceneRef,
			).Scan(&n); err != nil {
				return 0
			}
			return n
		}).Should(BeNumerically(">", 0),
			"owner (role IN ('owner','member')) MUST be allowed to emit a scene pose — "+
				"a scene_pose row in scene_log confirms the gate is role-selective, not scene-wide")
	})
})
