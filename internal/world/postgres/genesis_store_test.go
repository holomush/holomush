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

	"github.com/holomush/holomush/internal/world/postgres"
)

// genesisTestGame returns a unique per-test game id so the per-game feed counter,
// genesis checkpoints, and outbox rows this test allocates never collide with
// another test's game (world aggregates are single-game in Phase 5; scoping the
// GAME isolates the feed side). The seeded aggregate ids are what each assertion
// keys on, so the shared aggregate tables do not affect correctness.
func genesisTestGame(t *testing.T) string {
	t.Helper()
	return "genesis_test_" + ulid.Make().String()
}

// seedGenesisExit inserts a minimal exit row and returns its id.
func seedGenesisExit(ctx context.Context, t *testing.T, from, to ulid.ULID) ulid.ULID {
	t.Helper()
	exitID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO exits (id, from_location_id, to_location_id, name, visibility, created_at)
		VALUES ($1, $2, $3, 'genesis-door', 'visible', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, exitID.String(), from.String(), to.String())
	require.NoError(t, err)
	return exitID
}

// outboxRowsForAggregate counts outbox rows for a (game_id, aggregate_id) pair,
// returning the kind of the single row when exactly one exists.
func outboxRowsForAggregate(ctx context.Context, t *testing.T, gameID string, aggID ulid.ULID) (count int, kind string) {
	t.Helper()
	err := testPool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(kind), '') FROM outbox WHERE game_id = $1 AND aggregate_id = $2`,
		gameID, aggID.String()).Scan(&count, &kind)
	require.NoError(t, err)
	return count, kind
}

// checkpointRowsForAggregate counts world_genesis_checkpoint rows for an aggregate
// under a game (across all epochs).
func checkpointRowsForAggregate(ctx context.Context, t *testing.T, gameID string, aggID ulid.ULID) int {
	t.Helper()
	var count int
	err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world_genesis_checkpoint WHERE game_id = $1 AND aggregate_id = $2`,
		gameID, aggID.String()).Scan(&count)
	require.NoError(t, err)
	return count
}

// TestGenesisStoreEmitsOneEnvelopeAndCheckpointPerAggregateIdempotently proves the
// cutover snapshot emits exactly one envelope + one checkpoint per aggregate and
// that a same-epoch re-run emits nothing new (checkpoint-keyed idempotency,
// round-3 MEDIUM).
func TestGenesisStoreEmitsOneEnvelopeAndCheckpointPerAggregateIdempotently(t *testing.T) {
	ctx := context.Background()
	game := genesisTestGame(t)
	store := postgres.NewGenesisStore(testPool)

	locID := createCascadeTestLocation(ctx, t)
	objID := createCascadeTestObject(ctx, t, locID)
	charID := createCascadeTestCharacter(ctx, t, locID)
	loc2 := createCascadeTestLocation(ctx, t)
	exitID := seedGenesisExit(ctx, t, locID, loc2)

	// First run: each seeded aggregate gets exactly one envelope + checkpoint.
	res, err := store.EmitGenesisSnapshot(ctx, game)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, res.Emitted, 5, "at least the five seeded aggregates emit")
	assert.Equal(t, int64(1), res.Epoch, "cutover snapshot runs at epoch 1")

	for _, tc := range []struct {
		id       ulid.ULID
		wantKind string
	}{
		{locID, "location_created"},
		{loc2, "location_created"},
		{objID, "object_created"},
		{charID, "character_genesis"},
		{exitID, "exit_created"},
	} {
		count, kind := outboxRowsForAggregate(ctx, t, game, tc.id)
		assert.Equalf(t, 1, count, "aggregate %s emits exactly one genesis envelope", tc.id)
		assert.Equalf(t, tc.wantKind, kind, "aggregate %s uses its declared genesis kind", tc.id)
		assert.Equalf(t, 1, checkpointRowsForAggregate(ctx, t, game, tc.id), "aggregate %s has exactly one checkpoint", tc.id)
	}

	// Second run at the same epoch: idempotent — no aggregate gets a second row.
	res2, err := store.EmitGenesisSnapshot(ctx, game)
	require.NoError(t, err)
	assert.Equal(t, 0, res2.Emitted, "a same-epoch re-run emits nothing new")
	assert.GreaterOrEqual(t, res2.Skipped, 5, "the seeded aggregates are all skipped on re-run")
	for _, id := range []ulid.ULID{locID, loc2, objID, charID, exitID} {
		count, _ := outboxRowsForAggregate(ctx, t, game, id)
		assert.Equalf(t, 1, count, "aggregate %s still has exactly one envelope after a re-run (no duplicate)", id)
	}
}

