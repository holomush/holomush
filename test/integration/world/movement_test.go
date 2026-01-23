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

var _ = Describe("Character Movement", func() {
	var ctx context.Context
	var room1, room2 *world.Location

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)

		room1 = createTestLocation("Starting Room", "Where journeys begin.", world.LocationTypePersistent)
		room2 = createTestLocation("Destination", "Where journeys end.", world.LocationTypePersistent)
		Expect(env.Locations.Create(ctx, room1)).To(Succeed())
		Expect(env.Locations.Create(ctx, room2)).To(Succeed())
	})

	Describe("Bidirectional exits", func() {
		It("allows movement in both directions", func() {
			exit := createTestExit(room1.ID, room2.ID, "north")
			exit.Bidirectional = true
			exit.ReturnName = "south"
			Expect(env.Exits.Create(ctx, exit)).To(Succeed())

			// Can find exit going north from room1
			northExit, err := env.Exits.FindByName(ctx, room1.ID, "north")
			Expect(err).NotTo(HaveOccurred())
			Expect(northExit.ToLocationID).To(Equal(room2.ID))

			// Can find exit going south from room2
			southExit, err := env.Exits.FindByName(ctx, room2.ID, "south")
			Expect(err).NotTo(HaveOccurred())
			Expect(southExit.ToLocationID).To(Equal(room1.ID))
		})

		It("uses return_name for the reverse direction", func() {
			exit := createTestExit(room1.ID, room2.ID, "doorway")
			exit.Bidirectional = true
			exit.ReturnName = "back"
			Expect(env.Exits.Create(ctx, exit)).To(Succeed())

			returnExit, err := env.Exits.FindByName(ctx, room2.ID, "back")
			Expect(err).NotTo(HaveOccurred())
			Expect(returnExit.Name).To(Equal("back"))
		})
	})

	Describe("Exit visibility", func() {
		var ownerID, otherID ulid.ULID

		BeforeEach(func() {
			ownerID = createTestCharacterID()
			otherID = createTestCharacterID()
			room1.OwnerID = &ownerID
			Expect(env.Locations.Update(ctx, room1)).To(Succeed())
		})

		Context("when visibility is 'all'", func() {
			It("shows exit to any character", func() {
				exit := createTestExit(room1.ID, room2.ID, "public door")
				exit.Visibility = world.VisibilityAll
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				got, _ := env.Exits.Get(ctx, exit.ID)
				Expect(got.IsVisibleTo(otherID, nil)).To(BeTrue())
				Expect(got.IsVisibleTo(ownerID, nil)).To(BeTrue())
			})
		})

		Context("when visibility is 'owner'", func() {
			It("shows exit only to location owner", func() {
				exit := createTestExit(room1.ID, room2.ID, "owner door")
				exit.Visibility = world.VisibilityOwner
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				got, _ := env.Exits.Get(ctx, exit.ID)
				Expect(got.IsVisibleTo(ownerID, &ownerID)).To(BeTrue())
			})

			It("hides exit from non-owners", func() {
				exit := createTestExit(room1.ID, room2.ID, "secret door")
				exit.Visibility = world.VisibilityOwner
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				got, _ := env.Exits.Get(ctx, exit.ID)
				Expect(got.IsVisibleTo(otherID, &ownerID)).To(BeFalse())
			})
		})

		Context("when visibility is 'list'", func() {
			It("shows exit only to characters in visible_to list", func() {
				allowedID := createTestCharacterID()
				exit := createTestExit(room1.ID, room2.ID, "vip door")
				exit.Visibility = world.VisibilityList
				exit.VisibleTo = []ulid.ULID{allowedID}
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				got, _ := env.Exits.Get(ctx, exit.ID)
				Expect(got.IsVisibleTo(allowedID, nil)).To(BeTrue())
				Expect(got.IsVisibleTo(otherID, nil)).To(BeFalse())
			})
		})
	})

	Describe("Locked exits", func() {
		Context("with key lock", func() {
			It("stores lock configuration", func() {
				keyObjectID := "key-12345"
				exit := createTestExit(room1.ID, room2.ID, "locked door")
				exit.Locked = true
				exit.LockType = world.LockTypeKey
				exit.LockData = map[string]any{"key_object_id": keyObjectID}
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				got, err := env.Exits.Get(ctx, exit.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.Locked).To(BeTrue())
				Expect(got.LockType).To(Equal(world.LockTypeKey))
				Expect(got.LockData["key_object_id"]).To(Equal(keyObjectID))
			})
		})

		Context("with password lock", func() {
			It("stores password hash in lock_data", func() {
				exit := createTestExit(room1.ID, room2.ID, "password door")
				exit.Locked = true
				exit.LockType = world.LockTypePassword
				exit.LockData = map[string]any{"password_hash": "hashed_secret"}
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				got, err := env.Exits.Get(ctx, exit.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.LockType).To(Equal(world.LockTypePassword))
				Expect(got.LockData["password_hash"]).To(Equal("hashed_secret"))
			})
		})
	})

	Describe("Exit name matching", func() {
		BeforeEach(func() {
			exit := createTestExit(room1.ID, room2.ID, "Northern Gate")
			exit.Aliases = []string{"n", "north", "gate"}
			Expect(env.Exits.Create(ctx, exit)).To(Succeed())
		})

		It("matches by exact name (case-insensitive)", func() {
			found, err := env.Exits.FindByName(ctx, room1.ID, "northern gate")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("Northern Gate"))

			found, err = env.Exits.FindByName(ctx, room1.ID, "NORTHERN GATE")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("Northern Gate"))
		})

		It("matches by alias", func() {
			found, err := env.Exits.FindByName(ctx, room1.ID, "n")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("Northern Gate"))

			found, err = env.Exits.FindByName(ctx, room1.ID, "gate")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("Northern Gate"))
		})

		It("matches by fuzzy search with threshold", func() {
			found, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "nort", 0.3)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("Northern Gate"))
		})
	})
})
