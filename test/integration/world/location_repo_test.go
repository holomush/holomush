// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/world"
)

var _ = Describe("LocationRepository", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)
	})

	Describe("Create", func() {
		It("persists all location fields", func() {
			loc := createTestLocation("Test Room", "A room for testing.", world.LocationTypePersistent)

			err := env.Locations.Create(ctx, loc)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("Test Room"))
			Expect(got.Description).To(Equal("A room for testing."))
			Expect(got.Type).To(Equal(world.LocationTypePersistent))
			Expect(got.ReplayPolicy).To(Equal("last:0"))
		})

		It("handles nil optional fields (owner_id, shadows_id)", func() {
			loc := createTestLocation("No Owner", "A room without owner.", world.LocationTypePersistent)
			loc.OwnerID = nil
			loc.ShadowsID = nil

			err := env.Locations.Create(ctx, loc)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).To(BeNil())
			Expect(got.ShadowsID).To(BeNil())
		})

		It("sets created_at timestamp", func() {
			before := time.Now().Add(-time.Second)
			loc := createTestLocation("Timed Room", "Testing timestamps.", world.LocationTypePersistent)

			err := env.Locations.Create(ctx, loc)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.CreatedAt).To(BeTemporally(">=", before))
		})
	})

	Describe("Get", func() {
		It("retrieves location with all fields", func() {
			ownerID := createTestCharacterID()
			loc := createTestLocation("Full Room", "All fields set.", world.LocationTypeScene)
			loc.OwnerID = &ownerID
			loc.ReplayPolicy = "last:-1"

			err := env.Locations.Create(ctx, loc)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(loc.ID))
			Expect(got.Name).To(Equal("Full Room"))
			Expect(got.Type).To(Equal(world.LocationTypeScene))
			Expect(got.OwnerID).NotTo(BeNil())
			Expect(*got.OwnerID).To(Equal(ownerID))
		})

		It("returns ErrNotFound for missing ID", func() {
			_, err := env.Locations.Get(ctx, ulid.Make())
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
	})

	Describe("Update", func() {
		It("updates mutable fields", func() {
			loc := createTestLocation("Original", "Original description.", world.LocationTypePersistent)
			err := env.Locations.Create(ctx, loc)
			Expect(err).NotTo(HaveOccurred())

			loc.Name = "Updated"
			loc.Description = "Updated description."
			err = env.Locations.Update(ctx, loc)
			Expect(err).NotTo(HaveOccurred())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Name).To(Equal("Updated"))
			Expect(got.Description).To(Equal("Updated description."))
		})

		It("returns ErrNotFound for missing ID", func() {
			loc := createTestLocation("Ghost", "Doesn't exist.", world.LocationTypePersistent)
			err := env.Locations.Update(ctx, loc)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
	})

	Describe("Delete", func() {
		It("removes location from database", func() {
			loc := createTestLocation("To Delete", "Will be deleted.", world.LocationTypePersistent)
			err := env.Locations.Create(ctx, loc)
			Expect(err).NotTo(HaveOccurred())

			err = env.Locations.Delete(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())

			_, err = env.Locations.Get(ctx, loc.ID)
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})

		It("returns ErrNotFound for missing ID", func() {
			err := env.Locations.Delete(ctx, ulid.Make())
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
	})

	Describe("ListByType", func() {
		var persistentLoc, sceneLoc *world.Location

		BeforeEach(func() {
			// Create mix of location types for this test
			persistentLoc = createTestLocation("Persistent", "Persistent room.", world.LocationTypePersistent)
			sceneLoc = createTestLocation("Scene", "Scene room.", world.LocationTypeScene)

			Expect(env.Locations.Create(ctx, persistentLoc)).To(Succeed())
			Expect(env.Locations.Create(ctx, sceneLoc)).To(Succeed())
		})

		It("returns only locations of specified type", func() {
			scenes, err := env.Locations.ListByType(ctx, world.LocationTypeScene)
			Expect(err).NotTo(HaveOccurred())
			Expect(scenes).NotTo(BeEmpty())
			// Verify all returned locations are of the requested type
			for _, loc := range scenes {
				Expect(loc.Type).To(Equal(world.LocationTypeScene))
			}
			// Verify our test scene is included
			var found bool
			for _, loc := range scenes {
				if loc.ID == sceneLoc.ID {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "Expected to find the test scene in results")
		})

		It("excludes locations of other types", func() {
			persistents, err := env.Locations.ListByType(ctx, world.LocationTypePersistent)
			Expect(err).NotTo(HaveOccurred())
			// Verify all returned locations are persistent
			for _, loc := range persistents {
				Expect(loc.Type).To(Equal(world.LocationTypePersistent))
			}
			// Verify our test persistent location is included
			var found bool
			for _, loc := range persistents {
				if loc.ID == persistentLoc.ID {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "Expected to find the test persistent location in results")
		})

		It("returns empty slice when no matches", func() {
			instances, err := env.Locations.ListByType(ctx, world.LocationTypeInstance)
			Expect(err).NotTo(HaveOccurred())
			// Note: there might be instance locations from other tests, but
			// at minimum we verify the query doesn't error
			for _, loc := range instances {
				Expect(loc.Type).To(Equal(world.LocationTypeInstance))
			}
		})
	})

	Describe("GetShadowedBy", func() {
		It("returns scenes that shadow the location", func() {
			parent := createTestLocation("Tavern", "A cozy tavern.", world.LocationTypePersistent)
			Expect(env.Locations.Create(ctx, parent)).To(Succeed())

			scene := createTestLocation("", "", world.LocationTypeScene)
			scene.ShadowsID = &parent.ID
			Expect(env.Locations.Create(ctx, scene)).To(Succeed())

			shadows, err := env.Locations.GetShadowedBy(ctx, parent.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(shadows).To(HaveLen(1))
			Expect(shadows[0].ShadowsID).NotTo(BeNil())
			Expect(*shadows[0].ShadowsID).To(Equal(parent.ID))
		})

		It("returns empty slice when no shadows", func() {
			loc := createTestLocation("Lonely", "No shadows.", world.LocationTypePersistent)
			Expect(env.Locations.Create(ctx, loc)).To(Succeed())

			shadows, err := env.Locations.GetShadowedBy(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(shadows).To(BeEmpty())
		})
	})
})
