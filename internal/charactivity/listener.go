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
//
// It is a bounded RETRY rather than a single pass, because "the key moved" and
// "the key moved to something newer" are not the same fact. The interleaving
// that makes the difference load-bearing is the flusher's:
//
//  1. This listener Gets the key at revision N, holding a NEWER timestamp.
//  2. The flusher drains that same revision N — it writes the OLDER value to
//     the column and DeleteRevision(key, N) succeeds, so the key is gone.
//  3. This listener's Update at revision N is refused by the server.
//
// Treating that refusal as "someone stored something at least as fresh" drops
// the newer timestamp outright and leaves last_active_at behind by the delta
// until the character next acts — the very loss the monotonic guard above
// exists to prevent, merely relocated. Re-reading lands on the ErrKeyNotFound
// branch instead, where Create restores it (and nats.go's Create retries past
// the delete marker itself, jetstream/kv.go:1074). A genuinely newer concurrent
// value settles the retry at the monotonic early exit on the next pass, so the
// loop converges rather than spinning.
func (l *listener) buffer(ctx context.Context, id ulid.ULID, nanos int64) {
	key := id.String()
	val := []byte(strconv.FormatInt(nanos, 10))

	var lastErr error
	for attempt := 0; attempt < bufferAttempts; attempt++ {
		settled, err := l.tryBuffer(ctx, id, key, val, nanos)
		if settled {
			return
		}
		lastErr = err
	}
	if lastErr != nil {
		// NOT contention: a transport failure, a deleted bucket or a cancelled
		// context, which no amount of re-reading cures. Distinguishing it
		// matters because the give-up line below is invisible at the default
		// level, so folding the two together left a persistent failure confined
		// to already-buffered keys logging nothing an operator would ever see.
		l.logBufferFailure(ctx, id, lastErr)
		return
	}
	// Sustained contention on ONE character's key. Debug, not error: the buffer
	// is best-effort by construction and the next event this character causes
	// re-buffers.
	slog.DebugContext(ctx, "character activity listener gave up buffering under contention",
		"character_id", key, "attempts", bufferAttempts)
}

// bufferAttempts bounds the compare-and-set retry. Each refusal means the key
// moved between the read and the write, and a re-read either finds a newer
// value (settled at the monotonic exit) or an absent/older one (settled by
// Create/Update). Three passes is slack for a flusher tick and a redelivery
// landing on the same key at once; it is NOT a spin-until-success loop.
const bufferAttempts = 3

// tryBuffer performs one compare-and-set pass over the buffered value.
//
// It reports whether the outcome is SETTLED — stored, deliberately skipped as
// not-newer, or failed in a way no re-read can cure — and, when NOT settled,
// the error that refused the write. A nil error alongside a false return is a
// pure revision refusal (the key moved); a non-nil one is an infrastructure
// failure the caller escalates once the attempts are spent, rather than
// reporting every exhausted loop as contention.
func (l *listener) tryBuffer(ctx context.Context, id ulid.ULID, key string, val []byte, nanos int64) (bool, error) {
	entry, err := l.kv.Get(ctx, key)
	switch {
	case errors.Is(err, jetstream.ErrKeyNotFound):
		// Nothing buffered. Create rather than Put so a concurrent first
		// writer is detected instead of clobbered.
		if _, createErr := l.kv.Create(ctx, key, val); createErr != nil {
			if errors.Is(createErr, jetstream.ErrKeyExists) {
				// Someone else buffered between our read and our Create. Their
				// value may well be OLDER than ours — they raced us from the
				// same absent state — so re-read rather than assume we lost.
				return false, nil
			}
			l.logBufferFailure(ctx, id, createErr)
		}
		return true, nil
	case err != nil:
		l.logBufferFailure(ctx, id, err)
		return true, nil
	default:
		// An UNPARSABLE buffered value is not newer than anything — fall
		// through and cure it, exactly as the flusher's revision-conditional
		// drop of the same value would.
		if prev, perr := strconv.ParseInt(string(entry.Value()), 10, 64); perr == nil && prev >= nanos {
			return true, nil // an older (or duplicate) redelivery MUST NOT clobber newer buffered activity
		}
		if _, updErr := l.kv.Update(ctx, key, val, entry.Revision()); updErr != nil {
			if errors.Is(updErr, jetstream.ErrKeyExists) {
				// A pure REVISION refusal — the key moved between the Get and
				// the Update: a concurrent write OR the flusher's delete of the
				// very revision we read. Which one it was is only knowable by
				// re-reading, and exhausting the attempts on it is contention,
				// not failure. (nats.go surfaces a wrong-last-sequence publish
				// as ErrKeyExists, matched by error code 10071.)
				return false, nil
			}
			// Anything else — transport down, bucket deleted, ctx cancelled —
			// is NOT contention and no re-read cures it. Re-read anyway (the
			// loop is bounded at three), but carry the error so an exhausted
			// loop reports the real cause instead of blaming contention.
			return false, updErr
		}
		return true, nil
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
