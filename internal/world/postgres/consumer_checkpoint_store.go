// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// ErrOutOfOrder is the postgres-side signal that a delivery is BEYOND the next
// contiguous feed position for its (consumer, game). The setup adapter surfaces
// it; the reference consumer matches it (by code) to NAK the delivery for
// redelivery after the gap fills. Package postgres does not import
// internal/world/outbox, so the two packages share the CODE, not the sentinel.
var ErrOutOfOrder = errors.New("world/postgres: delivery beyond next contiguous position")

// CodeOutOfOrder is the oops code stamped on ErrOutOfOrder.
const CodeOutOfOrder = "WORLD_OUTBOX_OUT_OF_ORDER"

// TxExecutor is the narrow tx-bound handle ApplyOnce passes to the effect — its
// ONLY database handle, so any durable write it performs structurally runs on the
// receipt+watermark transaction (round-9 R6-5 #1). Satisfied by the pgx.Tx
// ApplyOnce begins. It is declared here (local, unexported-shape-identical to
// outbox.TxExecutor) so the setup adapter can bridge the two WITHOUT package
// postgres importing internal/world/outbox.
type TxExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ConsumerCheckpointStore is the durable idempotency store for the reference
// consumer: per-event receipts + a per-(consumer, game) contiguity-safe watermark.
// The receipt+watermark SQL lives here in the writer boundary (finding 6).
type ConsumerCheckpointStore struct {
	pool *pgxpool.Pool
}

// NewConsumerCheckpointStore constructs a ConsumerCheckpointStore.
func NewConsumerCheckpointStore(pool *pgxpool.Pool) *ConsumerCheckpointStore {
	return &ConsumerCheckpointStore{pool: pool}
}

// ApplyOnce runs effect exactly once per (consumer, event) atomically with the
// receipt claim and the contiguity-safe watermark advance:
//
//  1. BEGIN one tx.
//  2. Claim the world_consumer_receipts (consumer_name, event_id) row — a
//     duplicate (conflict) returns (false, nil), a no-op.
//  3. Classify the delivery against the (consumer, game) watermark: a
//     sub-watermark position records the receipt but SKIPS the effect (no
//     advance); the next contiguous position (same epoch: stored+1; a
//     strictly-greater epoch: the epoch origin) is applied; a beyond-next GAP
//     returns (false, ErrOutOfOrder) with NOTHING applied (rollback).
//  4. Run effect(effCtx, tx) — the tx IS the effect's only exec handle.
//  5. Advance the watermark via the single-lexicographic-predicate UPSERT.
//  6. COMMIT — the effect + receipt + watermark are atomic.
func (s *ConsumerCheckpointStore) ApplyOnce(
	ctx context.Context,
	consumer string,
	env wmodel.Envelope,
	effect func(effCtx context.Context, exec TxExecutor) error,
) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, oops.Code("WORLD_OUTBOX_APPLYONCE_BEGIN_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// 2. Claim the receipt.
	tag, err := tx.Exec(ctx, `
		INSERT INTO world_consumer_receipts (consumer_name, event_id)
		VALUES ($1, $2)
		ON CONFLICT (consumer_name, event_id) DO NOTHING`,
		consumer, env.EventID.String())
	if err != nil {
		return false, oops.Code("WORLD_OUTBOX_RECEIPT_CLAIM_FAILED").
			With("consumer", consumer).With("event_id", env.EventID.String()).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		// Already applied (receipt present) — idempotent no-op, even after a
		// restart or a retry beyond the JetStream dedup window.
		return false, nil
	}

	// 3. Classify against the watermark.
	var wmEpoch, wmPos int64
	hasRow := true
	err = tx.QueryRow(ctx, `
		SELECT epoch, feed_position FROM world_consumer_watermarks
		WHERE consumer_name = $1 AND game_id = $2`, consumer, env.GameID).Scan(&wmEpoch, &wmPos)
	if errors.Is(err, pgx.ErrNoRows) {
		hasRow = false
		// Baseline for a genuinely-fresh (consumer, game): the feed's origin
		// (position 0) in the incoming epoch, so the first contiguous position is 1.
		wmEpoch = env.Epoch
		wmPos = 0
	} else if err != nil {
		return false, oops.Code("WORLD_OUTBOX_WATERMARK_READ_FAILED").
			With("consumer", consumer).With("game_id", env.GameID).Wrap(err)
	}

	switch {
	case env.Epoch < wmEpoch:
		// Stale epoch — sub-watermark; record the receipt, no effect, no advance.
		return commitNoOp(ctx, tx)
	case env.Epoch == wmEpoch && env.FeedPosition <= wmPos:
		// Sub-watermark duplicate — record the receipt, no effect, no advance.
		return commitNoOp(ctx, tx)
	case env.Epoch > wmEpoch:
		// Strictly-greater epoch: the epoch boundary resets contiguity — apply.
	case env.Epoch == wmEpoch && env.FeedPosition == wmPos+1:
		// Next contiguous position — apply.
	default:
		// Beyond-next GAP: rollback (undoing the receipt claim) and NAK.
		return false, oops.Code(CodeOutOfOrder).
			With("consumer", consumer).With("game_id", env.GameID).
			With("watermark_epoch", wmEpoch).With("watermark_position", wmPos).
			With("delivery_epoch", env.Epoch).With("delivery_position", env.FeedPosition).
			Wrap(ErrOutOfOrder)
	}

	// 4. Run the effect on the tx.
	if effect != nil {
		if err := effect(ctx, tx); err != nil {
			return false, oops.Code("WORLD_OUTBOX_EFFECT_FAILED").
				With("consumer", consumer).With("event_id", env.EventID.String()).Wrap(err)
		}
	}

	// 5. Advance the watermark (single lexicographic predicate; UPSERT so the
	// first event for a (consumer, game) inserts). hasRow is informational.
	_ = hasRow
	if _, err := tx.Exec(ctx, `
		INSERT INTO world_consumer_watermarks (consumer_name, game_id, epoch, feed_position, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (consumer_name, game_id) DO UPDATE
		SET epoch = EXCLUDED.epoch, feed_position = EXCLUDED.feed_position, updated_at = EXCLUDED.updated_at
		WHERE world_consumer_watermarks.epoch < EXCLUDED.epoch
		   OR (world_consumer_watermarks.epoch = EXCLUDED.epoch AND world_consumer_watermarks.feed_position < EXCLUDED.feed_position)`,
		consumer, env.GameID, env.Epoch, env.FeedPosition, nowNS()); err != nil {
		return false, oops.Code("WORLD_OUTBOX_WATERMARK_ADVANCE_FAILED").
			With("consumer", consumer).With("game_id", env.GameID).Wrap(err)
	}

	// 6. Commit.
	if err := tx.Commit(ctx); err != nil {
		return false, oops.Code("WORLD_OUTBOX_APPLYONCE_COMMIT_FAILED").Wrap(err)
	}
	return true, nil
}

