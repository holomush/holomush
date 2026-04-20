// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package history serves QueryHistory requests by transparently crossing the
// JetStream / PostgreSQL audit retention boundary.
//
// Two tiers back every query:
//
//   - Hot (JetStream): events newer than the retention edge are still on the
//     JS stream. Served via an ordered ephemeral consumer with FilterSubject.
//   - Cold (PostgreSQL events_audit): every published event is projected into
//     events_audit (by internal/eventbus/audit). The "forever" archive.
//
// The Reader routes each query to the correct starting tier based on the
// cursor / bounds, then transparently crosses into the other tier when a page
// spans the retention edge. De-duplication at the boundary is by ULID —
// events newly archived are still within the JS retention window for up to
// `safetyMargin`, so either tier may legitimately return them during that
// overlap.
//
// Plugin-owned subjects route through the optional PluginHistoryRouter. When
// the manifest owner map says a subject belongs to a plugin (F5), the Reader
// does NOT query either tier locally; the plugin serves its own history.
// Without a router wired (F4 ship state), plugin-owned subjects return an
// error so a caller cannot silently be served an empty result.
package history

import (
	"context"
	"io"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/codec"
)

// DefaultSafetyMargin is subtracted from StreamMaxAge when computing the
// retention edge. Spec §5: 1 hour. Events with Timestamp >= edge are in the
// hot tier; older events are only in cold.
const DefaultSafetyMargin = time.Hour

// DefaultPageSize is the effective PageSize when the caller supplies 0.
// Mirrors the existing gRPC handler's default.
const DefaultPageSize = 50

// MaxPageSize caps the per-call PageSize host-side (spec §5).
const MaxPageSize = 200

// maxBufferMultiple is the runaway-loop guardrail for the crossover stream.
// The stream's internal buffer should never hold more than two full pages
// across both tiers (the worst legitimate case is one page per tier under
// crossover). 10× provides generous slack for test fixtures that inject
// artificially large tier responses while still tripping on a cursor-not-
// advanced regression before memory pressure becomes pathological.
const maxBufferMultiple = 10

// Tier identifies which backing store served or should serve a slice of
// history. Exported for observability (metrics, logs).
type Tier uint8

const (
	// TierJetStream is the hot tier — events still within JS retention.
	TierJetStream Tier = 1
	// TierPostgres is the cold tier — events_audit table.
	TierPostgres Tier = 2
)

// PluginHistoryRouter delegates QueryHistory to the plugin that owns the
// subject. F5 wires a PluginAuditService-backed implementation; F4 ships
// with a nil router, which surfaces as an explicit error for plugin-owned
// subjects rather than silently serving host-side data.
type PluginHistoryRouter interface {
	// QueryHistory asks the plugin to stream history for q.Subject. The
	// plugin performs domain authz, reads its own audit schema, and streams
	// back through the same HistoryStream contract.
	QueryHistory(ctx context.Context, pluginName string, q eventbus.HistoryQuery) (eventbus.HistoryStream, error)
}

// Option tunes NewReader.
type Option func(*Reader)

// WithSafetyMargin overrides the default 1h safety margin used to compute
// the retention edge.
func WithSafetyMargin(d time.Duration) Option {
	return func(r *Reader) {
		if d > 0 {
			r.safetyMargin = d
		}
	}
}

// WithOwners wires the subject-ownership map. Nil map ⇒ host owns every
// subject (Phase A default; Phase B Reader shares the same OwnerMap as the
// audit projection).
func WithOwners(owners *audit.OwnerMap) Option {
	return func(r *Reader) { r.owners = owners }
}

// WithPluginRouter wires the plugin-audit router for plugin-owned subjects.
// Unwired, plugin-owned subjects return EVENTBUS_PLUGIN_HISTORY_NOT_WIRED.
func WithPluginRouter(router PluginHistoryRouter) Option {
	return func(r *Reader) { r.router = router }
}

// WithCodecSelector injects the KeySelector used to decrypt JS payloads on
// read. Mirrors eventbus.WithSubscriberCodecSelector. Unset deployments
// decode identity-codec payloads (the default).
func WithCodecSelector(sel codec.KeySelector) Option {
	return func(r *Reader) { r.selector = sel }
}

