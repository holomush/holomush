// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// defaultSweepInterval is the periodic fallback wake if a LISTEN/NOTIFY is missed.
const defaultSweepInterval = 5 * time.Second

// defaultMaxPublishAttempts bounds transient publish retries per row before a
// Drain gives up for this pass (a sustained broker outage is transient, NOT
// poison — the relay resumes on the next sweep, in order).
const defaultMaxPublishAttempts = 3

// errPoison is the internal signal that a row is PERMANENTLY unpublishable
// (malformed envelope / rejected by the publisher's validation). It HALTS the
// relay — it MUST NOT be treated as a transient outage.
var errPoison = errors.New("outbox: poison (permanently unpublishable) envelope")

// Waker blocks until a wakeup arrives (a transaction-side pg_notify) or ctx is
// done. The concrete LISTEN-connection impl lives in internal/world/postgres and
// is injected; a nil Waker makes the relay sweep-only.
type Waker interface {
	Wait(ctx context.Context) error
}

// RelayConfig configures a Relay.
type RelayConfig struct {
	// Store hands out the fenced Lease. Required.
	Store OutboxStore
	// Publisher publishes world-change envelopes to JetStream. Required. The
	// publisher stamps Nats-Msg-Id = Event.ID (the event ULID) for dedup.
	Publisher eventbus.Publisher
	// GameID is the per-game feed this relay drains. Required.
	GameID string
	// Waker is the LISTEN wakeup; nil => sweep-only.
	Waker Waker
	// SweepInterval is the periodic fallback wake. Defaults to defaultSweepInterval.
	SweepInterval time.Duration
	// MaxPublishAttempts bounds transient retries per row per pass. Defaults to
	// defaultMaxPublishAttempts.
	MaxPublishAttempts int
	// Logger is optional.
	Logger *slog.Logger
}

// Relay is the single leased, position-ordered publisher of world-change
// envelopes. It reaches storage ONLY through a Lease (round-3 blocker #2), so its
// DB ops structurally run on the lock-holding connection. Publishing is
// AT-LEAST-ONCE across the external broker boundary: a partitioned old holder MAY
// already have sent a message PG-side fencing can never un-send (round-3 blocker
// #2). The fencing generation rejects a stale holder's DB ack, bounding DB-side
// progress to the current holder; wire-side correctness is Nats-Msg-Id dedup +
// the idempotent reference consumer. Split-brain double-publish is POSSIBLE,
// harmless, and deduplicated — never claimed impossible.
type Relay struct {
	cfg RelayConfig

	mu           sync.Mutex
	lease        Lease
	halted       bool
	haltPosition int64
}

// NewRelay constructs a Relay.
func NewRelay(cfg RelayConfig) *Relay {
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = defaultSweepInterval
	}
	if cfg.MaxPublishAttempts <= 0 {
		cfg.MaxPublishAttempts = defaultMaxPublishAttempts
	}
	return &Relay{cfg: cfg}
}

func (r *Relay) log() *slog.Logger {
	if r.cfg.Logger != nil {
		return r.cfg.Logger
	}
	return slog.Default()
}

// Halted reports whether the relay is halted on a poison row and, if so, at what
// feed_position — the halt-position health signal.
func (r *Relay) Halted() (halted bool, position int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.halted, r.haltPosition
}

// cancelErr wraps a context cancellation sentinel so callers still match it with
// errors.Is(err, context.Canceled) while satisfying wrapcheck.
func cancelErr(ctx context.Context) error {
	return oops.Code("WORLD_OUTBOX_CANCELLED").Wrap(ctx.Err())
}

// ensureLease acquires a fresh fenced lease if one is not currently held.
func (r *Relay) ensureLease(ctx context.Context) error {
	if r.lease != nil {
		return nil
	}
	lease, err := r.cfg.Store.AcquireLease(ctx, r.cfg.GameID)
	if err != nil {
		return oops.Code("WORLD_OUTBOX_ACQUIRE_LEASE_FAILED").
			With("game_id", r.cfg.GameID).Wrap(err)
	}
	r.lease = lease
	return nil
}

// dropLease releases and forgets the current lease (cancel-on-loss). The next
// operation re-acquires with a fresh, durably-bumped generation.
func (r *Relay) dropLease(ctx context.Context) {
	if r.lease == nil {
		return
	}
	if err := r.lease.Release(ctx); err != nil {
		r.log().WarnContext(ctx, "outbox relay: lease release failed", "err", err, "game_id", r.cfg.GameID)
	}
	r.lease = nil
}

// Stop releases the lease. Idempotent.
func (r *Relay) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropLease(ctx)
	return nil
}

// Run drains the feed, then blocks on a wakeup (LISTEN) or the periodic sweep,
// repeating until ctx is done.
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		if _, err := r.Drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
			r.log().WarnContext(ctx, "outbox relay drain error", "err", err, "game_id", r.cfg.GameID)
		}
		if ctx.Err() != nil {
			return cancelErr(ctx)
		}
		if r.cfg.Waker != nil {
			wctx, cancel := context.WithTimeout(ctx, r.cfg.SweepInterval)
			_ = r.cfg.Waker.Wait(wctx) //nolint:errcheck // wakeup/deadline signal; the relay re-drains regardless
			cancel()
			if ctx.Err() != nil {
				return cancelErr(ctx)
			}
		} else {
			select {
			case <-ctx.Done():
				return cancelErr(ctx)
			case <-ticker.C:
			}
		}
	}
}