// commitNoOp commits the receipt-only (sub-watermark discard) transaction.
func commitNoOp(ctx context.Context, tx pgx.Tx) (bool, error) {
	if err := tx.Commit(ctx); err != nil {
		return false, oops.Code("WORLD_OUTBOX_APPLYONCE_COMMIT_FAILED").Wrap(err)
	}
	return false, nil
}

// InitWatermark seeds the (consumer, game) watermark to the given high-water
// during bootstrap. Idempotent-monotonic: the single-lexicographic-predicate
// UPSERT never rewinds an existing watermark.
func (s *ConsumerCheckpointStore) InitWatermark(ctx context.Context, consumer, gameID string, epoch, position int64) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO world_consumer_watermarks (consumer_name, game_id, epoch, feed_position, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (consumer_name, game_id) DO UPDATE
		SET epoch = EXCLUDED.epoch, feed_position = EXCLUDED.feed_position, updated_at = EXCLUDED.updated_at
		WHERE world_consumer_watermarks.epoch < EXCLUDED.epoch
		   OR (world_consumer_watermarks.epoch = EXCLUDED.epoch AND world_consumer_watermarks.feed_position < EXCLUDED.feed_position)`,
		consumer, gameID, epoch, position, nowNS()); err != nil {
		return oops.Code("WORLD_OUTBOX_INIT_WATERMARK_FAILED").
			With("consumer", consumer).With("game_id", gameID).Wrap(err)
	}
	return nil
}

// Watermark reads the current (epoch, feed_position) watermark for
// (consumer, game); ok=false when no watermark row exists yet.
func (s *ConsumerCheckpointStore) Watermark(ctx context.Context, consumer, gameID string) (int64, int64, bool, error) {
	var epoch, position int64
	err := s.pool.QueryRow(ctx, `
		SELECT epoch, feed_position FROM world_consumer_watermarks
		WHERE consumer_name = $1 AND game_id = $2`, consumer, gameID).Scan(&epoch, &position)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, oops.Code("WORLD_OUTBOX_WATERMARK_QUERY_FAILED").
			With("consumer", consumer).With("game_id", gameID).Wrap(err)
	}
	return epoch, position, true, nil
}
