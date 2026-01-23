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

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
)

func TestObjectRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location for object containment
	locationID := core.NewULID()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Test Location', 'A test location', 'persistent', 'last:0', NOW())
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	t.Run("create and get", func(t *testing.T) {
		obj := &world.Object{
			ID:          core.NewULID(),
			Name:        "Test Sword",
			Description: "A shiny test sword.",
			LocationID:  &locationID,
			IsContainer: false,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, obj)
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Equal(t, obj.Name, got.Name)
		assert.Equal(t, obj.Description, got.Description)
		assert.NotNil(t, got.LocationID)
		assert.Equal(t, locationID, *got.LocationID)
		assert.False(t, got.IsContainer)

		// Cleanup
		_ = repo.Delete(ctx, obj.ID)
	})

	t.Run("update", func(t *testing.T) {
		obj := &world.Object{
			ID:          core.NewULID(),
			Name:        "Original Name",
			Description: "Original description.",
			LocationID:  &locationID,
			IsContainer: false,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, obj)
		require.NoError(t, err)

		obj.Name = "Updated Name"
		obj.Description = "Updated description."
		obj.IsContainer = true
		err = repo.Update(ctx, obj)
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", got.Name)
		assert.Equal(t, "Updated description.", got.Description)
		assert.True(t, got.IsContainer)

		// Cleanup
		_ = repo.Delete(ctx, obj.ID)
	})

	t.Run("delete", func(t *testing.T) {
		obj := &world.Object{
			ID:          core.NewULID(),
			Name:        "To Delete",
			Description: "Will be deleted.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, obj)
		require.NoError(t, err)

		err = repo.Delete(ctx, obj.ID)
		require.NoError(t, err)

		_, err = repo.Get(ctx, obj.ID)
		assert.Error(t, err)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, ulid.Make())
		assert.Error(t, err)
	})
}

func TestObjectRepository_ListAtLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location
	locationID := core.NewULID()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Test Location 2', 'Another test location', 'persistent', 'last:0', NOW())
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	// Create objects at location
	obj1 := &world.Object{
		ID:          core.NewULID(),
		Name:        "Object 1",
		Description: "First object.",
		LocationID:  &locationID,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
	obj2 := &world.Object{
		ID:          core.NewULID(),
		Name:        "Object 2",
		Description: "Second object.",
		LocationID:  &locationID,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}

	require.NoError(t, repo.Create(ctx, obj1))
	require.NoError(t, repo.Create(ctx, obj2))
	defer func() {
		_ = repo.Delete(ctx, obj1.ID)
		_ = repo.Delete(ctx, obj2.ID)
	}()

	objects, err := repo.ListAtLocation(ctx, locationID)
	require.NoError(t, err)
	assert.Len(t, objects, 2)

	// Verify both objects are returned
	foundNames := make(map[string]bool)
	for _, obj := range objects {
		foundNames[obj.Name] = true
	}
	assert.True(t, foundNames["Object 1"])
	assert.True(t, foundNames["Object 2"])
}

func TestObjectRepository_ListHeldBy(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location first
	locationID := core.NewULID()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Char Location', 'Location for character', 'persistent', 'last:0', NOW())
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	// Create a test player first with unique username
	playerID := core.NewULID()
	_, err = testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at)
		VALUES ($1, $2, 'testhash', NOW())
	`, playerID.String(), "player_"+playerID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	}()

	// Create a test character
	characterID := core.NewULID()
	_, err = testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id, created_at)
		VALUES ($1, $2, 'Test Character', $3, NOW())
	`, characterID.String(), playerID.String(), locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, characterID.String())
	}()

	// Create objects held by character
	obj := &world.Object{
		ID:                core.NewULID(),
		Name:              "Held Object",
		Description:       "Object held by character.",
		HeldByCharacterID: &characterID,
		CreatedAt:         time.Now().UTC().Truncate(time.Microsecond),
	}

	require.NoError(t, repo.Create(ctx, obj))
	defer func() {
		_ = repo.Delete(ctx, obj.ID)
	}()

	objects, err := repo.ListHeldBy(ctx, characterID)
	require.NoError(t, err)
	assert.Len(t, objects, 1)
	assert.Equal(t, "Held Object", objects[0].Name)
}

