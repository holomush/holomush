// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// setupPostgresContainer starts a PostgreSQL container for testing.
func setupPostgresContainer() (*store.PostgresEventStore, func(), error) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Run migrations using the new Migrator
	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		return nil, nil, err
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = pgEnv.Terminate(ctx)
		return nil, nil, err
	}
	_ = migrator.Close()

	eventStore, err := store.NewPostgresEventStore(ctx, pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		return nil, nil, err
	}

	cleanup := func() {
		eventStore.Close()
		_ = pgEnv.Terminate(ctx)
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

	Describe("ReplayTail", func() {
		const stream = "location:replay-tail-test"

		It("returns last N events in ascending order", func() {
			ctx := context.Background()
			baseTime := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)

			ids := make([]ulid.ULID, 10)
			for i := range 10 {
				ids[i] = core.NewULID()
				event := core.Event{
					ID:        ids[i],
					Stream:    stream,
					Type:      core.EventTypeSay,
					Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
					Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
					Payload:   []byte(`{}`),
				}
				err := eventStore.Append(ctx, event)
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(time.Millisecond)
			}

			events, err := eventStore.ReplayTail(ctx, stream, 3, time.Time{})
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(3))
			Expect(events[0].ID).To(Equal(ids[7]))
			Expect(events[1].ID).To(Equal(ids[8]))
			Expect(events[2].ID).To(Equal(ids[9]))
		})

		It("filters events before notBefore", func() {
			ctx := context.Background()
			streamNB := "location:replay-tail-nb"
			baseTime := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)

			ids := make([]ulid.ULID, 5)
			for i := range 5 {
				ids[i] = core.NewULID()
				event := core.Event{
					ID:        ids[i],
					Stream:    streamNB,
					Type:      core.EventTypeSay,
					Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
					Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-123"},
					Payload:   []byte(`{}`),
				}
				err := eventStore.Append(ctx, event)
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(time.Millisecond)
			}

			// notBefore = baseTime+3m excludes first 3 events.
			events, err := eventStore.ReplayTail(ctx, streamNB, 10, baseTime.Add(3*time.Minute))
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(2))
			Expect(events[0].ID).To(Equal(ids[3]))
			Expect(events[1].ID).To(Equal(ids[4]))
		})

		It("returns empty for nonexistent stream", func() {
			ctx := context.Background()
			events, err := eventStore.ReplayTail(ctx, "location:nonexistent-tail", 10, time.Time{})
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(BeEmpty())
		})
	})

	Describe("SubscribeSession", func() {
		It("delivers notifications for added streams", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sub, err := eventStore.SubscribeSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = sub.Close() }()

			Expect(sub.AddStream(ctx, "location:ss-a")).To(Succeed())
			Expect(sub.AddStream(ctx, "location:ss-b")).To(Succeed())

			e1 := core.Event{
				ID: core.NewULID(), Stream: "location:ss-a",
				Type: core.EventTypeSay, Timestamp: time.Now(),
				Actor: core.Actor{Kind: core.ActorCharacter, ID: "c1"},
				Payload: []byte(`{}`),
			}
			time.Sleep(time.Millisecond)
			e2 := core.Event{
				ID: core.NewULID(), Stream: "location:ss-b",
				Type: core.EventTypeSay, Timestamp: time.Now(),
				Actor: core.Actor{Kind: core.ActorCharacter, ID: "c2"},
				Payload: []byte(`{}`),
			}

			Expect(eventStore.Append(ctx, e1)).To(Succeed())
			Expect(eventStore.Append(ctx, e2)).To(Succeed())

			notifCh := sub.Notifications()
			Eventually(notifCh, 2*time.Second).Should(Receive(Equal(
				core.StreamNotification{Stream: "location:ss-a", EventID: e1.ID},
			)))
			Eventually(notifCh, 2*time.Second).Should(Receive(Equal(
				core.StreamNotification{Stream: "location:ss-b", EventID: e2.ID},
			)))
		})

		It("does not deliver for removed streams", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sub, err := eventStore.SubscribeSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = sub.Close() }()

			Expect(sub.AddStream(ctx, "location:ss-remove")).To(Succeed())
			Expect(sub.RemoveStream(ctx, "location:ss-remove")).To(Succeed())

			e := core.Event{
				ID: core.NewULID(), Stream: "location:ss-remove",
				Type: core.EventTypeSay, Timestamp: time.Now(),
				Actor: core.Actor{Kind: core.ActorCharacter, ID: "c1"},
				Payload: []byte(`{}`),
			}
			Expect(eventStore.Append(ctx, e)).To(Succeed())

			Consistently(sub.Notifications(), 500*time.Millisecond).ShouldNot(Receive())
		})

		It("isolates sessions from each other", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			sub1, err := eventStore.SubscribeSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = sub1.Close() }()

			sub2, err := eventStore.SubscribeSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = sub2.Close() }()

			Expect(sub1.AddStream(ctx, "location:iso-a")).To(Succeed())
			Expect(sub2.AddStream(ctx, "location:iso-b")).To(Succeed())

			e := core.Event{
				ID: core.NewULID(), Stream: "location:iso-a",
				Type: core.EventTypeSay, Timestamp: time.Now(),
				Actor: core.Actor{Kind: core.ActorCharacter, ID: "c1"},
				Payload: []byte(`{}`),
			}
			Expect(eventStore.Append(ctx, e)).To(Succeed())

			// sub1 should receive; sub2 should not.
			Eventually(sub1.Notifications(), 2*time.Second).Should(Receive())
			Consistently(sub2.Notifications(), 500*time.Millisecond).ShouldNot(Receive())
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

	Describe("Variant A Go/No-Go: Strict ULID-Ascending Cross-Stream Order (I-14)", func() {
		It("delivers events in strict ULID-ascending order to multiple subscribers", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			const (
				numStreams    = 4
				numEvents    = 1000
				numGoroutines = 10
				eventsPerGo  = numEvents / numGoroutines
			)

			streams := make([]string, numStreams)
			for i := range numStreams {
				streams[i] = fmt.Sprintf("location:ordering-%d", i)
			}

			// Wrap the store with EventWriter to serialize appends.
			// EventWriter stamps ULID + timestamp in its single goroutine,
			// guaranteeing ULID generation order = commit order.
			writer := core.NewEventWriter(eventStore)
			defer writer.Close()

			// Create 2 session subscriptions, both listening on all 4 streams.
			sub1, err := eventStore.SubscribeSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = sub1.Close() }()

			sub2, err := eventStore.SubscribeSession(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = sub2.Close() }()

			for _, s := range streams {
				Expect(sub1.AddStream(ctx, s)).To(Succeed())
				Expect(sub2.AddStream(ctx, s)).To(Succeed())
			}

			// Launch 10 goroutines, each appending 100 events spread
			// across the 4 streams via EventWriter. The writer stamps
			// IDs serially, so ULID order = commit order.
			var wg sync.WaitGroup
			for g := range numGoroutines {
				wg.Add(1)
				go func(goroutineIdx int) {
					defer wg.Done()
					for i := range eventsPerGo {
						streamIdx := (goroutineIdx*eventsPerGo + i) % numStreams
						event := core.Event{
							// ID left zero — EventWriter stamps it.
							Stream:  streams[streamIdx],
							Type:    core.EventTypeSay,
							Actor:   core.Actor{Kind: core.ActorCharacter, ID: fmt.Sprintf("c%d", goroutineIdx)},
							Payload: []byte(fmt.Sprintf(`{"g":%d,"i":%d}`, goroutineIdx, i)),
						}
						err := writer.Write(ctx, event)
						Expect(err).NotTo(HaveOccurred())
					}
				}(g)
			}

			// Collect notifications from both subscribers.
			collected1 := make([]core.StreamNotification, 0, numEvents)
			collected2 := make([]core.StreamNotification, 0, numEvents)

			collect := func(sub core.Subscription, out *[]core.StreamNotification) {
				for len(*out) < numEvents {
					select {
					case n, ok := <-sub.Notifications():
						if !ok {
							return
						}
						*out = append(*out, n)
					case <-ctx.Done():
						return
					}
				}
			}

			var collectWg sync.WaitGroup
			collectWg.Add(2)
			go func() { defer collectWg.Done(); collect(sub1, &collected1) }()
			go func() { defer collectWg.Done(); collect(sub2, &collected2) }()

			wg.Wait()        // Wait for all appends to finish.
			collectWg.Wait() // Wait for both collectors to receive all events.

			// Both subscribers must have received all events.
			Expect(collected1).To(HaveLen(numEvents),
				"subscriber 1 did not receive all events")
			Expect(collected2).To(HaveLen(numEvents),
				"subscriber 2 did not receive all events")

			// Both subscribers must have received events in identical order
			// (same sequence of StreamNotification, element by element).
			Expect(collected1).To(Equal(collected2),
				"I-14 VIOLATION: subscribers received events in different order")

			// RESTORED: strict ULID-ascending order. With EventWriter
			// serializing all appends, ULID generation order = commit
			// order = NOTIFY delivery order. Every event ID must be
			// strictly greater than the previous one.
			for i := 1; i < len(collected1); i++ {
				Expect(collected1[i].EventID.Compare(collected1[i-1].EventID)).To(
					BeNumerically(">", 0),
					"I-14 ULID-ascending violation at index %d: %s must be > %s",
					i, collected1[i].EventID, collected1[i-1].EventID,
				)
			}
		})
	})
})
