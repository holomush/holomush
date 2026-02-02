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

// createTestPlayer creates a player in the database for testing.
func createTestPlayer(ctx context.Context, t *testing.T) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at)
		VALUES ($1, $2, 'testhash', NOW())
	`, playerID.String(), "player_"+playerID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	return playerID
}

// createTestLocation creates a location in the database for testing.
func createTestLocation(ctx context.Context, t *testing.T) ulid.ULID {
	t.Helper()
	locationID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Test Loc', 'Test', 'persistent', 'last:0', NOW())
	`, locationID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	})

	return locationID
}

func TestCharacterRepository_Get(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("returns ErrNotFound for non-existent character", func(t *testing.T) {
		_, err := repo.Get(ctx, ulid.Make())
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("retrieves existing character", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		locationID := createTestLocation(ctx, t)

		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "TestHero",
			Description: "A test hero.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, char)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, char.ID, got.ID)
		assert.Equal(t, char.PlayerID, got.PlayerID)
		assert.Equal(t, char.Name, got.Name)
		assert.Equal(t, char.Description, got.Description)
		require.NotNil(t, got.LocationID)
		assert.Equal(t, locationID, *got.LocationID)
	})

	t.Run("retrieves character with nil location", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)

		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "NowhereMan",
			Description: "A character without a location.",
			LocationID:  nil,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, char)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID)
	})
}

func TestCharacterRepository_GetByLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("returns empty slice for location with no characters", func(t *testing.T) {
		chars, err := repo.GetByLocation(ctx, ulid.Make(), world.ListOptions{})
		require.NoError(t, err)
		assert.Empty(t, chars)
	})

	t.Run("returns characters at location", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		locationID := createTestLocation(ctx, t)

		char1 := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "Alice",
			Description: "First character.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		char2 := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "Bob",
			Description: "Second character.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		require.NoError(t, repo.Create(ctx, char1))
		require.NoError(t, repo.Create(ctx, char2))

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char1.ID)
			_ = repo.Delete(ctx, char2.ID)
		})

		chars, err := repo.GetByLocation(ctx, locationID, world.ListOptions{})
		require.NoError(t, err)
		assert.Len(t, chars, 2)

		// Check names are sorted alphabetically
		names := make([]string, len(chars))
		for i, c := range chars {
			names[i] = c.Name
		}
		assert.Equal(t, []string{"Alice", "Bob"}, names)
	})
}

func TestCharacterRepository_GetByLocation_Pagination(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	// Create test data: 5 characters at same location
	playerID := createTestPlayer(ctx, t)
	locationID := createTestLocation(ctx, t)

	charNames := []string{"Alice", "Bob", "Charlie", "Diana", "Eve"}
	charIDs := make([]ulid.ULID, len(charNames))

	for i, name := range charNames {
		charIDs[i] = ulid.Make()
		char := &world.Character{
			ID:          charIDs[i],
			PlayerID:    playerID,
			Name:        name,
			Description: name + " description",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, char))
	}

	t.Cleanup(func() {
		for _, id := range charIDs {
			_ = repo.Delete(ctx, id)
		}
	})

	t.Run("limit restricts results", func(t *testing.T) {
		chars, err := repo.GetByLocation(ctx, locationID, world.ListOptions{Limit: 2})
		require.NoError(t, err)
		assert.Len(t, chars, 2)
		// Results are ordered by name, so first 2 should be Alice, Bob
		assert.Equal(t, "Alice", chars[0].Name)
		assert.Equal(t, "Bob", chars[1].Name)
	})

	t.Run("offset skips results", func(t *testing.T) {
		chars, err := repo.GetByLocation(ctx, locationID, world.ListOptions{Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Len(t, chars, 2)
		// Skip Alice, Bob; get Charlie, Diana
		assert.Equal(t, "Charlie", chars[0].Name)
		assert.Equal(t, "Diana", chars[1].Name)
	})

	t.Run("offset beyond results returns empty", func(t *testing.T) {
		chars, err := repo.GetByLocation(ctx, locationID, world.ListOptions{Offset: 100})
		require.NoError(t, err)
		assert.Empty(t, chars)
	})

	t.Run("partial page returns remaining results", func(t *testing.T) {
		// Offset 4, limit 10 should return only Eve (1 result)
		chars, err := repo.GetByLocation(ctx, locationID, world.ListOptions{Limit: 10, Offset: 4})
		require.NoError(t, err)
		assert.Len(t, chars, 1)
		assert.Equal(t, "Eve", chars[0].Name)
	})

	t.Run("zero limit uses default", func(t *testing.T) {
		// With 5 characters, default limit (100) returns all
		chars, err := repo.GetByLocation(ctx, locationID, world.ListOptions{Limit: 0})
		require.NoError(t, err)
		assert.Len(t, chars, 5)
	})

	t.Run("empty ListOptions uses defaults", func(t *testing.T) {
		chars, err := repo.GetByLocation(ctx, locationID, world.ListOptions{})
		require.NoError(t, err)
		assert.Len(t, chars, 5)
	})
}

func TestCharacterRepository_Create(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("creates character successfully", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		locationID := createTestLocation(ctx, t)

		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "NewCharacter",
			Description: "A new character.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, char)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		// Verify creation
		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, char.Name, got.Name)
	})

	t.Run("creates character without location", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)

		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "Homeless",
			Description: "No home yet.",
			LocationID:  nil,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, char)
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID)
	})
}

