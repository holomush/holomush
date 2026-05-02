// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/telemetry"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// Session consumer defaults. Per spec §5 (Subscribe path) and §2 (stale-cursor
// edge case): InactiveThreshold=24h keeps sessions resumable across typical
// client outages; AckWait=30s balances redelivery latency against tolerating
// a slow Send. MaxAckPending caps in-flight unacked messages per session —
// slow clients stall rather than leaking memory.
const (
	// DefaultSessionAckWait is how long the server waits for an ack before
	// redelivering. 30s is the spec-recommended production value.
	DefaultSessionAckWait = 30 * time.Second
	// DefaultSessionMaxAckPending caps in-flight unacked deliveries per
	// session. Per spec §5 backpressure note.
	DefaultSessionMaxAckPending = 256
	// DefaultSessionInactiveThreshold deletes idle session consumers (and
	// their cursor state) automatically. Per spec §2 MUST stay less than
	// stream MaxAge.
	DefaultSessionInactiveThreshold = 24 * time.Hour
)

// sessionConsumerName returns the deterministic durable consumer name for the
// given session. Matches the spec's "session_<sessionID>" convention.
func sessionConsumerName(sessionID string) string {
	return "session_" + sessionID
}

// SubscribeOption tunes NewJetStreamSubscriber construction.
type SubscribeOption func(*JetStreamSubscriber)

// WithSubscriberCodecSelector injects the KeySelector used on receive to pick
// the decrypt key. Mirrors WithCodecSelector for the publisher. Unset
// deployments still decode identity-codec payloads (the default).
func WithSubscriberCodecSelector(sel codec.KeySelector) SubscribeOption {
	return func(s *JetStreamSubscriber) { s.selector = sel }
}

// WithSessionAckWait overrides the default AckWait on new session consumers.
func WithSessionAckWait(d time.Duration) SubscribeOption {
	return func(s *JetStreamSubscriber) {
		if d > 0 {
			s.ackWait = d
		}
	}
}

// WithSessionMaxAckPending overrides the default MaxAckPending.
func WithSessionMaxAckPending(n int) SubscribeOption {
	return func(s *JetStreamSubscriber) {
		if n > 0 {
			s.maxAckPending = n
		}
	}
}

// WithSessionInactiveThreshold overrides the default InactiveThreshold.
// Callers SHOULD keep this strictly less than Config.StreamMaxAge.
func WithSessionInactiveThreshold(d time.Duration) SubscribeOption {
	return func(s *JetStreamSubscriber) {
		if d > 0 {
			s.inactiveThreshold = d
		}
	}
}

// JetStreamSubscriber is the production Subscriber implementation. Sessions
// open long-lived durable consumers named session_<sessionID>; filter changes
// route through CreateOrUpdateConsumer so the server-side cursor is preserved
// across focus transitions (spec §5 SetFilters).
type JetStreamSubscriber struct {
	js                jetstream.JetStream
	selector          codec.KeySelector
	ackWait           time.Duration
	maxAckPending     int
	inactiveThreshold time.Duration
}