// TestGenesisStoreEpochAdvanceReopensGenesis proves an epoch advance re-opens
// genesis: after AdvanceEpoch, the snapshot legitimately re-emits at the new epoch
// (the checkpoint key includes epoch).
func TestGenesisStoreEpochAdvanceReopensGenesis(t *testing.T) {
	ctx := context.Background()
	game := genesisTestGame(t)
	store := postgres.NewGenesisStore(testPool)

	locID := createCascadeTestLocation(ctx, t)

	_, err := store.EmitGenesisSnapshot(ctx, game)
	require.NoError(t, err)
	count, _ := outboxRowsForAggregate(ctx, t, game, locID)
	require.Equal(t, 1, count, "one envelope at epoch 1")

	reset, err := store.AdvanceEpoch(ctx, game)
	require.NoError(t, err)
	assert.Equal(t, int64(2), reset.NewEpoch)

	epoch, err := store.CurrentEpoch(ctx, game)
	require.NoError(t, err)
	assert.Equal(t, int64(2), epoch, "epoch advanced to 2")

	_, err = store.EmitGenesisSnapshot(ctx, game)
	require.NoError(t, err)
	count2, _ := outboxRowsForAggregate(ctx, t, game, locID)
	assert.Equal(t, 2, count2, "the aggregate re-emits at the new epoch (checkpoint key includes epoch)")

	// The new envelope is at epoch 2.
	var epoch2Count int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM outbox WHERE game_id = $1 AND aggregate_id = $2 AND epoch = 2`,
		game, locID.String()).Scan(&epoch2Count))
	assert.Equal(t, 1, epoch2Count, "exactly one epoch-2 envelope for the aggregate")
}

// TestGenesisStoreAdvanceEpochQuarantinesOldRowsAndResetsOrigin proves the complete
// one-locked epoch reset (round-6 Codex MEDIUM): unpublished old-epoch rows are
// quarantined (not published under the new epoch), next_position restarts at the
// origin, and the relay's next-unpublished scan never returns the quarantined row.
func TestGenesisStoreAdvanceEpochQuarantinesOldRowsAndResetsOrigin(t *testing.T) {
	ctx := context.Background()
	game := genesisTestGame(t)
	store := postgres.NewGenesisStore(testPool)

	// Seed an unpublished old-epoch (epoch 1) outbox row directly, and advance the
	// counter's next_position past the origin so the reset is observable.
	staleEvent := ulid.Make()
	aggID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO world_feed_counter (game_id, next_position, epoch) VALUES ($1, 7, 1)
		ON CONFLICT (game_id) DO UPDATE SET next_position = 7, epoch = 1`, game)
	require.NoError(t, err)
	_, err = testPool.Exec(ctx, `
		INSERT INTO outbox (event_id, game_id, feed_position, epoch, kind, schema_version,
			actor, aggregate_id, aggregate_type, affected, payload)
		VALUES ($1, $2, 6, 1, 'location_updated', 1, 'system', $3, 'location', '[]'::jsonb, 'null'::jsonb)`,
		staleEvent.String(), game, aggID.String())
	require.NoError(t, err)

	reset, err := store.AdvanceEpoch(ctx, game)
	require.NoError(t, err)
	assert.Equal(t, int64(1), reset.PreviousEpoch)
	assert.Equal(t, int64(2), reset.NewEpoch)
	assert.GreaterOrEqual(t, reset.Quarantined, int64(1), "the unpublished old-epoch row is quarantined")
	assert.Equal(t, int64(1), reset.OriginPosition, "next_position resets to the origin")

	// The stale row is now marked published (quarantined), never to be published
	// under the new epoch.
	var publishedAt *int64
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT published_at FROM outbox WHERE event_id = $1`, staleEvent.String()).Scan(&publishedAt))
	assert.NotNil(t, publishedAt, "the old-epoch row is quarantined (published_at set)")

	// The counter is fully reset: epoch 2, next_position at the origin (not inherited).
	var epoch, nextPos int64
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT epoch, next_position FROM world_feed_counter WHERE game_id = $1`, game).Scan(&epoch, &nextPos))
	assert.Equal(t, int64(2), epoch)
	assert.Equal(t, int64(1), nextPos, "next_position restarts at the origin, not the inherited counter")

	// Relay coordination: the leased relay's next-unpublished scan does NOT return
	// the quarantined stale-epoch row (so an active relay never publishes it).
	outboxStore := postgres.NewOutboxStore(testPool)
	lease, err := outboxStore.AcquireLease(ctx, game)
	require.NoError(t, err)
	defer func() { _ = lease.Release(ctx) }()
	env, err := lease.NextUnpublished(ctx)
	require.NoError(t, err)
	if env != nil {
		assert.NotEqual(t, staleEvent, env.EventID, "the relay never returns the quarantined old-epoch row")
		assert.GreaterOrEqual(t, env.Epoch, int64(2), "any next-unpublished row is at the new epoch")
	}
}

// TestGenesisStoreConcurrentSnapshotCannotDoubleEmit proves two concurrent snapshot
// runs cannot double-emit an aggregate: the checkpoint PK + the serializing per-game
// counter lock make the race a constraint no-op, not a duplicate envelope.
func TestGenesisStoreConcurrentSnapshotCannotDoubleEmit(t *testing.T) {
	ctx := context.Background()
	game := genesisTestGame(t)
	store := postgres.NewGenesisStore(testPool)

	locID := createCascadeTestLocation(ctx, t)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = store.EmitGenesisSnapshot(ctx, game)
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	count, _ := outboxRowsForAggregate(ctx, t, game, locID)
	assert.Equal(t, 1, count, "concurrent snapshots emit the aggregate exactly once (checkpoint PK + counter-lock serialization)")
	assert.Equal(t, 1, checkpointRowsForAggregate(ctx, t, game, locID), "exactly one checkpoint despite the race")
}
