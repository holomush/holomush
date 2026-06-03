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
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/history/source"
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

// WithPluginDowngradeFence wires the Phase 7 read-side fence around the
// inner PluginHistoryRouter. The fence applies INV-CRYPTO-42 (manifest-set
// heuristic) and INV-CRYPTO-50 (DEK existence) checks before forwarding rows
// to the caller; refusals surface as per-row metadata_only=true.
//
// Production wiring at cmd/holomush/core.go (Task E.3 / bead 1r0v.5)
// supplies the always-sensitive set (built once at boot per INV-CRYPTO-44),
// the crypto_keys lookup, and the violation emitter. Until then this
// option is exposed for integration tests; an unset fence preserves
// pre-Phase-7 behaviour (router output passes through unfenced).
//
// Multiple WithPluginDowngradeFence calls overwrite the captured options;
// last-writer-wins.
func WithPluginDowngradeFence(
	alwaysSensitive map[string]struct{},
	lookup CryptoKeysLookup,
	emitter ViolationEmitter,
) Option {
	return WithPluginDowngradeFenceReadback(alwaysSensitive, lookup, emitter, nil, nil, nil)
}

// WithPluginDowngradeFenceReadback is WithPluginDowngradeFence plus the
// read-back crypto capabilities (guard / dek / audit) that let the fence
// DECRYPT a clean plugin-owned row for an authorized routed participant
// (INV-CRYPTO-32). A nil guard preserves the pre-T8 ciphertext-passthrough
// behaviour on clean rows (Crypto.Enabled=false deployments).
//
// The caller's CHARACTER identity is forwarded on HistoryQuery.Identity, so
// the fence's clean-row decrypt routes to the checkCharacter DEK-membership
// branch (ReadBack=false) rather than the plugin-readback path.
func WithPluginDowngradeFenceReadback(
	alwaysSensitive map[string]struct{},
	lookup CryptoKeysLookup,
	emitter ViolationEmitter,
	guard eventbus.SessionAuthGuard,
	dek eventbus.SessionDEKManager,
	auditEm eventbus.SessionAuditEmitter,
) Option {
	// Copy at capture-time so post-NewReader mutation by the caller cannot
	// alter the fence's refusal surface (INV-CRYPTO-44 "built once at boot"
	// applies even though fence construction is lazy in fencedRouter).
	copied := make(map[string]struct{}, len(alwaysSensitive))
	for k := range alwaysSensitive {
		copied[k] = struct{}{}
	}
	return func(r *Reader) {
		r.fenceOpts = []PluginDowngradeFenceOption{
			WithAlwaysSensitiveTypes(copied),
			WithCryptoKeysLookup(lookup),
			WithViolationEmitter(emitter),
			WithFenceReadbackCrypto(guard, dek, auditEm),
		}
	}
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

// WithCryptoCold forwards ColdTierOption values to the default
// PostgreSQL cold tier when NewReader builds it. Use this to wire
// AuthGuard / DEKManager / DecryptAuditEmitter into production cold
// reads via the public Reader API:
//
//	history.NewReader(js, pool, max, now,
//	    history.WithCryptoCold(
//	        history.WithColdHistoryAuthGuard(g),
//	        history.WithColdHistoryDEKManager(m),
//	        history.WithColdHistoryDecryptAuditEmitter(em),
//	    ),
//	)
//
// No-op when the caller supplies WithColdTier (the test-fake path) —
// the injected ColdTier owns its own option wiring. Multiple
// WithCryptoCold calls accumulate (last-writer-wins semantics inherited
// from the underlying ColdTierOption setters).
func WithCryptoCold(opts ...ColdTierOption) Option {
	return func(r *Reader) { r.coldOpts = append(r.coldOpts, opts...) }
}

// WithCryptoHot forwards HotTierOption values to the default
// JetStream hot tier when NewReader builds it. Mirrors WithCryptoCold
// for hot/cold parity:
//
//	history.NewReader(js, pool, max, now,
//	    history.WithCryptoHot(
//	        history.WithHistoryAuthGuard(g),
//	        history.WithHistoryDEKManager(m),
//	        history.WithHistoryDecryptAuditEmitter(em),
//	    ),
//	)
//
// No-op when the caller supplies WithHotTier (the test-fake path) —
// the injected HotTier owns its own option wiring. Multiple
// WithCryptoHot calls accumulate (last-writer-wins semantics inherited
// from the underlying HotTierOption setters).
func WithCryptoHot(opts ...HotTierOption) Option {
	return func(r *Reader) { r.hotOpts = append(r.hotOpts, opts...) }
}

// WithHistoryAuth wires AuthGuard + DEKManager + DecryptAuditEmitter
// into BOTH hot and cold tiers. This is the common case — production
// and tests always configure tiers symmetrically. Equivalent to
// calling WithCryptoHot and WithCryptoCold with the matching
// per-tier option constructors.
func WithHistoryAuth(
	g eventbus.SessionAuthGuard,
	m eventbus.SessionDEKManager,
	em eventbus.SessionAuditEmitter,
) Option {
	return func(r *Reader) {
		r.hotOpts = append(
			r.hotOpts,
			WithHistoryAuthGuard(g),
			WithHistoryDEKManager(m),
			WithHistoryDecryptAuditEmitter(em),
		)
		r.coldOpts = append(
			r.coldOpts,
			WithColdHistoryAuthGuard(g),
			WithColdHistoryDEKManager(m),
			WithColdHistoryDecryptAuditEmitter(em),
		)
	}
}

// WithHistoryAuthAndSourceResolver wires AuthGuard + DecryptAuditEmitter
// into both tiers PLUS a per-tier source.SourceResolver. The hot tier
// receives `hotResolver` (typically a *source.FallbackResolver wired to a
// cold-tier LookupByID seam, enabling INV-CRYPTO-22 hot→cold-tier fallback). The
// cold tier receives `coldResolver` (typically a *source.SimpleResolver —
// fallback on cold reads would recurse since cold IS the fallback target).
//
// DEKManager is also wired on both tiers as a backstop: the resolver-aware
// dispatcher path replaces the legacy dekMgr seam at runtime, but a nil
// resolver causes the dispatcher to fall back to the legacy path, so a
// DEKManager remains required for that branch.
//
// Sub-epic E T44 production wiring (holomush-jxo8.7.44).
func WithHistoryAuthAndSourceResolver(
	g eventbus.SessionAuthGuard,
	m eventbus.SessionDEKManager,
	em eventbus.SessionAuditEmitter,
	hotResolver, coldResolver source.SourceResolver,
) Option {
	return func(r *Reader) {
		r.hotOpts = append(
			r.hotOpts,
			WithHistoryAuthGuard(g),
			WithHistoryDEKManager(m),
			WithHistoryDecryptAuditEmitter(em),
			WithHistorySourceResolver(hotResolver),
		)
		r.coldOpts = append(
			r.coldOpts,
			WithColdHistoryAuthGuard(g),
			WithColdHistoryDEKManager(m),
			WithColdHistoryDecryptAuditEmitter(em),
			WithColdHistorySourceResolver(coldResolver),
		)
	}
}

// HotTier reads the JetStream-hosted recent slice of history. Exported so
// tests and alternate storage backends can plug in.
type HotTier interface {
	// Read returns up to pageSize events matching q, constrained to events
	// with Timestamp >= edge. Events are returned in q.Direction order.
	// snap is a per-QueryHistory-call cache of Stream.Info().State; it MAY
	// be nil (e.g. in unit tests that bypass the hot tier).
	Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int, snap *StreamStateSnapshot) ([]eventbus.Event, error)
}

