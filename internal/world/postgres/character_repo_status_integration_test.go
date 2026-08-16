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

// newDefaultCharacterFixture creates a player whose default_character_id points
// at a freshly-created active character, and returns both plus the character's
// DB-assigned version. Cleanup drops the pointer before the character so the FK
// never blocks teardown.
func newDefaultCharacterFixture(ctx context.Context, t *testing.T, repo *postgres.CharacterRepository) (playerID ulid.ULID, char *world.Character) {
	t.Helper()
	playerID = createTestPlayer(ctx, t)
	char = &world.Character{
		ID:          ulid.Make(),
		PlayerID:    playerID,
		Name:        charFixtureName("default pick"),
		Description: "A character that is its player's default.",
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, delErr(repo.Create(ctx, char, admit(ctx, t, char.Name))))
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `UPDATE players SET default_character_id = NULL WHERE id = $1`, playerID.String())
		_ = delErr(repo.Delete(ctx, char.ID, 0))
	})

	_, err := testPool.Exec(ctx,
		`UPDATE players SET default_character_id = $2 WHERE id = $1`,
		playerID.String(), char.ID.String())
	require.NoError(t, err)
	return playerID, char
}

// defaultCharacterID reads players.default_character_id, returning "" for NULL.
func defaultCharacterID(ctx context.Context, t *testing.T, playerID ulid.ULID) string {
	t.Helper()
	var got *string
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT default_character_id FROM players WHERE id = $1`, playerID.String()).Scan(&got))
	if got == nil {
		return ""
	}
	return *got
}

// currentStatus reads characters.status verbatim.
func currentStatus(ctx context.Context, t *testing.T, characterID ulid.ULID) string {
	t.Helper()
	var got string
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT status FROM characters WHERE id = $1`, characterID.String()).Scan(&got))
	return got
}

// TestCharacterRepositorySetStatusClearsDefaultCharacterPointer covers D-34: a
// soft retire clears players.default_character_id in the SAME transaction as
// the status write. The FK is ON DELETE SET NULL, so it self-heals on a hard
// delete only — without this clear a retire would leave the login paths reading
// a pointer to a retired character.
func TestCharacterRepositorySetStatusClearsDefaultCharacterPointer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("clears the pointer when retiring the player's default character", func(t *testing.T) {
		playerID, char := newDefaultCharacterFixture(ctx, t, repo)
		require.Equal(t, char.ID.String(), defaultCharacterID(ctx, t, playerID),
			"precondition: the player's default is the character under test")

		_, err := repo.SetStatus(ctx, char.ID, world.StatusRetired, char.Version)
		require.NoError(t, err)

		assert.Empty(t, defaultCharacterID(ctx, t, playerID), "retire clears the default pointer")
		assert.Equal(t, string(world.StatusRetired), currentStatus(ctx, t, char.ID))
	})

	t.Run("leaves the pointer untouched when the CAS is refused", func(t *testing.T) {
		// Same-transaction atomicity, proven through the CAS failure path: the
		// status write and the pointer clear abort together, so a stale writer
		// cannot strand a player with no default while the character stays active.
		playerID, char := newDefaultCharacterFixture(ctx, t, repo)

		_, err := repo.SetStatus(ctx, char.ID, world.StatusRetired, char.Version+99)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)

		assert.Equal(t, char.ID.String(), defaultCharacterID(ctx, t, playerID),
			"a refused CAS leaves the default pointer untouched")
		assert.Equal(t, string(world.StatusActive), currentStatus(ctx, t, char.ID),
			"a refused CAS leaves the status untouched")
	})

	t.Run("succeeds with no players rows affected when the character is nobody's default", func(t *testing.T) {
		char := newStatusFixtureCharacter(ctx, t, repo)
		// The fixture's player has no default pointer at all.
		require.Empty(t, defaultCharacterID(ctx, t, char.PlayerID))

		_, err := repo.SetStatus(ctx, char.ID, world.StatusRetired, char.Version)
		require.NoError(t, err)
		assert.Equal(t, string(world.StatusRetired), currentStatus(ctx, t, char.ID))
	})

	t.Run("does not touch the players table on the unretire path", func(t *testing.T) {
		// D-34 is asymmetric by design: retire clears the pointer, unretire does
		// NOT restore it (the old value is gone), and a status write to active
		// must not clear some OTHER pointer either.
		playerID, char := newDefaultCharacterFixture(ctx, t, repo)

		_, err := repo.SetStatus(ctx, char.ID, world.StatusActive, char.Version)
		require.NoError(t, err)

		assert.Equal(t, char.ID.String(), defaultCharacterID(ctx, t, playerID),
			"a status write to active leaves players untouched")
	})
}
