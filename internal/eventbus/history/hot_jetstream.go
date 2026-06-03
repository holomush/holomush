// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// hotFetchTimeout bounds a single JS Fetch. Generous to absorb JS server
// round-trips on a busy embedded server; short enough that a misrouted
// query surfaces quickly.
const hotFetchTimeout = 5 * time.Second

// hotPrecheckTimeout bounds the subject-emptiness precheck. Short
// because GetLastMsgForSubject is a metadata lookup that returns in
// milliseconds in the steady state; a missed deadline here falls
// through to the existing Fetch path so the worst case is no slower
// than today (we just pay the full hotFetchTimeout if the precheck
// itself stalls).
const hotPrecheckTimeout = 1 * time.Second

// HotTierOption tunes jetStreamHotTier construction.
type HotTierOption func(*jetStreamHotTier)

// WithHistoryAuthGuard injects the AuthGuard for sensitive event delivery
// decisions on the hot-tier history path. nil = pre-Phase 3b passthrough.
func WithHistoryAuthGuard(g eventbus.SessionAuthGuard) HotTierOption {
	return func(h *jetStreamHotTier) { h.authGuard = g }
}

// WithHistoryDEKManager injects the DEK Manager used to resolve plaintext key
// material for sensitive codec events on the hot-tier history path.
// Required when WithHistoryAuthGuard is set.
func WithHistoryDEKManager(m eventbus.SessionDEKManager) HotTierOption {
	return func(h *jetStreamHotTier) { h.dekManager = m }
}

// WithHistoryDecryptAuditEmitter injects the audit emitter for plugin decrypt
// records on the hot-tier history path.
func WithHistoryDecryptAuditEmitter(em eventbus.SessionAuditEmitter) HotTierOption {
	return func(h *jetStreamHotTier) { h.auditEmitter = em }
}

// WithHistorySourceResolver injects the source.SourceResolver used to
// resolve DEK material on the hot-tier history path. When set, the tier
// delegates to the resolver-aware dispatcher (newDispatcher.DispatchFor)
// instead of decodeAuthorizeAndDispatch, enabling the INV-CRYPTO-22 hot→cold-tier
// fallback. Production wiring at cmd/holomush/sub_grpc.go pairs this with
// a source.FallbackResolver; when unset the tier falls back to the legacy
// dekManager path. Sub-epic E T44 (holomush-jxo8.7.44).
func WithHistorySourceResolver(r source.SourceResolver) HotTierOption {
	return func(h *jetStreamHotTier) { h.sourceResolver = r }
}

// jetStreamHotTier serves history from JetStream via an ephemeral ordered
// consumer. One consumer is created per Read call, scoped to the filter
// subject and the relevant start policy. The consumer is released when
// Read returns — no durable state accumulates.
type jetStreamHotTier struct {
	js       jetstream.JetStream
	selector codec.KeySelector
	now      func() time.Time

	// authGuard evaluates sensitive event delivery decisions (Decision 5).
	// nil = pre-Phase 3b passthrough.
	authGuard eventbus.SessionAuthGuard
	// dekManager resolves DEK material for sensitive events. Legacy seam:
	// used when sourceResolver is nil. When sourceResolver IS set, dekManager
	// is bypassed in favor of the resolver-aware dispatcher path.
	dekManager eventbus.SessionDEKManager
	// auditEmitter logs plugin decrypt records.
	auditEmitter eventbus.SessionAuditEmitter
	// sourceResolver, when non-nil, routes sensitive history reads through
	// the resolver-aware dispatcher (DispatchFor) for INV-CRYPTO-22 hot→cold-tier
	// fallback. Sub-epic E T44 (holomush-jxo8.7.44).
	sourceResolver source.SourceResolver
}

func newJetStreamHotTier(js jetstream.JetStream, selector codec.KeySelector, now func() time.Time, opts ...HotTierOption) *jetStreamHotTier {
	if now == nil {
		now = time.Now
	}
	h := &jetStreamHotTier{js: js, selector: selector, now: now}
	for _, o := range opts {
		o(h)
	}
	return h
}