// WithClock overrides the wall clock used to compute the retention edge.
// Tests MUST inject this — wall-clock use is banned in the package tree.
func WithClock(now func() time.Time) Option {
	return func(r *Reader) {
		if now != nil {
			r.now = now
		}
	}
}

// WithHotTier replaces the JetStream-backed hot tier with a custom
// implementation. Primarily for unit tests that want to drive the Reader
// without an embedded NATS server.
func WithHotTier(h HotTier) Option {
	return func(r *Reader) { r.hot = h }
}

// WithColdTier replaces the PostgreSQL-backed cold tier with a custom
// implementation. Primarily for unit tests that want to drive the Reader
// without a Postgres instance.
func WithColdTier(c ColdTier) Option {
	return func(r *Reader) { r.cold = c }
}

// HotTier reads the JetStream-hosted recent slice of history. Exported so
// tests and alternate storage backends can plug in.
type HotTier interface {
	// Read returns up to pageSize events matching q, constrained to events
	// with Timestamp >= edge. Events are returned in q.Direction order.
	Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int) ([]eventbus.Event, error)
}

// ColdTier reads the events_audit-hosted archive slice of history. Exported
// so tests can plug in.
type ColdTier interface {
	// Read returns up to pageSize events matching q, constrained to events
	// with Timestamp < edge when crossover applies. When edge is the zero
	// value, no tier-boundary constraint is applied and the full archive is
	// queried.
	Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int) ([]eventbus.Event, error)
}

// Reader implements eventbus.HistoryReader by routing queries between
// JetStream (recent tail) and PostgreSQL events_audit (forever archive).
//
// The Reader is the public entry point — the gRPC QueryStreamHistory handler
// and plugin host RPCs call Reader.QueryHistory.
type Reader struct {
	js           jetstream.JetStream
	pool         *pgxpool.Pool
	streamMaxAge time.Duration
	safetyMargin time.Duration
	now          func() time.Time
	owners       *audit.OwnerMap
	router       PluginHistoryRouter
	selector     codec.KeySelector

	// hot / cold are overridable so tests can substitute deterministic
	// fakes. In production NewReader builds the JS-backed and
	// Postgres-backed implementations.
	hot  HotTier
	cold ColdTier
}

// NewReader constructs a Reader. `js` and `pool` MAY be nil if the caller
// supplies replacements via WithHotTier / WithColdTier (tests). Production
// code passes both non-nil; the default hot tier is JetStream-backed and the
// default cold tier is the events_audit table.
//
// streamMaxAge MUST match the JetStream EVENTS stream's MaxAge (i.e.,
// eventbus.Config.StreamMaxAge). Mismatched values misalign the retention
// edge and may cause spurious "not found" gaps at the boundary.
//
// `now` MUST be non-nil — wall-clock access is banned in this package.
func NewReader(js jetstream.JetStream, pool *pgxpool.Pool, streamMaxAge time.Duration, now func() time.Time, opts ...Option) *Reader {
	if now == nil {
		// Fall back to time.Now so a nil `now` can't silently return a
		// zero time from every edge computation. Callers that care about
		// forbidigo compliance in tests MUST pass a non-nil clock.
		now = time.Now
	}
	r := &Reader{
		js:           js,
		pool:         pool,
		streamMaxAge: streamMaxAge,
		safetyMargin: DefaultSafetyMargin,
		now:          now,
	}
	for _, o := range opts {
		o(r)
	}
	// Install default tier implementations only when the caller did not
	// inject a test fake AND the backing resource is non-nil. Leaving one
	// tier nil when its resource is nil is legitimate — e.g. a pure-unit
	// test that only exercises cold-side logic.
	if r.hot == nil && r.js != nil {
		r.hot = newJetStreamHotTier(r.js, r.selector, r.now)
	}
	if r.cold == nil && r.pool != nil {
		r.cold = newPostgresColdTier(r.pool)
	}
	return r
}

