// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"reflect"
	"time"

	"github.com/oklog/ulid/v2"
)

// Publisher writes events. Used by the EventSink facade in
// internal/plugin/event_emitter.go after Phase B (F1).
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// IsNilPublisher detects typed-nil interface values whose underlying
// concrete kind is nilable (pointer, slice, map, chan, func, interface).
// Returns false for non-nilable kinds (struct, value-receiver fakes).
//
// A bare `pub == nil` check misses a typed-nil concrete pointer boxed into
// the Publisher interface (e.g. a nil *someConcretePublisher passed as
// Publisher) — the interface value itself is non-nil even though the
// underlying pointer is nil. Callers that want to fail fast at
// construction (rather than nil-deref on first Publish call) MUST check
// both: `pub == nil || eventbus.IsNilPublisher(pub)`.
//
// Shared by internal/presence and internal/sysbroadcast, both of which
// construct Publisher-typed wrappers with this exact construction-time
// guard (07-review IN-01: extracted here instead of duplicating the
// reflect-based check in each package). internal/cluster's isNilConn
// performs the same check for a different interface (natsconn.Conn) and
// stays separate.
func IsNilPublisher(pub Publisher) bool {
	v := reflect.ValueOf(pub)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Subscriber opens long-lived session streams. Used by the gRPC Subscribe
// handler after Phase B (F3).
//
// The identity parameter carries the authenticated principal. Required at the
// API surface — with Crypto.Enabled=false the value is unused but must still
// be supplied. T10 wires the construction at the gRPC authentication boundary.
//
// SessionIdentity is defined in this package (eventbus) to avoid an import
// cycle: eventbus → authguard → plugin → eventbus. Callers with an
// authguard.Identity use authguard.ToSessionIdentity to convert.
type Subscriber interface {
	OpenSession(ctx context.Context, sessionID string, identity SessionIdentity, filters []Subject, minFloor time.Time) (SessionStream, error)
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
	// MetadataOnly reports whether the host's AuthGuard withheld plaintext
	// from this recipient. When true, Event().Payload is empty bytes.
	// The gRPC Subscribe handler reads this and stamps
	// EventFrame.metadata_only on the wire (Phase 3b grounding doc
	// Decision 4). False for identity-codec events and for legitimately
	// empty-payload sensitive events that were authorized.
	MetadataOnly() bool
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

// HistoryQuery describes a paginated history read. Caller identity is
// carried on the `Caller` field below — populated by the host's gRPC
// handler from the authenticated session record. Public-tier readers
// (hot JetStream, cold Postgres) ignore Caller; plugin-owned subject
// routes (PluginHistoryRouter) MUST forward it to the plugin's
// PluginAuditService.QueryHistory for membership enforcement. See
// spec §4.2.
//
// Pagination ordering is by JetStream stream sequence (js_seq), not by
// ULID. Cursors are (seq, id) pairs: AfterSeq/AfterID for forward reads,
// BeforeSeq/BeforeID for backward reads. The id field is a tripwire that
// validates the cursor's seq still names the same event in storage; on
// mismatch the reader returns ErrCursorStale or ErrCursorLag (see
// internal/eventbus/errors.go).
//
// Zero seq means "from the start" (forward) or "from the end" (backward).
// AfterID / BeforeID are required when their corresponding seq is non-zero
// for client-supplied cursors; internal callers MAY leave id zero (then no
// validation is performed).
type HistoryQuery struct {
	Subject Subject

	AfterSeq  uint64    // exclusive lower bound by JS stream seq
	AfterID   ulid.ULID // tripwire for AfterSeq; zero = skip validation
	BeforeSeq uint64    // exclusive upper bound by JS stream seq
	BeforeID  ulid.ULID // tripwire for BeforeSeq; zero = skip validation

	NotBefore time.Time
	NotAfter  time.Time
	Direction Direction
	PageSize  int

	// Caller identifies the principal on whose behalf the read is happening.
	// Populated by the host's gRPC handler from the authenticated session
	// record. Public-tier readers (hot JetStream, cold Postgres) ignore this
	// field; plugin-owned subject routes (PluginHistoryRouter) MUST forward
	// it to the plugin's PluginAuditService.QueryHistory for membership
	// enforcement. See spec §4.2.
	Caller Actor

	// Identity carries the typed authenticated principal for the hot-tier
	// AuthGuard path. Required when hot-tier AuthGuard is wired (T9);
	// zero-value is safe when Crypto.Enabled=false. T10 populates this from
	// the gRPC authentication boundary.
	Identity SessionIdentity
}

// HistoryStream is a server-streaming handle. Caller iterates Next()
// until io.EOF; for next-page resume, the caller records the ULID of the
// last Event returned and passes it as After on the next call.
type HistoryStream interface {
	Next(ctx context.Context) (Event, error)
	Close() error
}
