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

// writeOutboxRow writes one outbox row via WriteIntent inside a transaction and
// returns the finalized envelope.
func writeOutboxRow(ctx context.Context, t *testing.T, gameID string) *wmodel.Envelope {
	t.Helper()
	store := postgres.NewOutboxStore(testPool)
	tr := postgres.NewTransactor(testPool)
	intent := newTestIntent(gameID)
	delta := &wmodel.MutationDelta{Primary: wmodel.AffectedAggregate{
		Type: wmodel.AggregateLocation, ID: intent.AggregateID, BeforeVersion: 1, AfterVersion: 2,
	}}
	var env *wmodel.Envelope
	require.NoError(t, tr.InTransaction(ctx, func(txCtx context.Context) error {
		var err error
		env, err = store.WriteIntent(txCtx, intent, delta)
		return err
	}))
	return env
}

// TestAcquireLeaseBumpsDurableGeneration proves AcquireLease atomically bumps the
// durable world_feed_counter.lease_generation and the Lease carries that value
// (round-4 A2).
func TestAcquireLeaseBumpsDurableGeneration(t *testing.T) {
	ctx := context.Background()
	store := postgres.NewOutboxStore(testPool)
	game := ulid.Make().String()

	l1, err := store.AcquireLease(ctx, game)
	require.NoError(t, err)
	require.Equal(t, int64(1), l1.Generation(), "first acquire yields generation 1")
	require.NoError(t, l1.Release(ctx))

	l2, err := store.AcquireLease(ctx, game)
	require.NoError(t, err)
	require.Equal(t, int64(2), l2.Generation(), "re-acquire durably bumps the generation")
	require.NoError(t, l2.Release(ctx))
}

// TestMarkPublishedRejectsStaleGeneration proves MarkPublished re-reads the
// durable lease_generation column and REJECTS a stale holder's ack (round-4 A2).
func TestMarkPublishedRejectsStaleGeneration(t *testing.T) {
	ctx := context.Background()
	game := ulid.Make().String()
	env := writeOutboxRow(ctx, t, game)

	store := postgres.NewOutboxStore(testPool)
	lease, err := store.AcquireLease(ctx, game)
	require.NoError(t, err)
	defer func() { _ = lease.Release(ctx) }()

	staleGen := lease.Generation()

	// A NEW holder bumps the durable generation (simulated directly).
	_, err = testPool.Exec(ctx,
		`UPDATE world_feed_counter SET lease_generation = lease_generation + 1 WHERE game_id = $1`, game)
	require.NoError(t, err)

	// The old holder's DB ack is fenced out against the durable column.
	err = lease.MarkPublished(ctx, env.EventID, staleGen)
	require.Error(t, err)
	assert.True(t, errors.Is(err, postgres.ErrStaleLease),
		"a stale-generation MarkPublished is rejected as a stale lease")

	// The row remains unpublished — the stale ack made no DB-side progress.
	var publishedAt *int64
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT published_at FROM outbox WHERE event_id = $1`, env.EventID.String()).Scan(&publishedAt))
	assert.Nil(t, publishedAt, "the fenced-out ack did not mark the row published")
}

// TestNextUnpublishedReturnsPositionOrderAndMarkAdvances proves NextUnpublished
// yields rows in (epoch, feed_position) order and MarkPublished under the live
// generation advances the cursor.
func TestNextUnpublishedReturnsPositionOrderAndMarkAdvances(t *testing.T) {
	ctx := context.Background()
	game := ulid.Make().String()
	e1 := writeOutboxRow(ctx, t, game)
	e2 := writeOutboxRow(ctx, t, game)
	e3 := writeOutboxRow(ctx, t, game)

	store := postgres.NewOutboxStore(testPool)
	lease, err := store.AcquireLease(ctx, game)
	require.NoError(t, err)
	defer func() { _ = lease.Release(ctx) }()

	for _, want := range []*wmodel.Envelope{e1, e2, e3} {
		got, nerr := lease.NextUnpublished(ctx)
		require.NoError(t, nerr)
		require.NotNil(t, got)
		assert.Equal(t, want.EventID, got.EventID, "rows drain in (epoch, feed_position) order")
		require.NoError(t, lease.MarkPublished(ctx, got.EventID, lease.Generation()))
	}

	drained, err := lease.NextUnpublished(ctx)
	require.NoError(t, err)
	assert.Nil(t, drained, "feed fully drained")
}

// TestSkipMarkerIDPersistAndReuse proves the stable skip-marker id persists and
// is read back (round-4 A1 retry idempotency substrate).
func TestSkipMarkerIDPersistAndReuse(t *testing.T) {
	ctx := context.Background()
	game := ulid.Make().String()
	env := writeOutboxRow(ctx, t, game)

	store := postgres.NewOutboxStore(testPool)
	lease, err := store.AcquireLease(ctx, game)
	require.NoError(t, err)
	defer func() { _ = lease.Release(ctx) }()

	_, ok, err := lease.SkipMarkerID(ctx, env.FeedPosition)
	require.NoError(t, err)
	require.False(t, ok, "no marker persisted yet")

	markerID := ulid.Make()
	require.NoError(t, lease.PersistSkipMarkerID(ctx, env.FeedPosition, markerID))

	got, ok, err := lease.SkipMarkerID(ctx, env.FeedPosition)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, markerID, got, "the stable skip-marker id is reused on retry")
}
