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
	"github.com/holomush/holomush/pkg/errutil"
)

// newStatusFixtureCharacter creates an active character and registers cleanup.
// It returns the created character carrying its DB-assigned version.
func newStatusFixtureCharacter(ctx context.Context, t *testing.T, repo *postgres.CharacterRepository) *world.Character {
	t.Helper()
	playerID := createTestPlayer(ctx, t)
	char := &world.Character{
		ID:          ulid.Make(),
		PlayerID:    playerID,
		Name:        charFixtureName("status fixture"),
		Description: "A character used to exercise SetStatus.",
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, delErr(repo.Create(ctx, char, admit(ctx, t, char.Name))))
	t.Cleanup(func() {
		_ = delErr(repo.Delete(ctx, char.ID, 0))
	})
	return char
}

func TestCharacterRepositorySetStatus(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("writes retired and bumps the version on a matching expected version", func(t *testing.T) {
		char := newStatusFixtureCharacter(ctx, t, repo)

		delta, err := repo.SetStatus(ctx, char.ID, world.StatusRetired, char.Version)
		require.NoError(t, err)
		require.NotNil(t, delta)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, world.StatusRetired, got.Status)
		assert.Equal(t, char.Version+1, got.Version, "a status write bumps the version")
	})

	t.Run("writes active on the unretire path", func(t *testing.T) {
		char := newStatusFixtureCharacter(ctx, t, repo)

		_, err := repo.SetStatus(ctx, char.ID, world.StatusRetired, char.Version)
		require.NoError(t, err)
		_, err = repo.SetStatus(ctx, char.ID, world.StatusActive, char.Version+1)
		require.NoError(t, err)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, world.StatusActive, got.Status)
	})

	t.Run("returns WORLD_CONCURRENT_EDIT for a stale expected version on an existing row", func(t *testing.T) {
		char := newStatusFixtureCharacter(ctx, t, repo)

		_, err := repo.SetStatus(ctx, char.ID, world.StatusRetired, char.Version+99)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, world.StatusActive, got.Status, "a refused CAS leaves the status untouched")
	})

	t.Run("returns CHARACTER_NOT_FOUND for an absent row", func(t *testing.T) {
		_, err := repo.SetStatus(ctx, ulid.Make(), world.StatusRetired, 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("treats expectedVersion 0 as an unversioned write regardless of the current version", func(t *testing.T) {
		char := newStatusFixtureCharacter(ctx, t, repo)
		// Move the row's version away from the created value first, so the
		// unversioned write is demonstrably not matching on it.
		_, err := repo.SetStatus(ctx, char.ID, world.StatusIdle, char.Version)
		require.NoError(t, err)

		_, err = repo.SetStatus(ctx, char.ID, world.StatusRetired, 0)
		require.NoError(t, err)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, world.StatusRetired, got.Status)
	})

	t.Run("leaves the name reservation columns untouched", func(t *testing.T) {
		// INV-WORLD-6 retire half: retire preserves the name reservation.
		char := newStatusFixtureCharacter(ctx, t, repo)

		var beforeName, beforeNorm, beforeSkel string
		require.NoError(t, testPool.QueryRow(ctx,
			`SELECT name, normalized_name, name_skeleton FROM characters WHERE id = $1`,
			char.ID.String()).Scan(&beforeName, &beforeNorm, &beforeSkel))

		_, err := repo.SetStatus(ctx, char.ID, world.StatusRetired, char.Version)
		require.NoError(t, err)

		var afterName, afterNorm, afterSkel string
		require.NoError(t, testPool.QueryRow(ctx,
			`SELECT name, normalized_name, name_skeleton FROM characters WHERE id = $1`,
			char.ID.String()).Scan(&afterName, &afterNorm, &afterSkel))

		assert.Equal(t, beforeName, afterName)
		assert.Equal(t, beforeNorm, afterNorm)
		assert.Equal(t, beforeSkel, afterSkel)
	})
}
