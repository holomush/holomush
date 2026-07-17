// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package scenes_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// holomush-sjtlz: `scene info` metadata-read access, end to end under real ABAC.
//
// The vestigial unconditional seed:player-scene-read was removed from the host
// seed corpus (read twin of holomush-8m01u), so the `scene info` gate
// (plugins/core-scenes/commands.go `gated("info", "read", …)`) is governed
// solely by the plugin's three read policies
// (plugins/core-scenes/plugin.yaml): read-scene-as-participant,
// read-scene-as-invitee, and read-open-scene. Net contract (INV-SCENE-68):
// `scene info` denies only for a private scene the caller is neither a
// participant of nor invited to.
//
// WithRealABAC is mandatory: under the harness's allow-all default the gate is
// a no-op. The plugin policies evaluate against the REAL SceneResolver
// attributes (participants / invitees / visibility).
//
// The harness's Session.CreateScene helper creates visibility="open" scenes;
// the private cases below call CreateScene on the SceneService client directly
// with visibility="private".
var _ = Describe("holomush-sjtlz: scene info metadata-read access under real ABAC", func() {
	var (
		ts       *integrationtest.Server
		ctx      context.Context
		owner    *integrationtest.Session
		outsider *integrationtest.Session
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Second)
		DeferCleanup(cancel)
		ts = integrationtest.Start(
			suiteT,
			integrationtest.WithInTreePlugins(),
			integrationtest.WithPluginCrypto(),
			integrationtest.WithRealABAC(),
		)
		owner = ts.ConnectAuthed(ctx, "Owner")
		outsider = ts.ConnectAuthed(ctx, "Outsider")
	})

	AfterEach(func() {
		if outsider != nil {
			outsider.Logout(ctx)
		}
		if owner != nil {
			owner.Logout(ctx)
		}
		ts.Stop()
	})

	// createPrivateScene mirrors integrationtest.Session.CreateScene but with
	// visibility="private" (the helper hardcodes "open").
	createPrivateScene := func(s *integrationtest.Session, locationID ulid.ULID) ulid.ULID {
		resp, err := ts.SceneServiceClient().CreateScene(ctx, &scenev1.CreateSceneRequest{
			CharacterId: s.CharacterID.String(),
			Title:       "test scene",
			LocationId:  locationID.String(),
			Visibility:  "private",
		})
		Expect(err).NotTo(HaveOccurred(), "CreateScene(private)")
		id, err := ulid.Parse(resp.GetScene().GetId())
		Expect(err).NotTo(HaveOccurred(), "parse scene id")
		return id
	}

	// commandText unmarshals a command_response/command_error frame's payload.
	commandText := func(payload []byte) string {
		var crp eventvocab.CommandResponsePayload
		Expect(json.Unmarshal(payload, &crp)).To(Succeed(),
			"command event payload must unmarshal as CommandResponsePayload")
		return crp.Text
	}

	// Verifies: INV-SCENE-68
	It("denies scene info to a non-participant of a private scene", func() {
		loc := ts.NewLocation(ctx)
		sceneID := createPrivateScene(owner, loc)

		// Outsider is neither a participant nor an invitee. None of the three
		// read policies match (not in participants, not in invitees, visibility
		// != open), so the engine default-denies and the gated subcommand
		// surfaces a user-facing command_error — not scene metadata, and not an
		// RPC failure.
		Expect(outsider.SendCommand(ctx, "scene info #"+sceneID.String())).To(Succeed(),
			"a plugin-level permission denial is a user-facing command_error event, not an RPC failure")

		denialFrame := outsider.WaitForEvent(ctx, string(eventvocab.EventTypeCommandError))
		text := commandText(denialFrame.GetPayload())
		Expect(text).NotTo(ContainSubstring("Scene: test scene"),
			"holomush-sjtlz: a non-participant must NOT receive private scene metadata")
		Expect(text).NotTo(BeEmpty(), "the denial must carry a user-facing reason")
	})

	// Verifies: INV-SCENE-68
	It("permits a participant (the owner) to read a private scene's info", func() {
		loc := ts.NewLocation(ctx)
		sceneID := createPrivateScene(owner, loc)

		// Owner is a participant (role='owner' via CreateWithOwner), so
		// read-scene-as-participant permits — the positive control proving the
		// seed removal did not over-restrict (fail-closed) participant reads.
		Expect(owner.SendCommand(ctx, "scene info #"+sceneID.String())).To(Succeed())

		frame := owner.WaitForEvent(ctx, string(eventvocab.EventTypeCommandResponse))
		Expect(commandText(frame.GetPayload())).To(ContainSubstring("Scene: test scene"),
			"read-scene-as-participant MUST permit a participant's scene info")
	})

	// Verifies: INV-SCENE-68
	It("permits an invitee to read a private scene's info before joining", func() {
		loc := ts.NewLocation(ctx)
		sceneID := createPrivateScene(owner, loc)

		// Owner (a participant; invite-to-scene permits participants on
		// active/paused scenes) invites the outsider THROUGH the command path —
		// a direct SceneServiceClient().InviteToScene call fails Internal under
		// WithRealABAC because the service's evaluator gate needs the plugin
		// dispatch actor context that only command dispatch establishes. The
		// outsider is now in resource.scene.invitees but NOT in participants.
		Expect(owner.SendCommand(ctx,
			"scene invite #"+sceneID.String()+" "+outsider.CharacterID.String())).To(Succeed())
		inviteFrame := owner.WaitForEvent(ctx, string(eventvocab.EventTypeCommandResponse))
		Expect(commandText(inviteFrame.GetPayload())).To(ContainSubstring("Invited"),
			"precondition: the invite must actually land (not a silent command_error)")

		// read-scene-as-invitee permits: an invitee may inspect what they were
		// invited to before accepting.
		Expect(outsider.SendCommand(ctx, "scene info #"+sceneID.String())).To(Succeed())

		frame := outsider.WaitForEvent(ctx, string(eventvocab.EventTypeCommandResponse))
		Expect(commandText(frame.GetPayload())).To(ContainSubstring("Scene: test scene"),
			"read-scene-as-invitee MUST permit an invitee's scene info")
	})

	// Verifies: INV-SCENE-68
	It("permits a non-participant to read an open scene's info", func() {
		loc := ts.NewLocation(ctx)
		// Harness helper creates visibility="open".
		sceneID := owner.CreateScene(ctx, loc)

		// read-open-scene permits: an open scene's content is already
		// spectatable by anyone (spectate-open-scene), so its metadata is
		// coherently public too.
		Expect(outsider.SendCommand(ctx, "scene info #"+sceneID.String())).To(Succeed())

		frame := outsider.WaitForEvent(ctx, string(eventvocab.EventTypeCommandResponse))
		Expect(commandText(frame.GetPayload())).To(ContainSubstring("Scene: test scene"),
			"read-open-scene MUST permit a non-participant's info read of an open scene")
	})
})
