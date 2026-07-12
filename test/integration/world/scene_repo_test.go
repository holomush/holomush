// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/world"
)

// The world-layer scene-participant WRITE surface (AddParticipant/RemoveParticipant)
// was removed in 05-14 (D-07): it had no production caller. The READ methods
// (ListParticipants/GetScenesFor) still SELECT/JOIN the kept public.scene_participants
// table, so these specs seed the table directly via seedSceneParticipant.
var _ = Describe("SceneRepository", func() {
	var ctx context.Context
	var scene *world.Location
	var charID1, charID2 ulid.ULID

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)

		scene = createTestLocation("RP Scene", "A roleplay scene.", world.LocationTypeScene)
		Expect(delErr(env.Locations.Create(ctx, scene))).To(Succeed())

		charID1 = createTestCharacterID()
		charID2 = createTestCharacterID()
	})

	Describe("ListParticipants", func() {
		It("returns all participants with roles", func() {
			Expect(seedSceneParticipant(ctx, env.pool, scene.ID, charID1, world.RoleOwner)).To(Succeed())
			Expect(seedSceneParticipant(ctx, env.pool, scene.ID, charID2, world.RoleMember)).To(Succeed())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(HaveLen(2))
		})
	})

	Describe("GetScenesFor", func() {
		It("returns scenes character participates in", func() {
			scene2 := createTestLocation("Second Scene", "Another scene.", world.LocationTypeScene)
			Expect(delErr(env.Locations.Create(ctx, scene2))).To(Succeed())

			Expect(seedSceneParticipant(ctx, env.pool, scene.ID, charID1, world.RoleOwner)).To(Succeed())
			Expect(seedSceneParticipant(ctx, env.pool, scene2.ID, charID1, world.RoleMember)).To(Succeed())

			scenes, err := env.Scenes.GetScenesFor(ctx, charID1)
			Expect(err).NotTo(HaveOccurred())
			Expect(scenes).To(HaveLen(2))
		})
	})
})
