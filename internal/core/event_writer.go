// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// ErrWriterClosed is returned when Write is called after Close.
var ErrWriterClosed = errors.New("event writer is closed")

// BusFanoutFunc is the side-channel the EventWriter invokes after a
// successful EventStore.Append. Post-F3 the gRPC Subscribe handler reads
// deliveries from the JetStream EventBus, so host-originated engine events
// (say/pose/move/session_ended/etc.) MUST also be published to the bus or
// subscribers never see them. F6 drops the PG events table entirely, at
// which point Append goes away and the bus is the sole write path; until
// then we dual-write.
//
// Errors from the fanout are logged by the caller (via slog) but do NOT
// fail the Write — the authoritative store is still PG.
type BusFanoutFunc func(ctx context.Context, event Event) error

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
// Post-F3 the writer also fans each event out to the JetStream bus via the
// optional BusFanout side-channel. This is transitional — F6 drops PG and
// the bus becomes the only write path.
//
// Throughput: ~1000 events/second (bounded by single-INSERT latency).
// For a MUSH with ~200 concurrent players, this is ~100x headroom.
type EventWriter struct {
	store     EventStore
	busFanout BusFanoutFunc
	appendCh  chan appendRequest
	done      chan struct{}
	stopped   chan struct{}
	closed    atomic.Bool
	once      sync.Once
	enqueueMu sync.Mutex // serializes closed-check + channel-send in Write vs Close
	fanoutMu  sync.RWMutex
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
	req, err := w.enqueue(ctx, event)
	if err != nil {
		return err
	}
	select {
	case err := <-req.err:
		return err
	case <-ctx.Done():
		return oops.Wrap(ctx.Err())
	}
}

// enqueue atomically checks that the writer is open and sends the request
// to the worker goroutine. enqueueMu serializes this with Close() to
// prevent a write from blocking on appendCh after the worker has exited.
func (w *EventWriter) enqueue(ctx context.Context, event Event) (appendRequest, error) {
	w.enqueueMu.Lock()
	defer w.enqueueMu.Unlock()

	if w.closed.Load() {
		return appendRequest{}, ErrWriterClosed
	}
	req := appendRequest{
		ctx:   ctx,
		event: event,
		err:   make(chan error, 1),
	}
	select {
	case w.appendCh <- req:
		return req, nil
	case <-ctx.Done():
		return appendRequest{}, oops.Wrap(ctx.Err())
	case <-w.done:
		return appendRequest{}, ErrWriterClosed
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
func (w *EventWriter) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]Event, error) {
	events, err := w.store.ReplayTail(ctx, stream, count, notBefore, beforeID)
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

// Subscribe delegates the legacy per-stream subscription to the underlying
// store. This is a transitional pass-through: the server's Subscribe handler
// still uses the per-stream path via the legacySubscriber interface until B7
// (Subscribe handler refactor) replaces it with SubscribeSession.
//
// TODO(B7): Remove this method when the Subscribe handler is rewritten to
// use SubscribeSession exclusively.
func (w *EventWriter) Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error) {
	type subscriber interface {
		Subscribe(ctx context.Context, stream string) (<-chan ulid.ULID, <-chan error, error)
	}
	if s, ok := w.store.(subscriber); ok {
		eventCh, errCh, err = s.Subscribe(ctx, stream)
		if err != nil {
			return nil, nil, oops.With("stream", stream).Wrap(err)
		}
		return eventCh, errCh, nil
	}
	return nil, nil, oops.Errorf("underlying store does not support legacy Subscribe")
}

// Compile-time interface check.
var _ EventStore = (*EventWriter)(nil)

// Close shuts down the writer goroutine. Pending writes are drained
// before the goroutine exits.
func (w *EventWriter) Close() {
	w.once.Do(func() {
		w.enqueueMu.Lock()
		w.closed.Store(true)
		w.enqueueMu.Unlock()
		close(w.done)
	})
	<-w.stopped
}

func (w *EventWriter) stamp(req *appendRequest) {
	req.event.ID = NewULID()
	req.event.Timestamp = time.Now()
}

// SetBusFanout installs (or replaces) the bus fanout side-channel. Safe
// to call concurrently with in-flight Appends. Pass nil to disable.
func (w *EventWriter) SetBusFanout(f BusFanoutFunc) {
	w.fanoutMu.Lock()
	w.busFanout = f
	w.fanoutMu.Unlock()
}

// appendAndFanout writes the event, then fires the bus fanout side-channel.
//
// Pre-F6: PG is authoritative. Append to PG first; on success, fan out to the
// bus. Bus failures are logged but do not fail the Write.
//
// Post-F6 (bus fanout is wired, PG events table is dropped): the bus fanout is
// the authoritative write path. PG Append is attempted for backward compat but
// its failure is ignored — only the bus result is returned to the caller.
// F7 removes the PG leg entirely.
func (w *EventWriter) appendAndFanout(req appendRequest) {
	w.fanoutMu.RLock()
	f := w.busFanout
	w.fanoutMu.RUnlock()

	if f != nil {
		// F6 mode: bus is authoritative. Attempt PG write first (best-effort),
		// then publish to bus. Bus result is returned to caller.
		pgErr := w.store.Append(req.ctx, req.event)
		if pgErr != nil {
			slog.WarnContext(req.ctx, "event writer: PG append failed (events table dropped?) — proceeding with bus-only write",
				"event_id", req.event.ID.String(),
				"stream", req.event.Stream,
				"type", string(req.event.Type),
				"error", pgErr,
			)
		}
		busErr := f(req.ctx, req.event)
		req.err <- busErr
		return
	}

	// Pre-F6 mode: PG is authoritative, no bus fanout.
	req.err <- w.store.Append(req.ctx, req.event)
}

func (w *EventWriter) run() {
	defer close(w.stopped)
	for {
		select {
		case req := <-w.appendCh:
			w.stamp(&req)
			w.appendAndFanout(req)
		case <-w.done:
			// Drain remaining requests.
			for {
				select {
				case req := <-w.appendCh:
					w.stamp(&req)
					w.appendAndFanout(req)
				default:
					return
				}
			}
		}
	}
}