// subjectIsEmpty returns true when GetLastMsgForSubject confirms no
// message has ever matched subj on eventbus.StreamName. Returns false
// on success (a message exists), on any lookup error (graceful
// fallthrough — caller proceeds with the existing Fetch path), and on
// ctx cancellation (caller's Fetch will also short-circuit on the same
// ctx). Bounded by hotPrecheckTimeout independently of the caller's
// ctx so a slow precheck cannot consume the caller's full latency
// budget.
func (h *jetStreamHotTier) subjectIsEmpty(ctx context.Context, subj eventbus.Subject) bool {
	preCtx, cancel := context.WithTimeout(ctx, hotPrecheckTimeout)
	defer cancel()
	stream, err := h.js.Stream(preCtx, eventbus.StreamName)
	if err != nil {
		return false
	}
	if _, lastErr := stream.GetLastMsgForSubject(preCtx, string(subj)); errors.Is(lastErr, jetstream.ErrMsgNotFound) {
		return true
	}
	return false
}

// Read fetches a page of events from JetStream. The *StreamStateSnapshot
// parameter is intentionally ignored: the hot tier always derives its own
// stream state from the query parameters and (for tail reads) a fresh
// stream.Info() call — see the backward uncursored branch below.
func (h *jetStreamHotTier) Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int, _ *StreamStateSnapshot) ([]eventbus.Event, error) {
	if pageSize <= 0 {
		return nil, nil
	}
	// Subject-emptiness precheck (holomush-87qu connect-latency fix):
	// GetLastMsgForSubject is a cheap metadata lookup that returns
	// jetstream.ErrMsgNotFound when no message has ever matched the
	// subject filter. Without this, cons.Fetch below blocks for the
	// full hotFetchTimeout (5s) on truly-empty subjects — the
	// dominant component of fresh-guest connect latency (two ambient
	// streams × 5s wait = ~10s "syncing" window).
	//
	// The NATS-recommended primitive for this use case (per
	// nats-io/nats.go docs + Stream Management wiki) is exactly
	// GetLastMsgForSubject + ErrMsgNotFound sentinel. Alternatives
	// (Stream.Info aggregates, ConsumerInfo.NumPending) require
	// heavier ops; this is the lightest available probe.
	//
	// Failure modes are graceful: if the Stream lookup or precheck
	// itself errors (e.g. transient ctx deadline, NATS connection
	// hiccup), we fall through to the existing Fetch path and pay
	// the same worst-case wait as before — no regression.
	if h.subjectIsEmpty(ctx, q.Subject) {
		return nil, nil
	}
	// Determine the fetch size (how many messages to request from the JS
	// consumer) based on direction and cursor state.
	//
	// Forward reads:
	//   - With cursor (AfterSeq > 0): start AT AfterSeq (cursor echo), then
	//     take pageSize events. fetch = pageSize + 1 (echo slot + page).
	//   - Without cursor: start at the edge timestamp and walk forward;
	//     double-fetch to absorb per-event filter rejections from matchesQuery.
	//
	// Backward reads:
	//   - With cursor (BeforeSeq > 0): we start at BeforeSeq − pageSize and
	//     deliver ascending; matchesQuery excludes >= BeforeSeq. We need
	//     exactly pageSize events in the window, so fetch = pageSize.
	//   - Without cursor: we start at LastSeq − pageSize + 1 so the window
	//     holds exactly pageSize events. fetch = pageSize.
	//
	// The key invariant for backward reads: the in-loop cap (len(out) >=
	// pageSize) must never drop events that belong to the page. This is
	// guaranteed when the window contains at most pageSize in-scope events,
	// which the fetch sizing above enforces.
	isForwardCursor := q.AfterSeq > 0 && q.Direction != eventbus.DirectionBackward
	isBackward := q.Direction == eventbus.DirectionBackward
	var fetch int
	switch {
	case isForwardCursor:
		fetch = pageSize + 1 // cursor echo + pageSize events
	case isBackward:
		fetch = pageSize // window is sized to hold exactly pageSize events
	default:
		fetch = pageSize * 2 // forward uncursored: absorb filter rejections
	}
	if fetch > pageSize+MaxPageSize {
		fetch = pageSize + MaxPageSize
	}
	cfg, err := h.buildConfig(ctx, q, edge, fetch)
	if err != nil {
		return nil, err
	}
	cons, err := h.js.OrderedConsumer(ctx, eventbus.StreamName, cfg)
	if err != nil {
		return nil, oops.Code("EVENTBUS_HOT_CONSUMER_FAILED").
			With("subject", string(q.Subject)).
			Wrap(err)
	}
	// OrderedConsumer does not require Delete; ephemeral consumers are
	// released on the server side when the subscription ends. We do not
	// keep a Consume loop — one Fetch pulls the page and we move on.
	fetchCtx, cancel := context.WithTimeout(ctx, hotFetchTimeout)
	defer cancel()
	batch, err := cons.Fetch(fetch, jetstream.FetchMaxWait(hotFetchTimeout))
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, oops.Code("EVENTBUS_HOT_FETCH_FAILED").
			With("subject", string(q.Subject)).
			Wrap(err)
	}

	out := make([]eventbus.Event, 0, pageSize)

	// For forward reads with AfterSeq > 0, the consumer starts AT AfterSeq
	// (inclusive) and delivers the cursor row first as an echo. The cursor
	// echo must match the tripwire (AfterSeq, AfterID); if it doesn't, the
	// seq was reused by a JetStream rebuild and the cursor is stale.
	//
	// For backward reads with BeforeSeq > 0, the consumer starts at
	// max(1, BeforeSeq-fetch+1) and delivers messages in ascending order up
	// to BeforeSeq. The cursor exclusion is already handled by matchesQuery
	// (ev.Seq >= q.BeforeSeq → reject). There is no cursor echo to validate
	// because the cursor row is not the first message delivered.
	forwardCursorPending := q.AfterSeq > 0 && q.Direction != eventbus.DirectionBackward

	for msg := range batch.Messages() {
		if fetchCtx.Err() != nil {
			break
		}
		ev, decodeErr := decodeJetStreamMessage(ctx, msg, h.selector, q.Identity, h.authGuard, h.dekManager, h.auditEmitter, h.sourceResolver)
		if decodeErr != nil {
			// Skip the message rather than abort the whole page; one
			// undecodable message on a large history would be a DoS
			// vector. The codec registry drift detector surfaces these
			// separately.
			continue
		}
		// Populate Event.Seq from JetStream message metadata. When forward
		// cursor validation is pending the metadata MUST be available — a
		// missing/failed Metadata() lookup leaves ev.Seq=0, which would
		// then spuriously trigger ErrCursorStale on the cursor-echo
		// validation below. Surface the metadata error so the caller can
		// distinguish a genuine cursor-stale from a JS infra blip.
		meta, mErr := msg.Metadata()
		if mErr != nil {
			if forwardCursorPending {
				return nil, oops.Code("EVENTBUS_HOT_METADATA_FAILED").
					With("subject", string(q.Subject)).
					With("cursor_seq", q.AfterSeq).
					With("cursor_id", q.AfterID.String()).
					Wrap(mErr)
			}
			// No cursor in flight — Seq is best-effort; events without
			// metadata still match by time/subject filters.
		} else {
			ev.Seq = meta.Sequence.Stream
		}

		if forwardCursorPending {
			forwardCursorPending = false
			if ev.Seq != q.AfterSeq || ev.ID != q.AfterID {
				return nil, oops.Code("EVENTBUS_CURSOR_STALE").
					With("subject", string(q.Subject)).
					With("cursor_seq", q.AfterSeq).
					With("cursor_id", q.AfterID.String()).
					With("got_seq", ev.Seq).
					With("got_id", ev.ID.String()).
					Wrap(eventbus.ErrCursorStale)
			}
			// Discard the forward cursor echo; the real page starts after it.
			continue
		}

		if !matchesQuery(ev, q, edge, TierJetStream) {
			continue
		}
		out = append(out, ev)
		if len(out) >= pageSize {
			break
		}
	}
	if ferr := batch.Error(); ferr != nil && !errors.Is(ferr, context.DeadlineExceeded) && !errors.Is(ferr, context.Canceled) {
		return out, oops.Code("EVENTBUS_HOT_BATCH_FAILED").Wrap(ferr)
	}
	orderEvents(out, q)
	return out, nil
}