// QueryHistory implements eventbus.HistoryReader. See package doc for the
// crossover algorithm and spec §5 for the authoritative contract.
func (r *Reader) QueryHistory(ctx context.Context, q eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	if err := validateQuery(q); err != nil {
		return nil, err
	}
	q.PageSize = ClampPageSize(q.PageSize)

	// Plugin-owned subjects short-circuit to the router. Per spec §5,
	// plugin-owned subjects NEVER fall back to host storage — a missing
	// router is an operational error, not silently served from the host.
	if r.owners != nil {
		owner := r.owners.Resolve(string(q.Subject))
		if owner.PluginName != "" {
			if r.router == nil {
				return nil, oops.Code("EVENTBUS_PLUGIN_HISTORY_NOT_WIRED").
					With("subject", string(q.Subject)).
					With("plugin", owner.PluginName).
					Errorf("plugin-owned subject requires PluginHistoryRouter")
			}
			//nolint:wrapcheck // forwarding plugin RPC error to caller
			return r.router.QueryHistory(ctx, owner.PluginName, q)
		}
	}

	now := r.now()
	edge := now.Add(-r.streamMaxAge).Add(r.safetyMargin)
	startTier := selectStartTier(q, edge, now)

	return newCrossoverStream(ctx, r.hot, r.cold, q, edge, startTier), nil
}

// validateQuery enforces the invariants that are free to check before
// touching any backing store.
func validateQuery(q eventbus.HistoryQuery) error {
	if q.Subject == "" {
		return oops.Code("EVENTBUS_HISTORY_SUBJECT_REQUIRED").
			Errorf("HistoryQuery.Subject required")
	}
	// NotBefore / NotAfter ordering: if both non-zero, NotBefore <= NotAfter.
	// Zero values are "unbounded" sentinels.
	if !q.NotBefore.IsZero() && !q.NotAfter.IsZero() && q.NotBefore.After(q.NotAfter) {
		return oops.Code("EVENTBUS_HISTORY_INVALID_TIME_RANGE").
			With("not_before", q.NotBefore).
			With("not_after", q.NotAfter).
			Wrap(eventbus.ErrInvalidTimeRange)
	}
	switch q.Direction {
	case 0, eventbus.DirectionForward, eventbus.DirectionBackward:
		return nil
	default:
		return oops.Code("EVENTBUS_HISTORY_INVALID_DIRECTION").
			With("direction", q.Direction).
			Errorf("invalid Direction")
	}
}

// selectStartTier decides which tier services the first page.
//
// Rules (spec §5, with the cursor-ULID refinement):
//   - If After is set, decode its timestamp and pick the tier that holds it.
//   - Else (zero ULID cursor): use the time bound that points away from "now"
//     to decide. Forward ⇒ NotBefore picks the oldest endpoint to start
//     from; Backward ⇒ NotAfter picks the newest endpoint.
//   - Ties at the exact edge resolve to JS (hot). This is intentional: when
//     the edge is exactly observed, JS still has the data and PG may not yet
//     because the audit projection lags by up to ~safetyMargin.
func selectStartTier(q eventbus.HistoryQuery, edge, now time.Time) Tier {
	if !q.After.IsZero() {
		ts := ulid.Time(q.After.Time())
		if ts.Before(edge) {
			return TierPostgres
		}
		return TierJetStream
	}
	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionForward {
		if q.NotBefore.IsZero() {
			// Unbounded oldest ⇒ start in cold (events_audit has the full
			// archive; JS alone would miss older-than-retention events).
			return TierPostgres
		}
		if q.NotBefore.Before(edge) {
			return TierPostgres
		}
		return TierJetStream
	}
	// Backward
	if q.NotAfter.IsZero() {
		// Unbounded newest ⇒ "start from now" ⇒ hot.
		if now.Before(edge) {
			// Pathological: clock earlier than edge means the stream is
			// effectively empty of hot data. Fall back to cold.
			return TierPostgres
		}
		return TierJetStream
	}
	if q.NotAfter.Before(edge) {
		return TierPostgres
	}
	return TierJetStream
}

// ClampPageSize normalizes a caller-supplied PageSize into [1, MaxPageSize].
// Zero defaults to DefaultPageSize per spec §5.
func ClampPageSize(requested int) int {
	if requested <= 0 {
		return DefaultPageSize
	}
	if requested > MaxPageSize {
		return MaxPageSize
	}
	return requested
}

// ---------------------------------------------------------------------------
// Crossover iterator.
// ---------------------------------------------------------------------------

