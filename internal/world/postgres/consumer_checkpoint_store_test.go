// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// ensureEffectFixtureTable creates the durable fixture table the consumer effect
// writes into (round-9 R6-5 #1 — a REAL durable row, not an in-memory counter).
func ensureEffectFixtureTable(ctx context.Context, t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_consumer_effect_rows (
			event_id TEXT PRIMARY KEY,
			game_id  TEXT NOT NULL
		)`)
	require.NoError(t, err)
}

func fixtureRowVisible(ctx context.Context, t *testing.T, eventID ulid.ULID) bool {
	t.Helper()
	var one int
	err := testPool.QueryRow(ctx,
		`SELECT 1 FROM test_consumer_effect_rows WHERE event_id = $1`, eventID.String()).Scan(&one)
	return err == nil
}

func receiptPresent(ctx context.Context, t *testing.T, consumer string, eventID ulid.ULID) bool {
	t.Helper()
	var one int
	err := testPool.QueryRow(ctx,
		`SELECT 1 FROM world_consumer_receipts WHERE consumer_name = $1 AND event_id = $2`,
		consumer, eventID.String()).Scan(&one)
	return err == nil
}

func consEnv(game string, epoch, pos int64) wmodel.Envelope {
	return wmodel.Envelope{
		EventID:       ulid.Make(),
		GameID:        game,
		Kind:          "location_updated",
		SchemaVersion: 1,
		Actor:         "system",
		AggregateType: wmodel.AggregateLocation,
		AggregateID:   ulid.Make(),
		Epoch:         epoch,
		FeedPosition:  pos,
	}
}

func insertFixtureEffect(env wmodel.Envelope) func(context.Context, postgres.TxExecutor) error {
	return func(effCtx context.Context, exec postgres.TxExecutor) error {
		_, err := exec.Exec(effCtx,
			`INSERT INTO test_consumer_effect_rows (event_id, game_id) VALUES ($1, $2)`,
			env.EventID.String(), env.GameID)
		return err
	}
}

// TestApplyOnceIsIdempotentViaDurableReceipt proves a duplicate delivery applies
// the effect exactly once — the durable receipt (not an in-memory set) dedups.
func TestApplyOnceIsIdempotentViaDurableReceipt(t *testing.T) {
	ctx := context.Background()
	ensureEffectFixtureTable(ctx, t)
	store := postgres.NewConsumerCheckpointStore(testPool)
	consumer := "ref-" + ulid.Make().String()
	env := consEnv(ulid.Make().String(), 1, 1)

	applied, err := store.ApplyOnce(ctx, consumer, env, insertFixtureEffect(env))
	require.NoError(t, err)
	require.True(t, applied, "first delivery applies")
	require.True(t, fixtureRowVisible(ctx, t, env.EventID))

	applied, err = store.ApplyOnce(ctx, consumer, env, insertFixtureEffect(env))
	require.NoError(t, err)
	require.False(t, applied, "duplicate delivery is a no-op")
}

// TestApplyOnceEffectErrorRollsBackBothOrNeither proves the effect + receipt are
// atomic: an effect error leaves NEITHER the fixture row NOR the receipt visible
// (round-9 R6-5 #1). A subsequent success then applies exactly once.
func TestApplyOnceEffectErrorRollsBackBothOrNeither(t *testing.T) {
	ctx := context.Background()
	ensureEffectFixtureTable(ctx, t)
	store := postgres.NewConsumerCheckpointStore(testPool)
	consumer := "ref-" + ulid.Make().String()
	env := consEnv(ulid.Make().String(), 1, 1)

	boom := errors.New("effect boom")
	applied, err := store.ApplyOnce(ctx, consumer, env, func(effCtx context.Context, exec postgres.TxExecutor) error {
		// Write the durable fixture row THEN fail — the whole tx must roll back.
		if _, e := exec.Exec(effCtx,
			`INSERT INTO test_consumer_effect_rows (event_id, game_id) VALUES ($1, $2)`,
			env.EventID.String(), env.GameID); e != nil {
			return e
		}
		return boom
	})
	require.Error(t, err)
	require.False(t, applied)
	assert.False(t, fixtureRowVisible(ctx, t, env.EventID), "fixture row NOT visible after rollback")
	assert.False(t, receiptPresent(ctx, t, consumer, env.EventID), "receipt NOT present after rollback")

	// A subsequent success applies exactly once (the failed attempt left no trace).
	applied, err = store.ApplyOnce(ctx, consumer, env, insertFixtureEffect(env))
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, fixtureRowVisible(ctx, t, env.EventID))
}

// TestApplyOnceContiguityHoldsGapAndNeverSkips proves the contiguity-safe
// watermark (round-9 R6-5 #2): delivering 11 before 10 holds 11 (ErrOutOfOrder,
// nothing applied), 10 then applies, and the redelivered 11 applies — both
// exactly once, 10 never skipped.
func TestApplyOnceContiguityHoldsGapAndNeverSkips(t *testing.T) {
	ctx := context.Background()
	ensureEffectFixtureTable(ctx, t)
	store := postgres.NewConsumerCheckpointStore(testPool)
	consumer := "ref-" + ulid.Make().String()
	game := ulid.Make().String()

	// Bootstrap baseline: high-water at position 9 (next contiguous is 10).
	require.NoError(t, store.InitWatermark(ctx, consumer, game, 1, 9))

	env10 := consEnv(game, 1, 10)
	env11 := consEnv(game, 1, 11)

	// Deliver 11 first — a beyond-next GAP is held (NAK), nothing applied.
	applied, err := store.ApplyOnce(ctx, consumer, env11, insertFixtureEffect(env11))
	require.Error(t, err)
	assert.True(t, errors.Is(err, postgres.ErrOutOfOrder), "position 11 held as out-of-order")
	require.False(t, applied)
	assert.False(t, fixtureRowVisible(ctx, t, env11.EventID))
	assert.False(t, receiptPresent(ctx, t, consumer, env11.EventID), "held delivery claimed no receipt")

	// Deliver 10 — contiguous, applied.
	applied, err = store.ApplyOnce(ctx, consumer, env10, insertFixtureEffect(env10))
	require.NoError(t, err)
	require.True(t, applied, "position 10 is never permanently skipped")
	require.True(t, fixtureRowVisible(ctx, t, env10.EventID))

	// Redeliver 11 — now contiguous, applied.
	applied, err = store.ApplyOnce(ctx, consumer, env11, insertFixtureEffect(env11))
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, fixtureRowVisible(ctx, t, env11.EventID))
}

// TestApplyOnceExactlyOnceAcrossRestart proves the durable receipt survives a
// consumer "restart" (a fresh store instance) — a redelivery beyond an in-memory
// dedup window is still a no-op.
func TestApplyOnceExactlyOnceAcrossRestart(t *testing.T) {
	ctx := context.Background()
	ensureEffectFixtureTable(ctx, t)
	consumer := "ref-" + ulid.Make().String()
	env := consEnv(ulid.Make().String(), 1, 1)

	store1 := postgres.NewConsumerCheckpointStore(testPool)
	applied, err := store1.ApplyOnce(ctx, consumer, env, insertFixtureEffect(env))
	require.NoError(t, err)
	require.True(t, applied)

	// Simulate a restart: a brand-new store instance, no in-memory state.
	store2 := postgres.NewConsumerCheckpointStore(testPool)
	applied, err = store2.ApplyOnce(ctx, consumer, env, insertFixtureEffect(env))
	require.NoError(t, err)
	require.False(t, applied, "the durable receipt dedups across a restart")
}

// TestBootstrapInitWatermarkIsMonotonic proves InitWatermark never rewinds an
// existing watermark (round-9 R6-5 #3 high-water seed is idempotent-monotonic).
func TestBootstrapInitWatermarkIsMonotonic(t *testing.T) {
	ctx := context.Background()
	store := postgres.NewConsumerCheckpointStore(testPool)
	consumer := "ref-" + ulid.Make().String()
	game := ulid.Make().String()

	require.NoError(t, store.InitWatermark(ctx, consumer, game, 1, 100))
	require.NoError(t, store.InitWatermark(ctx, consumer, game, 1, 50)) // must NOT rewind

	epoch, pos, ok, err := store.Watermark(ctx, consumer, game)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(1), epoch)
	assert.Equal(t, int64(100), pos, "watermark never rewinds")
}