// buildConfig crafts the OrderedConsumerConfig used for a single page read.
// JS can express FilterSubject and a start time/sequence; finer bounds are
// applied client-side in matchesQuery.
//
// Start-policy table:
//   - Forward, cursor seq set (AfterSeq > 0): DeliverByStartSequencePolicy at
//     AfterSeq so the cursor row is delivered first as an echo for tripwire
//     validation. After validation the cursor row is discarded.
//   - Forward, no cursor: DeliverByStartTimePolicy at max(edge, NotBefore).
//   - Backward, cursor seq set (BeforeSeq > 0): DeliverByStartSequencePolicy
//     at max(1, BeforeSeq - pageSize) so the window holds exactly pageSize
//     in-scope events (those with seq < BeforeSeq). No cursor echo: matchesQuery
//     already excludes ev.Seq >= BeforeSeq.
//   - Backward, no cursor: tail-oriented — consult stream.Info().State.LastSeq
//     and start at max(LastSeq - pageSize + 1, 1) so the window holds exactly
//     pageSize events. The caller reverses the page in orderEvents to
//     present newest-first.
func (h *jetStreamHotTier) buildConfig(
	ctx context.Context,
	q eventbus.HistoryQuery,
	edge time.Time,
	fetch int,
) (jetstream.OrderedConsumerConfig, error) {
	cfg := jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{string(q.Subject)},
	}
	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}

	// FORWARD: cursor seq present → start AT seq inclusive.
	if dir == eventbus.DirectionForward {
		if q.AfterSeq > 0 {
			cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
			cfg.OptStartSeq = q.AfterSeq
			return cfg, nil
		}
		start := edge
		if !q.NotBefore.IsZero() && q.NotBefore.After(edge) {
			start = q.NotBefore
		}
		cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
		cfg.OptStartTime = &start
		return cfg, nil
	}

	// BACKWARD with cursor: walk forward from max(1, BeforeSeq - fetch) up to
	// BeforeSeq (exclusive per matchesQuery), then reverse in-memory.
	// Caller sets fetch = pageSize for backward reads, giving a window of
	// exactly pageSize in-scope events so the in-loop cap never drops events.
	if q.BeforeSeq > 0 {
		var startSeq uint64 = 1
		if q.BeforeSeq > uint64(fetch) { //nolint:gosec // fetch is a bounded positive int from ClampPageSize
			startSeq = q.BeforeSeq - uint64(fetch) //nolint:gosec // same bound; no +1 because we want [BeforeSeq-fetch, BeforeSeq)
		}
		cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		cfg.OptStartSeq = startSeq
		return cfg, nil
	}

	// BACKWARD without cursor: tail-oriented read. Start at LastSeq - fetch + 1
	// where fetch = pageSize, giving a window of exactly pageSize events.
	// The in-loop cap and subsequent orderEvents reversal then yield the newest
	// pageSize events in descending seq order.
	//
	// Intentionally uses a fresh stream.Info() (not the ignored snapshot param) so the returned page
	// reflects the current stream tail. The snapshot taken at query start is
	// appropriate when consistency-with-other-tier-reads matters; here we want
	// the latest events, so a fresh lookup is correct.
	stream, err := h.js.Stream(ctx, eventbus.StreamName)
	if err != nil {
		return cfg, oops.Code("EVENTBUS_HOT_STREAM_LOOKUP_FAILED").
			With("stream", eventbus.StreamName).
			Wrap(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return cfg, oops.Code("EVENTBUS_HOT_STREAM_INFO_FAILED").
			With("stream", eventbus.StreamName).
			Wrap(err)
	}
	last := info.State.LastSeq
	if last == 0 {
		cfg.DeliverPolicy = jetstream.DeliverAllPolicy
		return cfg, nil
	}
	var startSeq uint64 = 1
	if last >= uint64(fetch) { //nolint:gosec // fetch is a bounded positive int
		startSeq = last - uint64(fetch) + 1 //nolint:gosec // same bound
	}
	cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
	cfg.OptStartSeq = startSeq
	return cfg, nil
}

