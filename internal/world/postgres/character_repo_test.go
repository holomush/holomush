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

	"github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/pkg/errutil"
)

// createTestPlayer creates a player in the database for testing.
func createTestPlayer(ctx context.Context, t *testing.T) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
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
		VALUES ($1, 'Test Loc', 'Test', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
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
			CreatedAt:   time.Now().UTC(),
		}

		err := delErr(repo.Create(ctx, char))
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
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
			CreatedAt:   time.Now().UTC(),
		}

		err := delErr(repo.Create(ctx, char))
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
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
			CreatedAt:   time.Now().UTC(),
		}
		char2 := &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        "Bob",
			Description: "Second character.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC(),
		}

		require.NoError(t, delErr(repo.Create(ctx, char1)))
		require.NoError(t, delErr(repo.Create(ctx, char2)))

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char1.ID, 0))
			_ = delErr(repo.Delete(ctx, char2.ID, 0))
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
			CreatedAt:   time.Now().UTC(),
		}
		require.NoError(t, delErr(repo.Create(ctx, char)))
	}

	t.Cleanup(func() {
		for _, id := range charIDs {
			_ = delErr(repo.Delete(ctx, id, 0))
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
			CreatedAt:   time.Now().UTC(),
		}

		err := delErr(repo.Create(ctx, char))
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
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
			CreatedAt:   time.Now().UTC(),
		}

		err := delErr(repo.Create(ctx, char))
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
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
			CreatedAt:   time.Now().UTC(),
		}

		require.NoError(t, delErr(repo.Create(ctx, char)))

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
		})

		char.Name = "UpdatedName"
		char.Description = "Updated description."
		err := delErr(repo.Update(ctx, char))
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

		err := delErr(repo.Update(ctx, char))
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
			CreatedAt:   time.Now().UTC(),
		}

		require.NoError(t, delErr(repo.Create(ctx, char)))

		err := delErr(repo.Delete(ctx, char.ID, 0))
		require.NoError(t, err)

		_, err = repo.Get(ctx, char.ID)
		assert.ErrorIs(t, err, world.ErrNotFound)
	})

	t.Run("returns ErrNotFound for non-existent character", func(t *testing.T) {
		err := delErr(repo.Delete(ctx, ulid.Make(), 0))
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
			CreatedAt:   time.Now().UTC(),
		}

		require.NoError(t, delErr(repo.Create(ctx, char)))

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
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
			CreatedAt:   time.Now().UTC(),
		}

		require.NoError(t, delErr(repo.Create(ctx, char)))

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
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

func TestCharacterRepository_GetNamesByIDs(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	playerID := createTestPlayer(ctx, t)

	char1 := &world.Character{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		Name:      "alice",
		CreatedAt: time.Now().UTC(),
	}
	char2 := &world.Character{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		Name:      "bob",
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, delErr(repo.Create(ctx, char1)))
	require.NoError(t, delErr(repo.Create(ctx, char2)))

	t.Cleanup(func() {
		_ = delErr(repo.Delete(ctx, char1.ID, 0))
		_ = delErr(repo.Delete(ctx, char2.ID, 0))
	})

	t.Run("returns names for present IDs, omits missing IDs", func(t *testing.T) {
		missingID := ulid.Make()
		names, err := repo.GetNamesByIDs(ctx, []ulid.ULID{char1.ID, char2.ID, missingID})
		require.NoError(t, err)
		assert.Equal(t, "alice", names[char1.ID])
		assert.Equal(t, "bob", names[char2.ID])
		_, present := names[missingID]
		assert.False(t, present, "missing id MUST NOT be in result map")
	})

	t.Run("returns empty map for nil input", func(t *testing.T) {
		empty, err := repo.GetNamesByIDs(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, empty, "empty input MUST return non-nil empty map, not nil")
		assert.Empty(t, empty)
	})

	t.Run("returns empty map for empty slice", func(t *testing.T) {
		empty, err := repo.GetNamesByIDs(ctx, []ulid.ULID{})
		require.NoError(t, err)
		assert.NotNil(t, empty, "empty input MUST return non-nil empty map, not nil")
		assert.Empty(t, empty)
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
			CreatedAt:   time.Now().UTC(),
		}

		require.NoError(t, delErr(repo.Create(ctx, char)))

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
		})

		err := delErr(repo.UpdateLocation(ctx, char.ID, &loc2, 0))
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
			CreatedAt:   time.Now().UTC(),
		}

		require.NoError(t, delErr(repo.Create(ctx, char)))

		t.Cleanup(func() {
			_ = delErr(repo.Delete(ctx, char.ID, 0))
		})

		err := delErr(repo.UpdateLocation(ctx, char.ID, nil, 0))
		require.NoError(t, err)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID)
	})

	t.Run("returns ErrNotFound for non-existent character", func(t *testing.T) {
		locationID := createTestLocation(ctx, t)

		err := delErr(repo.UpdateLocation(ctx, ulid.Make(), &locationID, 0))
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})
}