// ColdTier reads the events_audit-hosted archive slice of history. Exported
// so tests can plug in.
type ColdTier interface {
	// Read returns up to pageSize events matching q, constrained to events
	// with Timestamp < edge when crossover applies. When edge is the zero
	// value, no tier-boundary constraint is applied and the full archive is
	// queried.
	// snap is a per-QueryHistory-call cache of Stream.Info().State; it MAY
	// be nil (e.g. in unit tests that bypass the cold tier). When non-nil
	// it is consulted to distinguish LAG from STALE for missing cursors.
	Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int, snap *StreamStateSnapshot) ([]eventbus.Event, error)
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

	// coldOpts accumulates ColdTierOption values supplied via
	// WithCryptoCold(...). They are forwarded to newPostgresColdTier
	// when NewReader builds the default cold tier. Ignored when the
	// caller injects a test fake via WithColdTier (that path owns its
	// own option wiring).
	coldOpts []ColdTierOption

	// hotOpts accumulates HotTierOption values supplied via
	// WithCryptoHot(...). They are forwarded to newJetStreamHotTier
	// when NewReader builds the default hot tier. Ignored when the
	// caller injects a test fake via WithHotTier (that path owns its
	// own option wiring).
	hotOpts []HotTierOption

	// fenceOpts captures the Phase 7 PluginDowngradeFence configuration
	// supplied via WithPluginDowngradeFence. nil/empty means no fence
	// is applied — router output flows through unfenced (pre-Phase-7
	// behaviour). When non-empty, fencedRouter() lazily wraps r.router
	// the first time a plugin-owned subject is queried.
	fenceOpts []PluginDowngradeFenceOption

	// fence is the lazily-built fence wrapping r.router. Built on the
	// first plugin-owned QueryHistory call when fenceOpts is non-empty;
	// nil otherwise. Plugin route is single-routered so a single fence
	// instance covers every plugin under this Reader.
	//
	// Reader is shared across concurrent gRPC requests; fenceOnce ensures
	// the lazy build is data-race-free and constructs exactly one fence
	// instance even under simultaneous plugin-owned QueryHistory calls.
	fence     PluginHistoryRouter
	fenceOnce sync.Once
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
		r.hot = newJetStreamHotTier(r.js, r.selector, r.now, r.hotOpts...)
	}
	if r.cold == nil && r.pool != nil {
		r.cold = newPostgresColdTier(r.pool, r.coldOpts...)
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
			//nolint:wrapcheck // forwarding plugin RPC / fence error to caller
			return r.fencedRouter().QueryHistory(ctx, owner.PluginName, q)
		}
	}

	now := r.now()
	edge := now.Add(-r.streamMaxAge).Add(r.safetyMargin)
	snap := newStreamStateSnapshot(r.js)
	startTier := selectStartTier(ctx, q, edge, now, snap)

	return newCrossoverStream(ctx, r.hot, r.cold, q, edge, startTier, snap), nil
}