// matchesQuery applies the per-event filters that the JS consumer config
// could not express natively.
//
// Boundary semantics:
//   - AfterSeq / BeforeSeq are EXCLUSIVE (by Seq).
//   - NotBefore / NotAfter are INCLUSIVE (by Timestamp).
//   - In the JetStream tier, events with Timestamp < edge are NOT served
//     from here (they may still appear for up to safetyMargin; the cold
//     tier serves the canonical pre-edge slice).
func matchesQuery(ev eventbus.Event, q eventbus.HistoryQuery, edge time.Time, tier Tier) bool {
	if q.AfterSeq > 0 && ev.Seq <= q.AfterSeq {
		return false
	}
	if q.BeforeSeq > 0 && ev.Seq >= q.BeforeSeq {
		return false
	}
	if !q.NotBefore.IsZero() && ev.Timestamp.Before(q.NotBefore) {
		return false
	}
	if !q.NotAfter.IsZero() && ev.Timestamp.After(q.NotAfter) {
		return false
	}
	// Tier-boundary enforcement: the hot tier is the authoritative source
	// for Timestamp >= edge. The cold tier is authoritative for
	// Timestamp < edge. Events within the overlap window (recently
	// projected into PG while still within JS retention) are still
	// served from the tier that provided them; the crossoverStream's
	// seen-map is responsible for dedup, not this filter.
	switch tier {
	case TierJetStream:
		if !edge.IsZero() && ev.Timestamp.Before(edge) {
			return false
		}
	case TierPostgres:
		// Cold tier may serve post-edge data when JS returned an empty
		// or truncated page (e.g. JS retention was just rebuilt).
		// Allowed; the seen-set dedups.
	}
	return true
}