// crossoverStream is the HistoryStream implementation returned by Reader.
// It lazily reads the start tier, then transparently continues from the
// other tier when the first tier is exhausted AND the page budget remains.
type crossoverStream struct {
	ctx       context.Context //nolint:containedctx // server-streaming iterator; ctx is bound to the RPC lifecycle
	hot       HotTier
	cold      ColdTier
	query     eventbus.HistoryQuery
	edge      time.Time
	startTier Tier

	// Loaded in chunks; Next() pops the head.
	buf []eventbus.Event
	pos int

	// crossoverDone gates a single tier transition per stream: we never
	// thrash back and forth.
	crossoverDone bool
	// Set once the stream has no more events to emit.
	exhausted bool

	// seenIDs deduplicates by ULID across the boundary. At the edge, events
	// that were recently projected into PG are still within JS retention,
	// so a page drawn straddling both tiers can legitimately observe the
	// same ULID twice.
	seenIDs map[ulid.ULID]struct{}
}

func newCrossoverStream(ctx context.Context, hot HotTier, cold ColdTier, q eventbus.HistoryQuery, edge time.Time, startTier Tier) *crossoverStream {
	return &crossoverStream{
		ctx:       ctx,
		hot:       hot,
		cold:      cold,
		query:     q,
		edge:      edge,
		startTier: startTier,
		seenIDs:   make(map[ulid.ULID]struct{}),
	}
}

// Next implements eventbus.HistoryStream.Next. Returns io.EOF when the query
// is exhausted in both tiers. Honours the caller's ctx so per-read
// deadlines/cancellation reach hot.Read / cold.Read; the stream's own ctx
// (bound at QueryHistory time) is used only as the parent for lifecycle
// cancellation, not as the per-read deadline.
func (s *crossoverStream) Next(ctx context.Context) (eventbus.Event, error) {
	for {
		if s.pos < len(s.buf) {
			e := s.buf[s.pos]
			s.pos++
			if _, dup := s.seenIDs[e.ID]; dup {
				continue
			}
			s.seenIDs[e.ID] = struct{}{}
			return e, nil
		}
		if s.exhausted {
			return eventbus.Event{}, io.EOF
		}
		if err := s.loadNextPage(ctx); err != nil {
			return eventbus.Event{}, err
		}
	}
}

// loadNextPage reads the start tier; if that tier returns fewer than the
// page size AND the crossover has not yet happened, it reads the other tier
// to continue. The result is concatenated into s.buf in the caller's
// direction order.
func (s *crossoverStream) loadNextPage(ctx context.Context) error {
	want := s.query.PageSize
	if want <= 0 {
		want = DefaultPageSize
	}

	var firstTier, second Tier
	if s.crossoverDone {
		firstTier = otherTier(s.startTier)
	} else {
		firstTier = s.startTier
		second = otherTier(s.startTier)
	}

	first, err := s.readTier(ctx, firstTier, want)
	if err != nil {
		return err
	}
	s.appendOrdered(first)
	s.advanceCursor(first)
	if bufCap := want * maxBufferMultiple; bufCap > 0 && len(s.buf) > bufCap {
		return oops.Code("EVENTBUS_HISTORY_BUFFER_OVERFLOW").
			With("page_size", want).
			With("buffer_len", len(s.buf)).
			With("subject", string(s.query.Subject)).
			Errorf("crossover buffer exceeded %d× page size; cursor advancement likely broken", maxBufferMultiple)
	}

	if len(first) < want && !s.crossoverDone {
		remaining := want - len(first)
		if remaining < 0 {
			remaining = 0
		}
		s.crossoverDone = true
		if remaining > 0 && s.canReadTier(second) && s.secondTierInWindow(second) {
			extra, err := s.readTier(ctx, second, remaining)
			if err != nil {
				return err
			}
			s.appendOrdered(extra)
			s.advanceCursor(extra)
			// A short read on the second tier means both tiers are drained
			// for this query window. Marking exhausted here avoids a
			// redundant probe-read on the next Next() call.
			if len(extra) < remaining {
				s.exhausted = true
			}
		} else {
			s.exhausted = true
		}
		return nil
	}
	// Past crossover, the primary tier is the one that didn't start; a short
	// read there also drains the query.
	if s.crossoverDone && len(first) < want {
		s.exhausted = true
	}
	if len(first) == 0 {
		s.exhausted = true
	}
	return nil
}

