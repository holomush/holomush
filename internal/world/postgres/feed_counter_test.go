// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestFeedCounterAllocateReturnsGapFreeMonotonicPositions proves two sequential
// allocations within one game+epoch return strictly-increasing gap-free
// positions (n, n+1) and the current epoch. The allocator runs inside the
// ambient mutation tx.
func TestFeedCounterAllocateReturnsGapFreeMonotonicPositions(t *testing.T) {
	ctx := context.Background()
	fc := postgres.NewFeedCounter(testPool)
	tr := postgres.NewTransactor(testPool)
	gameID := ulid.Make().String()

	var first, second int64
	var firstEpoch, secondEpoch int64
	require.NoError(t, tr.InTransaction(ctx, func(txCtx context.Context) error {
		var err error
		firstEpoch, first, err = fc.Allocate(txCtx, gameID)
		if err != nil {
			return err
		}
		secondEpoch, second, err = fc.Allocate(txCtx, gameID)
		return err
	}))

	assert.Equal(t, first+1, second, "positions are gap-free and strictly increasing")
	assert.Equal(t, int64(1), firstEpoch, "fresh counter starts at epoch 1")
	assert.Equal(t, firstEpoch, secondEpoch, "epoch is stable within the tx")
}

// TestFeedCounterAllocateSerializesConcurrentCallers proves the FOR UPDATE lock
// serializes concurrent allocators so no two callers ever get the same position.
func TestFeedCounterAllocateSerializesConcurrentCallers(t *testing.T) {
	ctx := context.Background()
	fc := postgres.NewFeedCounter(testPool)
	tr := postgres.NewTransactor(testPool)
	gameID := ulid.Make().String()

	const n = 8
	positions := make([]int64, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			_ = tr.InTransaction(ctx, func(txCtx context.Context) error {
				_, pos, err := fc.Allocate(txCtx, gameID)
				if err != nil {
					return err
				}
				positions[idx] = pos
				return nil
			})
		}(i)
	}
	wg.Wait()

	seen := make(map[int64]bool, n)
	for _, p := range positions {
		require.False(t, seen[p], "position %d handed out twice — FOR UPDATE did not serialize", p)
		seen[p] = true
	}
	assert.Len(t, seen, n, "every concurrent caller got a distinct position")
}

// TestFeedCounterAllocateLockTimeout proves a stuck FOR UPDATE surfaces the typed
// WORLD_FEED_LOCK_TIMEOUT within the configured bound rather than blocking
// indefinitely: one tx holds the counter row's lock, a second Allocate times out.
func TestFeedCounterAllocateLockTimeout(t *testing.T) {
	ctx := context.Background()
	fc := postgres.NewFeedCounter(testPool)
	tr := postgres.NewTransactor(testPool)
	gameID := ulid.Make().String()

	// Materialize the counter row so there is a row to lock.
	require.NoError(t, tr.InTransaction(ctx, func(txCtx context.Context) error {
		_, _, err := fc.Allocate(txCtx, gameID)
		return err
	}))

	// Hold the row's FOR UPDATE lock on a dedicated connection.
	holderConn, err := testPool.Acquire(ctx)
	require.NoError(t, err)
	defer holderConn.Release()
	holderTx, err := holderConn.Begin(ctx)
	require.NoError(t, err)
	defer holderTx.Rollback(ctx) //nolint:errcheck // best-effort cleanup
	var held int64
	require.NoError(t, holderTx.QueryRow(ctx,
		`SELECT next_position FROM world_feed_counter WHERE game_id = $1 FOR UPDATE`, gameID).Scan(&held))

	// A second allocator must time out on the lock, not block forever.
	allocErr := tr.InTransaction(ctx, func(txCtx context.Context) error {
		_, _, err := fc.Allocate(txCtx, gameID)
		return err
	})
	require.Error(t, allocErr, "allocation must not block indefinitely on a held lock")
	errutil.AssertErrorCode(t, allocErr, world.CodeFeedLockTimeout)
}