// orderEvents returns the page in the requested direction while preserving
// JetStream stream-sequence order within the page.
//
// Ordering is owned by JetStream per-stream sequence, not ULID lex order —
// concurrent publishers produce events whose ULIDs do NOT match stream
// sequence. For DirectionForward the page is already in stream-seq order
// (that's the order OrderedConsumer delivered them). For DirectionBackward
// we reverse the slice so the caller sees newest-first while still
// preserving JetStream ordering inside the page.
func orderEvents(events []eventbus.Event, q eventbus.HistoryQuery) {
	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	if dir == eventbus.DirectionBackward {
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
	}
}

// decodeJetStreamMessage is the Read-side inverse of the publisher's encode
// path. Identical in spirit to subscriber.decodeDeliveryWithAuth but local to
// this package. For sensitive-codec events when guard is non-nil it implements
// the Decision 5 order-of-operations; for identity-codec events it delivers
// without AuthGuard invocation.
//
// The history path returns a single eventbus.Event (not a metadataOnly bool)
// because the HistoryStream interface only yields events. Metadata-only events
// on the history path are represented as Events with empty Payload (the caller
// signals metadata_only via the EventFrame.metadata_only proto field in T10).
func decodeJetStreamMessage(
	ctx context.Context,
	msg jetstream.Msg,
	selector codec.KeySelector,
	identity eventbus.SessionIdentity,
	guard eventbus.SessionAuthGuard,
	dekMgr eventbus.SessionDEKManager,
	auditEm eventbus.SessionAuditEmitter,
	resolver source.SourceResolver,
) (eventbus.Event, error) {
	h := msg.Headers()
	msgIDStr := h.Get(eventbus.HeaderMsgID)
	if msgIDStr == "" {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_MISSING_HEADER").
			With("header", eventbus.HeaderMsgID).Errorf("missing header")
	}
	id, err := ulid.Parse(msgIDStr)
	if err != nil {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_BAD_MSG_ID").Wrap(err)
	}
	codecNameStr := h.Get(eventbus.HeaderCodec)
	if codecNameStr == "" {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_MISSING_HEADER").
			With("header", eventbus.HeaderCodec).Errorf("missing header")
	}
	_, err = codec.Resolve(codec.Name(codecNameStr))
	if err != nil {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_UNKNOWN_CODEC").
			With("codec", codecNameStr).Wrap(err)
	}

	// DECISION 0: proto-unmarshal FIRST. msg.Data is the marshaled
	// envelope (cleartext fields + maybe-ciphertext payload field).
	var envelope eventbusv1.Event
	if unmarshalErr := proto.Unmarshal(msg.Data(), &envelope); unmarshalErr != nil {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_UNMARSHAL_FAILED").Wrap(unmarshalErr)
	}
	// Stamp the parsed ULID into envelope.Id for downstream AAD/audit use.
	envelope.Id = id[:]

	var payload []byte

	codecName := codec.Name(codecNameStr)
	switch {
	case codecName == codec.NameIdentity:
		// Identity codec: deliver as-is. AuthGuard NOT invoked.
		payload = envelope.GetPayload()
	case guard != nil:
		// Sensitive codec with AuthGuard wired: full Decision 5 order-of-operations.
		// When the hot tier has a SourceResolver wired (sub-epic E T44), route
		// through the resolver-aware DispatchFor path for INV-CRYPTO-22 hot→cold-tier
		// fallback. Otherwise fall back to the legacy decodeAuthorizeAndDispatch
		// path that consumes dekMgr directly.
		var ev eventbus.Event
		var metaOnly bool
		var authErr error
		if resolver != nil {
			ev, metaOnly, authErr = decodeViaResolver(ctx, msg, &envelope, codecName, identity, guard, resolver, auditEm)
		} else {
			ev, metaOnly, authErr = decodeAndAuthorizeHistory(ctx, msg, &envelope, codecName, identity, guard, dekMgr, auditEm)
		}
		if authErr != nil {
			return eventbus.Event{}, authErr
		}
		ev.MetadataOnly = metaOnly
		return ev, nil
	default:
		// Guard nil: legacy passthrough decode with nil AAD (pre-Phase 3b / tests).
		var key codec.Key
		if selector != nil {
			k, kerr := selector.SelectForDecrypt(ctx, codecName, 0)
			if kerr != nil {
				return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_KEY_FETCH_FAILED").
					With("codec", codecNameStr).Wrap(kerr)
			}
			key = k
		}
		c, resolveErr := codec.Resolve(codecName) // already validated above
		if resolveErr != nil {
			return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_UNKNOWN_CODEC").
				With("codec", codecNameStr).Wrap(resolveErr)
		}
		plain, decErr := c.Decode(ctx, envelope.Payload, key, nil)
		if decErr != nil {
			return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_DECODE_FAILED").
				With("codec", codecNameStr).Wrap(decErr)
		}
		payload = plain
	}

	return eventbus.Event{
		ID:        id,
		Subject:   eventbus.Subject(envelope.GetSubject()),
		Type:      eventbus.Type(envelope.GetType()),
		Timestamp: envelope.GetTimestamp().AsTime(),
		Actor:     actorFromEnvelope(envelope.GetActor()),
		Payload:   payload,
		Rendering: eventbus.RenderingFromProto(envelope.GetRendering()),
	}, nil
}

