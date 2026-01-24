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

var _ = Describe("SceneRepository", func() {
	var ctx context.Context
	var scene *world.Location
	var charID1, charID2 ulid.ULID

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)

		scene = createTestLocation("RP Scene", "A roleplay scene.", world.LocationTypeScene)
		Expect(env.Locations.Create(ctx, scene)).To(Succeed())

		charID1 = createTestCharacterID()
		charID2 = createTestCharacterID()
	})

	Describe("AddParticipant", func() {
		It("adds character to scene with role", func() {
			err := env.Scenes.AddParticipant(ctx, scene.ID, charID1, world.RoleOwner)
			Expect(err).NotTo(HaveOccurred())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(HaveLen(1))
			Expect(participants[0].CharacterID).To(Equal(charID1))
			Expect(participants[0].Role).To(Equal(world.RoleOwner))
		})

		It("updates role if already participant", func() {
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, world.RoleMember)).To(Succeed())
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, world.RoleOwner)).To(Succeed())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(HaveLen(1))
			Expect(participants[0].Role).To(Equal(world.RoleOwner))
		})
	})

	Describe("RemoveParticipant", func() {
		It("removes character from scene", func() {
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, "member")).To(Succeed())

			err := env.Scenes.RemoveParticipant(ctx, scene.ID, charID1)
			Expect(err).NotTo(HaveOccurred())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(BeEmpty())
		})
	})

	Describe("ListParticipants", func() {
		It("returns all participants with roles", func() {
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, "owner")).To(Succeed())
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID2, "member")).To(Succeed())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(HaveLen(2))
		})
	})

	Describe("GetScenesFor", func() {
		It("returns scenes character participates in", func() {
			scene2 := createTestLocation("Second Scene", "Another scene.", world.LocationTypeScene)
			Expect(env.Locations.Create(ctx, scene2)).To(Succeed())

			Expect(env.Scenes.AddParticipant(ctx, scene.ID, charID1, "owner")).To(Succeed())
			Expect(env.Scenes.AddParticipant(ctx, scene2.ID, charID1, "member")).To(Succeed())

			scenes, err := env.Scenes.GetScenesFor(ctx, charID1)
			Expect(err).NotTo(HaveOccurred())
			Expect(scenes).To(HaveLen(2))
		})
	})
})