// Drain publishes every currently-unpublished row for the game in strict
// (epoch, feed_position) order, marking each published only after PubAck. It
// stops when the feed is drained, a transient publish outage occurs (retried on
// the next pass — resume-after-outage, in order), or a poison row HALTS the relay
// (halt-and-alert; positions after it are NOT published). Returns the count
// published this pass.
func (r *Relay) Drain(ctx context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.ensureLease(ctx); err != nil {
		return 0, err
	}

	published := 0
	for {
		if ctx.Err() != nil {
			return published, cancelErr(ctx)
		}

		env, err := r.lease.NextUnpublished(ctx)
		if IsStaleLease(err) {
			r.dropLease(ctx)
			if aerr := r.ensureLease(ctx); aerr != nil {
				return published, aerr
			}
			continue
		}
		if err != nil {
			return published, oops.Code("WORLD_OUTBOX_NEXT_UNPUBLISHED_FAILED").Wrap(err)
		}

		// Halt bookkeeping: once halted we stay halted until the poison position
		// is resolved (an operator skip published the same-position marker and
		// resolved the row, so the lowest unpublished position has advanced).
		if r.halted {
			if env == nil || env.FeedPosition != r.haltPosition {
				r.clearHalt()
			} else {
				return published, nil
			}
		}

		if env == nil {
			return published, nil // feed drained
		}

		if perr := r.publishOne(ctx, *env); perr != nil {
			if IsStaleLease(perr) {
				// Stale DB ack: the row is (possibly) already on the wire
				// (at-least-once — dedup absorbs it). Re-acquire and let the new
				// holder republish/mark; do not treat as poison.
				r.dropLease(ctx)
				if aerr := r.ensureLease(ctx); aerr != nil {
					return published, aerr
				}
				continue
			}
			if errors.Is(perr, errPoison) {
				r.halt(env.FeedPosition)
				r.log().ErrorContext(ctx, "outbox relay halted on poison envelope",
					"game_id", r.cfg.GameID, "feed_position", env.FeedPosition,
					"epoch", env.Epoch, "event_id", env.EventID.String(), "err", perr)
				return published, nil
			}
			// Transient outage: stop this pass; resume in order next sweep.
			r.log().WarnContext(ctx, "outbox relay transient publish failure; will retry",
				"game_id", r.cfg.GameID, "feed_position", env.FeedPosition, "err", perr)
			return published, nil
		}
		published++
	}
}

// publishOne builds the wire event, publishes it (retrying transient failures up
// to MaxPublishAttempts), and — only after PubAck — marks the row published under
// the fencing generation. Returns errPoison for a permanently unpublishable row,
// ErrStaleLease when the DB ack is fenced out, a transient error otherwise.
func (r *Relay) publishOne(ctx context.Context, env wmodel.Envelope) error {
	ev, err := EnvelopeToEvent(env)
	if err != nil {
		// A malformed envelope can never be published — poison.
		return oops.Wrapf(errPoison, "build wire event: %v", err)
	}

	var lastErr error
	for attempt := 1; attempt <= r.cfg.MaxPublishAttempts; attempt++ {
		if ctx.Err() != nil {
			return cancelErr(ctx)
		}
		lastErr = r.cfg.Publisher.Publish(ctx, ev)
		if lastErr == nil {
			break
		}
		if isPermanentPublishErr(lastErr) {
			return oops.Wrapf(errPoison, "publish rejected: %v", lastErr)
		}
	}
	if lastErr != nil {
		if isPermanentPublishErr(lastErr) {
			return oops.Wrapf(errPoison, "publish rejected: %v", lastErr)
		}
		return oops.Code("WORLD_OUTBOX_PUBLISH_TRANSIENT").Wrap(lastErr)
	}

	if err := r.lease.MarkPublished(ctx, env.EventID, r.lease.Generation()); err != nil {
		if IsStaleLease(err) {
			return ErrStaleLease
		}
		return oops.Code("WORLD_OUTBOX_MARK_PUBLISHED_FAILED").
			With("event_id", env.EventID.String()).Wrap(err)
	}
	relayPublished.WithLabelValues(r.cfg.GameID).Inc()
	return nil
}

// halt records the poison halt and raises the halt/lag alert metrics.
func (r *Relay) halt(position int64) {
	r.halted = true
	r.haltPosition = position
	relayHalts.WithLabelValues(r.cfg.GameID).Inc()
	relayHaltPosition.WithLabelValues(r.cfg.GameID).Set(float64(position))
}

// clearHalt clears the halt after the poison position was resolved.
func (r *Relay) clearHalt() {
	r.halted = false
	r.haltPosition = 0
	relayHaltPosition.WithLabelValues(r.cfg.GameID).Set(0)
}

// isPermanentPublishErr classifies a publish error as permanent (poison) vs
// transient (retry / resume). Transport failures (broker down, publish deadline)
// are transient; envelope-validation failures are permanent.
func isPermanentPublishErr(err error) bool {
	if err == nil {
		return false
	}
	var oe oops.OopsError
	if errors.As(err, &oe) {
		switch oe.Code() {
		case "EVENTBUS_PUBLISH_EXPIRED", "EVENTBUS_PUBLISH_FAILED", "EVENTBUS_PUBLISHER_NOT_READY":
			return false
		case "EVENTBUS_PAYLOAD_TOO_LARGE", "EVENTBUS_EVENT_ID_REQUIRED",
			"WORLD_OUTBOX_QUALIFY_FAILED", "WORLD_OUTBOX_BAD_KIND",
			"WORLD_OUTBOX_WIRE_MARSHAL_FAILED":
			return true
		}
	}
	if errors.Is(err, eventbus.ErrPayloadTooLarge) ||
		errors.Is(err, eventbus.ErrInvalidSubject) ||
		errors.Is(err, eventbus.ErrInvalidType) {
		return true
	}
	return false
}
