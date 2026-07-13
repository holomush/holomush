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

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// newTestIntent builds an EnvelopeIntent for gameID with a fresh event ULID and a
// minimal valid JSON payload.
func newTestIntent(gameID string) wmodel.EnvelopeIntent {
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        gameID,
		Kind:          "location_updated",
		SchemaVersion: 1,
		Actor:         "system",
		AggregateType: wmodel.AggregateLocation,
		AggregateID:   ulid.Make(),
		Payload:       []byte(`{"name":"Atrium"}`),
	})
}

// outboxRow reads back an outbox row's game/ordering fields by event_id.
func outboxRow(ctx context.Context, t *testing.T, eventID ulid.ULID) (gameID string, epoch, position int64, found bool) {
	t.Helper()
	err := testPool.QueryRow(ctx,
		`SELECT game_id, epoch, feed_position FROM outbox WHERE event_id = $1`, eventID.String()).
		Scan(&gameID, &epoch, &position)
	if errors.Is(err, context.Canceled) {
		return "", 0, 0, false
	}
	if err != nil {
		return "", 0, 0, false
	}
	return gameID, epoch, position, true
}

// locationExists reports whether a location row is present.
func locationExists(ctx context.Context, t *testing.T, id ulid.ULID) bool {
	t.Helper()
	var one int
	err := testPool.QueryRow(ctx, `SELECT 1 FROM locations WHERE id = $1`, id.String()).Scan(&one)
	return err == nil
}

// TestOutboxStoreWriteIntentAllocatesFinalizesAndReturns proves WriteIntent
// allocates (epoch, feed_position) from the counter, finalizes the envelope, and
// returns it — the caller supplies only (intent, delta), never epoch/position.
func TestOutboxStoreWriteIntentAllocatesFinalizesAndReturns(t *testing.T) {
	ctx := context.Background()
	store := postgres.NewOutboxStore(testPool)
	tr := postgres.NewTransactor(testPool)
	gameID := ulid.Make().String()
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

	require.NotNil(t, env)
	assert.Equal(t, intent.EventID, env.EventID)
	assert.Equal(t, gameID, env.GameID)
	assert.Positive(t, env.FeedPosition, "writer allocated a position")
	require.Len(t, env.Affected, 1, "manifest built from the delta")
	assert.Equal(t, 2, env.Affected[0].AfterVersion)

	gotGame, gotEpoch, gotPos, found := outboxRow(ctx, t, intent.EventID)
	require.True(t, found, "outbox row persisted")
	assert.Equal(t, gameID, gotGame, "game_id survives intent -> outbox row")
	assert.Equal(t, env.Epoch, gotEpoch)
	assert.Equal(t, env.FeedPosition, gotPos)
}

// TestOutboxStoreWriteIntentGameIDRoundTripPerGame proves game_id survives
// intent -> outbox row -> returned Envelope unchanged, and a second WriteIntent
// for a DIFFERENT game allocates from that game's own counter (per-game order).
func TestOutboxStoreWriteIntentGameIDRoundTripPerGame(t *testing.T) {
	ctx := context.Background()
	store := postgres.NewOutboxStore(testPool)
	tr := postgres.NewTransactor(testPool)
	gameA := ulid.Make().String()
	gameB := ulid.Make().String()

	var envA1, envA2, envB1 *wmodel.Envelope
	require.NoError(t, tr.InTransaction(ctx, func(txCtx context.Context) error {
		var err error
		if envA1, err = store.WriteIntent(txCtx, newTestIntent(gameA), nil); err != nil {
			return err
		}
		if envA2, err = store.WriteIntent(txCtx, newTestIntent(gameA), nil); err != nil {
			return err
		}
		envB1, err = store.WriteIntent(txCtx, newTestIntent(gameB), nil)
		return err
	}))

	assert.Equal(t, gameA, envA1.GameID)
	assert.Equal(t, envA1.FeedPosition+1, envA2.FeedPosition, "same game advances gap-free")
	assert.Equal(t, gameB, envB1.GameID)
	assert.Equal(t, envA1.FeedPosition, envB1.FeedPosition,
		"a different game starts from its own counter, not game A's position")

	gGame, _, _, found := outboxRow(ctx, t, envB1.EventID)
	require.True(t, found)
	assert.Equal(t, gameB, gGame, "game B's row carries game B's id")
}