func TestCharacterRepository_Update(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("updates character successfully", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		locationID := createTestLocation(ctx, t)

		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "OriginalName",
			Description: "Original description.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		require.NoError(t, repo.Create(ctx, char))

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		char.Name = "UpdatedName"
		char.Description = "Updated description."
		err := repo.Update(ctx, char)
		require.NoError(t, err)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, "UpdatedName", got.Name)
		assert.Equal(t, "Updated description.", got.Description)
	})

	t.Run("returns ErrNotFound for non-existent character", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "Ghost",
			Description: "Does not exist.",
		}

		err := repo.Update(ctx, char)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})
}

func TestCharacterRepository_Delete(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("deletes character successfully", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)

		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "ToDelete",
			Description: "Will be deleted.",
			LocationID:  nil,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		require.NoError(t, repo.Create(ctx, char))

		err := repo.Delete(ctx, char.ID)
		require.NoError(t, err)

		_, err = repo.Get(ctx, char.ID)
		assert.ErrorIs(t, err, world.ErrNotFound)
	})

	t.Run("returns ErrNotFound for non-existent character", func(t *testing.T) {
		err := repo.Delete(ctx, ulid.Make())
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})
}

func TestCharacterRepository_IsOwnedByPlayer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("returns true when character is owned by player", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "OwnedChar",
			Description: "Owned by player.",
			LocationID:  nil,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		require.NoError(t, repo.Create(ctx, char))

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		owned, err := repo.IsOwnedByPlayer(ctx, char.ID, playerID)
		require.NoError(t, err)
		assert.True(t, owned)
	})

	t.Run("returns false when character is owned by different player", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		otherPlayerID := createTestPlayer(ctx, t)
		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "NotYourChar",
			Description: "Owned by another player.",
			LocationID:  nil,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		require.NoError(t, repo.Create(ctx, char))

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		owned, err := repo.IsOwnedByPlayer(ctx, char.ID, otherPlayerID)
		require.NoError(t, err)
		assert.False(t, owned)
	})

	t.Run("returns false for non-existent character", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)

		owned, err := repo.IsOwnedByPlayer(ctx, ulid.Make(), playerID)
		require.NoError(t, err)
		assert.False(t, owned)
	})
}

func TestCharacterRepository_UpdateLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	t.Run("moves character to new location", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		loc1 := createTestLocation(ctx, t)
		loc2 := createTestLocation(ctx, t)

		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "Traveler",
			Description: "On the move.",
			LocationID:  &loc1,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		require.NoError(t, repo.Create(ctx, char))

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		err := repo.UpdateLocation(ctx, char.ID, &loc2)
		require.NoError(t, err)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		require.NotNil(t, got.LocationID)
		assert.Equal(t, loc2, *got.LocationID)
	})

	t.Run("removes character from world", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		locationID := createTestLocation(ctx, t)

		char := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "Vanisher",
			Description: "Going away.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		require.NoError(t, repo.Create(ctx, char))

		t.Cleanup(func() {
			_ = repo.Delete(ctx, char.ID)
		})

		err := repo.UpdateLocation(ctx, char.ID, nil)
		require.NoError(t, err)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID)
	})

	t.Run("returns ErrNotFound for non-existent character", func(t *testing.T) {
		locationID := createTestLocation(ctx, t)

		err := repo.UpdateLocation(ctx, ulid.Make(), &locationID)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})
}
