// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// EffectFunc is a consumer's per-envelope side effect. It receives a TX-BOUND
// executor (round-9 R6-5 #1) — its ONLY database handle — so any durable write it
// performs STRUCTURALLY runs on the receipt+watermark transaction. Non-
// transactional / foreign-pool / network side effects are PROHIBITED: the effect
// MUST use exec for every durable write, or its atomicity guarantee is void.
type EffectFunc func(effCtx context.Context, exec TxExecutor, env wmodel.Envelope) error

// Consumer is the reference idempotent world-change consumer. It is the ONLY
// consumer Phase 5 ships (zero product projections). Exactly-once is backed by
// the DURABLE receipt+watermark store reached through ApplyOnce — not an
// in-memory ULID set (which cannot survive a restart or a retry beyond the finite
// JetStream dedup window).
type Consumer struct {
	name   string
	store  ConsumerCheckpointStore
	effect EffectFunc
	logger *slog.Logger
}

// NewConsumer constructs a reference consumer. effect MAY be nil (the pure
// reference consumer records receipts+watermarks with no product projection).
func NewConsumer(name string, store ConsumerCheckpointStore, effect EffectFunc, logger *slog.Logger) *Consumer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{name: name, store: store, effect: effect, logger: logger}
}

// Name returns the durable consumer name.
func (c *Consumer) Name() string { return c.name }

// Apply processes exactly one delivered envelope idempotently. Returns
// (applied, err): a duplicate (receipt already present) or a sub-watermark
// delivery returns (false, nil) as a no-op; a beyond-next GAP returns
// (false, ErrOutOfOrder) so the caller NAKs for redelivery after the gap fills.
func (c *Consumer) Apply(ctx context.Context, env wmodel.Envelope) (bool, error) {
	applied, err := c.store.ApplyOnce(ctx, c.name, env, func(effCtx context.Context, exec TxExecutor) error {
		if c.effect == nil {
			return nil
		}
		return c.effect(effCtx, exec, env)
	})
	if err != nil {
		// Preserve the out-of-order signal unwrapped so the caller can NAK the
		// delivery for redelivery after the gap fills (round-9 R6-5 #2).
		if IsOutOfOrder(err) {
			return applied, err //nolint:wrapcheck // preserve the out-of-order signal unwrapped for caller NAK detection
		}
		return applied, oops.Code("WORLD_OUTBOX_CONSUMER_APPLY_FAILED").
			With("consumer", c.name).
			With("game_id", env.GameID).
			With("epoch", env.Epoch).
			With("feed_position", env.FeedPosition).Wrap(err)
	}
	return applied, nil
}

// SnapshotFunc captures the world-state snapshot AND returns the per-game feed
// HIGH-WATER (epoch, position) in ONE repeatable-read (or serializable)
// transaction (round-9 R6-5 #3). Because a world-state row and its feed_position
// increment commit in the SAME tx (INV-WORLD-1), the snapshot's max-visible
// feed_position EXACTLY partitions in-snapshot (≤ high-water) from not-in-snapshot
// (> high-water).
type SnapshotFunc func(ctx context.Context) (epoch, highWaterPosition int64, err error)

// BootstrapConfig configures Bootstrap.
type BootstrapConfig struct {
	// ConsumerName is the durable consumer + watermark key.
	ConsumerName string
	// GameID is the per-game feed.
	GameID string
	// Store is the durable checkpoint store.
	Store ConsumerCheckpointStore
	// EnsureDurable creates/ensures the reference consumer's DURABLE JetStream
	// consumer BEFORE the snapshot, so every message the relay publishes from now
	// is retained. Its JS sequence cursor is the resume mechanism (server.go:~1048)
	// — there is NO "subscribe from feed_position" EventBus op.
	EnsureDurable func(ctx context.Context) error
	// Snapshot captures the world-state snapshot AND the feed high-water.
	Snapshot SnapshotFunc
	// Logger is optional.
	Logger *slog.Logger
}

// Bootstrap aligns the snapshot with the FEED HIGH-WATER and hands off to the
// durable JetStream cursor (round-9 R6-5 #3). Exact sequence:
//
//  1. CREATE/ensure the durable JetStream consumer BEFORE the snapshot.
//  2. In ONE repeatable-read tx capture BOTH the world-state snapshot AND the
//     per-game world_feed_counter HIGH-WATER (next_position - 1 at the current
//     epoch); commit.
//  3. Initialize world_consumer_watermarks to the captured high-water.
//
// Thereafter the consumer consumes from the durable cursor, and ApplyOnce
// naturally DISCARDS deliveries at-or-below the high-water (sub-watermark no-op)
// and APPLIES only those strictly beyond it — so a mutation committing between
// the durable-create and the snapshot is neither missed (the durable retained it)
// nor double-applied (ApplyOnce discards it as ≤ high-water). feed_position is
// used ONLY as this discard cutoff, never as a JS subscribe cursor.
func Bootstrap(ctx context.Context, cfg BootstrapConfig) error {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.EnsureDurable != nil {
		if err := cfg.EnsureDurable(ctx); err != nil {
			return oops.Code("WORLD_OUTBOX_BOOTSTRAP_DURABLE_FAILED").
				With("consumer", cfg.ConsumerName).Wrap(err)
		}
	}
	if cfg.Snapshot == nil {
		return oops.Code("WORLD_OUTBOX_BOOTSTRAP_NO_SNAPSHOT").
			With("consumer", cfg.ConsumerName).
			Errorf("bootstrap requires a snapshot function")
	}
	epoch, highWater, err := cfg.Snapshot(ctx)
	if err != nil {
		return oops.Code("WORLD_OUTBOX_BOOTSTRAP_SNAPSHOT_FAILED").
			With("consumer", cfg.ConsumerName).With("game_id", cfg.GameID).Wrap(err)
	}
	if err := cfg.Store.InitWatermark(ctx, cfg.ConsumerName, cfg.GameID, epoch, highWater); err != nil {
		return oops.Code("WORLD_OUTBOX_BOOTSTRAP_INIT_WATERMARK_FAILED").
			With("consumer", cfg.ConsumerName).With("game_id", cfg.GameID).Wrap(err)
	}
	logger.InfoContext(ctx, "outbox consumer bootstrapped from feed high-water",
		"consumer", cfg.ConsumerName, "game_id", cfg.GameID, "epoch", epoch, "high_water", highWater)
	return nil
}