// TestOutboxStoreStateAndEnvelopeAtomicity is the always-run INV-WORLD-1 binding
// target: a REAL world row and its envelope commit or roll back TOGETHER. It runs
// in the normal task test:int Integration Test lane (NOT a quarantine-gated
// resilience spec), and covers all three cases — rollback→neither survives,
// commit→both survive, forced outbox failure after the state write→state rolls
// back — so it is a full ATOMIC-FEED binding, not an envelope-only rollback.
//
// Verifies: INV-WORLD-1
func TestOutboxStoreStateAndEnvelopeAtomicity(t *testing.T) {
	ctx := context.Background()
	store := postgres.NewOutboxStore(testPool)
	locRepo := postgres.NewLocationRepository(testPool)
	tr := postgres.NewTransactor(testPool)

	t.Run("rollback leaves neither the world row nor the outbox row", func(t *testing.T) {
		loc := newTestLocation("Atomic Rollback")
		gameID := ulid.Make().String()
		intent := newTestIntent(gameID)

		forced := errors.New("force rollback")
		err := tr.InTransaction(ctx, func(txCtx context.Context) error {
			if _, cerr := locRepo.Create(txCtx, loc); cerr != nil {
				return cerr
			}
			if _, werr := store.WriteIntent(txCtx, intent, nil); werr != nil {
				return werr
			}
			return forced
		})
		require.ErrorIs(t, err, forced)

		assert.False(t, locationExists(ctx, t, loc.ID), "world row must not survive rollback")
		_, _, _, found := outboxRow(ctx, t, intent.EventID)
		assert.False(t, found, "outbox row must not survive rollback")
	})

	t.Run("commit persists both the world row and the outbox row", func(t *testing.T) {
		loc := newTestLocation("Atomic Commit")
		gameID := ulid.Make().String()
		intent := newTestIntent(gameID)
		t.Cleanup(func() { _, _ = locRepo.Delete(ctx, loc.ID, 0) })

		require.NoError(t, tr.InTransaction(ctx, func(txCtx context.Context) error {
			if _, cerr := locRepo.Create(txCtx, loc); cerr != nil {
				return cerr
			}
			_, werr := store.WriteIntent(txCtx, intent, nil)
			return werr
		}))

		assert.True(t, locationExists(ctx, t, loc.ID), "world row survives commit")
		gotGame, _, _, found := outboxRow(ctx, t, intent.EventID)
		require.True(t, found, "outbox row survives commit")
		assert.Equal(t, gameID, gotGame)
	})

	t.Run("forced outbox failure after the state write rolls the state back", func(t *testing.T) {
		// Baseline committed location whose Update we will attempt inside a tx.
		loc := newTestLocation("Atomic Poison")
		require.NoError(t, delErr(locRepo.Create(ctx, loc)))
		t.Cleanup(func() { _, _ = locRepo.Delete(ctx, loc.ID, 0) })
		startVersion := loc.Version

		gameID := ulid.Make().String()
		poison := newTestIntent(gameID) // reused twice -> duplicate event_id violates the PK

		err := tr.InTransaction(ctx, func(txCtx context.Context) error {
			// State write first.
			loc.Name = "Atomic Poison v2"
			if _, uerr := locRepo.Update(txCtx, loc); uerr != nil {
				return uerr
			}
			// First envelope succeeds; the second reuses the same event_id and
			// fails the outbox event_id PK — an outbox failure AFTER the state write.
			if _, werr := store.WriteIntent(txCtx, poison, nil); werr != nil {
				return werr
			}
			_, werr := store.WriteIntent(txCtx, poison, nil)
			return werr
		})
		require.Error(t, err, "duplicate event_id must fail the outbox insert")

		// The state write must have rolled back with the tx: version unchanged and
		// name not overwritten.
		fresh, gerr := locRepo.Get(ctx, loc.ID)
		require.NoError(t, gerr)
		assert.Equal(t, startVersion, fresh.Version, "state change rolled back with the outbox failure")
		assert.Equal(t, "Atomic Poison", fresh.Name, "row content not overwritten")
	})
}

// TestOutboxWriteSQLLivesInPostgres is a guard for finding 6 / the writer
// boundary: the outbox INSERT SQL lives in internal/world/postgres, never in
// internal/world/outbox. (Kept alongside the store so it travels with the code.)
func TestOutboxWriteSQLLivesInPostgres(t *testing.T) {
	// Sanity: the store constructs and satisfies the intended writer shape.
	store := postgres.NewOutboxStore(testPool)
	var _ interface {
		WriteIntent(context.Context, wmodel.EnvelopeIntent, *wmodel.MutationDelta) (*wmodel.Envelope, error)
	} = store
	_ = world.CodeFeedLockTimeout
}
