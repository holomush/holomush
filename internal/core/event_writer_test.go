// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
)

func TestEventWriterSerializesAppends(t *testing.T) {
	store := core.NewMemoryEventStore()
	writer := core.NewEventWriter(store)
	defer writer.Close()

	const (
		numGoroutines = 10
		eventsPerGo   = 100
		totalEvents   = numGoroutines * eventsPerGo
	)

	ctx := context.Background()
	var wg sync.WaitGroup

	for g := range numGoroutines {
		wg.Add(1)
		go func(goroutineIdx int) {
			defer wg.Done()
			for i := range eventsPerGo {
				event := core.Event{
					// ID left zero — writer stamps it in the serialized goroutine.
					Stream:  "location:writer-test",
					Type:    core.EventTypeSay,
					Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
					Payload: []byte(`{}`),
				}
				err := writer.Write(ctx, event)
				require.NoError(t, err, "goroutine %d event %d", goroutineIdx, i)
			}
		}(g)
	}

	wg.Wait()

	events, err := store.Replay(ctx, "location:writer-test", ulid.ULID{}, totalEvents+1)
	require.NoError(t, err)
	require.Len(t, events, totalEvents)

	// Assert strict ULID-ascending order: each event's ID must be > previous.
	// Because the writer stamps IDs in its single goroutine immediately before
	// Append, ULID generation order = store insertion order.
	for i := 1; i < len(events); i++ {
		assert.Truef(t, events[i].ID.Compare(events[i-1].ID) > 0,
			"events[%d].ID (%s) must be > events[%d].ID (%s)",
			i, events[i].ID, i-1, events[i-1].ID)
	}
}

func TestEventWriterPropagatesErrors(t *testing.T) {
	expectedErr := errors.New("store failure")
	store := &failingStore{err: expectedErr}
	writer := core.NewEventWriter(store)
	defer writer.Close()

	event := core.Event{
		Stream:  "location:test",
		Type:    core.EventTypeSay,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload: []byte(`{}`),
	}

	err := writer.Write(context.Background(), event)
	assert.ErrorIs(t, err, expectedErr)
}

func TestEventWriterCloseReturnsErrorForSubsequentWrites(t *testing.T) {
	store := core.NewMemoryEventStore()
	writer := core.NewEventWriter(store)
	writer.Close()

	event := core.Event{
		Stream:  "location:test",
		Type:    core.EventTypeSay,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload: []byte(`{}`),
	}

	err := writer.Write(context.Background(), event)
	assert.ErrorIs(t, err, core.ErrWriterClosed)
}

func TestEventWriterAppendImplementsEventAppender(t *testing.T) {
	store := core.NewMemoryEventStore()
	writer := core.NewEventWriter(store)
	defer writer.Close()

	event := core.Event{
		Stream:  "location:appender-test",
		Type:    core.EventTypeSay,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload: []byte(`{}`),
	}

	err := writer.Append(context.Background(), event)
	require.NoError(t, err)

	events, err := store.Replay(context.Background(), "location:appender-test", ulid.ULID{}, 10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	// The writer stamps the ID, so we just check it's non-zero.
	assert.NotEqual(t, ulid.ULID{}, events[0].ID)
}

func TestEventWriterRespectsContextCancellation(t *testing.T) {
	store := core.NewMemoryEventStore()
	writer := core.NewEventWriter(store)
	defer writer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	event := core.Event{
		Stream:  "location:test",
		Type:    core.EventTypeSay,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload: []byte(`{}`),
	}

	err := writer.Write(ctx, event)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEventWriterStampsIDAndTimestamp(t *testing.T) {
	store := core.NewMemoryEventStore()
	writer := core.NewEventWriter(store)
	defer writer.Close()

	before := time.Now()
	event := core.Event{
		Stream:  "location:stamp-test",
		Type:    core.EventTypeSay,
		Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload: []byte(`{}`),
	}

	err := writer.Write(context.Background(), event)
	require.NoError(t, err)
	after := time.Now()

	events, err := store.Replay(context.Background(), "location:stamp-test", ulid.ULID{}, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.NotEqual(t, ulid.ULID{}, events[0].ID, "writer must stamp a non-zero ID")
	assert.False(t, events[0].Timestamp.Before(before), "timestamp must be >= before")
	assert.False(t, events[0].Timestamp.After(after), "timestamp must be <= after")
}

// failingStore is a test double that always returns an error from Append.
type failingStore struct {
	err error
}

func (f *failingStore) Append(_ context.Context, _ core.Event) error {
	return f.err
}

func (f *failingStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
	return nil, f.err
}

func (f *failingStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, f.err
}

func (f *failingStore) ReplayTail(_ context.Context, _ string, _ int, _ time.Time) ([]core.Event, error) {
	return nil, f.err
}

func (f *failingStore) SubscribeSession(_ context.Context) (core.Subscription, error) {
	return nil, f.err
}
