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

var _ = Describe("Object Handling", func() {
	var ctx context.Context
	var room *world.Location
	var charID ulid.ULID

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)

		room = createTestLocation("Item Room", "For object tests.", world.LocationTypePersistent)
		Expect(env.Locations.Create(ctx, room)).To(Succeed())

		charID = createTestCharacterID()
	})

	Describe("Picking up objects", func() {
		It("moves object from location to character inventory", func() {
			obj := createTestObject("Sword", "A sharp blade.", world.Containment{LocationID: &room.ID})
			Expect(env.Objects.Create(ctx, obj)).To(Succeed())

			err := env.Objects.Move(ctx, obj.ID, world.Containment{CharacterID: &charID})
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Objects.Get(ctx, obj.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.LocationID).To(BeNil())
			Expect(got.HeldByCharacterID).NotTo(BeNil())
			Expect(*got.HeldByCharacterID).To(Equal(charID))
		})
	})

	Describe("Dropping objects", func() {
		It("moves object from character inventory to location", func() {
			obj := createTestObject("Shield", "A sturdy shield.", world.Containment{CharacterID: &charID})
			Expect(env.Objects.Create(ctx, obj)).To(Succeed())

			err := env.Objects.Move(ctx, obj.ID, world.Containment{LocationID: &room.ID})
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Objects.Get(ctx, obj.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.HeldByCharacterID).To(BeNil())
			Expect(got.LocationID).NotTo(BeNil())
			Expect(*got.LocationID).To(Equal(room.ID))
		})
	})

	Describe("Container operations", func() {
		var container *world.Object

		BeforeEach(func() {
			container = createTestObject("Backpack", "A leather backpack.", world.Containment{LocationID: &room.ID})
			container.IsContainer = true
			Expect(env.Objects.Create(ctx, container)).To(Succeed())
		})

		Context("putting objects in containers", func() {
			It("moves object into container object", func() {
				item := createTestObject("Gem", "A sparkling gem.", world.Containment{LocationID: &room.ID})
				Expect(env.Objects.Create(ctx, item)).To(Succeed())

				err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID})
				Expect(err).NotTo(HaveOccurred())

				got, err := env.Objects.Get(ctx, item.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.ContainedInObjectID).NotTo(BeNil())
				Expect(*got.ContainedInObjectID).To(Equal(container.ID))
			})

			It("fails when target is not a container", func() {
				nonContainer := createTestObject("Rock", "Just a rock.", world.Containment{LocationID: &room.ID})
				nonContainer.IsContainer = false
				Expect(env.Objects.Create(ctx, nonContainer)).To(Succeed())

				item := createTestObject("Pebble", "A tiny pebble.", world.Containment{LocationID: &room.ID})
				Expect(env.Objects.Create(ctx, item)).To(Succeed())

				err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &nonContainer.ID})
				Expect(err).To(HaveOccurred())
			})

			It("fails when exceeding max nesting depth (3)", func() {
				// Create 3-level nesting
				level1 := container
				level2 := createTestObject("Pouch", "A small pouch.", world.Containment{ObjectID: &level1.ID})
				level2.IsContainer = true
				Expect(env.Objects.Create(ctx, level2)).To(Succeed())

				level3 := createTestObject("Wallet", "A tiny wallet.", world.Containment{ObjectID: &level2.ID})
				level3.IsContainer = true
				Expect(env.Objects.Create(ctx, level3)).To(Succeed())

				// Try to add to level3 (would be depth 4)
				tooDeep := createTestObject("Coin", "A gold coin.", world.Containment{LocationID: &room.ID})
				Expect(env.Objects.Create(ctx, tooDeep)).To(Succeed())

				err := env.Objects.Move(ctx, tooDeep.ID, world.Containment{ObjectID: &level3.ID})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("nesting depth"))
			})
		})

		Context("taking objects from containers", func() {
			It("moves object from container to character", func() {
				item := createTestObject("Ring", "A gold ring.", world.Containment{ObjectID: &container.ID})
				Expect(env.Objects.Create(ctx, item)).To(Succeed())

				err := env.Objects.Move(ctx, item.ID, world.Containment{CharacterID: &charID})
				Expect(err).NotTo(HaveOccurred())

				got, err := env.Objects.Get(ctx, item.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.ContainedInObjectID).To(BeNil())
				Expect(got.HeldByCharacterID).NotTo(BeNil())
			})
		})

		Context("circular containment prevention", func() {
			It("prevents putting container inside itself", func() {
				err := env.Objects.Move(ctx, container.ID, world.Containment{ObjectID: &container.ID})
				Expect(err).To(HaveOccurred())
			})

			It("prevents A->B->A circular chains", func() {
				containerB := createTestObject("Box B", "Another container.", world.Containment{ObjectID: &container.ID})
				containerB.IsContainer = true
				Expect(env.Objects.Create(ctx, containerB)).To(Succeed())

				// Try to put container (A) inside containerB (which is already in A)
				err := env.Objects.Move(ctx, container.ID, world.Containment{ObjectID: &containerB.ID})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("circular"))
			})

			It("prevents deep circular chains A->B->C->A", func() {
				containerB := createTestObject("Box B", "Container B.", world.Containment{ObjectID: &container.ID})
				containerB.IsContainer = true
				Expect(env.Objects.Create(ctx, containerB)).To(Succeed())

				containerC := createTestObject("Box C", "Container C.", world.Containment{ObjectID: &containerB.ID})
				containerC.IsContainer = true
				Expect(env.Objects.Create(ctx, containerC)).To(Succeed())

				// Try to put container (A) inside containerC (A->B->C, trying to make C->A)
				err := env.Objects.Move(ctx, container.ID, world.Containment{ObjectID: &containerC.ID})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("circular"))
			})
		})
	})

	Describe("Containment invariants", func() {
		It("ensures object is in exactly one place", func() {
			obj := createTestObject("Unique", "Can only be one place.", world.Containment{LocationID: &room.ID})
			Expect(env.Objects.Create(ctx, obj)).To(Succeed())

			// Move to character
			err := env.Objects.Move(ctx, obj.ID, world.Containment{CharacterID: &charID})
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Objects.Get(ctx, obj.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.LocationID).To(BeNil())
			Expect(got.HeldByCharacterID).NotTo(BeNil())
			Expect(got.ContainedInObjectID).To(BeNil())
		})

		It("clears previous containment when moving", func() {
			container := createTestObject("Chest", "A wooden chest.", world.Containment{LocationID: &room.ID})
			container.IsContainer = true
			Expect(env.Objects.Create(ctx, container)).To(Succeed())

			obj := createTestObject("Jewel", "A precious jewel.", world.Containment{ObjectID: &container.ID})
			Expect(env.Objects.Create(ctx, obj)).To(Succeed())

			// Move from container to room
			err := env.Objects.Move(ctx, obj.ID, world.Containment{LocationID: &room.ID})
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Objects.Get(ctx, obj.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ContainedInObjectID).To(BeNil())
			Expect(got.LocationID).NotTo(BeNil())
		})
	})
})
