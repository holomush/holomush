// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// feedCounterLockTimeout bounds the FOR UPDATE lock acquisition on the per-game
// counter row. The counter serializes ALL same-game writes, so a stuck lock must
// surface a typed timeout (WORLD_FEED_LOCK_TIMEOUT) rather than block the whole
// mutation transaction indefinitely (round-2 MEDIUM).
const feedCounterLockTimeout = "2s"

// pgLockNotAvailable is PostgreSQL SQLSTATE 55P03, raised when lock_timeout
// expires before a row lock can be acquired.
const pgLockNotAvailable = "55P03"

// FeedCounter is the locked per-game feed_position + epoch allocator. It is the
// ONLY source of feed_position (never BIGSERIAL / insert-time) — the commit-order
// proof depends on positions coming from this locked counter inside the mutation
// transaction.
type FeedCounter struct {
	pool *pgxpool.Pool
}

// NewFeedCounter constructs a FeedCounter backed by the given pool. The pool is
// only the fallback execer; every Allocate is expected to run inside an ambient
// mutation transaction (via execerFromCtx) so the position is committed
// atomically with the state change and the outbox row.
func NewFeedCounter(pool *pgxpool.Pool) *FeedCounter {
	return &FeedCounter{pool: pool}
}

// Allocate reserves the next feed_position for gameID under the game's current
// epoch, returning (epoch, position). It:
//
//  1. SET LOCAL lock_timeout so a stuck lock times out within the bound;
//  2. upsert-initializes the counter row if this is the game's first write;
//  3. SELECT next_position, epoch ... FOR UPDATE — the lock that serializes all
//     same-game writers so no two callers ever get the same position;
//  4. increments next_position for the next writer.
//
// It MUST run inside the ambient mutation tx (execerFromCtx). Acquire it as LATE
// as possible in the tx — right before the outbox insert — so the FOR UPDATE lock
// is held for the minimum time (finding 13). On lock-acquire timeout it returns
// oops.Code(WORLD_FEED_LOCK_TIMEOUT) wrapping world.ErrFeedLockTimeout.
//
// Keying on game_id makes multi-game a data change, not a schema change (single
// `main` today; resolves Open Question 2).
func (c *FeedCounter) Allocate(ctx context.Context, gameID string) (epoch, position int64, err error) {
	e := execerFromCtx(ctx, c.pool)
	q := querierFromCtx(ctx, c.pool)

	// Bound the lock acquisition. SET LOCAL scopes it to the ambient tx only.
	if _, err := e.Exec(ctx, `SET LOCAL lock_timeout = '`+feedCounterLockTimeout+`'`); err != nil {
		return 0, 0, oops.With("operation", "feed counter set lock_timeout").
			With("game_id", gameID).Wrap(err)
	}

	// Ensure the counter row exists. DO NOTHING does not lock an existing
	// committed row, so the FOR UPDATE below is where contention (and any
	// timeout) is observed.
	if _, err := e.Exec(ctx,
		`INSERT INTO world_feed_counter (game_id, next_position, epoch)
		 VALUES ($1, 1, 1)
		 ON CONFLICT (game_id) DO NOTHING`, gameID); err != nil {
		return 0, 0, oops.With("operation", "feed counter init").
			With("game_id", gameID).Wrap(err)
	}

	// Lock the counter row and read the position + epoch to hand out.
	if err := q.QueryRow(ctx,
		`SELECT next_position, epoch FROM world_feed_counter WHERE game_id = $1 FOR UPDATE`,
		gameID).Scan(&position, &epoch); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgLockNotAvailable {
			return 0, 0, oops.Code(world.CodeFeedLockTimeout).
				With("game_id", gameID).
				Wrap(world.ErrFeedLockTimeout)
		}
		return 0, 0, oops.With("operation", "feed counter lock").
			With("game_id", gameID).Wrap(err)
	}

	// Advance the counter so the next writer gets position+1 (gap-free).
	if _, err := e.Exec(ctx,
		`UPDATE world_feed_counter SET next_position = next_position + 1 WHERE game_id = $1`,
		gameID); err != nil {
		return 0, 0, oops.With("operation", "feed counter advance").
			With("game_id", gameID).Wrap(err)
	}

	return epoch, position, nil
}