// decodeAndAuthorizeHistory is the hot-tier wrapper that parses DEK
// headers from the JetStream message and delegates to the shared
// header-free decodeAuthorizeAndDispatch (see dispatcher.go). The cold
// tier supplies keyID/keyVersion from PG row columns and calls the
// dispatcher directly.
//
// Returns (event, metadataOnly, err).
func decodeAndAuthorizeHistory(
	ctx context.Context,
	msg jetstream.Msg,
	envelope *eventbusv1.Event,
	codecName codec.Name,
	identity eventbus.SessionIdentity,
	guard eventbus.SessionAuthGuard,
	dekMgr eventbus.SessionDEKManager,
	auditEm eventbus.SessionAuditEmitter,
) (eventbus.Event, bool, error) {
	h := msg.Headers()

	// Parse DEK headers. Both headers are required for sensitive (non-identity)
	// codec events: absent or empty headers indicate a publisher contract violation.
	// Fail closed rather than falling back to (0, 0), which would present as an
	// authorization miss when guard.Check compares against stored DEK participants.
	dekRefStr := h.Get(eventbus.HeaderDekRef)
	dekVersionStr := h.Get(eventbus.HeaderDekVersion)
	if dekRefStr == "" || dekVersionStr == "" {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_DEK_HEADER_MISSING").
			With("has_dek_ref", dekRefStr != "").
			With("has_dek_version", dekVersionStr != "").
			With("codec", string(codecName)).
			Errorf("sensitive codec event missing required DEK headers")
	}

	keyID, keyVersion, err := parseDEKHeaders(dekRefStr, dekVersionStr)
	if err != nil {
		return eventbus.Event{}, false, err
	}

	return decodeAuthorizeAndDispatch(
		ctx, envelope, codecName, keyID, keyVersion,
		identity, guard, dekMgr, auditEm, false,
	)
}

// decodeViaResolver is the resolver-aware variant of decodeAndAuthorizeHistory:
// parses DEK headers from the hot-tier JetStream message and dispatches via
// the newDispatcher (DispatchFor) configured with the provided resolver.
// Replaces the legacy dekMgr.Resolve seam with resolver.Resolve, enabling the
// INV-CRYPTO-22 hot→cold-tier fallback when the resolver is a *source.FallbackResolver.
// Sub-epic E T44 (holomush-jxo8.7.44).
func decodeViaResolver(
	ctx context.Context,
	msg jetstream.Msg,
	envelope *eventbusv1.Event,
	codecName codec.Name,
	identity eventbus.SessionIdentity,
	guard eventbus.SessionAuthGuard,
	resolver source.SourceResolver,
	auditEm eventbus.SessionAuditEmitter,
) (eventbus.Event, bool, error) {
	h := msg.Headers()
	dekRefStr := h.Get(eventbus.HeaderDekRef)
	dekVersionStr := h.Get(eventbus.HeaderDekVersion)
	if dekRefStr == "" || dekVersionStr == "" {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_DEK_HEADER_MISSING").
			With("has_dek_ref", dekRefStr != "").
			With("has_dek_version", dekVersionStr != "").
			With("codec", string(codecName)).
			Errorf("sensitive codec event missing required DEK headers")
	}
	keyID, keyVersion, err := parseDEKHeaders(dekRefStr, dekVersionStr)
	if err != nil {
		return eventbus.Event{}, false, err
	}
	d := newDispatcher(WithSourceResolver(resolver))
	return d.DispatchFor(ctx, envelope, codecName, keyID, keyVersion, identity, guard, auditEm)
}

