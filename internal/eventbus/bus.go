// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// Publisher writes events. Used by the EventSink facade in
// internal/plugin/event_emitter.go after Phase B (F1).
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// Subscriber opens long-lived session streams. Used by the gRPC Subscribe
// handler after Phase B (F3).
type Subscriber interface {
	OpenSession(ctx context.Context, sessionID string, filters []Subject) (SessionStream, error)
}

// HistoryReader serves paginated history reads. Used by gRPC QueryHistory
// handler after Phase B (F4).
type HistoryReader interface {
	QueryHistory(ctx context.Context, q HistoryQuery) (HistoryStream, error)
}

// EventBus is the concrete implementation that satisfies all three
// single-responsibility interfaces. Tests SHOULD depend on the narrow
// interface they actually need.
type EventBus interface {
	Publisher
	Subscriber
	HistoryReader
}

// Delivery is a typed handle for a single message in flight from a
// SessionStream. Replaces the prior (Event, AckFunc, error) tuple shape:
// typed handles are easier to mock, log, and extend.
type Delivery interface {
	Event() Event
	Ack() error
	// Nack signals the message should be redelivered. Use for transient
	// handler errors.
	Nack() error
	// InProgress extends the ack-wait timer. Use sparingly for handlers
	// expecting to exceed the default.
	InProgress() error
}

// SessionStream is a consumer-side handle bound to a JS durable consumer.
type SessionStream interface {
	// Next blocks until the next delivery or ctx done.
	Next(ctx context.Context) (Delivery, error)
	// SetFilters atomically replaces the FilterSubjects on the underlying
	// durable consumer. Cursor is preserved by JS UpdateConsumer.
	SetFilters(ctx context.Context, filters []Subject) error
	Close() error
}

// HistoryQuery describes a paginated history read. Auth flows via
// context.Context (auth.WithSession), not via this struct.
type HistoryQuery struct {
	Subject   Subject   // exact subject OR pattern with * / >
	After     ulid.ULID // exclusive lower bound; zero ULID = from start
	Before    ulid.ULID // exclusive upper bound; zero ULID = unbounded
	NotBefore time.Time // optional time bound
	NotAfter  time.Time // optional time bound
	Direction Direction
	PageSize  int // host caps at 200; default 50
}

// HistoryStream is a server-streaming handle. Caller iterates Next()
// until io.EOF; for next-page resume, the caller records the ULID of the
// last Event returned and passes it as After on the next call.
type HistoryStream interface {
	Next(ctx context.Context) (Event, error)
	Close() error
}
