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

var _ = Describe("ExitRepository", func() {
	var ctx context.Context
	var room1, room2 *world.Location

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)

		// Create two rooms for exit tests
		room1 = createTestLocation("Room One", "First room.", world.LocationTypePersistent)
		room2 = createTestLocation("Room Two", "Second room.", world.LocationTypePersistent)
		Expect(env.Locations.Create(ctx, room1)).To(Succeed())
		Expect(env.Locations.Create(ctx, room2)).To(Succeed())
	})

	Describe("Create", func() {
		It("creates exit with all fields", func() {
			exit := createTestExit(room1.ID, room2.ID, "north")
			exit.Aliases = []string{"n", "forward"}
			exit.Visibility = world.VisibilityAll

			err := env.Exits.Create(ctx, exit)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Exits.Get(ctx, exit.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("north"))
			Expect(got.Aliases).To(ConsistOf("n", "forward"))
			Expect(got.FromLocationID).To(Equal(room1.ID))
			Expect(got.ToLocationID).To(Equal(room2.ID))
		})

		It("auto-creates return exit when bidirectional", func() {
			exit := createTestExit(room1.ID, room2.ID, "north")
			exit.Bidirectional = true
			exit.ReturnName = "south"

			err := env.Exits.Create(ctx, exit)
			Expect(err).NotTo(HaveOccurred())

			// Find return exit
			returnExit, err := env.Exits.FindByName(ctx, room2.ID, "south")
			Expect(err).NotTo(HaveOccurred())
			Expect(returnExit.FromLocationID).To(Equal(room2.ID))
			Expect(returnExit.ToLocationID).To(Equal(room1.ID))
		})

		It("persists aliases as array", func() {
			exit := createTestExit(room1.ID, room2.ID, "door")
			exit.Aliases = []string{"d", "doorway", "entrance"}

			err := env.Exits.Create(ctx, exit)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Exits.Get(ctx, exit.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Aliases).To(HaveLen(3))
			Expect(got.Aliases).To(ContainElements("d", "doorway", "entrance"))
		})

		It("persists lock_data as JSONB", func() {
			exit := createTestExit(room1.ID, room2.ID, "vault")
			exit.Locked = true
			exit.LockType = world.LockTypeKey
			exit.LockData = map[string]any{"key_object_id": "abc123"}

			err := env.Exits.Create(ctx, exit)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Exits.Get(ctx, exit.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Locked).To(BeTrue())
			Expect(got.LockType).To(Equal(world.LockTypeKey))
			Expect(got.LockData["key_object_id"]).To(Equal("abc123"))
		})

		It("persists visible_to as array", func() {
			charID := createTestCharacterID()
			exit := createTestExit(room1.ID, room2.ID, "secret")
			exit.Visibility = world.VisibilityList
			exit.VisibleTo = []ulid.ULID{charID}

			err := env.Exits.Create(ctx, exit)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Exits.Get(ctx, exit.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Visibility).To(Equal(world.VisibilityList))
			Expect(got.VisibleTo).To(HaveLen(1))
			Expect(got.VisibleTo[0]).To(Equal(charID))
		})
	})

	Describe("Delete", func() {
		It("removes exit from database", func() {
			exit := createTestExit(room1.ID, room2.ID, "north")
			Expect(env.Exits.Create(ctx, exit)).To(Succeed())

			err := env.Exits.Delete(ctx, exit.ID)
			Expect(err).NotTo(HaveOccurred())

			_, err = env.Exits.Get(ctx, exit.ID)
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})

		It("removes return exit when bidirectional", func() {
			exit := createTestExit(room1.ID, room2.ID, "north")
			exit.Bidirectional = true
			exit.ReturnName = "south"
			Expect(env.Exits.Create(ctx, exit)).To(Succeed())

			// Verify return exit exists
			_, err := env.Exits.FindByName(ctx, room2.ID, "south")
			Expect(err).NotTo(HaveOccurred())

			// Delete primary exit
			err = env.Exits.Delete(ctx, exit.ID)
			Expect(err).NotTo(HaveOccurred())

			// Return exit should also be gone
			_, err = env.Exits.FindByName(ctx, room2.ID, "south")
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})

		It("handles missing return exit gracefully", func() {
			exit := createTestExit(room1.ID, room2.ID, "north")
			exit.Bidirectional = true
			exit.ReturnName = "south"
			Expect(env.Exits.Create(ctx, exit)).To(Succeed())

			// Manually delete return exit first
			returnExit, _ := env.Exits.FindByName(ctx, room2.ID, "south")
			_, _ = env.pool.Exec(ctx, "DELETE FROM exits WHERE id = $1", returnExit.ID.String())

			// Deleting primary should not error
			err := env.Exits.Delete(ctx, exit.ID)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("FindByName", func() {
		BeforeEach(func() {
			exit := createTestExit(room1.ID, room2.ID, "North Door")
			exit.Aliases = []string{"n", "door"}
			Expect(env.Exits.Create(ctx, exit)).To(Succeed())
		})

		It("matches exact name case-insensitively", func() {
			found, err := env.Exits.FindByName(ctx, room1.ID, "north door")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("North Door"))
		})

		It("matches aliases", func() {
			found, err := env.Exits.FindByName(ctx, room1.ID, "n")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("North Door"))

			found, err = env.Exits.FindByName(ctx, room1.ID, "door")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("North Door"))
		})

		It("returns ErrNotFound when no match", func() {
			_, err := env.Exits.FindByName(ctx, room1.ID, "nonexistent")
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
	})

	Describe("FindByNameFuzzy", func() {
		BeforeEach(func() {
			exits := []*world.Exit{
				createTestExit(room1.ID, room2.ID, "north"),
				createTestExit(room1.ID, room2.ID, "south"),
			}
			for _, e := range exits {
				Expect(env.Exits.Create(ctx, e)).To(Succeed())
			}
		})

		It("returns matches above threshold with typo", func() {
			// pg_trgm similarity for "nroth" vs "north" is ~0.25
			found, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "nroth", 0.2)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("north"))
		})

		It("returns matches with partial input", func() {
			// pg_trgm similarity for "nor" vs "north" is ~0.3
			found, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "nor", 0.3)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("north"))
		})

		It("returns ErrNotFound when below threshold", func() {
			_, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "xyz", 0.5)
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})

		It("validates threshold bounds (0.0-1.0)", func() {
			_, err := env.Exits.FindByNameFuzzy(ctx, room1.ID, "test", -0.1)
			Expect(err).To(HaveOccurred())

			_, err = env.Exits.FindByNameFuzzy(ctx, room1.ID, "test", 1.1)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListFromLocation", func() {
		It("returns all exits from location", func() {
			room3 := createTestLocation("Room Three", "Third room.", world.LocationTypePersistent)
			Expect(env.Locations.Create(ctx, room3)).To(Succeed())

			Expect(env.Exits.Create(ctx, createTestExit(room1.ID, room2.ID, "north"))).To(Succeed())
			Expect(env.Exits.Create(ctx, createTestExit(room1.ID, room3.ID, "east"))).To(Succeed())

			exits, err := env.Exits.ListFromLocation(ctx, room1.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(exits).To(HaveLen(2))
		})

		It("orders by name", func() {
			room3 := createTestLocation("Room Three", "Third room.", world.LocationTypePersistent)
			Expect(env.Locations.Create(ctx, room3)).To(Succeed())

			Expect(env.Exits.Create(ctx, createTestExit(room1.ID, room2.ID, "zulu"))).To(Succeed())
			Expect(env.Exits.Create(ctx, createTestExit(room1.ID, room3.ID, "alpha"))).To(Succeed())

			exits, err := env.Exits.ListFromLocation(ctx, room1.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(exits[0].Name).To(Equal("alpha"))
			Expect(exits[1].Name).To(Equal("zulu"))
		})
	})
})