// parseDEKHeaders parses the App-Dek-Ref / App-Dek-Version header values
// into typed (KeyID, version) pair. bitSize=63 enforces the parsed ref
// fits in int64, matching the crypto_keys.id BIGSERIAL column type.
// dek/store.go:128 casts KeyID to int64 for the SQL query; this
// parse-site bound makes that cast provably safe and silences CodeQL's
// incorrect-conversion warning.
func parseDEKHeaders(dekRefStr, dekVersionStr string) (codec.KeyID, uint32, error) {
	ref, parseErr := strconv.ParseUint(dekRefStr, 10, 63)
	if parseErr != nil {
		return 0, 0, oops.Code("EVENTBUS_DEK_HEADER_PARSE_FAILED").
			With("header", eventbus.HeaderDekRef).With("value", dekRefStr).Wrap(parseErr)
	}
	ver, parseErr := strconv.ParseUint(dekVersionStr, 10, 32)
	if parseErr != nil {
		return 0, 0, oops.Code("EVENTBUS_DEK_HEADER_PARSE_FAILED").
			With("header", eventbus.HeaderDekVersion).With("value", dekVersionStr).Wrap(parseErr)
	}
	return codec.KeyID(ref), uint32(ver), nil // safe: ParseUint(bitSize=32) guarantees fits in uint32
}

// isHistoryAuditQueueFull reports whether err is an AUDIT_QUEUE_FULL oops error.
// Mirrors isAuditQueueFull in subscriber.go; kept separate to avoid cross-package
// coupling between subscriber and history. Used to distinguish the narrowly
// acceptable TOCTOU fallback from unexpected emit errors that must propagate.
func isHistoryAuditQueueFull(err error) bool {
	if err == nil {
		return false
	}
	o, ok := oops.AsOops(err)
	if !ok {
		return false
	}
	code, ok := o.Code().(string)
	return ok && code == "AUDIT_QUEUE_FULL"
}

// buildHistoryEventFromEnvelope constructs an eventbus.Event from a proto
// envelope and payload. Used by decodeAndAuthorizeHistory for both permit and
// deny paths.
func buildHistoryEventFromEnvelope(id ulid.ULID, envelope *eventbusv1.Event, payload []byte) eventbus.Event {
	return eventbus.Event{
		ID:        id,
		Subject:   eventbus.Subject(envelope.GetSubject()),
		Type:      eventbus.Type(envelope.GetType()),
		Timestamp: envelope.GetTimestamp().AsTime(),
		Actor:     actorFromEnvelope(envelope.GetActor()),
		Payload:   payload,
		Rendering: eventbus.RenderingFromProto(envelope.GetRendering()),
	}
}

func actorFromEnvelope(a *eventbusv1.Actor) eventbus.Actor {
	if a == nil {
		return eventbus.Actor{}
	}
	out := eventbus.Actor{}
	switch a.GetKind() {
	case eventbusv1.ActorKind_ACTOR_KIND_CHARACTER:
		out.Kind = eventbus.ActorKindCharacter
	case eventbusv1.ActorKind_ACTOR_KIND_PLAYER:
		out.Kind = eventbus.ActorKindPlayer
	case eventbusv1.ActorKind_ACTOR_KIND_SYSTEM:
		out.Kind = eventbus.ActorKindSystem
	case eventbusv1.ActorKind_ACTOR_KIND_PLUGIN:
		out.Kind = eventbus.ActorKindPlugin
	default:
		out.Kind = eventbus.ActorKindUnknown
	}
	if id := a.GetId(); len(id) == 16 {
		var u ulid.ULID
		copy(u[:], id)
		out.ID = u
	}
	return out
}
