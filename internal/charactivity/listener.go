// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charactivity

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// listener is the handler behind the durable consumer. Its whole job is one KV
// Put per character-actor event.
//
// IT HOOKS ONLY BUS EVENTS. The connection lease-refresh path in
// internal/session/session.go is explicitly NOT a source: it fires once per
// lease interval per connection, which is exactly the hot per-connection write
// D-24 and D-42 both forbid. Activity is inferred from events the character
// already caused, and from nothing else. (The forbidden symbol is deliberately
// not spelled here — the phase's acceptance gate is a grep for it over this
// package, and a mention in prose would defeat a mechanical check.)
type listener struct {
	cfg Config
	kv  activityKV
	// ctx is the Activate context, stored so handle (which JetStream invokes on
	// its own goroutine, with no context of its own) can propagate cancellation
	// and trace context into the Put.
	ctx context.Context //nolint:containedctx // lifecycle ctx, not a request ctx — same shape as audit.projection.workerCtx
}

// handle is the Consume callback.
//
// It ALWAYS acks. A buffered-activity loss is operational degradation bounded
// by the next event the character causes; an unacked message would occupy a
// MaxAckPending slot and eventually stall a consumer subscribed to EVERY
// subject on the stream. The trade is deliberately asymmetric.
func (l *listener) handle(msg jetstream.Msg) {
	l.record(l.workerContext(), msg.Data())
	_ = msg.Ack() //nolint:errcheck // an ack failure is absorbed by redelivery, which is idempotent here
}

// record decodes one wire event and buffers its actor's activity.
//
// The actor and the timestamp are read from the message BODY rather than from
// the App-Actor-* headers: the body is the same envelope the publisher built,
// and it is the only place the event's own timestamp survives (the headers
// carry none, and JetStream's metadata timestamp is STORE time). Payload
// sensitivity is irrelevant here — only the cleartext envelope metadata is read.
func (l *listener) record(ctx context.Context, data []byte) {
	var wire eventbusv1.Event
	if err := proto.Unmarshal(data, &wire); err != nil {
		errutil.LogErrorContext(ctx, "character activity listener could not decode a delivered event",
			oops.Code("CHARACTER_ACTIVITY_EVENT_UNMARSHAL_FAILED").Wrap(err))
		return
	}

	actor := wire.GetActor()
	if actor.GetKind() != eventbusv1.ActorKind_ACTOR_KIND_CHARACTER {
		return // only a character actor means "this character did something"
	}
	var id ulid.ULID
	if len(actor.GetId()) != len(id) {
		// A character actor with no id names nobody. Publishers omit the field
		// only for a zero ULID, which is not a character.
		return
	}
	copy(id[:], actor.GetId())

	nanos := wire.GetTimestamp().AsTime().UnixNano()
	if nanos <= 0 {
		// 0 is characters.last_active_at's never-active sentinel, so buffering
		// it would advance nothing and merely add a key for the flusher to
		// churn on.
		return
	}

	l.buffer(ctx, id, nanos)
}

// buffer records one activity timestamp, MONOTONICALLY.
//
// An unconditional Put would let an older event destroy a newer buffered value.
// Delivery is at-least-once and unordered: a message whose AckWait expired
// after a later one was already handled is redelivered, and its older
// timestamp would overwrite the newer one at a FRESH revision. The flusher
// would then write the older value, the database predicate
// (last_active_at < $2, internal/world/postgres/activity.go:66) would absorb it
// as a no-op, and the newer timestamp would be gone entirely — leaving
// last_active_at behind by the delta until the character next acts. That
// exceeds the "bounded by up to one flush interval, BY CONSTRUCTION" claim in
// the package doc, so the guard belongs here and not only at the database.
//
// The write is revision-conditional for the same reason the flusher's delete
// is: a concurrent writer that already advanced the key must win, and losing
// that race is harmless because the value that won is the newer one.
func (l *listener) buffer(ctx context.Context, id ulid.ULID, nanos int64) {
	key := id.String()
	val := []byte(strconv.FormatInt(nanos, 10))

	entry, err := l.kv.Get(ctx, key)
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound):
		// Nothing buffered. Create rather than Put so a concurrent first
		// writer is detected instead of clobbered.
		if _, createErr := l.kv.Create(ctx, key, val); createErr != nil {
			if errors.Is(createErr, jetstream.ErrKeyExists) {
				// Someone else buffered first. Their value is at least as
				// fresh as ours was when we read; the next event re-buffers.
				return
			}
			l.logBufferFailure(ctx, id, createErr)
		}
	case err != nil:
		l.logBufferFailure(ctx, id, err)
	default:
		// An UNPARSABLE buffered value is not newer than anything — fall
		// through and cure it, exactly as the flusher's revision-conditional
		// drop of the same value would.
		if prev, perr := strconv.ParseInt(string(entry.Value()), 10, 64); perr == nil && prev >= nanos {
			return // an older (or duplicate) redelivery MUST NOT clobber newer buffered activity
		}
		if _, updErr := l.kv.Update(ctx, key, val, entry.Revision()); updErr != nil {
			// The key moved under us, which means a concurrent writer stored a
			// value we have no reason to believe is staler than ours. Dropping
			// this one is the safe answer; the next event re-buffers.
			slog.DebugContext(ctx, "character activity listener left a buffered value that changed under it",
				"character_id", key, "revision", entry.Revision())
		}
	}
}

func (l *listener) logBufferFailure(ctx context.Context, id ulid.ULID, err error) {
	errutil.LogErrorContext(ctx, "character activity listener could not buffer an activity timestamp",
		oops.Code("CHARACTER_ACTIVITY_BUFFER_FAILED").With("character_id", id.String()).Wrap(err),
		"bucket", BucketName)
}

// workerContext returns the context the Consume callback runs its Put under.
func (l *listener) workerContext() context.Context {
	if l.ctx == nil {
		return context.Background()
	}
	return l.ctx
}
