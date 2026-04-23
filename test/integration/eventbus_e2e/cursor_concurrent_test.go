// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	crand "crypto/rand"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// Cursor concurrent pagination specs reproduce the exact scenario the
// holomush-suos bead was filed to fix: two publishers writing to the same
// subject at high concurrency, where ULID lex order deliberately disagrees
// with JetStream stream-sequence order.
//
// Pre-suos (ULID-keyed pagination): the internal cursor advanced by ULID lex
// position rather than JS stream sequence. Concurrent publishers produce
// ULIDs whose lex order does NOT match arrival order, so the cursor could
// skip or revisit events between internal page loads — causing drops or
// duplicates in the result set.
//
// Post-suos (seq-keyed pagination): the internal cursor (AfterSeq/BeforeSeq)
// is derived from the last event's JS Seq. Seq is assigned monotonically by
// JetStream regardless of ULID lex order, so internal page boundaries are
// always stable.
//
// The specs force multiple internal page loads by setting pageSize=20 with
// 100 total events. The crossoverStream internally calls the hot tier
// multiple times via advanceCursor; each call must pick up exactly where
// the previous left off, with no drops or duplicates.
//
// Spec reference: §12.3 "concurrent publishers".
var _ = Describe("Cursor pagination under concurrent publishers with drifted ULIDs", func() {
	const (
		publishersCount = 2
		eventsPerPub    = 50
		totalEvents     = publishersCount * eventsPerPub
		// pageSize is intentionally small relative to totalEvents: this forces
		// the crossoverStream to make multiple internal hot-tier page loads,
		// exercising the seq-keyed cursor advancement on each boundary.
		pageSize = 20
	)

	// publishConcurrent fires publishersCount goroutines that each emit
	// eventsPerPub events with independently-generated ULIDs. Returns once
	// all publishers complete (or one fails).
	publishConcurrent := func(ctx context.Context, pub eventbus.Publisher, subject eventbus.Subject, eventType eventbus.Type) {
		var wg sync.WaitGroup
		errCh := make(chan error, publishersCount*eventsPerPub)
		for p := 0; p < publishersCount; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < eventsPerPub; i++ {
					id, err := ulid.New(ulid.Timestamp(time.Now()), crand.Reader)
					if err != nil {
						errCh <- err
						return
					}
					ev := eventbus.Event{
						ID:        id,
						Subject:   subject,
						Type:      eventType,
						Timestamp: time.Now().UTC(),
						Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
						Payload:   []byte("p"),
					}
					if pubErr := pub.Publish(ctx, ev); pubErr != nil {
						errCh <- pubErr
						return
					}
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			Expect(err).NotTo(HaveOccurred(), "publisher goroutine returned an error")
		}
	}

	Context("backward direction (newest → oldest)", func() {
		It("returns exactly totalEvents in strictly decreasing seq order with no duplicates", func() {
			bus := eventbustest.New(suiteT)
			pool := freshPool(suiteT)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			DeferCleanup(cancel)

			// Distinct subject so this spec doesn't cross-talk with any other
			// test in the package.
			subject := eventbus.Subject("events.main.concurrent.uliddrift")
			pub := bus.Bus.Publisher()

			// Record the stream position before we publish so we can express
			// the publish barrier as a target.
			seqBefore := currentStreamLastSeq(suiteT, bus)

			// Publish concurrently from two goroutines with independent
			// entropy sources. The lex order of their ULIDs will NOT agree
			// with the JS stream-sequence order in which messages land —
			// this is the "drift" the bead is about.
			publishConcurrent(ctx, pub, subject, eventbus.Type("test.concurrent"))

			// Stream-progress barrier. Use Eventually so the assertion is
			// expressed in the integration suite's standard form rather than
			// AwaitStreamLastSeq's internal poll loop. The bus.JS stream is
			// shared across tests in the package, so we anchor on the delta
			// from seqBefore rather than an absolute seq.
			Eventually(func() uint64 {
				return currentStreamLastSeq(suiteT, bus)
			}, 30*time.Second, 50*time.Millisecond).Should(BeNumerically(">=", seqBefore+totalEvents),
				"stream did not commit all %d publishes within timeout", totalEvents)

			// Open ONE stream with a small pageSize so the crossoverStream is
			// forced to make ceil(totalEvents/pageSize) = 5 internal page
			// loads. Each internal load exercises the cursor-advance logic.
			now := time.Now().UTC()
			streamMaxAge := 30 * 24 * time.Hour
			r := buildReader(bus, pool, streamMaxAge, now)
			stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
				Subject:   subject,
				Direction: eventbus.DirectionBackward,
				PageSize:  pageSize,
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = stream.Close() })

			allEvents := drainStream(suiteT, stream)

			// 1. No drops, no extra events.
			Expect(allEvents).To(HaveLen(totalEvents),
				"backward: expected exactly %d events (no drops, no dupes)", totalEvents)

			// 2. Strictly decreasing JS seq (backward = newest first).
			for i := 1; i < len(allEvents); i++ {
				Expect(allEvents[i-1].Seq).To(BeNumerically(">", allEvents[i].Seq),
					"backward: events must be in strictly decreasing seq order at positions %d→%d", i-1, i)
			}

			// 3. Unique seqs (no duplicates across internal page boundaries).
			seenSeqs := make(map[uint64]bool, len(allEvents))
			for _, ev := range allEvents {
				Expect(seenSeqs[ev.Seq]).To(BeFalse(),
					"backward: duplicate seq %d — internal cursor is not advancing", ev.Seq)
				seenSeqs[ev.Seq] = true
			}
		})
	})

	Context("forward direction (oldest → newest)", func() {
		It("returns exactly totalEvents in strictly increasing seq order with no duplicates", func() {
			bus := eventbustest.New(suiteT)
			pool := freshPool(suiteT)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			DeferCleanup(cancel)

			subject := eventbus.Subject("events.main.concurrent.uliddriftfwd")
			pub := bus.Bus.Publisher()
			seqBefore := currentStreamLastSeq(suiteT, bus)

			publishConcurrent(ctx, pub, subject, eventbus.Type("test.concurrent.fwd"))

			Eventually(func() uint64 {
				return currentStreamLastSeq(suiteT, bus)
			}, 30*time.Second, 50*time.Millisecond).Should(BeNumerically(">=", seqBefore+totalEvents),
				"stream did not commit all %d publishes within timeout", totalEvents)

			now := time.Now().UTC()
			streamMaxAge := 30 * 24 * time.Hour
			r := buildReader(bus, pool, streamMaxAge, now)
			stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
				Subject:   subject,
				Direction: eventbus.DirectionForward,
				PageSize:  pageSize,
			})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = stream.Close() })

			allEvents := drainStream(suiteT, stream)

			// 1. No drops, no extra events.
			Expect(allEvents).To(HaveLen(totalEvents),
				"forward: expected exactly %d events (no drops, no dupes)", totalEvents)

			// 2. Strictly increasing seq (forward = oldest first).
			for i := 1; i < len(allEvents); i++ {
				Expect(allEvents[i-1].Seq).To(BeNumerically("<", allEvents[i].Seq),
					"forward: events must be in strictly increasing seq order at positions %d→%d", i-1, i)
			}

			// 3. Unique seqs (no duplicates across internal page boundaries).
			seenSeqs := make(map[uint64]bool, len(allEvents))
			for _, ev := range allEvents {
				Expect(seenSeqs[ev.Seq]).To(BeFalse(),
					"forward: duplicate seq %d — internal cursor is not advancing", ev.Seq)
				seenSeqs[ev.Seq] = true
			}
		})
	})
})
