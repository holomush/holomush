// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	crand "crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// TestPaginationStableUnderConcurrentPublishersWithDriftedULIDs reproduces
// the exact scenario the holomush-suos bead was filed to fix: two publishers
// writing to the same subject at high concurrency, where ULID lex order
// deliberately disagrees with JetStream stream-sequence order.
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
// The test forces multiple internal page loads by setting pageSize=20 with
// 100 total events. The crossoverStream internally calls the hot tier
// multiple times via advanceCursor; each call must pick up exactly where
// the previous left off, with no drops or duplicates.
//
// Spec reference: §12.3 "concurrent publishers" + §12.5 (see subscribe-
// backfill test below for the LAG/STALE paths).
func TestPaginationStableUnderConcurrentPublishersWithDriftedULIDs(t *testing.T) {
	bus := eventbustest.New(t)
	pool := freshPool(t)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	const (
		publishersCount = 2
		eventsPerPub    = 50
		totalEvents     = publishersCount * eventsPerPub
		// pageSize is intentionally small relative to totalEvents: this forces
		// the crossoverStream to make multiple internal hot-tier page loads,
		// exercising the seq-keyed cursor advancement on each boundary.
		pageSize = 20
	)

	// Distinct subject so this test doesn't cross-talk with any other test in
	// the package (each scenario in cross_tier_query_test.go uses a different
	// ctq-prefixed subject).
	subject := eventbus.Subject("events.main.concurrent.uliddrift")
	pub := bus.Bus.Publisher()

	// Record the stream position before we start so we can set an accurate
	// await barrier.
	seqBefore := currentStreamLastSeq(t, bus)

	// Publish concurrently from two goroutines with independent entropy
	// sources. Because each goroutine picks entropy independently, the lex
	// order of their ULIDs will NOT agree with the JS stream-sequence order
	// in which the messages actually land — this is the "drift" the bead
	// is about.
	var wg sync.WaitGroup
	for p := 0; p < publishersCount; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < eventsPerPub; i++ {
				id, err := ulid.New(ulid.Timestamp(time.Now()), crand.Reader)
				if err != nil {
					// ulid.New only fails on a broken entropy source; treat
					// as test-fatal via panic (we cannot call t.Fatal from a
					// goroutine that isn't the test goroutine).
					panic("ulid.New: " + err.Error())
				}
				ev := eventbus.Event{
					ID:        id,
					Subject:   subject,
					Type:      eventbus.Type("test.concurrent"),
					Timestamp: time.Now().UTC(),
					Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
					Payload:   []byte("p"),
				}
				if pubErr := pub.Publish(ctx, ev); pubErr != nil {
					panic("pub.Publish: " + pubErr.Error())
				}
			}
		}()
	}
	wg.Wait()

	// Barrier: wait until the stream has committed all totalEvents messages
	// before we open the reader. Without this, the reader might open before
	// some publishes have landed and return fewer events than expected.
	bus.AwaitStreamLastSeq(t, seqBefore+totalEvents, 30*time.Second)

	// Open ONE stream with a small pageSize so the crossoverStream is forced
	// to make ceil(totalEvents/pageSize) = 5 internal page loads. Each internal
	// load exercises the cursor-advance logic.
	now := time.Now().UTC()
	streamMaxAge := 30 * 24 * time.Hour

	// --- Backward read (newest → oldest) ---
	{
		r := buildReader(bus, pool, streamMaxAge, now)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			Direction: eventbus.DirectionBackward,
			PageSize:  pageSize,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		allEvents := drainStream(t, stream)

		// 1. No drops and no extra events.
		assert.Len(t, allEvents, totalEvents,
			"backward: expected exactly %d events (no drops, no dupes)", totalEvents)

		// 2. Strictly decreasing JS seq (backward = newest first).
		for i := 1; i < len(allEvents); i++ {
			assert.Greater(t, allEvents[i-1].Seq, allEvents[i].Seq,
				"backward: events must be in strictly decreasing seq order at positions %d→%d", i-1, i)
		}

		// 3. Unique seqs (no duplicates across internal page boundaries).
		seenSeqs := make(map[uint64]bool, len(allEvents))
		for _, ev := range allEvents {
			assert.False(t, seenSeqs[ev.Seq],
				"backward: duplicate seq %d — internal cursor is not advancing", ev.Seq)
			seenSeqs[ev.Seq] = true
		}
	}
}

// TestPaginationForwardIsStableUnderConcurrentPublishers repeats the same
// concurrent-publisher scenario in the forward (oldest→newest) direction to
// prove that AfterSeq-based internal cursor advancement is also correct.
func TestPaginationForwardIsStableUnderConcurrentPublishers(t *testing.T) {
	bus := eventbustest.New(t)
	pool := freshPool(t)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	const (
		publishersCount = 2
		eventsPerPub    = 50
		totalEvents     = publishersCount * eventsPerPub
		pageSize        = 20
	)

	subject := eventbus.Subject("events.main.concurrent.uliddriftfwd")
	pub := bus.Bus.Publisher()
	seqBefore := currentStreamLastSeq(t, bus)

	var wg sync.WaitGroup
	for p := 0; p < publishersCount; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < eventsPerPub; i++ {
				id, err := ulid.New(ulid.Timestamp(time.Now()), crand.Reader)
				if err != nil {
					panic("ulid.New: " + err.Error())
				}
				ev := eventbus.Event{
					ID:        id,
					Subject:   subject,
					Type:      eventbus.Type("test.concurrent.fwd"),
					Timestamp: time.Now().UTC(),
					Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
					Payload:   []byte("p"),
				}
				if pubErr := pub.Publish(ctx, ev); pubErr != nil {
					panic("pub.Publish: " + pubErr.Error())
				}
			}
		}()
	}
	wg.Wait()

	bus.AwaitStreamLastSeq(t, seqBefore+totalEvents, 30*time.Second)

	now := time.Now().UTC()
	streamMaxAge := 30 * 24 * time.Hour

	r := buildReader(bus, pool, streamMaxAge, now)
	stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
		Subject:   subject,
		Direction: eventbus.DirectionForward,
		PageSize:  pageSize,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	allEvents := drainStream(t, stream)

	// 1. No drops and no extra events.
	assert.Len(t, allEvents, totalEvents,
		"forward: expected exactly %d events (no drops, no dupes)", totalEvents)

	// 2. Strictly increasing seq (forward = oldest first).
	for i := 1; i < len(allEvents); i++ {
		assert.Less(t, allEvents[i-1].Seq, allEvents[i].Seq,
			"forward: events must be in strictly increasing seq order at positions %d→%d", i-1, i)
	}

	// 3. Unique seqs (no duplicates across internal page boundaries).
	seenSeqs := make(map[uint64]bool, len(allEvents))
	for _, ev := range allEvents {
		assert.False(t, seenSeqs[ev.Seq],
			"forward: duplicate seq %d — internal cursor is not advancing", ev.Seq)
		seenSeqs[ev.Seq] = true
	}
}