// NewJetStreamSubscriber constructs a Subscriber backed by the given JetStream
// context. Defaults per spec §5 apply unless overridden.
func NewJetStreamSubscriber(js jetstream.JetStream, opts ...SubscribeOption) *JetStreamSubscriber {
	s := &JetStreamSubscriber{
		js:                js,
		selector:          identityKeySelector{},
		ackWait:           DefaultSessionAckWait,
		maxAckPending:     DefaultSessionMaxAckPending,
		inactiveThreshold: DefaultSessionInactiveThreshold,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// OpenSession implements Subscriber. Creates-or-updates the session's durable
// consumer with the supplied filters. Repeated calls with the same sessionID
// and different filters atomically re-filter while preserving the cursor —
// the durable consumer's acked-seq is kept by JetStream across
// CreateOrUpdateConsumer.
//
// Callers MUST call SessionStream.Close when done. Close does NOT delete the
// consumer — sessions resume across reconnect via the durable's retained
// cursor. Consumer teardown is handled by InactiveThreshold (passive) or by
// the session-lifecycle listener in F5 (active, on session_ended).
func (s *JetStreamSubscriber) OpenSession(ctx context.Context, sessionID string, filters []Subject) (SessionStream, error) {
	if s.js == nil {
		return nil, oops.Code("EVENTBUS_SUBSCRIBER_NOT_READY").Errorf("JetStream context is nil")
	}
	if sessionID == "" {
		return nil, oops.Code("EVENTBUS_INVALID_SESSION_ID").Errorf("sessionID required")
	}
	name := sessionConsumerName(sessionID)
	subjects := subjectsToStrings(filters)
	// An empty FilterSubjects set turns the durable into an unfiltered
	// subscriber on the whole EVENTS stream. Reject here so a bad focus
	// restore (or removing the last stream) cannot leak unrelated traffic
	// into a session.
	if len(subjects) == 0 {
		return nil, oops.Code("EVENTBUS_SESSION_FILTERS_REQUIRED").
			With("session_id", sessionID).
			Errorf("at least one subject filter required")
	}
	cfg := jetstream.ConsumerConfig{
		Durable:           name,
		Name:              name,
		FilterSubjects:    subjects,
		AckPolicy:         jetstream.AckExplicitPolicy,
		AckWait:           s.ackWait,
		MaxAckPending:     s.maxAckPending,
		InactiveThreshold: s.inactiveThreshold,
		DeliverPolicy:     jetstream.DeliverAllPolicy,
	}
	cons, err := s.js.CreateOrUpdateConsumer(ctx, StreamName, cfg)
	if err != nil {
		return nil, oops.Code("EVENTBUS_SESSION_CONSUMER_FAILED").
			With("session_id", sessionID).
			With("consumer", name).
			Wrap(err)
	}
	return newSessionStream(ctx, sessionID, name, s.js, s.selector, s.ackWait, s.maxAckPending, s.inactiveThreshold, cons)
}

// newSessionStream constructs the session-backed iterator with a pending-msg
// channel fed by consumer.Consume. Consume is used instead of Messages because
// it issues an immediate initial pull and manages re-pull on every handler
// return, which is the semantics our gRPC Subscribe handler expects (a single
// event at a time, block until one arrives, ack, fetch next).
func newSessionStream(
	_ context.Context,
	sessionID, consumerName string,
	js jetstream.JetStream,
	selector codec.KeySelector,
	ackWait time.Duration,
	maxPending int,
	inactiveTTL time.Duration,
	cons jetstream.Consumer,
) (*jetStreamSessionStream, error) {
	// Buffered channel matches MaxAckPending: Consume blocks on the
	// callback, so a slow consumer cannot cause unbounded buffering —
	// but we want enough slack for a burst to land in buffers between
	// acks.
	bufSize := maxPending
	if bufSize <= 0 {
		bufSize = 1
	}
	s := &jetStreamSessionStream{
		sessionID:    sessionID,
		consumerName: consumerName,
		js:           js,
		selector:     selector,
		ackWait:      ackWait,
		maxPending:   maxPending,
		inactiveTTL:  inactiveTTL,
		cons:         cons,
		inbox:        make(chan jetstream.Msg, bufSize),
	}
	cc, err := cons.Consume(s.handle, jetstream.PullMaxMessages(maxPending))
	if err != nil {
		return nil, oops.Code("EVENTBUS_SESSION_CONSUME_FAILED").
			With("session_id", sessionID).
			With("consumer", consumerName).
			Wrap(err)
	}
	s.cc = cc
	return s, nil
}

// Subscriber returns a Subscriber backed by this Subsystem. Nil when the
// Subsystem has not started.
func (s *Subsystem) Subscriber(opts ...SubscribeOption) Subscriber {
	if s.js == nil {
		return nil
	}
	return NewJetStreamSubscriber(s.js, opts...)
}

// jetStreamSessionStream binds a session's durable consumer to a live pull
// iterator. SetFilters re-issues CreateOrUpdateConsumer with new FilterSubjects;
// JetStream preserves the cursor across the update. Close tears down the local
// iterator only — the consumer stays durable for reconnect (spec §5).
type jetStreamSessionStream struct {
	sessionID    string
	consumerName string
	js           jetstream.JetStream
	selector     codec.KeySelector
	ackWait      time.Duration
	maxPending   int
	inactiveTTL  time.Duration

	cons   jetstream.Consumer
	cc     jetstream.ConsumeContext
	inbox  chan jetstream.Msg
	closed atomic.Bool // guards Close idempotency against concurrent Close + pending Next
}

// handle is the Consume callback. Pushes the raw jetstream.Msg onto the
// session's inbox channel; Next reads from the inbox on demand. Blocking on
// the channel send is intentional: it provides the natural backpressure
// between JetStream's MaxAckPending and our per-session consumer.
func (j *jetStreamSessionStream) handle(msg jetstream.Msg) {
	// A closed inbox means Close was called; drop the message — JS will
	// redeliver after AckWait if the consumer rebinds.
	defer func() {
		// Recover from send-on-closed-channel in the rare case of a
		// race between the Consume goroutine and Close. The recovered
		// value is intentionally discarded: the only possible panic
		// here is "send on closed channel", which is a normal
		// teardown outcome, not a caller-actionable error.
		if r := recover(); r != nil {
			_ = r
		}
	}()
	j.inbox <- msg
}

// Next blocks on the inbox channel until a delivery arrives, ctx is
// cancelled, or the iterator is drained/closed. Decodes the payload via the
// registered codec and returns a Delivery typed handle.
func (j *jetStreamSessionStream) Next(ctx context.Context) (Delivery, error) {
	select {
	case <-ctx.Done():
		return nil, oops.Wrap(ctx.Err())
	case msg, ok := <-j.inbox:
		if !ok {
			return nil, oops.Wrap(jetstream.ErrMsgIteratorClosed)
		}
		event, err := decodeDelivery(ctx, msg, j.selector)
		if err != nil {
			return nil, err
		}
		return &jetStreamDelivery{msg: msg, event: event}, nil
	}
}

// SetFilters atomically replaces the FilterSubjects on the underlying durable
// consumer. JetStream preserves the cursor across the update so events already
// acked stay acked; events newly matching the filter begin delivering; events
// that no longer match stop delivering. Per spec §5 SetFilters semantics.
//
// Callers SHOULD serialize SetFilters against concurrent SetFilters for the
// same sessionID. The subscriber does not serialize internally because the
// owning gRPC Subscribe handler already owns the session's control plane.
func (j *jetStreamSessionStream) SetFilters(ctx context.Context, filters []Subject) error {
	if j.js == nil {
		return oops.Code("EVENTBUS_SUBSCRIBER_NOT_READY").Errorf("JetStream context is nil")
	}
	subjects := subjectsToStrings(filters)
	// Mirrors the OpenSession guard: an empty filter set is indistinguishable
	// from an unfiltered consumer once it reaches JetStream, so fail fast.
	if len(subjects) == 0 {
		return oops.Code("EVENTBUS_SESSION_FILTERS_REQUIRED").
			With("session_id", j.sessionID).
			Errorf("at least one subject filter required")
	}
	cfg := jetstream.ConsumerConfig{
		Durable:           j.consumerName,
		Name:              j.consumerName,
		FilterSubjects:    subjects,
		AckPolicy:         jetstream.AckExplicitPolicy,
		AckWait:           j.ackWait,
		MaxAckPending:     j.maxPending,
		InactiveThreshold: j.inactiveTTL,
		DeliverPolicy:     jetstream.DeliverAllPolicy,
	}
	_, err := j.js.CreateOrUpdateConsumer(ctx, StreamName, cfg)
	if err != nil {
		return oops.Code("EVENTBUS_SESSION_SETFILTERS_FAILED").
			With("session_id", j.sessionID).
			With("consumer", j.consumerName).
			Wrap(err)
	}
	return nil
}

// Close tears down the local Consume loop. Does NOT delete the durable
// consumer — it persists across reconnects so the session resumes from the
// last acked seq. Idempotent: safe to call multiple times.
//
// Concurrency: Close may race with in-flight Next / handle callbacks. We
// must NOT set j.inbox = nil: readers in Next already hold a reference to
// the channel, and nilling it racily is undefined. Instead we close the
// channel and let the closed-state propagate (receivers see ok=false).
// handle() guards against a second close via recover.
func (j *jetStreamSessionStream) Close() error {
	if j.closed.Swap(true) {
		return nil
	}
	if j.cc != nil {
		j.cc.Stop()
		// Wait for Consume to finish draining so handle() can't race
		// a channel close below. Bounded by the AckWait on the
		// consumer config so a slow server cannot pin Close forever.
		select {
		case <-j.cc.Closed():
		case <-time.After(j.ackWait + time.Second):
		}
	}
	// Close (but do NOT nil) the inbox. Pending receivers in Next see
	// ok=false and return ErrMsgIteratorClosed. A concurrent send from
	// handle() is guarded by the recover() there.
	close(j.inbox)
	return nil
}

// jetStreamDelivery wraps a jetstream.Msg as an eventbus.Delivery.
type jetStreamDelivery struct {
	msg   jetstream.Msg
	event Event
}

func (d *jetStreamDelivery) Event() Event { return d.event }
func (d *jetStreamDelivery) Ack() error   { return oops.Wrap(d.msg.Ack()) }
func (d *jetStreamDelivery) Nack() error  { return oops.Wrap(d.msg.Nak()) }
func (d *jetStreamDelivery) InProgress() error {
	return oops.Wrap(d.msg.InProgress())
}

// decodeDelivery reads headers, resolves codec, decodes payload, and
// proto-unmarshals to an eventbus.Event. Mirrors the publisher's encode path
// in reverse — any header missing/empty is a contract violation.
func decodeDelivery(ctx context.Context, msg jetstream.Msg, selector codec.KeySelector) (Event, error) {
	h := msg.Headers()
	// Extract OTEL context off the wire before decode so downstream spans
	// link. No-op when the publisher did not inject headers.
	_ = telemetry.ExtractContext(ctx, h)

	msgIDStr := h.Get(HeaderMsgID)
	if msgIDStr == "" {
		return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_MISSING_HEADER").
			With("header", HeaderMsgID).Errorf("missing header")
	}
	id, err := ulid.Parse(msgIDStr)
	if err != nil {
		return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_BAD_MSG_ID").
			With("value", msgIDStr).Wrap(err)
	}
	schemaVer := h.Get(HeaderSchemaVersion)
	if schemaVer == "" {
		return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_MISSING_HEADER").
			With("header", HeaderSchemaVersion).Errorf("missing header")
	}
	if schemaVer != SchemaVersion {
		return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_SCHEMA_MISMATCH").
			With("got", schemaVer).
			With("want", SchemaVersion).
			Errorf("schema version mismatch")
	}
	codecNameStr := h.Get(HeaderCodec)
	if codecNameStr == "" {
		return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_MISSING_HEADER").
			With("header", HeaderCodec).Errorf("missing header")
	}
	c, err := codec.Resolve(codec.Name(codecNameStr))
	if err != nil {
		return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_UNKNOWN_CODEC").
			With("codec", codecNameStr).Wrap(err)
	}

	// DECISION 0: proto-unmarshal FIRST. msg.Data is the marshaled
	// envelope (cleartext fields + maybe-ciphertext payload field).
	var envelope eventbusv1.Event
	if unmarshalErr := proto.Unmarshal(msg.Data(), &envelope); unmarshalErr != nil {
		return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_UNMARSHAL_FAILED").Wrap(unmarshalErr)
	}

	// For identity codec, envelope.Payload IS the plaintext — no decode.
	// For sensitive codecs, T9 will add AuthGuard.Check + AAD reconstruct
	// + decrypt-or-stamp-metadata_only here. T1 keeps the existing
	// Phase 3a "non-identity gets a passthrough decode with nil AAD"
	// behavior so that tests remain green; that gets replaced in T9.
	if codec.Name(codecNameStr) != codec.NameIdentity {
		var key codec.Key
		if selector != nil {
			k, kerr := selector.SelectForDecrypt(ctx, codec.Name(codecNameStr), 0)
			if kerr != nil {
				return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_KEY_FETCH_FAILED").
					With("codec", codecNameStr).Wrap(kerr)
			}
			key = k
		}
		plain, decErr := c.Decode(ctx, envelope.Payload, key, nil)
		if decErr != nil {
			return Event{}, oops.Code("EVENTBUS_SUBSCRIBE_DECODE_FAILED").
				With("codec", codecNameStr).Wrap(decErr)
		}
		envelope.Payload = plain
	}

	ev := Event{
		ID:        id,
		Subject:   Subject(envelope.GetSubject()),
		Type:      Type(envelope.GetType()),
		Timestamp: envelope.GetTimestamp().AsTime(),
		Actor:     actorFromProto(envelope.GetActor()),
		Payload:   envelope.GetPayload(),
		Rendering: RenderingFromProto(envelope.GetRendering()),
	}
	if meta, mErr := msg.Metadata(); mErr == nil && meta != nil {
		ev.Seq = meta.Sequence.Stream
	}
	return ev, nil
}

// AckSyncForTest performs a server-confirmed ack on a Delivery that was
// produced by this package. Tests that need an ack barrier before reopening
// a session call this to avoid racing the durable consumer's cursor. Not for
// production use — production code calls Delivery.Ack which is fire-and-forget.
func AckSyncForTest(ctx context.Context, d Delivery) error {
	jd, ok := d.(*jetStreamDelivery)
	if !ok {
		//nolint:wrapcheck // forwarding to caller-owned Delivery interface
		return d.Ack()
	}
	return oops.Wrap(jd.msg.DoubleAck(ctx))
}

// DeliveryMetadataForTest exposes the underlying jetstream.MsgMetadata of a
// Delivery produced by this package. Only the invariant-ordering test needs
// the server-assigned Stream seq to prove cross-subscriber sequence
// identity. NOT for production use — Delivery intentionally hides this.
func DeliveryMetadataForTest(d Delivery) (*jetstream.MsgMetadata, error) {
	jd, ok := d.(*jetStreamDelivery)
	if !ok {
		return nil, oops.Code("EVENTBUS_DELIVERY_UNKNOWN_IMPL").
			Errorf("Delivery is not the jetstream-backed implementation")
	}
	//nolint:wrapcheck // forwarding server metadata to test caller
	return jd.msg.Metadata()
}

func actorFromProto(a *eventbusv1.Actor) Actor {
	if a == nil {
		return Actor{}
	}
	out := Actor{Kind: actorKindFromProto(a.GetKind())}
	if id := a.GetId(); len(id) == 16 {
		var u ulid.ULID
		copy(u[:], id)
		out.ID = u
	} else if legacy := a.GetLegacyId(); legacy != "" {
		// Plugin-authored actors carry a string LegacyID rather than a
		// ULID. Propagate it here so non-ULID actor identities aren't
		// silently dropped on the subscriber path.
		out.LegacyID = legacy
	}
	return out
}

func actorKindFromProto(k eventbusv1.ActorKind) ActorKind {
	switch k {
	case eventbusv1.ActorKind_ACTOR_KIND_CHARACTER:
		return ActorKindCharacter
	case eventbusv1.ActorKind_ACTOR_KIND_PLAYER:
		return ActorKindPlayer
	case eventbusv1.ActorKind_ACTOR_KIND_SYSTEM:
		return ActorKindSystem
	case eventbusv1.ActorKind_ACTOR_KIND_PLUGIN:
		return ActorKindPlugin
	default:
		return ActorKindUnknown
	}
}

func subjectsToStrings(filters []Subject) []string {
	if len(filters) == 0 {
		return nil
	}
	out := make([]string, 0, len(filters))
	for _, f := range filters {
		out = append(out, string(f))
	}
	return out
}
