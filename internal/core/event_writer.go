// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// ErrWriterClosed is returned when Write is called after Close.
var ErrWriterClosed = errors.New("event writer is closed")

// EventWriter serializes all event appends through a single goroutine,
// ensuring ULID generation order = Postgres commit order = NOTIFY delivery
// order = Replay ORDER BY id order. This is the enforcement mechanism for
// invariant I-14 (strict ULID-ascending ordering).
//
// All production code paths that append events MUST go through EventWriter,
// not directly through EventStore.Append. The writer owns the Append call;
// callers submit events via the Write method and block until the append
// completes.
//
// The writer goroutine stamps each event's ID (via core.NewULID()) and
// Timestamp immediately before calling Append. Because ULID generation
// and the database INSERT happen in the same serialized goroutine, ULID
// order is guaranteed to match commit order.
//
// Throughput: ~1000 events/second (bounded by single-INSERT latency).
// For a MUSH with ~200 concurrent players, this is ~100x headroom.
type EventWriter struct {
	store    EventStore
	appendCh chan appendRequest
	done     chan struct{}
	stopped  chan struct{}
	closed   atomic.Bool
	once     sync.Once
}

type appendRequest struct {
	ctx   context.Context
	event Event
	err   chan error
}

// NewEventWriter creates a writer that serializes appends to the given store.
// Call Close() when done to shut down the writer goroutine.
func NewEventWriter(store EventStore) *EventWriter {
	w := &EventWriter{
		store:    store,
		appendCh: make(chan appendRequest, 256),
		done:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go w.run()
	return w
}

// Write submits an event for serialized append. Blocks until the append
// completes or ctx is cancelled.
//
// The writer goroutine stamps the event's ID via core.NewULID() and
// Timestamp immediately before calling Append, ensuring ULID generation
// order = commit order. Callers SHOULD leave event.ID zero; any pre-set
// ID is overwritten.
func (w *EventWriter) Write(ctx context.Context, event Event) error {
	if w.closed.Load() {
		return ErrWriterClosed
	}
	req := appendRequest{
		ctx:   ctx,
		event: event,
		err:   make(chan error, 1),
	}
	select {
	case w.appendCh <- req:
	case <-ctx.Done():
		return oops.Wrap(ctx.Err())
	case <-w.done:
		return ErrWriterClosed
	}
	select {
	case err := <-req.err:
		return err
	case <-ctx.Done():
		return oops.Wrap(ctx.Err())
	}
}

// Append implements the EventAppender interface by delegating to Write.
// This allows EventWriter to be used wherever EventAppender is expected
// (e.g., world.EventStoreAdapter).
func (w *EventWriter) Append(ctx context.Context, event Event) error {
	return w.Write(ctx, event)
}

// Replay delegates to the underlying EventStore.
func (w *EventWriter) Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error) {
	events, err := w.store.Replay(ctx, stream, afterID, limit)
	if err != nil {
		return nil, oops.Wrap(err)
	}
	return events, nil
}

// LastEventID delegates to the underlying EventStore.
func (w *EventWriter) LastEventID(ctx context.Context, stream string) (ulid.ULID, error) {
	id, err := w.store.LastEventID(ctx, stream)
	if err != nil {
		return ulid.ULID{}, oops.Wrap(err)
	}
	return id, nil
}

// ReplayTail delegates to the underlying EventStore.
func (w *EventWriter) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time) ([]Event, error) {
	events, err := w.store.ReplayTail(ctx, stream, count, notBefore)
	if err != nil {
		return nil, oops.Wrap(err)
	}
	return events, nil
}

// SubscribeSession delegates to the underlying EventStore.
func (w *EventWriter) SubscribeSession(ctx context.Context) (Subscription, error) {
	sub, err := w.store.SubscribeSession(ctx)
	if err != nil {
		return nil, oops.Wrap(err)
	}
	return sub, nil
}

// Subscribe delegates to the underlying store's Subscribe method if the
// store implements it. This supports the legacy per-stream subscription
// path used by location-following until it is migrated to SubscribeSession.
func (w *EventWriter) Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, subErr error) {
	type subscriber interface {
		Subscribe(ctx context.Context, stream string) (<-chan ulid.ULID, <-chan error, error)
	}
	if s, ok := w.store.(subscriber); ok {
		ch, ech, err := s.Subscribe(ctx, stream)
		if err != nil {
			return nil, nil, oops.Wrap(err)
		}
		return ch, ech, nil
	}
	return nil, nil, errors.New("underlying store does not support Subscribe")
}

// Compile-time interface check.
var _ EventStore = (*EventWriter)(nil)

// Close shuts down the writer goroutine. Pending writes are drained
// before the goroutine exits.
func (w *EventWriter) Close() {
	w.once.Do(func() {
		w.closed.Store(true)
		close(w.done)
	})
	<-w.stopped
}

func (w *EventWriter) stamp(req *appendRequest) {
	req.event.ID = NewULID()
	req.event.Timestamp = time.Now()
}

func (w *EventWriter) run() {
	defer close(w.stopped)
	for {
		select {
		case req := <-w.appendCh:
			w.stamp(&req)
			req.err <- w.store.Append(req.ctx, req.event)
		case <-w.done:
			// Drain remaining requests.
			for {
				select {
				case req := <-w.appendCh:
					w.stamp(&req)
					req.err <- w.store.Append(req.ctx, req.event)
				default:
					return
				}
			}
		}
	}
}