func TestObjectRepository_ListContainedIn(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location
	locationID := core.NewULID()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Container Location', 'Location for container test', 'persistent', 'last:0', NOW())
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	// Create a container object
	container := &world.Object{
		ID:          core.NewULID(),
		Name:        "Chest",
		Description: "A wooden chest.",
		LocationID:  &locationID,
		IsContainer: true,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
	require.NoError(t, repo.Create(ctx, container))
	defer func() {
		_ = repo.Delete(ctx, container.ID)
	}()

	// Create object inside container
	item := &world.Object{
		ID:                  core.NewULID(),
		Name:                "Gold Coin",
		Description:         "A shiny gold coin.",
		ContainedInObjectID: &container.ID,
		CreatedAt:           time.Now().UTC().Truncate(time.Microsecond),
	}
	require.NoError(t, repo.Create(ctx, item))
	defer func() {
		_ = repo.Delete(ctx, item.ID)
	}()

	objects, err := repo.ListContainedIn(ctx, container.ID)
	require.NoError(t, err)
	assert.Len(t, objects, 1)
	assert.Equal(t, "Gold Coin", objects[0].Name)
}

func TestObjectRepository_Move(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create two test locations
	loc1ID := core.NewULID()
	loc2ID := core.NewULID()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Location 1', 'First location', 'persistent', 'last:0', NOW()),
		       ($2, 'Location 2', 'Second location', 'persistent', 'last:0', NOW())
	`, loc1ID.String(), loc2ID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id IN ($1, $2)`, loc1ID.String(), loc2ID.String())
	}()

	t.Run("move to location", func(t *testing.T) {
		obj := &world.Object{
			ID:          core.NewULID(),
			Name:        "Movable Object",
			Description: "Can be moved.",
			LocationID:  &loc1ID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, obj))
		defer func() {
			_ = repo.Delete(ctx, obj.ID)
		}()

		// Move to second location
		err := repo.Move(ctx, obj.ID, world.Containment{LocationID: &loc2ID})
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.NotNil(t, got.LocationID)
		assert.Equal(t, loc2ID, *got.LocationID)
		assert.Nil(t, got.HeldByCharacterID)
		assert.Nil(t, got.ContainedInObjectID)
	})

	t.Run("move to container", func(t *testing.T) {
		container := &world.Object{
			ID:          core.NewULID(),
			Name:        "Box",
			Description: "A box.",
			LocationID:  &loc1ID,
			IsContainer: true,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, container))
		defer func() {
			_ = repo.Delete(ctx, container.ID)
		}()

		item := &world.Object{
			ID:          core.NewULID(),
			Name:        "Key",
			Description: "A small key.",
			LocationID:  &loc1ID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, item))
		defer func() {
			_ = repo.Delete(ctx, item.ID)
		}()

		// Move key into box
		err := repo.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID})
		require.NoError(t, err)

		got, err := repo.Get(ctx, item.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID)
		assert.Nil(t, got.HeldByCharacterID)
		assert.NotNil(t, got.ContainedInObjectID)
		assert.Equal(t, container.ID, *got.ContainedInObjectID)
	})

	t.Run("move to non-container fails", func(t *testing.T) {
		nonContainer := &world.Object{
			ID:          core.NewULID(),
			Name:        "Rock",
			Description: "A rock.",
			LocationID:  &loc1ID,
			IsContainer: false,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, nonContainer))
		defer func() {
			_ = repo.Delete(ctx, nonContainer.ID)
		}()

		item := &world.Object{
			ID:          core.NewULID(),
			Name:        "Pebble",
			Description: "A small pebble.",
			LocationID:  &loc1ID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, item))
		defer func() {
			_ = repo.Delete(ctx, item.ID)
		}()

		// Try to move pebble into rock (should fail)
		err := repo.Move(ctx, item.ID, world.Containment{ObjectID: &nonContainer.ID})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a container")
	})

	t.Run("invalid containment fails", func(t *testing.T) {
		obj := &world.Object{
			ID:          core.NewULID(),
			Name:        "Test Object",
			Description: "Test.",
			LocationID:  &loc1ID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, obj))
		defer func() {
			_ = repo.Delete(ctx, obj.ID)
		}()

		// Empty containment should fail
		err := repo.Move(ctx, obj.ID, world.Containment{})
		assert.Error(t, err)
	})

	t.Run("move to non-existent container fails", func(t *testing.T) {
		item := &world.Object{
			ID:          core.NewULID(),
			Name:        "Lost Item",
			Description: "Item looking for container.",
			LocationID:  &loc1ID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, item))
		defer func() {
			_ = repo.Delete(ctx, item.ID)
		}()

		// Try to move to a container that doesn't exist
		nonExistentID := core.NewULID()
		err := repo.Move(ctx, item.ID, world.Containment{ObjectID: &nonExistentID})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "container object not found")
	})
}
