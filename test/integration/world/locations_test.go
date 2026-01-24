// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/world"
)

var _ = Describe("Location Management", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)
	})

	Describe("Location types", func() {
		It("creates persistent locations (permanent world rooms)", func() {
			loc := createTestLocation("Town Square", "The center of town.", world.LocationTypePersistent)
			Expect(env.Locations.Create(ctx, loc)).To(Succeed())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Type).To(Equal(world.LocationTypePersistent))
		})

		It("creates scene locations (temporary RP rooms)", func() {
			loc := createTestLocation("Private Meeting", "A private scene.", world.LocationTypeScene)
			Expect(env.Locations.Create(ctx, loc)).To(Succeed())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Type).To(Equal(world.LocationTypeScene))
		})

		It("creates instance locations (future instanced content)", func() {
			loc := createTestLocation("Dungeon Instance", "An instanced dungeon.", world.LocationTypeInstance)
			Expect(env.Locations.Create(ctx, loc)).To(Succeed())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Type).To(Equal(world.LocationTypeInstance))
		})
	})

	Describe("Replay policy", func() {
		Context("persistent locations", func() {
			It("defaults to 'last:0' (no replay)", func() {
				loc := createTestLocation("No Replay", "Testing default.", world.LocationTypePersistent)
				Expect(env.Locations.Create(ctx, loc)).To(Succeed())

				got, err := env.Locations.Get(ctx, loc.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.ReplayPolicy).To(Equal("last:0"))
			})
		})

		Context("scene locations", func() {
			It("defaults to 'last:-1' (full history)", func() {
				loc := createTestLocation("Full Replay", "Testing scene default.", world.LocationTypeScene)
				Expect(env.Locations.Create(ctx, loc)).To(Succeed())

				got, err := env.Locations.Get(ctx, loc.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.ReplayPolicy).To(Equal("last:-1"))
			})
		})

		It("parses 'last:N' format correctly", func() {
			Expect(world.ParseReplayPolicy("last:0")).To(Equal(0))
			Expect(world.ParseReplayPolicy("last:10")).To(Equal(10))
			Expect(world.ParseReplayPolicy("last:-1")).To(Equal(-1))
		})

		It("supports custom replay limits", func() {
			loc := createTestLocation("Limited Replay", "Custom replay.", world.LocationTypePersistent)
			loc.ReplayPolicy = "last:50"
			Expect(env.Locations.Create(ctx, loc)).To(Succeed())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ReplayPolicy).To(Equal("last:50"))
			Expect(world.ParseReplayPolicy(got.ReplayPolicy)).To(Equal(50))
		})
	})

	Describe("Location ownership", func() {
		It("tracks owner_id for builder permissions", func() {
			ownerID := createTestCharacterID()
			loc := createTestLocation("Built Room", "A builder-owned room.", world.LocationTypePersistent)
			loc.OwnerID = &ownerID
			Expect(env.Locations.Create(ctx, loc)).To(Succeed())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).NotTo(BeNil())
			Expect(*got.OwnerID).To(Equal(ownerID))
		})

		It("allows nil owner for system-created locations", func() {
			loc := createTestLocation("System Room", "Created by system.", world.LocationTypePersistent)
			loc.OwnerID = nil
			Expect(env.Locations.Create(ctx, loc)).To(Succeed())

			got, err := env.Locations.Get(ctx, loc.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.OwnerID).To(BeNil())
		})
	})
})
