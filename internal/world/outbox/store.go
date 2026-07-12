// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package outbox holds the world-change feed RELAY and reference CONSUMER: the
// single leased publisher that drains the transactional-outbox rows (05-05) to
// JetStream in strict (epoch, feed_position) order, and the reference idempotent
// consumer that applies them exactly-once through a durable receipt+watermark
// store.
//
// It reaches durable storage ONLY through the consumer-owned interfaces declared
// in this file (OutboxStore/Lease and ConsumerCheckpointStore/TxExecutor). The
// concrete implementations live in internal/world/postgres and are injected by
// internal/world/setup, so this package does NOT import internal/world/postgres
// (round-2 second import cycle outbox → postgres → outbox; round-3 blocker #2 —
// the Lease abstraction is what proves relay DB ops run on the lock-holding
// connection, which a pool-shaped store method could not). It also does NOT
// import internal/world (round-3 forbidden edge outbox → world).
//
// pgx IS imported here for the narrow TxExecutor row/exec types — pgx is a
// driver dependency, not one of the eight forbidden internal edges.
package outbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// ErrStaleLease is the typed signal a Lease method returns once its dedicated
// advisory-lock connection is gone or the durable lease_generation has moved
// past the lease's own generation (round-4 A2). A relay that sees it MUST drop
// the lease and re-AcquireLease (new generation) before any further DB work.
var ErrStaleLease = errors.New("outbox: stale lease")

// CodeStaleLease is the oops code stamped alongside ErrStaleLease.
const CodeStaleLease = "WORLD_OUTBOX_STALE_LEASE"

// ErrOutOfOrder is the typed signal ApplyOnce returns when a delivered envelope
// is BEYOND the next contiguous feed position for its (consumer, game): the
// watermark is not advanced and the effect is not applied, so the consumer NAKs
// the delivery for redelivery after the gap fills. A still-in-flight lower
// position is therefore never permanently skipped (round-9 R6-5 #2).
var ErrOutOfOrder = errors.New("outbox: delivery beyond next contiguous position")

// CodeOutOfOrder is the oops code stamped alongside ErrOutOfOrder.
const CodeOutOfOrder = "WORLD_OUTBOX_OUT_OF_ORDER"

// IsStaleLease reports whether err is a stale-lease signal — matched by the
// outbox sentinel OR the shared oops code (the postgres impl stamps the same code
// without importing this package, so sentinel identity cannot be relied on).
func IsStaleLease(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrStaleLease) {
		return true
	}
	return hasCode(err, CodeStaleLease)
}

// IsOutOfOrder reports whether err is a beyond-next gap signal — matched by the
// outbox sentinel OR the shared oops code.
func IsOutOfOrder(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrOutOfOrder) {
		return true
	}
	return hasCode(err, CodeOutOfOrder)
}

// hasCode reports whether any oops error in err's chain carries code.
func hasCode(err error, code string) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		var oe oops.OopsError
		if errors.As(e, &oe) {
			if oe.Code() == code {
				return true
			}
		}
	}
	return false
}

// OutboxStore is the consumer-owned storage seam for the relay. Its ONLY method
// hands out a Lease; there is no pool-shaped method the relay could use to
// bypass the lock-holding connection (round-3 blocker #2). The concrete impl
// lives in internal/world/postgres and is injected by setup.
type OutboxStore interface {
	// AcquireLease pins a DEDICATED connection, takes the session-level per-game
	// advisory lock on it, durably bumps world_feed_counter.lease_generation, and
	// returns a Lease bound to that connection carrying the bumped generation.
	AcquireLease(ctx context.Context, gameID string) (Lease, error)
}

// Lease is the single-writer capability the relay holds. Every method executes
// its SQL on the dedicated advisory-lock connection AcquireLease pinned; a method
// invoked after the connection/lock is gone returns ErrStaleLease. Because the
// relay can reach storage ONLY through a Lease, its queries structurally run on
// the lock-holding connection.
type Lease interface {
	// Generation returns the durable lease generation this lease acquired
	// (world_feed_counter.lease_generation at AcquireLease time).
	Generation() int64
	// NextUnpublished returns the lowest (epoch, feed_position) unpublished row
	// for the game, or (nil, nil) when the feed is fully drained.
	NextUnpublished(ctx context.Context) (*wmodel.Envelope, error)
	// MarkPublished marks the row for eventID published, but ONLY if the durable
	// lease_generation still equals generation (the fencing comparison, round-4
	// A2). A stale generation → ErrStaleLease; the stale holder's DB ack is
	// rejected. Wire-side dedup is separate (Nats-Msg-Id).
	MarkPublished(ctx context.Context, eventID ulid.ULID, generation int64) error
	// Prune deletes published rows for the game (retention bounding is OPS-02;
	// here it is prune-after-publish only).
	Prune(ctx context.Context) (int64, error)
	// MarkSkipResolved marks the poison row at position resolved (published_at set)
	// AFTER the SkipService PubAcked its same-position marker.
	MarkSkipResolved(ctx context.Context, position int64) error
	// CurrentEpoch returns the game's current feed epoch.
	CurrentEpoch(ctx context.Context) (int64, error)
	// PersistSkipMarkerID stores the STABLE skip-marker event id on the poison row
	// BEFORE the marker is published, so a crash-then-retry reuses the same
	// Nats-Msg-Id (round-4 A1).
	PersistSkipMarkerID(ctx context.Context, position int64, eventID ulid.ULID) error
	// SkipMarkerID reads back the poison row's stable skip-marker id, reporting
	// whether one was already persisted.
	SkipMarkerID(ctx context.Context, position int64) (id ulid.ULID, ok bool, err error)
	// Release runs pg_advisory_unlock and returns the pinned connection to the
	// pool. Idempotent.
	Release(ctx context.Context) error
}

// TxExecutor is the narrow, tx-bound handle ApplyOnce passes to the consumer's
// effect (round-9 R6-5 #1). It exposes ONLY Exec/QueryRow — the effect's only
// database handle — so any durable write the effect performs STRUCTURALLY runs
// on the receipt+watermark transaction; there is no ambient-ctx path to a
// foreign pool. Satisfied by the pgx.Tx the postgres impl begins.
type TxExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ConsumerCheckpointStore is the consumer-owned durable idempotency store. The
// concrete impl lives in internal/world/postgres and is injected by setup.
type ConsumerCheckpointStore interface {
	// ApplyOnce runs effect exactly once per (consumer, event) atomically with the
	// receipt claim and the contiguity-safe watermark advance (round-9 R6-5 #1/#2).
	// A duplicate event → (false, nil) no-op. A beyond-next gap → (false,
	// ErrOutOfOrder) with nothing applied.
	ApplyOnce(ctx context.Context, consumer string, envelope wmodel.Envelope, effect func(effCtx context.Context, exec TxExecutor) error) (applied bool, err error)
	// InitWatermark seeds the (consumer, game) watermark to the given high-water
	// during bootstrap (round-9 R6-5 #3). Idempotent-monotonic: it never rewinds.
	InitWatermark(ctx context.Context, consumer, gameID string, epoch, position int64) error
	// Watermark reads the current (epoch, feed_position) watermark for
	// (consumer, game); ok=false when no watermark row exists yet.
	Watermark(ctx context.Context, consumer, gameID string) (epoch, position int64, ok bool, err error)
}
