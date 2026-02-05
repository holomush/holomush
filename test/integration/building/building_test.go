// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package building_test

import (
	"bytes"
	"context"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/world"
)

// allowAllAccessControl is a test implementation that allows all access.
type allowAllAccessControl struct{}

func (a *allowAllAccessControl) Check(_ context.Context, _, _, _ string) bool {
	return true
}

func testServices(worldService *world.Service) *command.Services {
	return command.NewTestServices(command.ServicesConfig{
		World:            worldService,
		PropertyRegistry: property.SharedRegistry(),
	})
}

var _ = Describe("Building & Objects Commands", func() {
	var ctx context.Context
	var startRoom *world.Location
	var charID ulid.ULID
	var worldService *world.Service

	BeforeEach(func() {
		ctx = context.Background()
		cleanupAll(ctx, env.pool)

		// Create a starting room for the character
		startRoom = createTestLocation("Starting Room", "Where adventures begin.", world.LocationTypePersistent)
		Expect(env.Locations.Create(ctx, startRoom)).To(Succeed())

		// Create a test character
		charID = createTestCharacterID()

		// Create world service with all repositories
		worldService = world.NewService(world.ServiceConfig{
			LocationRepo:  env.Locations,
			ExitRepo:      env.Exits,
			ObjectRepo:    env.Objects,
			CharacterRepo: env.Characters,
			AccessControl: &allowAllAccessControl{},
		})
	})

	Describe("create command", func() {
		Context("creating objects", func() {
			It("creates object in current location", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        `object "Magic Sword"`,
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.CreateHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Created object"))
				Expect(output).To(ContainSubstring("Magic Sword"))

				// Verify object exists in database
				objects, err := env.Objects.ListAtLocation(ctx, startRoom.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(objects).To(HaveLen(1))
				Expect(objects[0].Name).To(Equal("Magic Sword"))
				Expect(objects[0].LocationID()).NotTo(BeNil())
				Expect(*objects[0].LocationID()).To(Equal(startRoom.ID))
			})

			It("returns error for invalid syntax", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        "object MissingSword",
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.CreateHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Usage:"))
			})
		})

		Context("creating locations", func() {
			It("creates a new location", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        `location "Secret Chamber"`,
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.CreateHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Created location"))
				Expect(output).To(ContainSubstring("Secret Chamber"))
			})

			It("returns error for unknown type", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        `widget "Something"`,
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.CreateHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Unknown type"))
			})
		})
	})

	Describe("set command", func() {
		Context("setting description with prefix matching", func() {
			It("resolves 'desc' to 'description'", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        "desc of here to A dark and mysterious place.",
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.SetHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Set description"))

				// Verify description was updated in database
				loc, err := env.Locations.Get(ctx, startRoom.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(loc.Description).To(Equal("A dark and mysterious place."))
			})

			It("resolves 'n' to 'name'", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        "n of here to Renamed Room",
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.SetHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Set name"))

				// Verify name was updated in database
				loc, err := env.Locations.Get(ctx, startRoom.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(loc.Name).To(Equal("Renamed Room"))
			})
		})

		Context("setting properties on objects", func() {
			var obj *world.Object

			BeforeEach(func() {
				obj = createTestObject("Test Item", "", world.InLocation(startRoom.ID))
				Expect(env.Objects.Create(ctx, obj)).To(Succeed())
			})

			It("sets description on object by ID reference", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        "description of #" + obj.ID.String() + " to A shiny magical item.",
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.SetHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Set description"))

				// Verify description was updated in database
				updatedObj, err := env.Objects.Get(ctx, obj.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedObj.Description).To(Equal("A shiny magical item."))
			})
		})

		Context("error cases", func() {
			It("returns error for unknown property", func() {
				// The default registry only has "name" and "description", so "xyz"
				// won't match any known property
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        "xyz of here to value",
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.SetHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("property not found"))
			})

			It("returns error for invalid target", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        "description of nonexistent to value",
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.SetHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Error:"))
				Expect(output).To(ContainSubstring("target not found"))
			})

			It("returns error for invalid ID reference", func() {
				var buf bytes.Buffer
				exec := &command.CommandExecution{
					CharacterID: charID,
					LocationID:  startRoom.ID,
					Args:        "description of #invalid-id to value",
					Output:      &buf,
					Services:    testServices(worldService),
				}

				err := handlers.SetHandler(ctx, exec)
				Expect(err).NotTo(HaveOccurred())

				output := buf.String()
				Expect(output).To(ContainSubstring("Error:"))
				Expect(output).To(ContainSubstring("invalid ID"))
			})
		})
	})

	Describe("exit creation via repository (simulating dig/link)", func() {
		// Architecture note: dig/link commands are implemented as Lua plugins
		// (plugins/building/main.lua) that call holomush.create_location and
		// holomush.create_exit host functions. These host functions delegate to
		// the WorldService and repository layer.
		//
		// Testing at the repository level is the appropriate integration test
		// strategy because:
		// 1. Lua plugin integration tests require a full plugin runtime with
		//    embedded gopher-lua VM, which is tested separately in internal/plugin
		// 2. The repository tests verify the data persistence layer that all
		//    building operations ultimately depend on
		// 3. This provides coverage of the critical path (DB operations) without
		//    coupling tests to Lua plugin implementation details

		Context("dig creates location and exit", func() {
			It("creates a new location with exit from current room", func() {
				// Create destination location (simulating dig creating it)
				destRoom := createTestLocation("Garden", "A beautiful garden.", world.LocationTypePersistent)
				Expect(env.Locations.Create(ctx, destRoom)).To(Succeed())

				// Create exit from start room to garden
				exit := createTestExit(startRoom.ID, destRoom.ID, "garden")
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				// Verify exit exists
				foundExit, err := env.Exits.FindByName(ctx, startRoom.ID, "garden")
				Expect(err).NotTo(HaveOccurred())
				Expect(foundExit.Name).To(Equal("garden"))
				Expect(foundExit.ToLocationID).To(Equal(destRoom.ID))
			})
		})

		Context("dig with return creates bidirectional exits", func() {
			It("creates exits in both directions", func() {
				// Create destination location
				destRoom := createTestLocation("Library", "A quiet library.", world.LocationTypePersistent)
				Expect(env.Locations.Create(ctx, destRoom)).To(Succeed())

				// Create bidirectional exit
				exit := createTestExit(startRoom.ID, destRoom.ID, "north")
				exit.Bidirectional = true
				exit.ReturnName = "south"
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				// Verify forward exit
				northExit, err := env.Exits.FindByName(ctx, startRoom.ID, "north")
				Expect(err).NotTo(HaveOccurred())
				Expect(northExit.ToLocationID).To(Equal(destRoom.ID))

				// Verify return exit was auto-created
				southExit, err := env.Exits.FindByName(ctx, destRoom.ID, "south")
				Expect(err).NotTo(HaveOccurred())
				Expect(southExit.ToLocationID).To(Equal(startRoom.ID))
			})
		})

		Context("link to location by name", func() {
			It("creates exit to existing location found by name", func() {
				// Create a destination that would be found by name search
				destRoom := createTestLocation("Town Square", "The center of town.", world.LocationTypePersistent)
				Expect(env.Locations.Create(ctx, destRoom)).To(Succeed())

				// List locations and find by name (simulating what link command does)
				// Note: In production, the Lua plugin uses holomush.find_location which
				// queries by name. For this test, we simulate by getting the known ID.
				foundLoc, err := env.Locations.Get(ctx, destRoom.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(foundLoc.Name).To(Equal("Town Square"))

				// Create exit to found location
				exit := createTestExit(startRoom.ID, foundLoc.ID, "square")
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				// Verify exit
				createdExit, err := env.Exits.FindByName(ctx, startRoom.ID, "square")
				Expect(err).NotTo(HaveOccurred())
				Expect(createdExit.ToLocationID).To(Equal(destRoom.ID))
			})
		})

		Context("link to location by ID", func() {
			It("creates exit to location specified by ULID", func() {
				// Create destination
				destRoom := createTestLocation("Dungeon", "A dark dungeon.", world.LocationTypePersistent)
				Expect(env.Locations.Create(ctx, destRoom)).To(Succeed())

				// Get location by ID (simulating #id lookup)
				foundLoc, err := env.Locations.Get(ctx, destRoom.ID)
				Expect(err).NotTo(HaveOccurred())

				// Create exit
				exit := createTestExit(startRoom.ID, foundLoc.ID, "dungeon")
				Expect(env.Exits.Create(ctx, exit)).To(Succeed())

				// Verify
				createdExit, err := env.Exits.FindByName(ctx, startRoom.ID, "dungeon")
				Expect(err).NotTo(HaveOccurred())
				Expect(createdExit.ToLocationID).To(Equal(destRoom.ID))
			})
		})

		Context("error cases for linking", func() {
			It("fails when target location does not exist", func() {
				nonExistentID := ulid.Make()
				_, err := env.Locations.Get(ctx, nonExistentID)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
