// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// ── scenes board command integration tests (iokti.14) ─────────────────────────
//
// These tests exercise handleScenesBoard through a real SceneStore+SceneService
// stack (Postgres testcontainer) to verify:
//
//  (a) A non-participant character can browse the open scene board — the board
//      requires no membership gate (browse-open-scenes-board policy permits any
//      authenticated character).
//
//  (b) A persistent PLAYER content.cw_block surfaced via the settings client
//      actually hides matching scenes from the board output (the iokti.13
//      safety end-to-end the review asked for).
//
// The integration test drives the real command path (handleScenesBoard →
// service.ListScenes → store.ListBoard) against a fresh Postgres schema. The
// settings client is a scopedFakeSettingsClient — a real settings-store
// integration would require wiring the full host SDK token machinery, which is
// disproportionate for this scope. The fake captures the per-scope GetSetting
// reads and returns the configured block list, which is sufficient to prove the
// CW-block union path fires end-to-end through the real store layer.

var _ = Describe("scenes board command (iokti.14)", func() {
	var (
		ctx   context.Context
		store *SceneStore
		svc   *SceneServiceImpl
		p     *scenePlugin
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = newTestStore()
		svc = newTestService(GinkgoT(), store)
		svc.SetEventSink(&recordingEventSink{})
		p = &scenePlugin{service: svc, evaluator: allowEvaluator{}}
	})

	Describe("non-participant can browse the open scene board", func() {
		It("returns open scenes to a character with no membership in any of them", func() {
			// Create an open scene with a different owner/no participants.
			mustCreateScene(store, "board-int-scene-1", "char-owner-board", "open")

			resp, err := p.HandleCommand(ctx, pluginsdk.CommandRequest{
				Command:     "scenes",
				Args:        "",
				CharacterID: "char-non-member",
				PlayerID:    "player-non-member",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
			Expect(resp.Output).To(ContainSubstring("board-int-scene-1"),
				"non-participant must see open scenes on the board")
		})
	})

	Describe("persistent player CW block hides matching scenes from the board", func() {
		It("excludes scenes whose CW overlaps the player block list", func() {
			// Create two scenes: one with CW 'violence', one clean.
			mustCreateSceneWithCW(store, "board-int-cw-violent", "char-owner-cw", "open", []string{"violence"})
			mustCreateScene(store, "board-int-cw-clean", "char-owner-cw", "open")

			// Wire a settings client that returns "violence" as a player-scope block.
			svc.settings = &scopedFakeSettingsClient{
				byScope: map[pluginsdk.SettingScope]scopedFakeOutcome{
					pluginsdk.SettingScopePlayer: {values: []string{"violence"}, found: true},
				},
			}

			resp, err := p.HandleCommand(ctx, pluginsdk.CommandRequest{
				Command:     "scenes",
				Args:        "",
				CharacterID: "char-alice",
				PlayerID:    "player-alice",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
			Expect(resp.Output).NotTo(ContainSubstring("board-int-cw-violent"),
				"scene with blocked CW must not appear in board output")
			Expect(resp.Output).To(ContainSubstring("board-int-cw-clean"),
				"scene without blocked CW must appear in board output")
		})
	})

	Describe("hide: arg in command filters board by CW without authentication identity", func() {
		It("excludes scenes matching an explicit hide: arg independent of player settings", func() {
			mustCreateSceneWithCW(store, "board-int-hide-gore", "char-owner-hide", "open", []string{"gore"})
			mustCreateScene(store, "board-int-hide-safe", "char-owner-hide", "open")

			resp, err := p.HandleCommand(ctx, pluginsdk.CommandRequest{
				Command:     "scenes",
				Args:        "hide:gore",
				CharacterID: "char-bob",
				PlayerID:    "player-bob",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Status).To(Equal(pluginsdk.CommandOK))
			Expect(resp.Output).NotTo(ContainSubstring("board-int-hide-gore"),
				"scene with hide:-excluded CW must not appear in output")
			Expect(resp.Output).To(ContainSubstring("board-int-hide-safe"),
				"scene without blocked CW must appear in output")
		})
	})
})

// mustCreateSceneWithCW inserts a minimal scene row with specified content warnings.
func mustCreateSceneWithCW(store *SceneStore, sceneID, ownerID, visibility string, cws []string) *SceneRow {
	GinkgoHelper()
	row := &SceneRow{
		ID:              sceneID,
		Title:           "Test Scene " + sceneID,
		OwnerID:         ownerID,
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      visibility,
		ContentWarnings: cws,
		Tags:            []string{},
	}
	Expect(store.Create(context.Background(), row)).NotTo(HaveOccurred())
	return row
}
