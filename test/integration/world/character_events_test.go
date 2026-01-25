// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// allowAllAccessControl is a simple access control that allows all operations.
type allowAllAccessControl struct{}

func (a *allowAllAccessControl) Check(_ context.Context, _, _, _ string) bool {
	return true
}

var _ = Describe("Character Movement Events", func() {
	var ctx context.Context
	var room1, room2 *world.Location
	var playerID ulid.ULID
	var service *world.Service
	var emitter *world.EventStoreAdapter

	BeforeEach(func() {
		ctx = context.Background()
		cleanupLocations(ctx, env.pool)

		// Create test locations
		room1 = createTestLocation("Starting Room", "The beginning.", world.LocationTypePersistent)
		room2 = createTestLocation("Destination Room", "The end.", world.LocationTypePersistent)
		Expect(env.Locations.Create(ctx, room1)).To(Succeed())
		Expect(env.Locations.Create(ctx, room2)).To(Succeed())

		// Create a player for our test characters (use full ULID for uniqueness)
		playerID = core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash)
			VALUES ($1, $2, 'test_hash')`,
			playerID.String(), "testplayer_"+playerID.String())
		Expect(err).NotTo(HaveOccurred())

		// Create the event store adapter
		emitter = world.NewEventStoreAdapter(env.eventStore)

		// Create the service with real repositories and event emitter
		service = world.NewService(world.ServiceConfig{
			LocationRepo:  env.Locations,
			CharacterRepo: env.Characters,
			AccessControl: &allowAllAccessControl{},
			EventEmitter:  emitter,
		})
	})

	Describe("MoveCharacter", func() {
		It("emits move event to destination location stream", func() {
			// Create a character in room1
			charID := core.NewULID()
			_, err := env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, location_id)
				VALUES ($1, $2, $3, $4)`,
				charID.String(), playerID.String(), "TestMover", room1.ID.String())
			Expect(err).NotTo(HaveOccurred())

			// Move character from room1 to room2
			err = service.MoveCharacter(ctx, "system", charID, room2.ID)
			Expect(err).NotTo(HaveOccurred())

			// Query event store for events on destination stream
			stream := world.LocationStream(room2.ID)
			events, err := env.eventStore.Replay(ctx, stream, ulid.ULID{}, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(1), "expected exactly one move event on destination stream")

			// Verify event structure
			event := events[0]
			Expect(event.Stream).To(Equal(stream))
			Expect(event.Type).To(Equal(core.EventTypeMove))
			Expect(event.Actor.Kind).To(Equal(core.ActorSystem))
			Expect(event.Actor.ID).To(Equal("world-service"))

			// Parse and verify payload
			var payload world.MovePayload
			err = json.Unmarshal(event.Payload, &payload)
			Expect(err).NotTo(HaveOccurred())

			Expect(payload.EntityType).To(Equal(world.EntityTypeCharacter))
			Expect(payload.EntityID).To(Equal(charID))
			Expect(payload.FromType).To(Equal(world.ContainmentTypeLocation))
			Expect(payload.FromID).NotTo(BeNil())
			Expect(*payload.FromID).To(Equal(room1.ID))
			Expect(payload.ToType).To(Equal(world.ContainmentTypeLocation))
			Expect(payload.ToID).To(Equal(room2.ID))
		})

		It("emits move event for first-time placement", func() {
			// Create a character with no location (nil LocationID)
			charID := core.NewULID()
			_, err := env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, location_id)
				VALUES ($1, $2, $3, NULL)`,
				charID.String(), playerID.String(), "NewCharacter")
			Expect(err).NotTo(HaveOccurred())

			// Move character to room1 (first-time placement)
			err = service.MoveCharacter(ctx, "system", charID, room1.ID)
			Expect(err).NotTo(HaveOccurred())

			// Query event store for events on destination stream
			stream := world.LocationStream(room1.ID)
			events, err := env.eventStore.Replay(ctx, stream, ulid.ULID{}, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(1), "expected exactly one move event on destination stream")

			// Parse and verify payload has FromType="none" and FromID=nil
			var payload world.MovePayload
			err = json.Unmarshal(events[0].Payload, &payload)
			Expect(err).NotTo(HaveOccurred())

			Expect(payload.EntityType).To(Equal(world.EntityTypeCharacter))
			Expect(payload.EntityID).To(Equal(charID))
			Expect(payload.FromType).To(Equal(world.ContainmentTypeNone))
			Expect(payload.FromID).To(BeNil())
			Expect(payload.ToType).To(Equal(world.ContainmentTypeLocation))
			Expect(payload.ToID).To(Equal(room1.ID))
		})

		It("emits multiple events for sequential moves", func() {
			// Create a third location for this test
			room3 := createTestLocation("Third Room", "Yet another room.", world.LocationTypePersistent)
			Expect(env.Locations.Create(ctx, room3)).To(Succeed())

			// Create a character in room1
			charID := core.NewULID()
			_, err := env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, location_id)
				VALUES ($1, $2, $3, $4)`,
				charID.String(), playerID.String(), "Traveler", room1.ID.String())
			Expect(err).NotTo(HaveOccurred())

			// Move character: room1 -> room2 -> room3
			err = service.MoveCharacter(ctx, "system", charID, room2.ID)
			Expect(err).NotTo(HaveOccurred())

			// Small delay to ensure distinct timestamps
			time.Sleep(10 * time.Millisecond)

			err = service.MoveCharacter(ctx, "system", charID, room3.ID)
			Expect(err).NotTo(HaveOccurred())

			// Verify event on room2 stream
			stream2 := world.LocationStream(room2.ID)
			events2, err := env.eventStore.Replay(ctx, stream2, ulid.ULID{}, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(events2).To(HaveLen(1))

			var payload2 world.MovePayload
			Expect(json.Unmarshal(events2[0].Payload, &payload2)).To(Succeed())
			Expect(payload2.FromID).NotTo(BeNil())
			Expect(*payload2.FromID).To(Equal(room1.ID))
			Expect(payload2.ToID).To(Equal(room2.ID))

			// Verify event on room3 stream
			stream3 := world.LocationStream(room3.ID)
			events3, err := env.eventStore.Replay(ctx, stream3, ulid.ULID{}, 100)
			Expect(err).NotTo(HaveOccurred())
			Expect(events3).To(HaveLen(1))

			var payload3 world.MovePayload
			Expect(json.Unmarshal(events3[0].Payload, &payload3)).To(Succeed())
			Expect(payload3.FromID).NotTo(BeNil())
			Expect(*payload3.FromID).To(Equal(room2.ID))
			Expect(payload3.ToID).To(Equal(room3.ID))
		})
	})
})