func TestCharacterRepository_ListAll(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	playerID := createTestPlayer(ctx, t)

	// Unique names so residue from sibling tests can't collide; "AAA-" sorts
	// before "ZZZ-" under ORDER BY name ASC.
	alice := &world.Character{ID: ulid.Make(), PlayerID: playerID, Name: "AAA-" + ulid.Make().String()}
	bob := &world.Character{ID: ulid.Make(), PlayerID: playerID, Name: "ZZZ-" + ulid.Make().String()}
	require.NoError(t, delErr(repo.Create(ctx, alice)))
	require.NoError(t, delErr(repo.Create(ctx, bob)))
	t.Cleanup(func() {
		_ = delErr(repo.Delete(ctx, alice.ID, 0))
		_ = delErr(repo.Delete(ctx, bob.ID, 0))
	})

	got, err := repo.ListAll(ctx)
	require.NoError(t, err)

	// Locate the two seeded rows by ID (the shared pool may hold others).
	idxAlice, idxBob := -1, -1
	for i, c := range got {
		switch c.ID {
		case alice.ID:
			idxAlice = i
			assert.Equal(t, alice.Name, c.Name, "name returned for alice")
		case bob.ID:
			idxBob = i
			assert.Equal(t, bob.Name, c.Name, "name returned for bob")
		}
	}
	require.NotEqual(t, -1, idxAlice, "ListAll must include the first seeded character")
	require.NotEqual(t, -1, idxBob, "ListAll must include the second seeded character")
	assert.Less(t, idxAlice, idxBob, "ListAll must be ordered by name ascending")
}

// characterDBVersion reads the stored version column directly (out-of-band of the
// repo) so a test can assert the guard did/did not advance the row.
func characterDBVersion(ctx context.Context, t *testing.T, id ulid.ULID) int {
	t.Helper()
	var v int
	err := testPool.QueryRow(ctx, `SELECT version FROM characters WHERE id = $1`, id.String()).Scan(&v)
	require.NoError(t, err)
	return v
}

// characterDBPreferences reads the stored preferences JSONB directly (out-of-band
// of the repo) so a test can assert the guarded write landed the bytes.
func characterDBPreferences(ctx context.Context, t *testing.T, id ulid.ULID) []byte {
	t.Helper()
	var raw []byte
	err := testPool.QueryRow(ctx, `SELECT preferences FROM characters WHERE id = $1`, id.String()).Scan(&raw)
	require.NoError(t, err)
	return raw
}

