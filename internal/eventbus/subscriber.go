// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
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

// WithSubscriberAuthGuard injects the AuthGuard for sensitive event delivery
// decisions. When nil (the default), the pre-Phase 3b identity-codec passthrough
// path is used for all events. Required when deploying with Crypto.Enabled=true.
func WithSubscriberAuthGuard(g SessionAuthGuard) SubscribeOption {
	return func(s *JetStreamSubscriber) { s.authGuard = g }
}

// WithSubscriberDEKManager injects the DEK Manager used to resolve plaintext
// keys during decryption. Required when WithSubscriberAuthGuard is set.
func WithSubscriberDEKManager(m SessionDEKManager) SubscribeOption {
	return func(s *JetStreamSubscriber) { s.dekManager = m }
}

// WithSubscriberDecryptAuditEmitter injects the audit emitter for plugin decrypt
// records (Decision 3, master spec §7.6). Optional — nil is safe when no plugin
// sessions are expected (but required when plugins subscribe to sensitive events).
func WithSubscriberDecryptAuditEmitter(em SessionAuditEmitter) SubscribeOption {
	return func(s *JetStreamSubscriber) { s.auditEmitter = em }
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

	// authGuard is optional. nil = pre-Phase 3b passthrough (all events delivered
	// via identity-codec path). non-nil activates the Decision 5 order-of-operations
	// for sensitive (non-identity) codec events.
	authGuard SessionAuthGuard
	// dekManager resolves plaintext key material for sensitive codec events.
	// Required when authGuard is non-nil.
	dekManager SessionDEKManager
	// auditEmitter logs plugin decrypt records. nil is safe when no plugins subscribe
	// to sensitive events (plugin branch is skipped for non-plugin identities).
	auditEmitter SessionAuditEmitter
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
// identity carries the authenticated principal. Required at the API surface —
// with Crypto.Enabled=false (no authGuard set) the value is stored but unused
// on the decode path. T10 wires the construction at the gRPC authentication
// boundary.
//
// Callers MUST call SessionStream.Close when done. Close does NOT delete the
// consumer — sessions resume across reconnect via the durable's retained
// cursor. Consumer teardown is handled by InactiveThreshold (passive) or by
// the session-lifecycle listener in F5 (active, on session_ended).
func (s *JetStreamSubscriber) OpenSession(ctx context.Context, sessionID string, identity SessionIdentity, filters []Subject, minFloor time.Time) (SessionStream, error) {
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
	cfg, err := buildConsumerConfig(ctx, jsConsumerLookupAdapter{js: s.js}, StreamName, name, subjects, minFloor)
	if err != nil {
		return nil, err
	}
	// Augment cfg with the non-start-policy defaults that OpenSession owns.
	cfg.AckPolicy = jetstream.AckExplicitPolicy
	cfg.AckWait = s.ackWait
	cfg.MaxAckPending = s.maxAckPending
	cfg.InactiveThreshold = s.inactiveThreshold

	cons, err := s.js.CreateOrUpdateConsumer(ctx, StreamName, cfg)
	if err != nil {
		return nil, oops.Code("EVENTBUS_SESSION_CONSUMER_FAILED").
			With("session_id", sessionID).
			With("consumer", name).
			Wrap(err)
	}
	return newSessionStream(ctx, sessionID, name, s.js, s.selector, identity, s.authGuard, s.dekManager, s.auditEmitter, s.ackWait, s.maxAckPending, s.inactiveThreshold, cons)
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
	identity SessionIdentity,
	guard SessionAuthGuard,
	dekMgr SessionDEKManager,
	auditEm SessionAuditEmitter,
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
		identity:     identity,
		authGuard:    guard,
		dekManager:   dekMgr,
		auditEmitter: auditEm,
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

	// identity is the authenticated principal for this session. Stored on
	// the stream so decodeAndAuthorize can evaluate per-event access decisions.
	identity SessionIdentity
	// authGuard evaluates sensitive event delivery decisions (Decision 5).
	// nil = pre-Phase 3b passthrough (identity-codec path for all events).
	authGuard SessionAuthGuard
	// dekManager resolves DEK material for sensitive events.
	dekManager SessionDEKManager
	// auditEmitter logs plugin decrypt records.
	auditEmitter SessionAuditEmitter

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
		event, metaOnly, err := decodeDeliveryWithAuth(ctx, msg, j.selector, j.identity, j.authGuard, j.dekManager, j.auditEmitter)
		if err != nil {
			return nil, err
		}
		return &jetStreamDelivery{msg: msg, event: event, metadataOnly: metaOnly}, nil
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
	// SetFilters is called on an existing durable — the builder's
	// existing-consumer branch preserves DeliverPolicy/OptStartTime/OptStartSeq
	// from the live consumer, so the cursor is not reset on filter rotation.
	// minFloor is unused here (the consumer already exists); pass zero.
	cfg, err := buildConsumerConfig(ctx, jsConsumerLookupAdapter{js: j.js}, StreamName, j.consumerName, subjects, time.Time{})
	if err != nil {
		return oops.Code("EVENTBUS_SESSION_SETFILTERS_FAILED").
			With("session_id", j.sessionID).
			With("consumer", j.consumerName).
			Wrap(err)
	}
	// Augment cfg with the non-start-policy defaults that SetFilters owns.
	cfg.AckPolicy = jetstream.AckExplicitPolicy
	cfg.AckWait = j.ackWait
	cfg.MaxAckPending = j.maxPending
	cfg.InactiveThreshold = j.inactiveTTL

	_, err = j.js.CreateOrUpdateConsumer(ctx, StreamName, cfg)
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
	msg          jetstream.Msg
	event        Event
	metadataOnly bool // stamped by decodeDelivery when AuthGuard denies (T9)
}

func (d *jetStreamDelivery) Event() Event       { return d.event }
func (d *jetStreamDelivery) MetadataOnly() bool { return d.metadataOnly }
func (d *jetStreamDelivery) Ack() error         { return oops.Wrap(d.msg.Ack()) }
func (d *jetStreamDelivery) Nack() error        { return oops.Wrap(d.msg.Nak()) }
func (d *jetStreamDelivery) InProgress() error {
	return oops.Wrap(d.msg.InProgress())
}

// decodeDelivery reads headers, resolves codec, decodes payload, and
// proto-unmarshals to an eventbus.Event. Mirrors the publisher's encode path
// in reverse — any header missing/empty is a contract violation.
//
// This is the internal form used by decode_delivery_test.go unit tests and
// as the header-parsing entry point for decodeDeliveryWithAuth. The selector
// argument is retained for API compatibility with existing call sites but is
// unused when guard is nil and codec is identity (the test-path default).
func decodeDelivery(ctx context.Context, msg jetstream.Msg, _ codec.KeySelector) (Event, error) {
	ev, _, err := decodeDeliveryWithAuth(ctx, msg, nil, SessionIdentity{}, nil, nil, nil)
	return ev, err
}

// decodeDeliveryWithAuth is the full decode path that implements Decision 5
// order-of-operations. It returns (event, metadataOnly, err):
//   - err != nil: message cannot be processed at all (fail-closed).
//   - metadataOnly=true: AuthGuard denied or TOCTOU backpressure; payload is empty.
//   - metadataOnly=false: identity codec or AuthGuard permitted; payload is plaintext.
func decodeDeliveryWithAuth(
	ctx context.Context,
	msg jetstream.Msg,
	selector codec.KeySelector, // used in guard==nil fallback (T1/Phase 3a passthrough path)
	identity SessionIdentity,
	guard SessionAuthGuard,
	dekMgr SessionDEKManager,
	auditEm SessionAuditEmitter,
) (Event, bool, error) {
	h := msg.Headers()
	// Extract OTEL context off the wire before decode so downstream spans
	// link. No-op when the publisher did not inject headers.
	_ = telemetry.ExtractContext(ctx, h)

	msgIDStr := h.Get(HeaderMsgID)
	if msgIDStr == "" {
		return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_MISSING_HEADER").
			With("header", HeaderMsgID).Errorf("missing header")
	}
	id, err := ulid.Parse(msgIDStr)
	if err != nil {
		return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_BAD_MSG_ID").
			With("value", msgIDStr).Wrap(err)
	}
	schemaVer := h.Get(HeaderSchemaVersion)
	if schemaVer == "" {
		return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_MISSING_HEADER").
			With("header", HeaderSchemaVersion).Errorf("missing header")
	}
	if schemaVer != SchemaVersion {
		return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_SCHEMA_MISMATCH").
			With("got", schemaVer).
			With("want", SchemaVersion).
			Errorf("schema version mismatch")
	}
	codecNameStr := h.Get(HeaderCodec)
	if codecNameStr == "" {
		return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_MISSING_HEADER").
			With("header", HeaderCodec).Errorf("missing header")
	}
	_, err = codec.Resolve(codec.Name(codecNameStr))
	if err != nil {
		return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_UNKNOWN_CODEC").
			With("codec", codecNameStr).Wrap(err)
	}

	// DECISION 0: proto-unmarshal FIRST. msg.Data is the marshaled
	// envelope (cleartext fields + maybe-ciphertext payload field).
	var envelope eventbusv1.Event
	if unmarshalErr := proto.Unmarshal(msg.Data(), &envelope); unmarshalErr != nil {
		return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_UNMARSHAL_FAILED").Wrap(unmarshalErr)
	}
	// Stamp the parsed ULID so we don't re-parse it downstream.
	envelope.Id = id[:]

	var (
		payload      []byte
		metadataOnly bool
	)

	codecName := codec.Name(codecNameStr)
	switch {
	case codecName == codec.NameIdentity:
		// Decision 5 §2: identity codec — deliver as-is. AuthGuard NOT invoked.
		payload = envelope.GetPayload()
	case guard != nil:
		// Decision 5 §3: sensitive codec with AuthGuard wired. Full order-of-operations.
		ev, mo, authErr := decodeAndAuthorize(ctx, msg, &envelope, codecName, identity, guard, dekMgr, auditEm)
		if authErr != nil {
			return Event{}, false, authErr
		}
		// Return the event built by decodeAndAuthorize directly; populate Seq.
		if meta, mErr := msg.Metadata(); mErr == nil && meta != nil {
			ev.Seq = meta.Sequence.Stream
		}
		return ev, mo, nil
	default:
		// No AuthGuard configured (pre-Phase 3b / test without guard):
		// restore T1 Phase 3a passthrough behavior — fetch the key via the
		// selector (if present) and decode with nil AAD. Phase 3d's flag flip
		// wires AuthGuard in production; until then this branch preserves the
		// pre-T9 subscriber path so Phase 3a encrypting tests remain green.
		c, resolveErr := codec.Resolve(codecName)
		if resolveErr != nil {
			return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_UNKNOWN_CODEC").
				With("codec", codecNameStr).Wrap(resolveErr)
		}
		var key codec.Key
		if selector != nil {
			k, kerr := selector.SelectForDecrypt(ctx, codecName, 0)
			if kerr != nil {
				return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_KEY_FETCH_FAILED").
					With("codec", codecNameStr).Wrap(kerr)
			}
			key = k
		}
		plain, decErr := c.Decode(ctx, envelope.Payload, key, nil)
		if decErr != nil {
			return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_DECODE_FAILED").
				With("codec", codecNameStr).Wrap(decErr)
		}
		payload = plain
		metadataOnly = false
	}

	ev := Event{
		ID:        id,
		Subject:   Subject(envelope.GetSubject()),
		Type:      Type(envelope.GetType()),
		Timestamp: envelope.GetTimestamp().AsTime(),
		Actor:     actorFromProto(envelope.GetActor()),
		Payload:   payload,
		Rendering: RenderingFromProto(envelope.GetRendering()),
	}
	if meta, mErr := msg.Metadata(); mErr == nil && meta != nil {
		ev.Seq = meta.Sequence.Stream
	}
	return ev, metadataOnly, nil
}

// decodeAndAuthorize implements the Decision 5 §3 order-of-operations for
// sensitive (non-identity) codec deliveries. It is called by
// decodeDeliveryWithAuth when guard != nil and the codec is non-identity.
//
// Returns (event, metadataOnly, err). err != nil is fail-closed (message
// cannot be processed). metadataOnly=true signals that the AuthGuard denied
// access; the returned Event has an empty payload.
//
// Exported signature for unit testing within the same package; not part of
// the public API.
func decodeAndAuthorize(
	ctx context.Context,
	msg jetstream.Msg,
	envelope *eventbusv1.Event,
	codecName codec.Name,
	identity SessionIdentity,
	guard SessionAuthGuard,
	dekMgr SessionDEKManager,
	auditEm SessionAuditEmitter,
) (Event, bool, error) {
	h := msg.Headers()

	// Parse DEK headers: App-Dek-Ref and App-Dek-Version. Both headers are
	// required for sensitive (non-identity) codec events. Absent or empty
	// headers indicate a publisher contract violation; fail closed rather than
	// falling back to (0, 0), which would present as an authorization miss.
	dekRefStr := h.Get(HeaderDekRef)
	dekVersionStr := h.Get(HeaderDekVersion)
	if dekRefStr == "" || dekVersionStr == "" {
		return Event{}, false, oops.Code("EVENTBUS_DEK_HEADER_MISSING").
			With("has_dek_ref", dekRefStr != "").
			With("has_dek_version", dekVersionStr != "").
			With("codec", string(codecName)).
			Errorf("sensitive codec event missing required DEK headers")
	}

	var keyID codec.KeyID
	var keyVersion uint32
	// bitSize=63 enforces the parsed value fits in int64, matching the
	// crypto_keys.id BIGSERIAL column type. dek/store.go:128 casts KeyID
	// to int64 for the SQL query; this parse-site bound makes that cast
	// provably safe and silences CodeQL's incorrect-conversion warning.
	ref, parseErr := strconv.ParseUint(dekRefStr, 10, 63)
	if parseErr != nil {
		return Event{}, false, oops.Code("EVENTBUS_DEK_HEADER_PARSE_FAILED").
			With("header", HeaderDekRef).With("value", dekRefStr).Wrap(parseErr)
	}
	keyID = codec.KeyID(ref)

	ver, parseErr := strconv.ParseUint(dekVersionStr, 10, 32)
	if parseErr != nil {
		return Event{}, false, oops.Code("EVENTBUS_DEK_HEADER_PARSE_FAILED").
			With("header", HeaderDekVersion).With("value", dekVersionStr).Wrap(parseErr)
	}
	keyVersion = uint32(ver) // safe: ParseUint(bitSize=32) guarantees fits in uint32

	// Recover event ULID from the pre-stamped bytes (set by decodeDeliveryWithAuth).
	var eventID ulid.ULID
	if rawID := envelope.GetId(); len(rawID) == 16 {
		copy(eventID[:], rawID)
	}

	req := SessionCheckRequest{
		Identity:   identity,
		KeyID:      keyID,
		KeyVersion: keyVersion,
		EventType:  envelope.GetType(),
		EventID:    eventID,
	}

	decision, err := guard.Check(ctx, req)
	if err != nil {
		return Event{}, false, oops.Code("EVENTBUS_AUTHGUARD_CHECK_FAILED").
			With("event_type", envelope.GetType()).
			Wrap(err)
	}

	// If not permitted: return metadata-only event with empty payload.
	if !decision.Permit {
		ev := buildEventFromEnvelope(eventID, envelope, nil)
		ev.NoPlaintextReason = NoPlaintextReasonAuthGuardDeny
		return ev, true, nil
	}

	// Permit: resolve key, build AAD, decode plaintext.
	// Guard against misconfiguration: WithSubscriberAuthGuard set without
	// WithSubscriberDEKManager. Fail closed rather than panic.
	if dekMgr == nil {
		return Event{}, false, oops.Code("EVENTBUS_DEK_MANAGER_NIL").
			Errorf("AuthGuard permitted decrypt but DEKManager is nil — misconfiguration")
	}
	key, err := dekMgr.Resolve(ctx, keyID, keyVersion)
	if err != nil {
		return Event{}, false, oops.Code("EVENTBUS_DEK_RESOLVE_FAILED").
			With("key_id", uint64(keyID)).With("key_version", keyVersion).
			Wrap(err)
	}

	aadBytes, err := aad.Build(envelope, string(codecName), uint64(keyID), keyVersion)
	if err != nil {
		return Event{}, false, oops.Code("EVENTBUS_AAD_BUILD_FAILED").Wrap(err)
	}

	c, err := codec.Resolve(codecName)
	if err != nil {
		return Event{}, false, oops.Code("EVENTBUS_SUBSCRIBE_UNKNOWN_CODEC").
			With("codec", string(codecName)).Wrap(err)
	}

	plaintext, err := c.Decode(ctx, envelope.GetPayload(), key, aadBytes)
	if err != nil {
		return Event{}, false, oops.Code("EVENTBUS_CODEC_DECODE_FAILED").
			With("codec", string(codecName)).Wrap(err)
	}

	// Plugin recipient branch: INV-CRYPTO-11 — every plugin decrypt MUST produce an
	// audit record. Fail closed if the emitter is absent or fails unexpectedly.
	if identity.Kind == IdentityKindPlugin {
		if auditEm == nil {
			// AuthGuard permitted the read but no emitter is wired — configuration
			// error. Fail closed rather than deliver plaintext without audit.
			return Event{}, false, oops.Code("EVENTBUS_AUDIT_EMITTER_NIL").
				Errorf("AuthGuard permitted plugin decrypt but no DecryptAuditEmitter configured (INV-CRYPTO-11)")
		}
		rec := PluginDecryptRecord{
			PluginName:       identity.PluginName,
			PluginInstanceID: identity.InstanceID,
			EventID:          eventID,
			EventSubject:     Subject(envelope.GetSubject()),
			EventType:        Type(envelope.GetType()),
			DEKRef:           keyID,
			DEKVersion:       keyVersion,
			GrantID:          decision.GrantID,
		}
		if emitErr := auditEm.EmitPluginDecrypt(ctx, rec); emitErr != nil {
			// Narrow: only AUDIT_QUEUE_FULL gets the plaintext-zero +
			// metadata_only fallback (TOCTOU defense per Decision 3).
			// Any other emit error means we cannot confirm the audit
			// landed — fail closed.
			if isAuditQueueFull(emitErr) {
				for i := range plaintext {
					plaintext[i] = 0
				}
				ev := buildEventFromEnvelope(eventID, envelope, nil)
				ev.NoPlaintextReason = NoPlaintextReasonAuditQueueFull
				return ev, true, nil
			}
			return Event{}, false, oops.Code("EVENTBUS_AUDIT_EMIT_FAILED").
				With("emit_error", emitErr.Error()).
				Errorf("plugin decrypt audit emit failed — cannot confirm audit landed (INV-CRYPTO-11)")
		}
	}

	return buildEventFromEnvelope(eventID, envelope, plaintext), false, nil
}

// isAuditQueueFull reports whether err is an AUDIT_QUEUE_FULL oops error.
// Used to distinguish the narrowly acceptable TOCTOU fallback (plaintext-zero +
// metadata_only) from unexpected emit errors that must propagate (fail closed).
func isAuditQueueFull(err error) bool {
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

// buildEventFromEnvelope constructs an Event from a proto envelope and a
// (possibly nil) payload. Used by decodeAndAuthorize for both the permit and
// deny paths.
func buildEventFromEnvelope(id ulid.ULID, envelope *eventbusv1.Event, payload []byte) Event {
	return Event{
		ID:        id,
		Subject:   Subject(envelope.GetSubject()),
		Type:      Type(envelope.GetType()),
		Timestamp: envelope.GetTimestamp().AsTime(),
		Actor:     actorFromProto(envelope.GetActor()),
		Payload:   payload,
		Rendering: RenderingFromProto(envelope.GetRendering()),
	}
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
