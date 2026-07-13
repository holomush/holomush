// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// PlayerReapingGuard is the genesis-side half of the D-06 anti-TOCTOU
// serialization (round-6 R6-2). Its EnsureNotReaping runs
// SELECT reaping_at FROM players WHERE id = $1 FOR UPDATE on the CALLER's
// transaction connection (the character-genesis creation tx) so the players ROW
// LOCK is held until that tx commits — an in-flight genesis holding the lock
// forces the reaper's MarkReaping UPDATE to block until the character commits
// (then enumeration sees + tombstones it), and any genesis starting after the
// mark commits observes reaping_at set and is rejected. Together with
// MarkReaping (internal/auth/postgres) this makes it impossible to create a
// character for a player after its reaping mark, so no un-tombstoned character
// can reach the player-delete FK cascade.
//
// LAYERING EXCEPTION — DURABLE AND INTENTIONAL. This guard deliberately reads
// the AUTH `players` table from within internal/world/postgres, on the WORLD
// mutation transaction's connection. That cross-layer read is REQUIRED, not a
// smell: the FOR UPDATE row lock can only serialize with the reaper's
// MarkReaping (which runs on the auth player_repo's own pool, same database) if
// it executes on the genesis tx connection — which means it MUST reach the
// private querierFromCtx/txKey that live in this package. Placing it in
// internal/auth would break that serialization (the players row lock could not
// be held through the genesis character INSERT). It is a READ
// (SELECT ... FOR UPDATE), NOT a world-table mutation, so it is outside the AST
// SQL fence (05-09) — the same exception is captured beside the SQL-fence
// allowlist commentary. A future layering pass MUST NOT "clean this up" by
// relocating the read out of the world tx connection (round-9 both-reviewer
// MEDIUM/LOW).
type PlayerReapingGuard struct {
	pool *pgxpool.Pool
}

// NewReapingGuard constructs the genesis-side reaping-reject guard. pool is only
// the fallback querier — EnsureNotReaping is expected to run inside the
// caller-owned genesis transaction and enrolls via querierFromCtx so the
// FOR UPDATE lock is held on the genesis tx connection.
func NewReapingGuard(pool *pgxpool.Pool) *PlayerReapingGuard {
	if pool == nil {
		panic("NewReapingGuard: pool must not be nil")
	}
	return &PlayerReapingGuard{pool: pool}
}

// EnsureNotReaping returns nil when players.reaping_at is NULL (not reaping) and
// oops.Code("PLAYER_REAPING") when it is set. It runs the locking read on the
// ambient transaction connection (querierFromCtx) so the players row lock is
// held until the genesis tx commits — serializing with the reaper's MarkReaping.
// A missing player row is treated as not-reaping (nil): the genesis INSERT will
// fail on the characters.player_id FK anyway, so there is nothing to reject.
func (g *PlayerReapingGuard) EnsureNotReaping(ctx context.Context, playerID ulid.ULID) error {
	var reapingAt *int64
	err := querierFromCtx(ctx, g.pool).QueryRow(
		ctx,
		`SELECT reaping_at FROM players WHERE id = $1 FOR UPDATE`,
		playerID.String(),
	).Scan(&reapingAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return oops.With("operation", "reaping guard lock read").
			With("player_id", playerID.String()).Wrap(err)
	}
	if reapingAt != nil {
		return oops.Code("PLAYER_REAPING").
			With("player_id", playerID.String()).
			Errorf("player is being reaped; character creation rejected")
	}
	return nil
}