// TestCharacterRepository_UpdatePreferencesVersionGuard binds MODEL-03 for the
// folded-in character-settings write (round-4 C5 / D-05): the preferences write
// lives in the world boundary and is version-guarded, so a stale writer surfaces
// WORLD_CONCURRENT_EDIT rather than a silent last-write-wins.
func TestCharacterRepository_UpdatePreferencesVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	newChar := func(t *testing.T, name string) *world.Character {
		t.Helper()
		playerID := createTestPlayer(ctx, t)
		return &world.Character{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			Name:      name,
			CreatedAt: time.Now().UTC(),
		}
	}

	t.Run("successful preferences update increments version and lands bytes", func(t *testing.T) {
		char := newChar(t, "prefs-ok")
		require.NoError(t, delErr(repo.Create(ctx, char)))
		require.Equal(t, 1, char.Version)
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

		prefs := []byte(`{"host":"eyJrIjoidiJ9"}`)
		delta, err := repo.UpdatePreferences(ctx, char.ID, prefs, char.Version)
		require.NoError(t, err)
		assert.Equal(t, 1, delta.Primary.BeforeVersion)
		assert.Equal(t, 2, delta.Primary.AfterVersion)
		assert.Equal(t, 2, characterDBVersion(ctx, t, char.ID))
		assert.JSONEq(t, string(prefs), string(characterDBPreferences(ctx, t, char.ID)))
	})

	t.Run("stale-version preferences update returns WORLD_CONCURRENT_EDIT", func(t *testing.T) {
		char := newChar(t, "prefs-stale")
		require.NoError(t, delErr(repo.Create(ctx, char)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

		// A concurrent winner advances the row past the caller's read version.
		_, err := testPool.Exec(ctx, `UPDATE characters SET version = version + 1 WHERE id = $1`, char.ID.String())
		require.NoError(t, err)

		_, err = repo.UpdatePreferences(ctx, char.ID, []byte(`{"host":"bG9zZXI="}`), 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
	})

	t.Run("preferences update of an absent character returns CHARACTER_NOT_FOUND", func(t *testing.T) {
		_, err := repo.UpdatePreferences(ctx, ulid.Make(), []byte(`{}`), 3)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})
}

// TestCharacterRepository_UpdateVersionGuard binds MODEL-03 for character Update.
func TestCharacterRepository_UpdateVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	newChar := func(t *testing.T, name string) *world.Character {
		t.Helper()
		playerID := createTestPlayer(ctx, t)
		locationID := createTestLocation(ctx, t)
		return &world.Character{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			Name:        name,
			Description: "d",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC(),
		}
	}

	t.Run("create populates version 1 and Get reads it back", func(t *testing.T) {
		char := newChar(t, "guard-create")
		delta, err := repo.Create(ctx, char)
		require.NoError(t, err)
		assert.Equal(t, 1, char.Version)
		assert.Equal(t, 1, delta.Primary.AfterVersion)
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, got.Version)
	})

	t.Run("successful update increments version by 1 and refreshes struct", func(t *testing.T) {
		char := newChar(t, "guard-update")
		require.NoError(t, delErr(repo.Create(ctx, char)))
		require.Equal(t, 1, char.Version)
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

		char.Name = "guard-update-v2"
		delta, err := repo.Update(ctx, char)
		require.NoError(t, err)
		assert.Equal(t, 2, char.Version)
		assert.Equal(t, 1, delta.Primary.BeforeVersion)
		assert.Equal(t, 2, delta.Primary.AfterVersion)
		assert.Equal(t, 2, characterDBVersion(ctx, t, char.ID))
	})

	t.Run("stale-version update returns WORLD_CONCURRENT_EDIT and does not overwrite", func(t *testing.T) {
		char := newChar(t, "guard-stale")
		require.NoError(t, delErr(repo.Create(ctx, char)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

		// A concurrent winner advances the row past the caller's read version.
		_, err := testPool.Exec(ctx, `UPDATE characters SET version = version + 1, name = $2 WHERE id = $1`,
			char.ID.String(), "winner")
		require.NoError(t, err)

		char.Name = "loser"
		_, err = repo.Update(ctx, char)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

		got, err := repo.Get(ctx, char.ID)
		require.NoError(t, err)
		assert.Equal(t, "winner", got.Name)
	})

	t.Run("update of an absent character returns CHARACTER_NOT_FOUND", func(t *testing.T) {
		playerID := createTestPlayer(ctx, t)
		char := &world.Character{ID: ulid.Make(), PlayerID: playerID, Name: "ghost", Version: 4}
		_, err := repo.Update(ctx, char)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})
}

// TestCharacterRepository_MoveVersionGuard binds MODEL-03 for the character move
// write (UpdateLocation), which now carries expectedVersion.
func TestCharacterRepository_MoveVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	newChar := func(t *testing.T, name string) *world.Character {
		t.Helper()
		playerID := createTestPlayer(ctx, t)
		locationID := createTestLocation(ctx, t)
		return &world.Character{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			Name:       name,
			LocationID: &locationID,
			CreatedAt:  time.Now().UTC(),
		}
	}

	t.Run("successful move increments version", func(t *testing.T) {
		char := newChar(t, "move-ok")
		require.NoError(t, delErr(repo.Create(ctx, char)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })
		dest := createTestLocation(ctx, t)

		delta, err := repo.UpdateLocation(ctx, char.ID, &dest, char.Version)
		require.NoError(t, err)
		assert.Equal(t, 1, delta.Primary.BeforeVersion)
		assert.Equal(t, 2, delta.Primary.AfterVersion)
		assert.Equal(t, 2, characterDBVersion(ctx, t, char.ID))
	})

	t.Run("stale-version move returns WORLD_CONCURRENT_EDIT", func(t *testing.T) {
		char := newChar(t, "move-stale")
		require.NoError(t, delErr(repo.Create(ctx, char)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })
		dest := createTestLocation(ctx, t)

		_, err := testPool.Exec(ctx, `UPDATE characters SET version = version + 1 WHERE id = $1`, char.ID.String())
		require.NoError(t, err)

		_, err = repo.UpdateLocation(ctx, char.ID, &dest, 1) // stale expected version
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
	})

	t.Run("move of an absent character returns CHARACTER_NOT_FOUND", func(t *testing.T) {
		dest := createTestLocation(ctx, t)
		_, err := repo.UpdateLocation(ctx, ulid.Make(), &dest, 3)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})
}

// TestCharacterRepository_DeleteVersionGuard binds MODEL-03 for character Delete.
func TestCharacterRepository_DeleteVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	newChar := func(t *testing.T, name string) *world.Character {
		t.Helper()
		playerID := createTestPlayer(ctx, t)
		locationID := createTestLocation(ctx, t)
		return &world.Character{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			Name:       name,
			LocationID: &locationID,
			CreatedAt:  time.Now().UTC(),
		}
	}

	t.Run("stale-version delete returns WORLD_CONCURRENT_EDIT", func(t *testing.T) {
		char := newChar(t, "del-stale")
		require.NoError(t, delErr(repo.Create(ctx, char)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, char.ID, 0)) })

		_, err := testPool.Exec(ctx, `UPDATE characters SET version = version + 1 WHERE id = $1`, char.ID.String())
		require.NoError(t, err)

		_, err = repo.Delete(ctx, char.ID, 1) // stale expected version
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
	})

	t.Run("delete of an absent character returns CHARACTER_NOT_FOUND", func(t *testing.T) {
		_, err := repo.Delete(ctx, ulid.Make(), 2)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
	})

	t.Run("version-matched delete succeeds and returns a tombstone delta", func(t *testing.T) {
		char := newChar(t, "del-ok")
		require.NoError(t, delErr(repo.Create(ctx, char)))

		delta, err := repo.Delete(ctx, char.ID, char.Version)
		require.NoError(t, err)
		assert.True(t, delta.Primary.Tombstone)
		assert.Equal(t, 1, delta.Primary.BeforeVersion)

		_, err = repo.Get(ctx, char.ID)
		assert.ErrorIs(t, err, world.ErrNotFound)
	})
}