// fencedRouter returns r.router unwrapped when no fence is configured
// (Phase B / pre-7 behaviour preserved), or a lazily-built
// PluginDowngradeFence wrapping r.router when WithPluginDowngradeFence
// supplied options. The fence is built once and reused across plugin
// queries — the always-sensitive set is captured by copy at construction
// per INV-CRYPTO-44 (no hot-reload).
func (r *Reader) fencedRouter() PluginHistoryRouter {
	if len(r.fenceOpts) == 0 {
		return r.router
	}
	r.fenceOnce.Do(func() {
		r.fence = NewPluginDowngradeFence(r.router, r.fenceOpts...)
	})
	return r.fence
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
// Rules (spec §8, with the seq-cursor refinement):
//   - If a cursor seq is set, compare it against the snapshot's FirstSeq:
//     if cursor.Seq >= FirstSeq the cursor is still within the live JS stream
//     (hot); otherwise it predates JS retention (cold).
//   - Else (no cursor): use the time bound that points away from "now" to
//     decide. Forward ⇒ NotBefore picks the oldest endpoint; Backward ⇒
//     NotAfter picks the newest endpoint.
//   - Ties at the exact edge resolve to JS (hot). This is intentional: when
//     the edge is exactly observed, JS still has the data and PG may not yet
//     because the audit projection lags by up to ~safetyMargin.
func selectStartTier(ctx context.Context, q eventbus.HistoryQuery, edge, now time.Time, snap *StreamStateSnapshot) Tier {
	cursorSeq := q.AfterSeq
	if q.Direction == eventbus.DirectionBackward {
		cursorSeq = q.BeforeSeq
	}
	if cursorSeq > 0 && snap != nil {
		firstSeq, _, err := snap.Get(ctx)
		if err == nil && firstSeq > 0 {
			if cursorSeq >= firstSeq {
				return TierJetStream
			}
			return TierPostgres
		}
	}

	// No cursor or snapshot unavailable — fall back to time-bound routing.
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

	// seenSeqs deduplicates by JetStream stream sequence across the boundary.
	// At the edge, events that were recently projected into PG are still within
	// JS retention, so a page drawn straddling both tiers can legitimately
	// observe the same sequence number twice.
	seenSeqs map[uint64]struct{}

	// stuckPageReads counts consecutive loadNextPage calls that made no
	// forward progress (either returned zero new events, or returned events
	// we'd already seen). A non-zero count that exceeds maxBufferMultiple
	// means the tier is ignoring our cursor — trip EVENTBUS_HISTORY_BUFFER_OVERFLOW
	// to avoid unbounded memory growth and infinite loops.
	stuckPageReads int

	// snap is the per-call Stream.Info cache threaded from Reader.QueryHistory.
	snap *StreamStateSnapshot
}

func newCrossoverStream(ctx context.Context, hot HotTier, cold ColdTier, q eventbus.HistoryQuery, edge time.Time, startTier Tier, snap *StreamStateSnapshot) *crossoverStream {
	return &crossoverStream{
		ctx:       ctx,
		hot:       hot,
		cold:      cold,
		query:     q,
		edge:      edge,
		startTier: startTier,
		seenSeqs:  make(map[uint64]struct{}),
		snap:      snap,
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
			if _, dup := s.seenSeqs[e.Seq]; dup {
				continue
			}
			s.seenSeqs[e.Seq] = struct{}{}
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
	// Compact the buffer before loading more: Next() only advances s.pos,
	// so without compaction len(s.buf) grows unbounded across pages and a
	// buffer-length-based guard would fire spuriously on long scans while
	// pinning all consumed events in memory.
	if s.pos >= len(s.buf) {
		s.buf = s.buf[:0]
		s.pos = 0
	}

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

	// Snapshot the cursor before the tier read; if the read returns events
	// but advanceCursor doesn't change the cursor, we know the tier is
	// ignoring it (pathological — would loop forever) and we must abort.
	cursorBefore := s.currentCursor()

	first, err := s.readTier(ctx, firstTier, want)
	if err != nil {
		return err
	}
	s.appendOrdered(first)
	s.advanceCursor(first)

	// Detect the cursor-ignored regression: a non-empty tier read that
	// didn't move the cursor (either the tier returned the same events
	// again, or advanceCursor is broken). Allow one such read (bounded
	// dedup churn at the tier boundary) but trip the guard after
	// maxBufferMultiple consecutive stuck reads.
	//
	// NOTE: seenSeqs is updated only in Next(), NOT in loadNextPage(), so
	// comparing len(seenSeqs) before and after a tier read would always
	// register as "no growth" even on productive pages. The cursor-moved
	// check below is the authoritative stuck-loop detector.
	if len(first) > 0 && s.currentCursor() == cursorBefore {
		s.stuckPageReads++
	} else {
		s.stuckPageReads = 0
	}
	if s.stuckPageReads > maxBufferMultiple {
		return oops.Code("EVENTBUS_HISTORY_BUFFER_OVERFLOW").
			With("page_size", want).
			With("stuck_reads", s.stuckPageReads).
			With("subject", string(s.query.Subject)).
			Errorf("crossover buffer exceeded progress budget: %d consecutive tier reads made no forward progress; cursor advancement likely broken", s.stuckPageReads)
	}

	if len(first) < want && !s.crossoverDone {
		remaining := want - len(first)
		if remaining < 0 {
			remaining = 0
		}
		s.crossoverDone = true
		// Reset seq cursor at the tier boundary. The seq from the first
		// tier is only meaningful within that tier's sequence space (e.g.
		// a cold event's js_seq is an aged-out JS seq, no longer in JS
		// retention). The second tier derives its own seq cursor from the
		// first events it returns; clearing here forces it to use time-based
		// routing rather than attempting to start at a seq it can't echo.
		// The ID cursor (AfterID / BeforeID) is preserved so matchesQuery
		// can still filter at the boundary.
		s.query.AfterSeq = 0
		s.query.BeforeSeq = 0
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

// currentCursor returns the query's active cursor seq (AfterSeq for
// forward, BeforeSeq for backward). Used to detect pathological tiers
// that ignore the cursor and return the same events on every read.
func (s *crossoverStream) currentCursor() uint64 {
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionForward {
		return s.query.AfterSeq
	}
	return s.query.BeforeSeq
}

// advanceCursor updates the query's cursor bound so the next tier read
// picks up where this one left off. Forward direction advances AfterSeq +
// AfterID; backward advances BeforeSeq + BeforeID. Without this,
// loadNextPage would re-read the same page indefinitely (dedup at Next
// hides the events but s.buf and the read-loop still grow without bound —
// an O(n) memory leak per call).
func (s *crossoverStream) advanceCursor(events []eventbus.Event) {
	if len(events) == 0 {
		return
	}
	last := events[len(events)-1]
	dir := s.query.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionForward {
		s.query.AfterSeq = last.Seq
		s.query.AfterID = last.ID
	} else {
		s.query.BeforeSeq = last.Seq
		s.query.BeforeID = last.ID
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
		events, err := s.hot.Read(readCtx, s.query, s.edge, pageSize, s.snap)
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
		events, err := s.cold.Read(readCtx, s.query, s.edge, pageSize, s.snap)
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
// asked for, sorted by JetStream stream sequence. Each tier is expected to
// return events already in the requested order; appendOrdered re-sorts the
// tail (unread region) to keep a stable merge when the two tiers overlap
// across the edge.
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
			return tail[i].Seq > tail[j].Seq
		}
		return tail[i].Seq < tail[j].Seq
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
