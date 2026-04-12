// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// ErrStreamEmpty is returned when a stream has no events.
var ErrStreamEmpty = errors.New("stream is empty")

// EventStore persists and retrieves events.
type EventStore interface {
	// Append persists an event to a stream.
	Append(ctx context.Context, event Event) error

	// Replay returns up to limit events from a stream, starting after afterID.
	// If afterID is zero ULID, starts from beginning.
	Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]Event, error)

	// LastEventID returns the most recent event ID for a stream.
	LastEventID(ctx context.Context, stream string) (ulid.ULID, error)

	// Subscribe starts listening for new events on the given stream.
	// Returns a channel of event IDs and an error channel.
	// The caller should use Replay() to fetch full events by ID.
	// Channels are closed when context is cancelled.
	Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error)

	// ReplayTail returns up to count most recent events on stream,
	// ordered ascending by event ID. If notBefore is non-zero, events
	// with timestamps before it are excluded. Count is capped server-side
	// at 500. Used by FocusKindPolicy implementations for bounded-tail
	// reads and by QueryStreamHistory for client scrollback.
	ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time) ([]Event, error)

	// SubscribeSession opens a new session-wide subscription on a dedicated
	// pgx.Conn. The returned Subscription supports dynamic add/remove of
	// streams while preserving strict commit-order delivery across all
	// currently-subscribed streams (invariant I-14).
	//
	// This REPLACES the per-stream Subscribe(ctx, stream) method. All
	// callers that subscribe to multiple streams for a single logical
	// session MUST use a single Subscription to preserve I-14.
	SubscribeSession(ctx context.Context) (Subscription, error)
}

// Subscription is a session-wide stream subscription on a dedicated PG
// connection. Notifications for all streams added to this subscription
// are delivered in strict PostgreSQL commit order via a single Go channel.
//
// Invariant I-14 depends on this: strict commit-order delivery across
// streams is only preserved if a session uses ONE Subscription for all
// its event stream needs.
type Subscription interface {
	// AddStream begins listening on a new stream. Idempotent. Notifications
	// for the stream begin arriving on Notifications() immediately;
	// ordering with other streams is guaranteed by PG.
	AddStream(ctx context.Context, stream string) error

	// RemoveStream stops listening on a stream. Notifications already
	// queued in the connection's buffer MAY still be delivered.
	RemoveStream(ctx context.Context, stream string) error

	// Notifications returns the single channel of notifications, delivered
	// in strict PG commit order across all added streams.
	Notifications() <-chan StreamNotification

	// Errors returns an error channel for unrecoverable failures.
	Errors() <-chan error

	// Close releases the underlying PG connection. Must be called when the
	// Subscribe RPC handler exits.
	Close() error
}

// StreamNotification is a single PG NOTIFY event relayed from the
// subscription's multi-LISTEN connection.
type StreamNotification struct {
	Stream  string
	EventID ulid.ULID
}
