// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// This file lives in internal/world/postgres for the same reason
// identity_backfill.go does: `characters` is a FENCED world table
// (test/meta/world_sql_fence_test.go), and this directory is the ONLY one
// allowlisted to carry raw world-table mutation SQL. Its caller — the
// character-activity flush ticker in internal/charactivity — holds no SQL of
// its own and imports no database driver; it receives this function as an
// injected value wired at the composition root.

// ActivityExecutor is the minimal executor UpdateCharacterLastActive needs,
// declared CONSUMER-SIDE in this package. Both *pgxpool.Pool and pgx.Tx
// satisfy it, so the flush can run on the plain pool (its production shape)
// without the caller reaching for a repository it has no other use for.
type ActivityExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// UpdateCharacterLastActive advances characters.last_active_at for one
// character, and only ever forwards.
//
// # It is INV-WORLD-4's fourth sanctioned out-of-world writer, and the only
// envelope-exempt one
//
// last_active_at is an OPERATIONAL column, not world state: it records when a
// character was last seen doing something, and no reader of the world feed is
// entitled to a notification about it. So this write deliberately does NOT go
// through the world write executor, bumps NO characters.version, and emits NO
// outbox envelope. Routing it through mutate() instead would publish a spurious
// world-change envelope on every flush tick — one per active character per
// interval — which is the failure this exemption exists to prevent.
//
// # The monotonic guard is the whole idempotency argument
//
// The statement is a single UPDATE predicated on `last_active_at < $2`. There
// is no read-modify-write and no transaction, because the predicate carries
// every property the caller needs:
//
//   - a STALE buffered value (older than what is already stored) matches zero
//     rows and is silently absorbed — a key left behind by a revision-conditional
//     delete is re-flushed with its old value on the next tick and must not
//     regress the column;
//   - the SAME value twice matches zero rows the second time, so the duplicate
//     keys jetstream's ListKeys may report under churn cost nothing;
//   - an UNKNOWN character id (hard-deleted between buffer and flush) matches
//     zero rows.
//
// None of those is an error, so zero rows affected returns nil. The value is
// epoch NANOSECONDS (INV-STORE-1); 0 is the never-active sentinel
// (world.NeverActive), and the strict `<` means the first positive value
// overwrites it.
func UpdateCharacterLastActive(ctx context.Context, db ActivityExecutor, characterID ulid.ULID, lastActiveNanos int64) error {
	if _, err := db.Exec(
		ctx,
		`UPDATE characters SET last_active_at = $2 WHERE id = $1 AND last_active_at < $2`,
		characterID.String(), lastActiveNanos,
	); err != nil {
		return oops.Code("CHARACTER_ACTIVITY_FLUSH_FAILED").
			With("character_id", characterID.String()).
			With("last_active_nanos", lastActiveNanos).
			Wrap(err)
	}
	return nil
}
