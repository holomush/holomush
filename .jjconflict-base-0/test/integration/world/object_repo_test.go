// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/world"
)

var _ = Describe("ObjectRepository", func() {
	var ctx context.Context
	var room *world.Location
	var charID ulid.ULID

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)

		room = createTestLocation("Test Room", "For object tests.", world.LocationTypePersistent)
		Expect(env.Locations.Create(ctx, room)).To(Succeed())

		charID = createTestCharacterID()
	})

	Describe("Create", func() {
		It("creates object with containment in location", func() {
			obj := createTestObject("Sword", "A sharp sword.", world.Containment{LocationID: &room.ID})

			err := env.Objects.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Objects.Get(ctx, obj.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("Sword"))
			Expect(got.LocationID()).NotTo(BeNil())
			Expect(*got.LocationID()).To(Equal(room.ID))
		})

		It("creates object with containment held by character", func() {
			obj := createTestObject("Shield", "A sturdy shield.", world.Containment{CharacterID: &charID})

			err := env.Objects.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Objects.Get(ctx, obj.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.HeldByCharacterID()).NotTo(BeNil())
			Expect(*got.HeldByCharacterID()).To(Equal(charID))
		})
	})

	Describe("Move", func() {
		It("updates containment atomically", func() {
			obj := createTestObject("Gem", "A sparkling gem.", world.Containment{LocationID: &room.ID})
			Expect(env.Objects.Create(ctx, obj)).To(Succeed())

			err := env.Objects.Move(ctx, obj.ID, world.Containment{CharacterID: &charID})
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Objects.Get(ctx, obj.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.LocationID()).To(BeNil())
			Expect(got.HeldByCharacterID()).NotTo(BeNil())
			Expect(*got.HeldByCharacterID()).To(Equal(charID))
		})

		It("validates target is container when moving to object", func() {
			container := createTestObject("Bag", "A leather bag.", world.Containment{LocationID: &room.ID})
			container.IsContainer = false // NOT a container
			Expect(env.Objects.Create(ctx, container)).To(Succeed())

			item := createTestObject("Coin", "A gold coin.", world.Containment{LocationID: &room.ID})
			Expect(env.Objects.Create(ctx, item)).To(Succeed())

			err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, world.ErrInvalidContainment)).To(BeTrue(), "expected ErrInvalidContainment")
		})

		It("allows moving to actual container", func() {
			container := createTestObject("Chest", "A wooden chest.", world.Containment{LocationID: &room.ID})
			container.IsContainer = true
			Expect(env.Objects.Create(ctx, container)).To(Succeed())

			item := createTestObject("Ring", "A gold ring.", world.Containment{LocationID: &room.ID})
			Expect(env.Objects.Create(ctx, item)).To(Succeed())

			err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID})
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Objects.Get(ctx, item.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ContainedInObjectID()).NotTo(BeNil())
			Expect(*got.ContainedInObjectID()).To(Equal(container.ID))
		})

		It("enforces max nesting depth", func() {
			// Create chain: container1 -> container2 -> container3
			c1 := createTestObject("Box 1", "Container 1.", world.Containment{LocationID: &room.ID})
			c1.IsContainer = true
			Expect(env.Objects.Create(ctx, c1)).To(Succeed())

			c2 := createTestObject("Box 2", "Container 2.", world.Containment{ObjectID: &c1.ID})
			c2.IsContainer = true
			Expect(env.Objects.Create(ctx, c2)).To(Succeed())

			c3 := createTestObject("Box 3", "Container 3.", world.Containment{ObjectID: &c2.ID})
			c3.IsContainer = true
			Expect(env.Objects.Create(ctx, c3)).To(Succeed())

			// Try to add item to c3 (would be depth 4)
			item := createTestObject("Pebble", "A small pebble.", world.Containment{LocationID: &room.ID})
			Expect(env.Objects.Create(ctx, item)).To(Succeed())

			err := env.Objects.Move(ctx, item.ID, world.Containment{ObjectID: &c3.ID})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("nesting depth"))
		})

		It("prevents circular containment", func() {
			c1 := createTestObject("Container A", "First container.", world.Containment{LocationID: &room.ID})
			c1.IsContainer = true
			Expect(env.Objects.Create(ctx, c1)).To(Succeed())

			c2 := createTestObject("Container B", "Second container.", world.Containment{ObjectID: &c1.ID})
			c2.IsContainer = true
			Expect(env.Objects.Create(ctx, c2)).To(Succeed())

			// Try to put c1 into c2 (circular: c2 is already in c1)
			err := env.Objects.Move(ctx, c1.ID, world.Containment{ObjectID: &c2.ID})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("circular"))
		})

		It("prevents self-containment", func() {
			container := createTestObject("Self Box", "Tries to contain itself.", world.Containment{LocationID: &room.ID})
			container.IsContainer = true
			Expect(env.Objects.Create(ctx, container)).To(Succeed())

			err := env.Objects.Move(ctx, container.ID, world.Containment{ObjectID: &container.ID})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListAtLocation", func() {
		It("returns objects at location", func() {
			Expect(env.Objects.Create(ctx, createTestObject("Obj1", "First.", world.Containment{LocationID: &room.ID}))).To(Succeed())
			Expect(env.Objects.Create(ctx, createTestObject("Obj2", "Second.", world.Containment{LocationID: &room.ID}))).To(Succeed())

			objects, err := env.Objects.ListAtLocation(ctx, room.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(objects).To(HaveLen(2))
		})

		It("returns empty slice for empty location", func() {
			emptyRoom := createTestLocation("Empty", "Nothing here.", world.LocationTypePersistent)
			Expect(env.Locations.Create(ctx, emptyRoom)).To(Succeed())

			objects, err := env.Objects.ListAtLocation(ctx, emptyRoom.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(objects).To(BeEmpty())
		})
	})

	Describe("ListHeldBy", func() {
		It("returns objects held by character", func() {
			Expect(env.Objects.Create(ctx, createTestObject("Held1", "First held.", world.Containment{CharacterID: &charID}))).To(Succeed())
			Expect(env.Objects.Create(ctx, createTestObject("Held2", "Second held.", world.Containment{CharacterID: &charID}))).To(Succeed())

			objects, err := env.Objects.ListHeldBy(ctx, charID)
			Expect(err).NotTo(HaveOccurred())
			Expect(objects).To(HaveLen(2))
		})
	})

	Describe("ListContainedIn", func() {
		It("returns objects inside container", func() {
			container := createTestObject("Backpack", "A sturdy backpack.", world.Containment{LocationID: &room.ID})
			container.IsContainer = true
			Expect(env.Objects.Create(ctx, container)).To(Succeed())

			Expect(env.Objects.Create(ctx, createTestObject("Item1", "First item.", world.Containment{ObjectID: &container.ID}))).To(Succeed())
			Expect(env.Objects.Create(ctx, createTestObject("Item2", "Second item.", world.Containment{ObjectID: &container.ID}))).To(Succeed())

			objects, err := env.Objects.ListContainedIn(ctx, container.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(objects).To(HaveLen(2))
		})
	})
})