// advanceCursor updates the query's cursor bound so the next tier read
// picks up where this one left off. Forward direction advances After;
// backward advances Before. Without this, loadNextPage would re-read the
// same page indefinitely (dedup at Next hides the events but s.buf and
// the read-loop still grow without bound — an O(n) memory leak per call).
func (s *crossoverStream) advanceCursor(events []eventbus.Event) {
	if len(events) == 0 {
		return
	}
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	last := events[len(events)-1].ID
	if dir == eventbus.DirectionForward {
		s.query.After = last
	} else {
		s.query.Before = last
	}
}

// secondTierInWindow reports whether the second tier can hold events that
// intersect the query's time window. Crossover is skipped when the second
// tier's retention range falls outside the query bounds — e.g. a
// forward-from-JS query whose NotBefore is already past the edge can't
// usefully cross into cold (which only holds events with Timestamp < edge).
// This short-circuit saves one futile tier read per boundary.
func (s *crossoverStream) secondTierInWindow(t Tier) bool {
	switch t {
	case TierJetStream:
		// JS holds events with Timestamp >= edge. Skip if the query's
		// upper bound is strictly older than edge.
		if !s.query.NotAfter.IsZero() && s.query.NotAfter.Before(s.edge) {
			return false
		}
	case TierPostgres:
		// PG audit holds events with Timestamp < edge. Skip if the query's
		// lower bound is already at or past edge.
		if !s.query.NotBefore.IsZero() && !s.query.NotBefore.Before(s.edge) {
			return false
		}
	}
	return true
}

// canReadTier reports whether the second tier is wired. A nil second tier
// (e.g., test fixture without PG) is treated as "no more data" rather than
// an error — the Reader is designed so a single-tier test is still valid.
func (s *crossoverStream) canReadTier(t Tier) bool {
	switch t {
	case TierJetStream:
		return s.hot != nil
	case TierPostgres:
		return s.cold != nil
	default:
		return false
	}
}

func (s *crossoverStream) readTier(ctx context.Context, t Tier, pageSize int) ([]eventbus.Event, error) {
	// Respect caller's Next ctx (per-read deadline/cancellation). Fall back
	// to the stream's lifecycle ctx if the caller passed a nil/background
	// ctx — neither should happen in production but defensive handling
	// keeps the previous single-ctx semantics available.
	readCtx := ctx
	if readCtx == nil {
		readCtx = s.ctx
	}
	switch t {
	case TierJetStream:
		if s.hot == nil {
			return nil, nil
		}
		events, err := s.hot.Read(readCtx, s.query, s.edge, pageSize)
		if err != nil {
			return nil, oops.Code("EVENTBUS_HISTORY_HOT_READ_FAILED").
				With("subject", string(s.query.Subject)).
				Wrap(err)
		}
		return events, nil
	case TierPostgres:
		if s.cold == nil {
			return nil, nil
		}
		events, err := s.cold.Read(readCtx, s.query, s.edge, pageSize)
		if err != nil {
			return nil, oops.Code("EVENTBUS_HISTORY_COLD_READ_FAILED").
				With("subject", string(s.query.Subject)).
				Wrap(err)
		}
		return events, nil
	default:
		return nil, nil
	}
}

// appendOrdered appends events to the buffer in the direction the query
// asked for. Each tier is expected to return events already in the requested
// order; appendOrdered re-sorts the tail (unread region) to keep a stable
// merge when the two tiers overlap across the edge.
func (s *crossoverStream) appendOrdered(events []eventbus.Event) {
	if len(events) == 0 {
		return
	}
	s.buf = append(s.buf, events...)
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	tail := s.buf[s.pos:]
	sort.SliceStable(tail, func(i, j int) bool {
		if dir == eventbus.DirectionBackward {
			return tail[i].ID.Compare(tail[j].ID) > 0
		}
		return tail[i].ID.Compare(tail[j].ID) < 0
	})
}

// Close implements eventbus.HistoryStream. The iterator owns no server-side
// resources: ephemeral JS consumers are scoped to a single Read() call and
// released when HotTier.Read returns. Close is idempotent.
func (s *crossoverStream) Close() error {
	s.exhausted = true
	s.buf = nil
	return nil
}

func otherTier(t Tier) Tier {
	if t == TierJetStream {
		return TierPostgres
	}
	return TierJetStream
}

// compile-time assertion that *crossoverStream satisfies the eventbus
// HistoryStream contract.
var _ eventbus.HistoryStream = (*crossoverStream)(nil)
