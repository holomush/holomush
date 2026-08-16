// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
)

// newActivityCharacter creates a character whose last_active_at is the
// never-active sentinel, and returns it.
func newActivityCharacter(ctx context.Context, t *testing.T) *world.Character {
	t.Helper()
	repo := postgres.NewCharacterRepository(testPool)
	char := &world.Character{
		ID:          ulid.Make(),
		PlayerID:    createTestPlayer(ctx, t),
		Name:        charFixtureName("activity"),
		Description: "A character whose activity is flushed periodically.",
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, delErr(repo.Create(ctx, char, admit(ctx, t, char.Name))))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })
	return char
}

// lastActiveAt reads characters.last_active_at verbatim.
func lastActiveAt(ctx context.Context, t *testing.T, id ulid.ULID) int64 {
	t.Helper()
	var got int64
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT last_active_at FROM characters WHERE id = $1`, id.String()).Scan(&got))
	return got
}

// characterVersion reads characters.version verbatim.
func characterVersion(ctx context.Context, t *testing.T, id ulid.ULID) int64 {
	t.Helper()
	var got int64
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT version FROM characters WHERE id = $1`, id.String()).Scan(&got))
	return got
}

// outboxRowsFor counts every outbox row for one aggregate, across games and
// epochs — the flush must add none of them.
func outboxRowsFor(ctx context.Context, t *testing.T, id ulid.ULID) int64 {
	t.Helper()
	var got int64
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE aggregate_id = $1`, id.String()).Scan(&got))
	return got
}

func TestUpdateCharacterLastActiveAdvancesTheColumnOnlyForwards(t *testing.T) {
	ctx := context.Background()

	t.Run("the first positive value overwrites the never-active sentinel", func(t *testing.T) {
		char := newActivityCharacter(ctx, t)
		require.Equal(t, world.NeverActive, lastActiveAt(ctx, t, char.ID))

		require.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, char.ID, 1_000))
		assert.Equal(t, int64(1_000), lastActiveAt(ctx, t, char.ID))
	})

	t.Run("a newer value advances the column", func(t *testing.T) {
		char := newActivityCharacter(ctx, t)
		require.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, char.ID, 1_000))

		require.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, char.ID, 2_000))
		assert.Equal(t, int64(2_000), lastActiveAt(ctx, t, char.ID))
	})

	t.Run("a stale value leaves the column untouched and reports no error", func(t *testing.T) {
		char := newActivityCharacter(ctx, t)
		require.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, char.ID, 2_000))

		// A key left behind by a revision-conditional delete is re-flushed with
		// its OLD value on the next tick. That must not regress the column.
		require.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, char.ID, 1_000))
		assert.Equal(t, int64(2_000), lastActiveAt(ctx, t, char.ID))
	})

	t.Run("flushing the same value twice is a no-op", func(t *testing.T) {
		char := newActivityCharacter(ctx, t)
		require.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, char.ID, 2_000))

		// ListKeys may report a duplicate key under churn; the writer absorbs it.
		require.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, char.ID, 2_000))
		assert.Equal(t, int64(2_000), lastActiveAt(ctx, t, char.ID))
	})

	t.Run("an unknown character id is not an error", func(t *testing.T) {
		// The character may have been hard-deleted between buffer and flush.
		assert.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, ulid.Make(), 1_000))
	})
}

// Verifies: INV-WORLD-4
func TestUpdateCharacterLastActiveBumpsNoVersionAndEmitsNoEnvelope(t *testing.T) {
	ctx := context.Background()
	char := newActivityCharacter(ctx, t)

	versionBefore := characterVersion(ctx, t, char.ID)
	outboxBefore := outboxRowsFor(ctx, t, char.ID)

	require.NoError(t, postgres.UpdateCharacterLastActive(ctx, testPool, char.ID, 3_000))

	assert.Equal(t, int64(3_000), lastActiveAt(ctx, t, char.ID))
	assert.Equal(t, versionBefore, characterVersion(ctx, t, char.ID),
		"last_active_at is an operational column: the flush is the one sanctioned writer that bumps no version")
	assert.Equal(t, outboxBefore, outboxRowsFor(ctx, t, char.ID),
		"INV-WORLD-4's fourth writer is envelope-EXEMPT: a spurious envelope per flush would corrupt the feed")
}
