// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/charname"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/pkg/errutil"
)

// Interleave 1 (round-6 R6-2): a genesis creation attempted AFTER the reaper has
// marked the player reaping is REJECTED (PLAYER_REAPING) with no character
// created; resuming the reap tombstones every pre-existing character and deletes
// the player. No un-tombstoned character reaches the player-delete cascade.
func TestGuestReaperRaceGenesisAfterMarkIsRejected(t *testing.T) {
	ctx := context.Background()
	pool := reaperPool(t)
	genesis := newReapGenesis(t, pool)
	reaping := newReapService(t, pool)
	playerRepo := authpg.NewPlayerRepository(pool)

	playerID := seedReapGuest(t, pool)
	existing := reapCharFor(t, playerID, "Existing Guest")
	require.NoError(t, genesis.Create(ctx, existing, admitReapName(ctx, t, pool, existing.Name), "initial_bind_guest"))

	// The reaper MARKS the player reaping (step 1 of DeleteGuestPlayer).
	require.NoError(t, playerRepo.MarkReaping(ctx, playerID))

	// A genesis creation attempted now is rejected — no character row created.
	late := reapCharFor(t, playerID, "Late Guest")
	err := genesis.Create(ctx, late, admitReapName(ctx, t, pool, late.Name), "initial_bind_guest")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLAYER_REAPING")
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM characters WHERE id = $1`, late.ID.String()),
		"a genesis after the reaping mark must create no character")

	// Resume the reap: every pre-existing character is tombstoned; player deleted.
	require.NoError(t, reaping.DeleteGuestPlayer(ctx, playerID))
	assert.Equal(t, 1, outboxKindCount(t, pool, existing.ID, "character_deleted"))
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM characters WHERE player_id = $1`, playerID.String()))
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM players WHERE id = $1`, playerID.String()))
}

// Interleave 2 (round-6 R6-2): a genesis in flight holding the players FOR UPDATE
// lock (via the reaping-reject guard) when MarkReaping is attempted forces
// MarkReaping to BLOCK until the genesis commits; the reaper then enumerates the
// newly-created character and tombstones it. The mark UPDATE and the genesis
// SELECT ... FOR UPDATE serialize on the same players row, so no character
// created concurrently with reaping escapes the tombstone.
func TestGuestReaperRaceGenesisInFlightBlocksMarkThenCharacterTombstoned(t *testing.T) {
	ctx := context.Background()
	pool := reaperPool(t)
	reaping := newReapService(t, pool)
	charRepo := worldpg.NewCharacterRepository(pool)
	transactor := worldpg.NewTransactor(pool)
	guard := worldpg.NewReapingGuard(pool)
	playerRepo := authpg.NewPlayerRepository(pool)

	playerID := seedReapGuest(t, pool)
	inflight := reapCharFor(t, playerID, "Inflight Guest")

	lockAcquired := make(chan struct{})
	release := make(chan struct{})
	txDone := make(chan error, 1)

	// Goroutine A: an in-flight genesis-shaped tx that runs the reaping-reject
	// guard (acquiring the players FOR UPDATE lock), inserts the character in the
	// SAME tx, then holds the tx open until released (lock held), then commits.
	go func() {
		txDone <- transactor.InTransaction(ctx, func(txCtx context.Context) error {
			if gErr := guard.EnsureNotReaping(txCtx, playerID); gErr != nil {
				return gErr
			}
			if _, cErr := charRepo.Create(txCtx, inflight, admitReapName(ctx, t, pool, inflight.Name)); cErr != nil {
				return cErr
			}
			close(lockAcquired)
			<-release
			return nil
		})
	}()

	<-lockAcquired

	// MarkReaping (the reaper's step 1) must BLOCK while the genesis tx holds the
	// FOR UPDATE lock on the players row.
	markDone := make(chan error, 1)
	go func() { markDone <- playerRepo.MarkReaping(ctx, playerID) }()

	select {
	case <-markDone:
		t.Fatal("MarkReaping returned while the in-flight genesis held the players FOR UPDATE lock")
	case <-time.After(300 * time.Millisecond):
		// Still blocked — the serialization holds.
	}

	// Commit the in-flight genesis: the character is now durable, the lock released.
	close(release)
	require.NoError(t, <-txDone)
	require.NoError(t, <-markDone, "MarkReaping must succeed once the genesis commits")

	// The reaper enumerates the newly-committed character and tombstones it — no
	// un-tombstoned character reaches the player-delete cascade.
	require.NoError(t, reaping.DeleteGuestPlayer(ctx, playerID))
	assert.Equal(t, 1, outboxKindCount(t, pool, inflight.ID, "character_deleted"),
		"a character created concurrently with reaping must still be tombstoned")
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM characters WHERE player_id = $1`, playerID.String()))
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM players WHERE id = $1`, playerID.String()))
}

// admitReapName mints a real charname.Admitted for a reaper-suite fixture name,
// repairing the corpus first.
//
// These tests are plain testing.T (not Ginkgo), so they carry their own helper
// rather than sharing the suite's Gomega-flavoured one. charname.Admitted has
// exactly one constructor by design (plan 02-06) — there is no hand-built value
// to reach for.
func admitReapName(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) charname.Admitted {
	t.Helper()

	rows, err := pool.Query(ctx, `
		SELECT id, name FROM characters
		WHERE normalized_name IS NULL
		   OR name_skeleton IS NULL
		   OR name_skeleton_unicode_version IS NULL
	`)
	require.NoError(t, err)
	type pending struct{ id, key, skeleton string }
	var todo []pending
	for rows.Next() {
		var id, rowName string
		require.NoError(t, rows.Scan(&id, &rowName))
		normalized, nErr := charname.Normalize(rowName)
		if nErr != nil {
			todo = append(todo, pending{id: id, key: id, skeleton: id})
			continue
		}
		todo = append(todo, pending{id: id, key: normalized.Key, skeleton: charname.Skeleton(normalized.Key)})
	}
	require.NoError(t, rows.Err())
	rows.Close()
	for _, p := range todo {
		_, execErr := pool.Exec(ctx, `
			UPDATE characters
			SET normalized_name = $2, name_skeleton = $3, name_skeleton_unicode_version = $4
			WHERE id = $1
		`, p.id, p.key, p.skeleton, charname.UnicodeVersion)
		require.NoError(t, execErr)
	}

	gate := &charname.Gate{Skeletons: worldpg.NewSkeletonLookup(pool)}
	admitted, err := gate.Admit(ctx, name)
	require.NoError(t, err, "reaper fixture name %q must be admissible", name)
	return admitted
}
