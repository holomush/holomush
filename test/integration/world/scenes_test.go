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

var _ = Describe("Scene Management", func() {
	var ctx context.Context
	var ownerID ulid.ULID

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)
		ownerID = createTestCharacterID()
	})

	Describe("Creating scenes", func() {
		It("creates scene with type 'scene'", func() {
			scene := createTestLocation("My Scene", "A private scene.", world.LocationTypeScene)
			Expect(env.Locations.Create(ctx, scene)).To(Succeed())

			got, err := env.Locations.Get(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Type).To(Equal(world.LocationTypeScene))
		})

		It("sets default replay policy to 'last:-1' (full history)", func() {
			scene := createTestLocation("RP Scene", "For roleplay.", world.LocationTypeScene)
			Expect(env.Locations.Create(ctx, scene)).To(Succeed())

			got, err := env.Locations.Get(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ReplayPolicy).To(Equal("last:-1"))
		})

		It("assigns creator as scene owner", func() {
			scene := createTestLocation("Owned Scene", "Has an owner.", world.LocationTypeScene)
			scene.OwnerID = &ownerID
			Expect(env.Locations.Create(ctx, scene)).To(Succeed())

			got, err := env.Locations.Get(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).NotTo(BeNil())
			Expect(*got.OwnerID).To(Equal(ownerID))
		})
	})

	Describe("Scene shadowing", func() {
		var tavern *world.Location

		BeforeEach(func() {
			tavern = createTestLocation("The Tavern", "A cozy tavern with a roaring fire.", world.LocationTypePersistent)
			Expect(env.Locations.Create(ctx, tavern)).To(Succeed())
		})

		Context("when shadowing a persistent location", func() {
			It("inherits name from parent when scene name is empty", func() {
				scene := createTestLocation("", "", world.LocationTypeScene)
				scene.ShadowsID = &tavern.ID
				Expect(env.Locations.Create(ctx, scene)).To(Succeed())

				got, err := env.Locations.Get(ctx, scene.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.ShadowsID).NotTo(BeNil())

				// Get effective name from parent
				effectiveName := got.EffectiveName(tavern)
				Expect(effectiveName).To(Equal("The Tavern"))
			})

			It("inherits description from parent when empty", func() {
				scene := createTestLocation("", "", world.LocationTypeScene)
				scene.ShadowsID = &tavern.ID
				Expect(env.Locations.Create(ctx, scene)).To(Succeed())

				got, err := env.Locations.Get(ctx, scene.ID)
				Expect(err).NotTo(HaveOccurred())

				// Get effective description from parent
				effectiveDesc := got.EffectiveDescription(tavern)
				Expect(effectiveDesc).To(Equal("A cozy tavern with a roaring fire."))
			})

			It("uses own name/description when provided (override)", func() {
				scene := createTestLocation("Private Room", "The back room of the tavern.", world.LocationTypeScene)
				scene.ShadowsID = &tavern.ID
				Expect(env.Locations.Create(ctx, scene)).To(Succeed())

				got, err := env.Locations.Get(ctx, scene.ID)
				Expect(err).NotTo(HaveOccurred())

				effectiveName := got.EffectiveName(tavern)
				Expect(effectiveName).To(Equal("Private Room"))

				effectiveDesc := got.EffectiveDescription(tavern)
				Expect(effectiveDesc).To(Equal("The back room of the tavern."))
			})
		})

		It("lists all scenes shadowing a location via GetShadowedBy", func() {
			scene1 := createTestLocation("Scene 1", "", world.LocationTypeScene)
			scene1.ShadowsID = &tavern.ID
			Expect(env.Locations.Create(ctx, scene1)).To(Succeed())

			scene2 := createTestLocation("Scene 2", "", world.LocationTypeScene)
			scene2.ShadowsID = &tavern.ID
			Expect(env.Locations.Create(ctx, scene2)).To(Succeed())

			shadows, err := env.Locations.GetShadowedBy(ctx, tavern.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(shadows).To(HaveLen(2))
		})
	})

	Describe("Scene participants", func() {
		var scene *world.Location
		var char1, char2 ulid.ULID

		BeforeEach(func() {
			scene = createTestLocation("RP Scene", "A scene.", world.LocationTypeScene)
			Expect(env.Locations.Create(ctx, scene)).To(Succeed())
			char1 = createTestCharacterID()
			char2 = createTestCharacterID()
		})

		It("adds participant with 'member' role by default", func() {
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "member")).To(Succeed())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(HaveLen(1))
			Expect(participants[0].Role).To(Equal("member"))
		})

		It("supports 'owner', 'member', 'invited' roles", func() {
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "owner")).To(Succeed())
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, char2, "invited")).To(Succeed())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(HaveLen(2))
		})

		It("removes participant from scene", func() {
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "member")).To(Succeed())
			Expect(env.Scenes.RemoveParticipant(ctx, scene.ID, char1)).To(Succeed())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(BeEmpty())
		})

		It("lists all participants in a scene", func() {
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "owner")).To(Succeed())
			Expect(env.Scenes.AddParticipant(ctx, scene.ID, char2, "member")).To(Succeed())

			participants, err := env.Scenes.ListParticipants(ctx, scene.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(participants).To(HaveLen(2))
		})

		It("lists all scenes a character participates in", func() {
			scene2 := createTestLocation("Scene 2", "Another scene.", world.LocationTypeScene)
			Expect(env.Locations.Create(ctx, scene2)).To(Succeed())

			Expect(env.Scenes.AddParticipant(ctx, scene.ID, char1, "owner")).To(Succeed())
			Expect(env.Scenes.AddParticipant(ctx, scene2.ID, char1, "member")).To(Succeed())

			scenes, err := env.Scenes.GetScenesFor(ctx, char1)
			Expect(err).NotTo(HaveOccurred())
			Expect(scenes).To(HaveLen(2))
		})
	})
})