// TestCharacterRepository_ListByPlayer binds round-6 R6-1: the canonical
// version-scanning list the guest reaper (05-16) uses — each listed character
// carries its STORED version, so a subsequent CAS Delete(ctx, id, listedVersion)
// matches and succeeds rather than permanently conflicting on Version==0.
func TestCharacterRepository_ListByPlayer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)

	playerID := createTestPlayer(ctx, t)
	locationID := createTestLocation(ctx, t)

	c1 := &world.Character{ID: ulid.Make(), PlayerID: playerID, Name: "AAA-" + ulid.Make().String(), LocationID: &locationID}
	c2 := &world.Character{ID: ulid.Make(), PlayerID: playerID, Name: "ZZZ-" + ulid.Make().String(), LocationID: &locationID}
	require.NoError(t, delErr(repo.Create(ctx, c1)))
	require.NoError(t, delErr(repo.Create(ctx, c2)))
	t.Cleanup(func() {
		_ = delErr(repo.Delete(ctx, c1.ID, 0))
		_ = delErr(repo.Delete(ctx, c2.ID, 0))
	})

	// Advance one character's version so listed-vs-stored is a non-trivial check.
	c1.Name = "AAA-updated"
	require.NoError(t, delErr(repo.Update(ctx, c1)))
	require.Equal(t, 2, c1.Version)

	listed, err := repo.ListByPlayer(ctx, playerID)
	require.NoError(t, err)

	byID := map[ulid.ULID]*world.Character{}
	for _, c := range listed {
		byID[c.ID] = c
	}
	require.Contains(t, byID, c1.ID)
	require.Contains(t, byID, c2.ID)

	// Listed version == stored version for every character.
	assert.Equal(t, characterDBVersion(ctx, t, c1.ID), byID[c1.ID].Version)
	assert.Equal(t, characterDBVersion(ctx, t, c2.ID), byID[c2.ID].Version)
	assert.Equal(t, 2, byID[c1.ID].Version)
	assert.Equal(t, 1, byID[c2.ID].Version)

	// A guarded CAS delete keyed on the LISTED version succeeds (the reaper path).
	_, err = repo.Delete(ctx, c1.ID, byID[c1.ID].Version)
	require.NoError(t, err)
}

