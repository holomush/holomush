// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventvocab"
	corecomm "github.com/holomush/holomush/plugins/core-communication"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventAssignsMonotonicID(t *testing.T) {
	before := time.Now()
	ev := core.NewEvent("location:test", eventvocab.EventType(corecomm.EventTypeSay), core.Actor{
		Kind: core.ActorCharacter,
		ID:   "char-1",
	}, []byte(`{"message":"hello"}`))
	after := time.Now()

	require.NotEqual(t, ulid.ULID{}, ev.ID, "ID must be non-zero")
	assert.Equal(t, "location:test", ev.Stream)
	assert.Equal(t, eventvocab.EventType(corecomm.EventTypeSay), ev.Type)
	assert.Equal(t, core.ActorCharacter, ev.Actor.Kind)
	assert.Equal(t, "char-1", ev.Actor.ID)
	assert.Equal(t, []byte(`{"message":"hello"}`), ev.Payload)
	assert.False(t, ev.Timestamp.Before(before), "Timestamp must be >= before")
	assert.False(t, ev.Timestamp.After(after), "Timestamp must be <= after")
}

func TestNewEventProducesUniqueIDs(t *testing.T) {
	seen := make(map[ulid.ULID]struct{}, 100)
	for range 100 {
		ev := core.NewEvent("location:test", eventvocab.EventType(corecomm.EventTypeSay), core.Actor{
			Kind: core.ActorSystem,
			ID:   "system",
		}, []byte(`{}`))
		_, dup := seen[ev.ID]
		require.False(t, dup, "NewEvent must produce unique IDs")
		seen[ev.ID] = struct{}{}
	}
}

func TestNewEventIDsAreMonotonicWithinGoroutine(t *testing.T) {
	var prev ulid.ULID
	for range 1000 {
		ev := core.NewEvent("location:test", eventvocab.EventType(corecomm.EventTypeSay), core.Actor{
			Kind: core.ActorSystem,
			ID:   "system",
		}, []byte(`{}`))
		if prev != (ulid.ULID{}) {
			assert.True(t, ev.ID.Compare(prev) > 0,
				"IDs must be strictly monotonic: prev=%s cur=%s", prev, ev.ID)
		}
		prev = ev.ID
	}
}

func TestEventIDMonotonicityUnderLoad(t *testing.T) {
	// I-16 stress test: 10 goroutines x 10,000 NewEvent calls.
	// Each goroutine collects its IDs; within each goroutine the IDs
	// must be strictly monotonically increasing (the monotonic entropy
	// source is protected by entropyLock in ulid.go).
	const goroutines = 10
	const eventsPerGoroutine = 10_000

	type idSlice []ulid.ULID

	results := make([]idSlice, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := range goroutines {
		go func(idx int) {
			defer wg.Done()
			ids := make(idSlice, 0, eventsPerGoroutine)
			for range eventsPerGoroutine {
				ev := core.NewEvent("stress:test", eventvocab.EventType(corecomm.EventTypeSay), core.Actor{
					Kind: core.ActorSystem,
					ID:   "stress",
				}, []byte(`{}`))
				ids = append(ids, ev.ID)
			}
			results[idx] = ids
		}(g)
	}
	wg.Wait()

	// Merge all IDs into a single slice preserving per-goroutine order.
	all := make([]ulid.ULID, 0, goroutines*eventsPerGoroutine)
	for _, ids := range results {
		all = append(all, ids...)
	}

	// Sort the merged slice. Since NewULID is monotonic (mutex-protected),
	// the global sequence must also be strictly monotonic -- no two IDs
	// should be equal.
	sort.Slice(all, func(i, j int) bool {
		return all[i].Compare(all[j]) < 0
	})

	for i := 1; i < len(all); i++ {
		require.NotEqual(t, all[i], all[i-1],
			"duplicate ID at sorted position %d: %s", i, all[i])
	}

	// Within each goroutine, IDs must be strictly ascending (monotonic).
	for g, ids := range results {
		for i := 1; i < len(ids); i++ {
			require.True(t, ids[i].Compare(ids[i-1]) > 0,
				"goroutine %d: non-monotonic at position %d: prev=%s cur=%s",
				g, i, ids[i-1], ids[i])
		}
	}
}
