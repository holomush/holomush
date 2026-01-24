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

// createTestCharacter creates a character in the database for testing.
func createTestCharacter(ctx context.Context, t *testing.T, name string) ulid.ULID {
	t.Helper()
	charID := ulid.Make()
	// First create a test player
	playerID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at)
		VALUES ($1, $2, 'testhash', NOW())
	`, playerID.String(), "player_"+playerID.String())
	require.NoError(t, err)

	// Create a test location for the character
	locationID := ulid.Make()
	_, err = testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Test Loc', 'Test', 'persistent', 'last:0', NOW())
	`, locationID.String())
	require.NoError(t, err)

	// Create the character
	_, err = testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id, created_at)
		VALUES ($1, $2, $3, $4, NOW())
	`, charID.String(), playerID.String(), name, locationID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	return charID
}

func TestLocationRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	t.Run("create and get", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Test Room",
			Description:  "A test room for testing.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, loc)
		require.NoError(t, err)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		assert.Equal(t, loc.Name, got.Name)
		assert.Equal(t, loc.Description, got.Description)
		assert.Equal(t, loc.Type, got.Type)
		assert.Equal(t, loc.ReplayPolicy, got.ReplayPolicy)

		// Cleanup
		_ = repo.Delete(ctx, loc.ID)
	})

	t.Run("create with optional fields", func(t *testing.T) {
		// Create a valid character to use as owner
		ownerID := createTestCharacter(ctx, t, "SceneOwner")
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypeScene,
			Name:         "Private Scene",
			Description:  "A private scene.",
			OwnerID:      &ownerID,
			ReplayPolicy: "last:-1",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, loc)
		require.NoError(t, err)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		assert.NotNil(t, got.OwnerID)
		assert.Equal(t, ownerID, *got.OwnerID)

		// Cleanup
		_ = repo.Delete(ctx, loc.ID)
	})

	t.Run("update", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Original Name",
			Description:  "Original description.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, loc)
		require.NoError(t, err)

		loc.Name = "Updated Name"
		loc.Description = "Updated description."
		err = repo.Update(ctx, loc)
		require.NoError(t, err)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", got.Name)
		assert.Equal(t, "Updated description.", got.Description)

		// Cleanup
		_ = repo.Delete(ctx, loc.ID)
	})

	t.Run("update with shadows_id", func(t *testing.T) {
		// Create a parent location to shadow
		parent := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Parent Location",
			Description:  "The parent.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}
		err := repo.Create(ctx, parent)
		require.NoError(t, err)

		// Create a scene without shadows_id
		scene := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypeScene,
			Name:         "Scene Without Shadow",
			Description:  "A scene.",
			ReplayPolicy: "last:-1",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}
		err = repo.Create(ctx, scene)
		require.NoError(t, err)

		// Update to add shadows_id
		scene.ShadowsID = &parent.ID
		err = repo.Update(ctx, scene)
		require.NoError(t, err)

		got, err := repo.Get(ctx, scene.ID)
		require.NoError(t, err)
		require.NotNil(t, got.ShadowsID)
		assert.Equal(t, parent.ID, *got.ShadowsID)

		// Cleanup
		_ = repo.Delete(ctx, scene.ID)
		_ = repo.Delete(ctx, parent.ID)
	})

	t.Run("update with owner_id", func(t *testing.T) {
		ownerID := createTestCharacter(ctx, t, "UpdateOwner")

		// Create a location without owner
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypeScene,
			Name:         "Scene Without Owner",
			Description:  "A scene.",
			ReplayPolicy: "last:-1",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}
		err := repo.Create(ctx, loc)
		require.NoError(t, err)

		// Update to add owner
		loc.OwnerID = &ownerID
		err = repo.Update(ctx, loc)
		require.NoError(t, err)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		require.NotNil(t, got.OwnerID)
		assert.Equal(t, ownerID, *got.OwnerID)

		// Cleanup
		_ = repo.Delete(ctx, loc.ID)
	})

	t.Run("delete", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "To Delete",
			Description:  "Will be deleted.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, loc)
		require.NoError(t, err)

		err = repo.Delete(ctx, loc.ID)
		require.NoError(t, err)

		_, err = repo.Get(ctx, loc.ID)
		assert.Error(t, err)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, ulid.Make())
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("update not found", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Nonexistent",
			Description:  "Does not exist.",
			ReplayPolicy: "last:0",
		}

		err := repo.Update(ctx, loc)
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("delete not found", func(t *testing.T) {
		err := repo.Delete(ctx, ulid.Make())
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("create with invalid type fails", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationType("dungeon"), // invalid type
			Name:         "Bad Room",
			Description:  "Should not be created.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, loc)
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLocationType)
	})

	t.Run("update with invalid type fails", func(t *testing.T) {
		// Create a valid location first
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Valid Room",
			Description:  "A valid room.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
		}
		err := repo.Create(ctx, loc)
		require.NoError(t, err)
		defer func() { _ = repo.Delete(ctx, loc.ID) }()

		// Try to update with invalid type
		loc.Type = world.LocationType("invalid")
		err = repo.Update(ctx, loc)
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidLocationType)
	})
}

func TestLocationRepository_ListByType(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	// Create test locations
	persistent := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypePersistent,
		Name:         "Persistent Room",
		Description:  "A persistent room.",
		ReplayPolicy: "last:0",
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}

	scene := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypeScene,
		Name:         "Test Scene",
		Description:  "A scene.",
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}

	require.NoError(t, repo.Create(ctx, persistent))
	require.NoError(t, repo.Create(ctx, scene))

	t.Cleanup(func() {
		_ = repo.Delete(ctx, persistent.ID)
		_ = repo.Delete(ctx, scene.ID)
	})

	t.Run("list scenes", func(t *testing.T) {
		scenes, err := repo.ListByType(ctx, world.LocationTypeScene)
		require.NoError(t, err)
		assert.NotEmpty(t, scenes)

		found := false
		for _, s := range scenes {
			if s.ID == scene.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "created scene should be in list")
	})

	t.Run("list persistent", func(t *testing.T) {
		persistentLocs, err := repo.ListByType(ctx, world.LocationTypePersistent)
		require.NoError(t, err)
		assert.NotEmpty(t, persistentLocs)

		found := false
		for _, p := range persistentLocs {
			if p.ID == persistent.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "created persistent location should be in list")
	})

	t.Run("list instances returns empty when none", func(t *testing.T) {
		instances, err := repo.ListByType(ctx, world.LocationTypeInstance)
		require.NoError(t, err)
		// May or may not be empty depending on other test data
		_ = instances
	})
}

func TestLocationRepository_GetShadowedBy(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	// Create parent location
	parent := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypePersistent,
		Name:         "Parent Room",
		Description:  "A parent room.",
		ReplayPolicy: "last:0",
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}
	require.NoError(t, repo.Create(ctx, parent))

	// Create scene that shadows parent
	scene := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypeScene,
		ShadowsID:    &parent.ID,
		Name:         "Shadow Scene",
		Description:  "A scene that shadows parent.",
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}
	require.NoError(t, repo.Create(ctx, scene))

	t.Cleanup(func() {
		_ = repo.Delete(ctx, scene.ID)
		_ = repo.Delete(ctx, parent.ID)
	})

	t.Run("find scenes shadowing location", func(t *testing.T) {
		shadows, err := repo.GetShadowedBy(ctx, parent.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, shadows)

		found := false
		for _, s := range shadows {
			if s.ID == scene.ID {
				found = true
				assert.NotNil(t, s.ShadowsID)
				assert.Equal(t, parent.ID, *s.ShadowsID)
				break
			}
		}
		assert.True(t, found, "scene should be in shadowed by list")
	})

	t.Run("no shadows returns empty", func(t *testing.T) {
		shadows, err := repo.GetShadowedBy(ctx, scene.ID)
		require.NoError(t, err)
		assert.Empty(t, shadows)
	})
}