// TestCharRepoAdapter_ListByPlayerScansVersion binds round-6 R6-1/R6-3: the
// production auth-facing list path (setup.CharRepoAdapter.ListByPlayer) — the
// actual list a subsequent guarded write/delete consumes — must carry the stored
// version, not 0, or the guest reaper's CAS Delete permanently conflicts.
func TestCharRepoAdapter_ListByPlayerScansVersion(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	adapter := setup.NewCharRepoAdapter(testPool, repo)

	playerID := createTestPlayer(ctx, t)
	locationID := createTestLocation(ctx, t)
	c := &world.Character{ID: ulid.Make(), PlayerID: playerID, Name: "adapter-ver-" + ulid.Make().String(), LocationID: &locationID}
	require.NoError(t, delErr(repo.Create(ctx, c)))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, c.ID, 0)) })

	// Advance the version so listed-vs-stored is a non-trivial check.
	c.Name = "adapter-ver-updated"
	require.NoError(t, delErr(repo.Update(ctx, c)))
	require.Equal(t, 2, c.Version)

	listed, err := adapter.ListByPlayer(ctx, playerID)
	require.NoError(t, err)
	var found *world.Character
	for _, lc := range listed {
		if lc.ID == c.ID {
			found = lc
		}
	}
	require.NotNil(t, found, "adapter ListByPlayer must return the seeded character")
	assert.Equal(t, characterDBVersion(ctx, t, c.ID), found.Version, "adapter must scan version")
	assert.Equal(t, 2, found.Version)

	// The listed version drives a successful guarded CAS delete (reaper path).
	_, err = repo.Delete(ctx, c.ID, found.Version)
	require.NoError(t, err)
}
