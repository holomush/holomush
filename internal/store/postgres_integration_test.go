// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/store"
)

// setupPostgresContainer starts a PostgreSQL container for testing.
func setupPostgresContainer() (*store.PostgresEventStore, func(), error) {
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("holomush"),
		postgres.WithPassword("holomush"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, nil, err
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, nil, err
	}

	// Run migrations using the new Migrator
	migrator, err := store.NewMigrator(connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, err
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = container.Terminate(ctx)
		return nil, nil, err
	}
	_ = migrator.Close()

	eventStore, err := store.NewPostgresEventStore(ctx, connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, err
	}

	cleanup := func() {
		eventStore.Close()
		_ = container.Terminate(ctx)
	}

	return eventStore, cleanup, nil
}

var _ = Describe("PostgresEventStore", func() {
	var eventStore *store.PostgresEventStore
	var cleanup func()

	BeforeEach(func() {
		var err error
		eventStore, cleanup, err = setupPostgresContainer()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Append", func() {
		It("stores events correctly", func() {
			ctx := context.Background()
			event := core.Event{
				ID:        core.NewULID(),
				Stream:    "location:test-room",
				Type:      core.EventTypeSay,
				Timestamp: time.Now(),
				Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
				Payload:   []byte(`{"message":"Hello, world!"}`),
			}

			err := eventStore.Append(ctx, event)
			Expect(err).NotTo(HaveOccurred())

			// Verify event was stored
			events, err := eventStore.Replay(ctx, "location:test-room", ulid.ULID{}, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(1))
			Expect(events[0].ID).To(Equal(event.ID))
		})
	})

	Describe("Replay", func() {
		var ids []ulid.ULID
		const stream = "location:replay-test"

		BeforeEach(func() {
			ctx := context.Background()
			ids = make([]ulid.ULID, 5)
			for i := range 5 {
				ids[i] = core.NewULID()
				event := core.Event{
					ID:        ids[i],
					Stream:    stream,
					Type:      core.EventTypeSay,
					Timestamp: time.Now(),
					Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
					Payload:   []byte(`{"message":"test"}`),
				}
				err := eventStore.Append(ctx, event)
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(time.Millisecond) // Ensure ULID ordering
			}
		})

		It("replays all events from beginning", func() {
			ctx := context.Background()
			events, err := eventStore.Replay(ctx, stream, ulid.ULID{}, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(5))
		})

		It("replays events after a specific ID", func() {
			ctx := context.Background()
			events, err := eventStore.Replay(ctx, stream, ids[1], 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(3))
		})

		It("respects the limit parameter", func() {
			ctx := context.Background()
			events, err := eventStore.Replay(ctx, stream, ulid.ULID{}, 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(2))
		})

		It("returns empty slice for nonexistent stream", func() {
			ctx := context.Background()
			events, err := eventStore.Replay(ctx, "nonexistent:stream", ulid.ULID{}, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(BeEmpty())
		})
	})

	Describe("LastEventID", func() {
		const stream = "location:last-id-test"

		It("returns ErrStreamEmpty for empty stream", func() {
			ctx := context.Background()
			_, err := eventStore.LastEventID(ctx, stream)
			Expect(err).To(Equal(core.ErrStreamEmpty))
		})

		Context("with events in stream", func() {
			var lastID ulid.ULID

			BeforeEach(func() {
				ctx := context.Background()
				for i := range 3 {
					lastID = core.NewULID()
					event := core.Event{
						ID:        lastID,
						Stream:    stream,
						Type:      core.EventTypeSay,
						Timestamp: time.Now(),
						Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
						Payload:   []byte(`{}`),
					}
					err := eventStore.Append(ctx, event)
					Expect(err).NotTo(HaveOccurred(), "Append %d failed", i)
					time.Sleep(time.Millisecond)
				}
			})

			It("returns the last event ID", func() {
				ctx := context.Background()
				id, err := eventStore.LastEventID(ctx, stream)
				Expect(err).NotTo(HaveOccurred())
				Expect(id).To(Equal(lastID))
			})
		})
	})

	Describe("EventTypes", func() {
		It("stores and retrieves all event types correctly", func() {
			ctx := context.Background()
			stream := "location:event-types-test"

			eventTypes := []core.EventType{
				core.EventTypeSay,
				core.EventTypePose,
				core.EventTypeArrive,
				core.EventTypeLeave,
				core.EventTypeSystem,
			}

			for _, et := range eventTypes {
				event := core.Event{
					ID:        core.NewULID(),
					Stream:    stream,
					Type:      et,
					Timestamp: time.Now(),
					Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
					Payload:   []byte(`{}`),
				}
				err := eventStore.Append(ctx, event)
				Expect(err).NotTo(HaveOccurred())
			}

			events, err := eventStore.Replay(ctx, stream, ulid.ULID{}, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(len(eventTypes)))

			for i, et := range eventTypes {
				Expect(events[i].Type).To(Equal(et))
			}
		})
	})

	Describe("ActorKinds", func() {
		It("stores and retrieves all actor kinds correctly", func() {
			ctx := context.Background()
			stream := "location:actor-kinds-test"

			actorKinds := []core.ActorKind{
				core.ActorCharacter,
				core.ActorSystem,
				core.ActorPlugin,
			}

			for _, ak := range actorKinds {
				event := core.Event{
					ID:        core.NewULID(),
					Stream:    stream,
					Type:      core.EventTypeSay,
					Timestamp: time.Now(),
					Actor:     core.Actor{Kind: ak, ID: "test-actor"},
					Payload:   []byte(`{}`),
				}
				err := eventStore.Append(ctx, event)
				Expect(err).NotTo(HaveOccurred())
			}

			events, err := eventStore.Replay(ctx, stream, ulid.ULID{}, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(len(actorKinds)))

			for i, ak := range actorKinds {
				Expect(events[i].Actor.Kind).To(Equal(ak))
			}
		})
	})

	Describe("SystemInfo", func() {
		It("returns error for missing key", func() {
			ctx := context.Background()
			_, err := eventStore.GetSystemInfo(ctx, "nonexistent")
			Expect(err).To(HaveOccurred())
		})

		It("sets and gets system info", func() {
			ctx := context.Background()
			err := eventStore.SetSystemInfo(ctx, "test_key", "test_value")
			Expect(err).NotTo(HaveOccurred())

			value, err := eventStore.GetSystemInfo(ctx, "test_key")
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal("test_value"))
		})

		It("updates existing key", func() {
			ctx := context.Background()
			err := eventStore.SetSystemInfo(ctx, "update_key", "original")
			Expect(err).NotTo(HaveOccurred())

			err = eventStore.SetSystemInfo(ctx, "update_key", "updated")
			Expect(err).NotTo(HaveOccurred())

			value, err := eventStore.GetSystemInfo(ctx, "update_key")
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal("updated"))
		})
	})

	Describe("InitGameID", func() {
		It("generates new game_id when none exists", func() {
			ctx := context.Background()
			gameID, err := eventStore.InitGameID(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(gameID).NotTo(BeEmpty())
			Expect(gameID).To(HaveLen(26)) // Valid ULID length
		})

		It("returns existing game_id on subsequent calls", func() {
			ctx := context.Background()
			firstID, err := eventStore.InitGameID(ctx)
			Expect(err).NotTo(HaveOccurred())

			secondID, err := eventStore.InitGameID(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(secondID).To(Equal(firstID))
		})

		It("persists game_id in database", func() {
			ctx := context.Background()
			gameID, err := eventStore.InitGameID(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Verify via GetSystemInfo
			storedID, err := eventStore.GetSystemInfo(ctx, "game_id")
			Expect(err).NotTo(HaveOccurred())
			Expect(storedID).To(Equal(gameID))
		})
	})

	Describe("Subscribe", func() {
		const stream = "location:subscribe-test"

		It("receives events via LISTEN/NOTIFY", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Start subscription before appending
			eventCh, errCh, err := eventStore.Subscribe(ctx, stream)
			Expect(err).NotTo(HaveOccurred())

			// Append an event
			event := core.Event{
				ID:        core.NewULID(),
				Stream:    stream,
				Type:      core.EventTypeSay,
				Timestamp: time.Now(),
				Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
				Payload:   []byte(`{"message":"Hello via NOTIFY!"}`),
			}
			err = eventStore.Append(ctx, event)
			Expect(err).NotTo(HaveOccurred())

			// Should receive the event via subscription
			Eventually(eventCh, 2*time.Second).Should(Receive(Equal(event.ID)))

			// No errors
			Consistently(errCh, 100*time.Millisecond).ShouldNot(Receive())
		})

		It("receives multiple events in order", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			eventCh, errCh, err := eventStore.Subscribe(ctx, stream)
			Expect(err).NotTo(HaveOccurred())

			// Append multiple events
			ids := make([]ulid.ULID, 3)
			for i := range 3 {
				ids[i] = core.NewULID()
				event := core.Event{
					ID:        ids[i],
					Stream:    stream,
					Type:      core.EventTypeSay,
					Timestamp: time.Now(),
					Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
					Payload:   []byte(`{}`),
				}
				err := eventStore.Append(ctx, event)
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(10 * time.Millisecond) // Ensure ordering
			}

			// Should receive all events in order
			for _, expectedID := range ids {
				Eventually(eventCh, 2*time.Second).Should(Receive(Equal(expectedID)))
			}

			Consistently(errCh, 100*time.Millisecond).ShouldNot(Receive())
		})

		It("stops receiving when context is cancelled", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			eventCh, _, err := eventStore.Subscribe(ctx, stream)
			Expect(err).NotTo(HaveOccurred())

			// Cancel context
			cancel()

			// Channel should eventually close
			Eventually(eventCh).Should(BeClosed())
		})

		It("isolates subscriptions by stream", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Subscribe to a specific stream
			eventCh, _, err := eventStore.Subscribe(ctx, "location:stream-a")
			Expect(err).NotTo(HaveOccurred())

			// Append to a different stream
			event := core.Event{
				ID:        core.NewULID(),
				Stream:    "location:stream-b",
				Type:      core.EventTypeSay,
				Timestamp: time.Now(),
				Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
				Payload:   []byte(`{}`),
			}
			err = eventStore.Append(ctx, event)
			Expect(err).NotTo(HaveOccurred())

			// Should not receive event from other stream
			Consistently(eventCh, 500*time.Millisecond).ShouldNot(Receive())
		})
	})
})
