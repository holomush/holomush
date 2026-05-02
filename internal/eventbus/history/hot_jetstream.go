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
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// hotFetchTimeout bounds a single JS Fetch. Generous to absorb JS server
// round-trips on a busy embedded server; short enough that a misrouted
// query surfaces quickly.
const hotFetchTimeout = 5 * time.Second

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
	authGuard    eventbus.SessionAuthGuard
	// dekManager resolves DEK material for sensitive events.
	dekManager   eventbus.SessionDEKManager
	// auditEmitter logs plugin decrypt records.
	auditEmitter eventbus.SessionAuditEmitter
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

// Read satisfies HotTier. Builds an OrderedConsumer rooted at the earliest
// start position implied by q, fetches up to pageSize messages, decodes
// each, and filters in-process for the Before/After/NotAfter bounds the JS
// consumer cannot express directly.
// Read fetches a page of events from JetStream. The *StreamStateSnapshot
// parameter is intentionally ignored: the hot tier always derives its own
// stream state from the query parameters and (for tail reads) a fresh
// stream.Info() call — see the backward uncursored branch below.
func (h *jetStreamHotTier) Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int, _ *StreamStateSnapshot) ([]eventbus.Event, error) {
	if pageSize <= 0 {
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
		ev, decodeErr := decodeJetStreamMessage(ctx, msg, h.selector, q.Identity, h.authGuard, h.dekManager, h.auditEmitter)
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
		ev, metaOnly, authErr := decodeAndAuthorizeHistory(ctx, msg, &envelope, codecName, identity, guard, dekMgr, auditEm)
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

// decodeAndAuthorizeHistory mirrors decodeAndAuthorize from the subscriber
// package but lives in the history package to avoid cross-package coupling.
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

	// Parse DEK headers.
	var keyID codec.KeyID
	var keyVersion uint32
	if dekRefStr := h.Get(eventbus.HeaderDekRef); dekRefStr != "" {
		ref, parseErr := strconv.ParseUint(dekRefStr, 10, 64)
		if parseErr != nil {
			return eventbus.Event{}, false, oops.Code("EVENTBUS_DEK_HEADER_PARSE_FAILED").
				With("header", eventbus.HeaderDekRef).With("value", dekRefStr).Wrap(parseErr)
		}
		keyID = codec.KeyID(ref)
	}
	if dekVersionStr := h.Get(eventbus.HeaderDekVersion); dekVersionStr != "" {
		ver, parseErr := strconv.ParseUint(dekVersionStr, 10, 32)
		if parseErr != nil {
			return eventbus.Event{}, false, oops.Code("EVENTBUS_DEK_HEADER_PARSE_FAILED").
				With("header", eventbus.HeaderDekVersion).With("value", dekVersionStr).Wrap(parseErr)
		}
		keyVersion = uint32(ver) // safe: ParseUint(bitSize=32) guarantees fits in uint32
	}

	// Recover event ULID from the pre-stamped bytes.
	var eventID ulid.ULID
	if rawID := envelope.GetId(); len(rawID) == 16 {
		copy(eventID[:], rawID)
	}

	req := eventbus.SessionCheckRequest{
		Identity:   identity,
		KeyID:      keyID,
		KeyVersion: keyVersion,
		EventType:  envelope.GetType(),
		EventID:    eventID,
	}

	decision, err := guard.Check(ctx, req)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_AUTHGUARD_CHECK_FAILED").
			With("event_type", envelope.GetType()).
			Wrap(err)
	}

	if !decision.Permit {
		return buildHistoryEventFromEnvelope(eventID, envelope, nil), true, nil
	}

	// Permit: resolve key, build AAD, decode.
	key, err := dekMgr.Resolve(ctx, keyID, keyVersion)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_DEK_RESOLVE_FAILED").
			With("key_id", uint64(keyID)).With("key_version", keyVersion).
			Wrap(err)
	}

	aadBytes, err := aad.Build(envelope, string(codecName), uint64(keyID), keyVersion)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_AAD_BUILD_FAILED").Wrap(err)
	}

	c, err := codec.Resolve(codecName)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_UNKNOWN_CODEC").
			With("codec", string(codecName)).Wrap(err)
	}

	plaintext, err := c.Decode(ctx, envelope.GetPayload(), key, aadBytes)
	if err != nil {
		return eventbus.Event{}, false, oops.Code("EVENTBUS_CODEC_DECODE_FAILED").
			With("codec", string(codecName)).Wrap(err)
	}

	// Plugin recipient: emit audit record; on AUDIT_QUEUE_FULL, zero plaintext + return metadata-only.
	if identity.Kind == eventbus.IdentityKindPlugin && auditEm != nil {
		rec := eventbus.PluginDecryptRecord{
			PluginName:       identity.PluginName,
			PluginInstanceID: identity.InstanceID,
			EventID:          eventID,
			EventSubject:     eventbus.Subject(envelope.GetSubject()),
			EventType:        eventbus.Type(envelope.GetType()),
			DEKRef:           keyID,
			DEKVersion:       keyVersion,
			GrantID:          decision.GrantID,
		}
		if emitErr := auditEm.EmitPluginDecrypt(ctx, rec); emitErr != nil {
			for i := range plaintext {
				plaintext[i] = 0
			}
			return buildHistoryEventFromEnvelope(eventID, envelope, nil), true, nil
		}
	}

	return buildHistoryEventFromEnvelope(eventID, envelope, plaintext), false, nil
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
	} else if legacy := a.GetLegacyId(); legacy != "" {
		out.LegacyID = legacy
	}
	return out
}
