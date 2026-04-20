// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// hotFetchTimeout bounds a single JS Fetch. Generous to absorb JS server
// round-trips on a busy embedded server; short enough that a misrouted
// query surfaces quickly.
const hotFetchTimeout = 5 * time.Second

// jetStreamHotTier serves history from JetStream via an ephemeral ordered
// consumer. One consumer is created per Read call, scoped to the filter
// subject and the relevant start policy. The consumer is released when
// Read returns — no durable state accumulates.
type jetStreamHotTier struct {
	js       jetstream.JetStream
	selector codec.KeySelector
	now      func() time.Time
}

func newJetStreamHotTier(js jetstream.JetStream, selector codec.KeySelector, now func() time.Time) *jetStreamHotTier {
	if now == nil {
		now = time.Now
	}
	return &jetStreamHotTier{js: js, selector: selector, now: now}
}

// Read satisfies HotTier. Builds an OrderedConsumer rooted at the earliest
// start position implied by q, fetches up to pageSize messages, decodes
// each, and filters in-process for the Before/After/NotAfter bounds the JS
// consumer cannot express directly.
func (h *jetStreamHotTier) Read(ctx context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int) ([]eventbus.Event, error) {
	if pageSize <= 0 {
		return nil, nil
	}
	// Overfetch when client-side filters will reject some messages.
	fetch := pageSize * 2
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
	for msg := range batch.Messages() {
		if fetchCtx.Err() != nil {
			break
		}
		ev, decodeErr := decodeJetStreamMessage(ctx, msg, h.selector)
		if decodeErr != nil {
			// Skip the message rather than abort the whole page; one
			// undecodable message on a large history would be a DoS
			// vector. The codec registry drift detector surfaces these
			// separately.
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
// DirectionBackward needs special care: JetStream has no "start from newest
// and read backward" policy, so a naive DeliverByStartTimePolicy + forward
// fetch would return the OLDEST events inside the window, not the tail.
// For backward reads we therefore consult stream.Info().State.LastSeq and
// start at max(LastSeq - fetch + 1, 1) via DeliverByStartSequencePolicy so
// the forward-fetch page is actually the stream's tail. The caller then
// reverses the page in orderEvents to present newest-first.
func (h *jetStreamHotTier) buildConfig(
	ctx context.Context,
	q eventbus.HistoryQuery,
	edge time.Time,
	fetch int,
) (jetstream.OrderedConsumerConfig, error) {
	cfg := jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{string(q.Subject)},
	}
	// Start position:
	//   - If the caller gave us After, start from its timestamp (the
	//     closest JS can get to a ULID cursor). matchesQuery then
	//     filters out ULIDs <= After.Time.
	//   - Else use the lower bound that applies — NotBefore forward,
	//     edge for unbounded queries.
	if !q.After.IsZero() {
		start := ulid.Time(q.After.Time())
		cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
		cfg.OptStartTime = &start
		return cfg, nil
	}
	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	switch dir {
	case eventbus.DirectionForward:
		start := edge
		if !q.NotBefore.IsZero() && q.NotBefore.After(edge) {
			start = q.NotBefore
		}
		cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
		cfg.OptStartTime = &start
	case eventbus.DirectionBackward:
		// Tail-oriented: get stream's last sequence and compute a start
		// seq that puts `fetch` messages between start and last. If we
		// can't reach the stream or the stream is empty, fall back to
		// DeliverByStartTimePolicy so the caller still gets *something*
		// ordered (the pre-existing behaviour, with the same caveat that
		// paging is truncated).
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
			// Empty stream: nothing to serve, but the downstream Fetch
			// will return an empty batch quickly under any policy.
			cfg.DeliverPolicy = jetstream.DeliverAllPolicy
			return cfg, nil
		}
		// fetch is expected to fit in uint64; Fetch rejects negatives
		// upstream, so this conversion is safe.
		window := uint64(0)
		if fetch > 0 {
			window = uint64(fetch)
		}
		var startSeq uint64 = 1
		if window > 0 && last > window {
			startSeq = last - window + 1
		}
		cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		cfg.OptStartSeq = startSeq
	}
	return cfg, nil
}

// matchesQuery applies the per-event filters that the JS consumer config
// could not express natively.
//
// Boundary semantics:
//   - After / Before are EXCLUSIVE.
//   - NotBefore / NotAfter are INCLUSIVE.
//   - In the JetStream tier, events with Timestamp < edge are NOT served
//     from here (they may still appear for up to safetyMargin; the cold
//     tier serves the canonical pre-edge slice).
func matchesQuery(ev eventbus.Event, q eventbus.HistoryQuery, edge time.Time, tier Tier) bool {
	if !q.After.IsZero() && ev.ID.Compare(q.After) <= 0 {
		return false
	}
	if !q.Before.IsZero() && ev.ID.Compare(q.Before) >= 0 {
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
	// ULID-seen map is responsible for dedup, not this filter.
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
// path. Identical in spirit to subscriber.decodeDelivery but local to this
// package because we want to avoid exporting that symbol.
func decodeJetStreamMessage(ctx context.Context, msg jetstream.Msg, selector codec.KeySelector) (eventbus.Event, error) {
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
	codecName := h.Get(eventbus.HeaderCodec)
	if codecName == "" {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_MISSING_HEADER").
			With("header", eventbus.HeaderCodec).Errorf("missing header")
	}
	c, err := codec.Resolve(codec.Name(codecName))
	if err != nil {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_UNKNOWN_CODEC").
			With("codec", codecName).Wrap(err)
	}
	var key codec.Key
	if codec.Name(codecName) != codec.NameIdentity && selector != nil {
		k, kerr := selector.SelectForDecrypt(ctx, codec.Name(codecName), 0)
		if kerr != nil {
			return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_KEY_FETCH_FAILED").
				With("codec", codecName).Wrap(kerr)
		}
		key = k
	}
	plain, err := c.Decode(ctx, msg.Data(), key)
	if err != nil {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_DECODE_FAILED").
			With("codec", codecName).Wrap(err)
	}
	var envelope eventbusv1.Event
	if unmarshalErr := proto.Unmarshal(plain, &envelope); unmarshalErr != nil {
		return eventbus.Event{}, oops.Code("EVENTBUS_HISTORY_UNMARSHAL_FAILED").Wrap(unmarshalErr)
	}
	return eventbus.Event{
		ID:        id,
		Subject:   eventbus.Subject(envelope.GetSubject()),
		Type:      eventbus.Type(envelope.GetType()),
		Timestamp: envelope.GetTimestamp().AsTime(),
		Actor:     actorFromEnvelope(envelope.GetActor()),
		Payload:   envelope.GetPayload(),
	}, nil
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
