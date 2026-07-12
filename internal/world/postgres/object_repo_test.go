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

func TestObjectRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location for object containment
	locationID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Test Location', 'A test location', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	t.Run("create and get", func(t *testing.T) {
		obj, err := world.NewObjectWithID(ulid.Make(), "Test Sword", world.InLocation(locationID))
		require.NoError(t, err)
		obj.Description = "A shiny test sword."
		obj.IsContainer = false
		obj.CreatedAt = time.Now().UTC()

		err = delErr(repo.Create(ctx, obj))
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Equal(t, obj.Name, got.Name)
		assert.Equal(t, obj.Description, got.Description)
		assert.NotNil(t, got.LocationID())
		assert.Equal(t, locationID, *got.LocationID())
		assert.False(t, got.IsContainer)

		// Cleanup
		_ = delErr(repo.Delete(ctx, obj.ID, 0))
	})

	// Note: "create with invalid containment - no location" test removed.
	// The Object type now enforces containment at construction time via NewObject/NewObjectWithID.
	// Invalid objects cannot be created from external packages, so this defense-in-depth
	// test is no longer applicable at the repository level.

	t.Run("update", func(t *testing.T) {
		obj, err := world.NewObjectWithID(ulid.Make(), "Original Name", world.InLocation(locationID))
		require.NoError(t, err)
		obj.Description = "Original description."
		obj.IsContainer = false
		obj.CreatedAt = time.Now().UTC()

		err = delErr(repo.Create(ctx, obj))
		require.NoError(t, err)

		obj.Name = "Updated Name"
		obj.Description = "Updated description."
		obj.IsContainer = true
		err = delErr(repo.Update(ctx, obj))
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", got.Name)
		assert.Equal(t, "Updated description.", got.Description)
		assert.True(t, got.IsContainer)

		// Cleanup
		_ = delErr(repo.Delete(ctx, obj.ID, 0))
	})

	t.Run("update with owner_id", func(t *testing.T) {
		// Create a character to be the owner
		charID := ulid.Make()
		playerID := ulid.Make()
		_, err := testPool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash, created_at, updated_at)
			VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		`, playerID.String(), "player_update_"+playerID.String())
		require.NoError(t, err)
		_, err = testPool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, created_at)
			VALUES ($1, $2, 'UpdateOwnerChar', $3, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
		`, charID.String(), playerID.String(), locationID.String())
		require.NoError(t, err)

		obj, err := world.NewObjectWithID(ulid.Make(), "Object For Owner Test", world.InLocation(locationID))
		require.NoError(t, err)
		obj.Description = "An object."
		obj.IsContainer = false
		obj.CreatedAt = time.Now().UTC()

		err = delErr(repo.Create(ctx, obj))
		require.NoError(t, err)

		// Update to add owner
		obj.OwnerID = &charID
		err = delErr(repo.Update(ctx, obj))
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		require.NotNil(t, got.OwnerID)
		assert.Equal(t, charID, *got.OwnerID)

		// Cleanup
		_ = delErr(repo.Delete(ctx, obj.ID, 0))
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	t.Run("update change containment to held_by", func(t *testing.T) {
		// Create a character to hold the object
		charID := ulid.Make()
		playerID := ulid.Make()
		_, err := testPool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash, created_at, updated_at)
			VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		`, playerID.String(), "player_held_"+playerID.String())
		require.NoError(t, err)
		_, err = testPool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, created_at)
			VALUES ($1, $2, 'HolderChar', $3, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
		`, charID.String(), playerID.String(), locationID.String())
		require.NoError(t, err)

		obj, err := world.NewObjectWithID(ulid.Make(), "Object To Hold", world.InLocation(locationID))
		require.NoError(t, err)
		obj.Description = "An object."
		obj.IsContainer = false
		obj.CreatedAt = time.Now().UTC()

		err = delErr(repo.Create(ctx, obj))
		require.NoError(t, err)

		// Update to be held by character (move from location to inventory)
		err = obj.SetContainment(world.Containment{CharacterID: &charID})
		require.NoError(t, err)
		err = delErr(repo.Update(ctx, obj))
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID())
		require.NotNil(t, got.HeldByCharacterID())
		assert.Equal(t, charID, *got.HeldByCharacterID())

		// Cleanup
		_ = delErr(repo.Delete(ctx, obj.ID, 0))
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	t.Run("update change containment to container", func(t *testing.T) {
		// Create a container object
		container, err := world.NewObjectWithID(ulid.Make(), "Container", world.InLocation(locationID))
		require.NoError(t, err)
		container.Description = "A container."
		container.IsContainer = true
		container.CreatedAt = time.Now().UTC()
		err = delErr(repo.Create(ctx, container))
		require.NoError(t, err)

		// Create an object to put in the container
		obj, err := world.NewObjectWithID(ulid.Make(), "Object To Contain", world.InLocation(locationID))
		require.NoError(t, err)
		obj.Description = "An object."
		obj.IsContainer = false
		obj.CreatedAt = time.Now().UTC()
		err = delErr(repo.Create(ctx, obj))
		require.NoError(t, err)

		// Update to be contained in container
		err = obj.SetContainment(world.Containment{ObjectID: &container.ID})
		require.NoError(t, err)
		err = delErr(repo.Update(ctx, obj))
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID())
		require.NotNil(t, got.ContainedInObjectID())
		assert.Equal(t, container.ID, *got.ContainedInObjectID())

		// Cleanup
		_ = delErr(repo.Delete(ctx, obj.ID, 0))
		_ = delErr(repo.Delete(ctx, container.ID, 0))
	})

	t.Run("delete", func(t *testing.T) {
		obj, err := world.NewObjectWithID(ulid.Make(), "To Delete", world.InLocation(locationID))
		require.NoError(t, err)
		obj.Description = "Will be deleted."
		obj.CreatedAt = time.Now().UTC()

		err = delErr(repo.Create(ctx, obj))
		require.NoError(t, err)

		err = delErr(repo.Delete(ctx, obj.ID, 0))
		require.NoError(t, err)

		_, err = repo.Get(ctx, obj.ID)
		assert.Error(t, err)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, ulid.Make())
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("update not found", func(t *testing.T) {
		obj, err := world.NewObjectWithID(ulid.Make(), "Nonexistent", world.InLocation(locationID))
		require.NoError(t, err)
		obj.Description = "Does not exist."
		obj.CreatedAt = time.Now().UTC()
		err = delErr(repo.Update(ctx, obj))
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("delete not found", func(t *testing.T) {
		err := delErr(repo.Delete(ctx, ulid.Make(), 0))
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	// Note: "update with invalid containment - no location" test removed.
	// The Object type now enforces containment via SetContainment() which validates input.
	// Attempting SetContainment with empty Containment returns an error, preventing
	// invalid state from being persisted. The database constraint remains as defense-in-depth
	// but cannot be tested from external packages since invalid objects cannot be constructed.
}

func TestObjectRepository_ListAtLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location
	locationID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Test Location 2', 'Another test location', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	// Create objects at location
	obj1, err := world.NewObjectWithID(ulid.Make(), "Object 1", world.InLocation(locationID))
	require.NoError(t, err)
	obj1.Description = "First object."
	obj1.CreatedAt = time.Now().UTC()
	obj2, err := world.NewObjectWithID(ulid.Make(), "Object 2", world.InLocation(locationID))
	require.NoError(t, err)
	obj2.Description = "Second object."
	obj2.CreatedAt = time.Now().UTC()

	require.NoError(t, delErr(repo.Create(ctx, obj1)))
	require.NoError(t, delErr(repo.Create(ctx, obj2)))
	defer func() {
		_ = delErr(repo.Delete(ctx, obj1.ID, 0))
		_ = delErr(repo.Delete(ctx, obj2.ID, 0))
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

func TestObjectRepository_ListAtLocation_Empty(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create an empty location
	emptyLocationID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Empty Location', 'No objects here', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, emptyLocationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, emptyLocationID.String())
	}()

	// Query for objects at empty location
	objects, err := repo.ListAtLocation(ctx, emptyLocationID)
	require.NoError(t, err)
	assert.NotNil(t, objects, "should return non-nil empty slice, not nil")
	assert.Empty(t, objects)
}

func TestObjectRepository_ListHeldBy(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location first
	locationID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Char Location', 'Location for character', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	// Create a test player first with unique username
	playerID := ulid.Make()
	_, err = testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, playerID.String(), "player_"+playerID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	}()

	// Create a test character
	characterID := ulid.Make()
	_, err = testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id, created_at)
		VALUES ($1, $2, 'Test Character', $3, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, characterID.String(), playerID.String(), locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, characterID.String())
	}()

	// Create objects held by character
	obj, err := world.NewObjectWithID(ulid.Make(), "Held Object", world.HeldBy(characterID))
	require.NoError(t, err)
	obj.Description = "Object held by character."
	obj.CreatedAt = time.Now().UTC()

	require.NoError(t, delErr(repo.Create(ctx, obj)))
	defer func() {
		_ = delErr(repo.Delete(ctx, obj.ID, 0))
	}()

	objects, err := repo.ListHeldBy(ctx, characterID)
	require.NoError(t, err)
	assert.Len(t, objects, 1)
	assert.Equal(t, "Held Object", objects[0].Name)
}

func TestObjectRepository_ListHeldBy_OrderingWithMultipleObjects(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location
	locationID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Inventory Test Location', 'Location for inventory ordering test', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	// Create a test player
	playerID := ulid.Make()
	_, err = testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, playerID.String(), "player_order_"+playerID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	}()

	// Create a test character
	characterID := ulid.Make()
	_, err = testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id, created_at)
		VALUES ($1, $2, 'Inventory Test Character', $3, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, characterID.String(), playerID.String(), locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, characterID.String())
	}()

	// Create 3 objects with distinct creation times to verify ordering.
	// Objects are ordered by created_at DESC (newest first).
	baseTime := time.Now().UTC()

	obj1, err := world.NewObjectWithID(ulid.Make(), "First Object (oldest)", world.HeldBy(characterID))
	require.NoError(t, err)
	obj1.Description = "Created first."
	obj1.CreatedAt = baseTime.Add(-2 * time.Second) // oldest

	obj2, err := world.NewObjectWithID(ulid.Make(), "Second Object (middle)", world.HeldBy(characterID))
	require.NoError(t, err)
	obj2.Description = "Created second."
	obj2.CreatedAt = baseTime.Add(-1 * time.Second) // middle

	obj3, err := world.NewObjectWithID(ulid.Make(), "Third Object (newest)", world.HeldBy(characterID))
	require.NoError(t, err)
	obj3.Description = "Created third."
	obj3.CreatedAt = baseTime // newest

	// Create in random order to ensure ORDER BY is doing the work
	require.NoError(t, delErr(repo.Create(ctx, obj2)))
	require.NoError(t, delErr(repo.Create(ctx, obj1)))
	require.NoError(t, delErr(repo.Create(ctx, obj3)))
	defer func() {
		_ = delErr(repo.Delete(ctx, obj1.ID, 0))
		_ = delErr(repo.Delete(ctx, obj2.ID, 0))
		_ = delErr(repo.Delete(ctx, obj3.ID, 0))
	}()

	objects, err := repo.ListHeldBy(ctx, characterID)
	require.NoError(t, err)
	require.Len(t, objects, 3)

	// Verify ordering: newest first (ORDER BY created_at DESC)
	assert.Equal(t, "Third Object (newest)", objects[0].Name, "newest object should be first")
	assert.Equal(t, "Second Object (middle)", objects[1].Name, "middle object should be second")
	assert.Equal(t, "First Object (oldest)", objects[2].Name, "oldest object should be last")

	// Verify created_at values are in descending order
	assert.True(t, objects[0].CreatedAt.After(objects[1].CreatedAt) || objects[0].CreatedAt.Equal(objects[1].CreatedAt),
		"first object created_at should be >= second")
	assert.True(t, objects[1].CreatedAt.After(objects[2].CreatedAt) || objects[1].CreatedAt.Equal(objects[2].CreatedAt),
		"second object created_at should be >= third")
}

func TestObjectRepository_ListContainedIn(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location
	locationID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Container Location', 'Location for container test', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	// Create a container object
	container, err := world.NewObjectWithID(ulid.Make(), "Chest", world.InLocation(locationID))
	require.NoError(t, err)
	container.Description = "A wooden chest."
	container.IsContainer = true
	container.CreatedAt = time.Now().UTC()
	require.NoError(t, delErr(repo.Create(ctx, container)))
	defer func() {
		_ = delErr(repo.Delete(ctx, container.ID, 0))
	}()

	// Create object inside container
	item, err := world.NewObjectWithID(ulid.Make(), "Gold Coin", world.InContainer(container.ID))
	require.NoError(t, err)
	item.Description = "A shiny gold coin."
	item.CreatedAt = time.Now().UTC()
	require.NoError(t, delErr(repo.Create(ctx, item)))
	defer func() {
		_ = delErr(repo.Delete(ctx, item.ID, 0))
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
	loc1ID := ulid.Make()
	loc2ID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Location 1', 'First location', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT),
		       ($2, 'Location 2', 'Second location', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, loc1ID.String(), loc2ID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id IN ($1, $2)`, loc1ID.String(), loc2ID.String())
	}()

	t.Run("move to location", func(t *testing.T) {
		obj, err := world.NewObjectWithID(ulid.Make(), "Movable Object", world.InLocation(loc1ID))
		require.NoError(t, err)
		obj.Description = "Can be moved."
		obj.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, obj)))
		defer func() {
			_ = delErr(repo.Delete(ctx, obj.ID, 0))
		}()

		// Move to second location
		err = delErr(repo.Move(ctx, obj.ID, world.Containment{LocationID: &loc2ID}, 0))
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.NotNil(t, got.LocationID())
		assert.Equal(t, loc2ID, *got.LocationID())
		assert.Nil(t, got.HeldByCharacterID())
		assert.Nil(t, got.ContainedInObjectID())
	})

	t.Run("move to container", func(t *testing.T) {
		container, err := world.NewObjectWithID(ulid.Make(), "Box", world.InLocation(loc1ID))
		require.NoError(t, err)
		container.Description = "A box."
		container.IsContainer = true
		container.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, container)))
		defer func() {
			_ = delErr(repo.Delete(ctx, container.ID, 0))
		}()

		item, err := world.NewObjectWithID(ulid.Make(), "Key", world.InLocation(loc1ID))
		require.NoError(t, err)
		item.Description = "A small key."
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() {
			_ = delErr(repo.Delete(ctx, item.ID, 0))
		}()

		// Move key into box
		err = delErr(repo.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID}, 0))
		require.NoError(t, err)

		got, err := repo.Get(ctx, item.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID())
		assert.Nil(t, got.HeldByCharacterID())
		assert.NotNil(t, got.ContainedInObjectID())
		assert.Equal(t, container.ID, *got.ContainedInObjectID())
	})

	t.Run("move to non-container fails", func(t *testing.T) {
		nonContainer, err := world.NewObjectWithID(ulid.Make(), "Rock", world.InLocation(loc1ID))
		require.NoError(t, err)
		nonContainer.Description = "A rock."
		nonContainer.IsContainer = false
		nonContainer.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, nonContainer)))
		defer func() {
			_ = delErr(repo.Delete(ctx, nonContainer.ID, 0))
		}()

		item, err := world.NewObjectWithID(ulid.Make(), "Pebble", world.InLocation(loc1ID))
		require.NoError(t, err)
		item.Description = "A small pebble."
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() {
			_ = delErr(repo.Delete(ctx, item.ID, 0))
		}()

		// Try to move pebble into rock (should fail)
		err = delErr(repo.Move(ctx, item.ID, world.Containment{ObjectID: &nonContainer.ID}, 0))
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment, "should wrap ErrInvalidContainment for non-container target")
	})

	t.Run("invalid containment fails", func(t *testing.T) {
		obj, err := world.NewObjectWithID(ulid.Make(), "Test Object", world.InLocation(loc1ID))
		require.NoError(t, err)
		obj.Description = "Test."
		obj.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, obj)))
		defer func() {
			_ = delErr(repo.Delete(ctx, obj.ID, 0))
		}()

		// Empty containment should fail
		err = delErr(repo.Move(ctx, obj.ID, world.Containment{}, 0))
		assert.Error(t, err)
	})

	t.Run("move to non-existent container fails", func(t *testing.T) {
		item, err := world.NewObjectWithID(ulid.Make(), "Lost Item", world.InLocation(loc1ID))
		require.NoError(t, err)
		item.Description = "Item looking for container."
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() {
			_ = delErr(repo.Delete(ctx, item.ID, 0))
		}()

		// Try to move to a container that doesn't exist
		nonExistentID := ulid.Make()
		err = delErr(repo.Move(ctx, item.ID, world.Containment{ObjectID: &nonExistentID}, 0))
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound, "should wrap ErrNotFound for missing container")
		errutil.AssertErrorCode(t, err, "CONTAINER_NOT_FOUND")
	})

	t.Run("move to character", func(t *testing.T) {
		// Create a test player first
		playerID := ulid.Make()
		_, err := testPool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash, created_at, updated_at)
			VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		`, playerID.String(), "player_move_"+playerID.String())
		require.NoError(t, err)
		defer func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
		}()

		// Create a test character
		characterID := ulid.Make()
		_, err = testPool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, created_at)
			VALUES ($1, $2, 'Move Test Character', $3, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
		`, characterID.String(), playerID.String(), loc1ID.String())
		require.NoError(t, err)
		defer func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, characterID.String())
		}()

		item, err := world.NewObjectWithID(ulid.Make(), "Portable Item", world.InLocation(loc1ID))
		require.NoError(t, err)
		item.Description = "Can be picked up."
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() {
			_ = delErr(repo.Delete(ctx, item.ID, 0))
		}()

		// Move to character
		err = delErr(repo.Move(ctx, item.ID, world.Containment{CharacterID: &characterID}, 0))
		require.NoError(t, err)

		got, err := repo.Get(ctx, item.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID())
		assert.NotNil(t, got.HeldByCharacterID())
		assert.Equal(t, characterID, *got.HeldByCharacterID())
		assert.Nil(t, got.ContainedInObjectID())
	})

	t.Run("move exceeds max nesting depth fails", func(t *testing.T) {
		// Create containers nested 3 deep (at max depth)
		// level1 -> level2 -> level3 -> item (should fail to add level4)

		level1, err := world.NewObjectWithID(ulid.Make(), "Level1 Container", world.InLocation(loc1ID))
		require.NoError(t, err)
		level1.Description = "Top level container."
		level1.IsContainer = true
		level1.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, level1)))
		defer func() { _ = delErr(repo.Delete(ctx, level1.ID, 0)) }()

		level2, err := world.NewObjectWithID(ulid.Make(), "Level2 Container", world.InContainer(level1.ID))
		require.NoError(t, err)
		level2.Description = "Second level container."
		level2.IsContainer = true
		level2.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, level2)))
		defer func() { _ = delErr(repo.Delete(ctx, level2.ID, 0)) }()

		level3, err := world.NewObjectWithID(ulid.Make(), "Level3 Container", world.InContainer(level2.ID))
		require.NoError(t, err)
		level3.Description = "Third level container."
		level3.IsContainer = true
		level3.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, level3)))
		defer func() { _ = delErr(repo.Delete(ctx, level3.ID, 0)) }()

		// Try to add an item at level 4 - should fail (exceeds max depth of 3)
		item, err := world.NewObjectWithID(ulid.Make(), "Deep Item", world.InLocation(loc1ID))
		require.NoError(t, err)
		item.Description = "Too deep."
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() { _ = delErr(repo.Delete(ctx, item.ID, 0)) }()

		err = delErr(repo.Move(ctx, item.ID, world.Containment{ObjectID: &level3.ID}, 0))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max nesting depth")
		errutil.AssertErrorCode(t, err, "NESTING_DEPTH_EXCEEDED")
	})

	t.Run("move container with nested items exceeds depth fails", func(t *testing.T) {
		// This tests the scenario where we move a container that already has items
		// into another container, which could exceed max depth.
		// Max depth is 3, so:
		// - Create containerA (in room) with itemA inside (depth 2)
		// - Create containerB (in room) with containerC inside (depth 2)
		// - Moving containerA into containerC would make itemA at depth 4 - should fail

		// Container A in room with an item inside
		containerA, err := world.NewObjectWithID(ulid.Make(), "Container A with item", world.InLocation(loc1ID))
		require.NoError(t, err)
		containerA.Description = "Has an item inside."
		containerA.IsContainer = true
		containerA.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, containerA)))
		defer func() { _ = delErr(repo.Delete(ctx, containerA.ID, 0)) }()

		itemA, err := world.NewObjectWithID(ulid.Make(), "Item in Container A", world.InContainer(containerA.ID))
		require.NoError(t, err)
		itemA.Description = "Nested item."
		itemA.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, itemA)))
		defer func() { _ = delErr(repo.Delete(ctx, itemA.ID, 0)) }()

		// Container B in room containing Container C
		containerB, err := world.NewObjectWithID(ulid.Make(), "Container B", world.InLocation(loc1ID))
		require.NoError(t, err)
		containerB.Description = "Top level."
		containerB.IsContainer = true
		containerB.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, containerB)))
		defer func() { _ = delErr(repo.Delete(ctx, containerB.ID, 0)) }()

		containerC, err := world.NewObjectWithID(ulid.Make(), "Container C", world.InContainer(containerB.ID))
		require.NoError(t, err)
		containerC.Description = "Inside B."
		containerC.IsContainer = true
		containerC.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, containerC)))
		defer func() { _ = delErr(repo.Delete(ctx, containerC.ID, 0)) }()

		// Moving containerA (which has itemA inside) into containerC would create:
		// B -> C -> A -> itemA (depth 4, exceeds max of 3)
		err = delErr(repo.Move(ctx, containerA.ID, world.Containment{ObjectID: &containerC.ID}, 0))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max nesting depth")
		errutil.AssertErrorCode(t, err, "NESTING_DEPTH_EXCEEDED")
	})

	t.Run("move creates circular containment fails", func(t *testing.T) {
		// Create container A containing container B
		containerA, err := world.NewObjectWithID(ulid.Make(), "Container A", world.InLocation(loc1ID))
		require.NoError(t, err)
		containerA.Description = "First container."
		containerA.IsContainer = true
		containerA.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, containerA)))
		defer func() { _ = delErr(repo.Delete(ctx, containerA.ID, 0)) }()

		containerB, err := world.NewObjectWithID(ulid.Make(), "Container B", world.InContainer(containerA.ID))
		require.NoError(t, err)
		containerB.Description = "Second container inside A."
		containerB.IsContainer = true
		containerB.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, containerB)))
		defer func() { _ = delErr(repo.Delete(ctx, containerB.ID, 0)) }()

		// Try to move A into B - should fail (circular)
		err = delErr(repo.Move(ctx, containerA.ID, world.Containment{ObjectID: &containerB.ID}, 0))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular containment")
		errutil.AssertErrorCode(t, err, "CIRCULAR_CONTAINMENT")
	})

	t.Run("move object into itself fails", func(t *testing.T) {
		container, err := world.NewObjectWithID(ulid.Make(), "Self Container", world.InLocation(loc1ID))
		require.NoError(t, err)
		container.Description = "Cannot contain itself."
		container.IsContainer = true
		container.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, container)))
		defer func() { _ = delErr(repo.Delete(ctx, container.ID, 0)) }()

		// Try to move container into itself - should fail
		err = delErr(repo.Move(ctx, container.ID, world.Containment{ObjectID: &container.ID}, 0))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular containment")
		errutil.AssertErrorCode(t, err, "CIRCULAR_CONTAINMENT")
	})

	t.Run("move non-existent object fails", func(t *testing.T) {
		nonExistentID := ulid.Make()
		err := delErr(repo.Move(ctx, nonExistentID, world.Containment{LocationID: &loc1ID}, 0))
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("move with multiple containment fields fails", func(t *testing.T) {
		// Create a character for the test
		charID := ulid.Make()
		playerID := ulid.Make()
		_, err := testPool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash, created_at, updated_at)
			VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		`, playerID.String(), "player_multi_"+playerID.String())
		require.NoError(t, err)
		_, err = testPool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, created_at)
			VALUES ($1, $2, 'MultiTestChar', $3, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
		`, charID.String(), playerID.String(), loc1ID.String())
		require.NoError(t, err)
		defer func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
		}()

		item, err := world.NewObjectWithID(ulid.Make(), "Multi Containment Item", world.InLocation(loc1ID))
		require.NoError(t, err)
		item.Description = "Item for testing invalid containment."
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() { _ = delErr(repo.Delete(ctx, item.ID, 0)) }()

		// Try to move with both LocationID and CharacterID set - should fail
		err = delErr(repo.Move(ctx, item.ID, world.Containment{
			LocationID:  &loc1ID,
			CharacterID: &charID,
		}, 0))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one")
	})

	t.Run("concurrent move to same container is serialized", func(t *testing.T) {
		// This test verifies that SELECT FOR UPDATE prevents concurrent moves
		// from racing. We start a transaction that locks the container, then
		// verify that another Move() call blocks until the first transaction completes.
		container, err := world.NewObjectWithID(ulid.Make(), "Concurrent Test Container", world.InLocation(loc1ID))
		require.NoError(t, err)
		container.Description = "Container for concurrent test."
		container.IsContainer = true
		container.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, container)))
		defer func() { _ = delErr(repo.Delete(ctx, container.ID, 0)) }()

		item, err := world.NewObjectWithID(ulid.Make(), "Concurrent Test Item", world.InLocation(loc1ID))
		require.NoError(t, err)
		item.Description = "Item for concurrent test."
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() { _ = delErr(repo.Delete(ctx, item.ID, 0)) }()

		// Start a transaction that locks the container
		tx, err := testPool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()

		// Lock the container row
		var isContainer bool
		err = tx.QueryRow(ctx, `SELECT is_container FROM objects WHERE id = $1 FOR UPDATE`, container.ID.String()).Scan(&isContainer)
		require.NoError(t, err)

		// Try to move the item with a short timeout - should block and timeout
		shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		err = delErr(repo.Move(shortCtx, item.ID, world.Containment{ObjectID: &container.ID}, 0))
		// The move should fail due to context deadline because the container is locked
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		// Rollback the blocking transaction
		require.NoError(t, tx.Rollback(ctx))

		// Now the move should succeed
		err = delErr(repo.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID}, 0))
		require.NoError(t, err)

		got, err := repo.Get(ctx, item.ID)
		require.NoError(t, err)
		assert.Equal(t, container.ID, *got.ContainedInObjectID())
	})
}

func TestObjectRepository_CustomMaxNestingDepth(t *testing.T) {
	ctx := context.Background()

	// Create a test location
	locID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Depth Test Location', 'For depth testing', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String())
	}()

	t.Run("depth 5 allows deeper nesting", func(t *testing.T) {
		// Use custom depth of 5
		repo := postgres.NewObjectRepositoryWithDepth(testPool, 5)

		// Create containers nested 4 deep, then add item (total 5) - should succeed
		level1, err := world.NewObjectWithID(ulid.Make(), "Deep1", world.InLocation(locID))
		require.NoError(t, err)
		level1.Description = "Level 1"
		level1.IsContainer = true
		level1.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, level1)))
		defer func() { _ = delErr(repo.Delete(ctx, level1.ID, 0)) }()

		level2, err := world.NewObjectWithID(ulid.Make(), "Deep2", world.InContainer(level1.ID))
		require.NoError(t, err)
		level2.Description = "Level 2"
		level2.IsContainer = true
		level2.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, level2)))
		defer func() { _ = delErr(repo.Delete(ctx, level2.ID, 0)) }()

		level3, err := world.NewObjectWithID(ulid.Make(), "Deep3", world.InContainer(level2.ID))
		require.NoError(t, err)
		level3.Description = "Level 3"
		level3.IsContainer = true
		level3.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, level3)))
		defer func() { _ = delErr(repo.Delete(ctx, level3.ID, 0)) }()

		level4, err := world.NewObjectWithID(ulid.Make(), "Deep4", world.InContainer(level3.ID))
		require.NoError(t, err)
		level4.Description = "Level 4"
		level4.IsContainer = true
		level4.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, level4)))
		defer func() { _ = delErr(repo.Delete(ctx, level4.ID, 0)) }()

		// Move item to level4 (depth 5) - should succeed with custom depth
		item, err := world.NewObjectWithID(ulid.Make(), "Deep Item", world.InLocation(locID))
		require.NoError(t, err)
		item.Description = "At depth 5"
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() { _ = delErr(repo.Delete(ctx, item.ID, 0)) }()

		err = delErr(repo.Move(ctx, item.ID, world.Containment{ObjectID: &level4.ID}, 0))
		require.NoError(t, err) // Should succeed with depth 5

		// Verify the item is at level4
		got, err := repo.Get(ctx, item.ID)
		require.NoError(t, err)
		assert.NotNil(t, got.ContainedInObjectID())
		assert.Equal(t, level4.ID, *got.ContainedInObjectID())
	})

	t.Run("depth 2 allows only shallow nesting", func(t *testing.T) {
		// Use custom depth of 2 (container + one item, no nested containers)
		repo := postgres.NewObjectRepositoryWithDepth(testPool, 2)

		container, err := world.NewObjectWithID(ulid.Make(), "Single Container", world.InLocation(locID))
		require.NoError(t, err)
		container.Description = "Top level only"
		container.IsContainer = true
		container.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, container)))
		defer func() { _ = delErr(repo.Delete(ctx, container.ID, 0)) }()

		item, err := world.NewObjectWithID(ulid.Make(), "Item", world.InLocation(locID))
		require.NoError(t, err)
		item.Description = "Goes in container"
		item.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, item)))
		defer func() { _ = delErr(repo.Delete(ctx, item.ID, 0)) }()

		// Moving item to container (total depth 2) should succeed
		err = delErr(repo.Move(ctx, item.ID, world.Containment{ObjectID: &container.ID}, 0))
		require.NoError(t, err)

		// But adding another layer would fail
		container2, err := world.NewObjectWithID(ulid.Make(), "Nested Container", world.InLocation(locID))
		require.NoError(t, err)
		container2.Description = "Should fail to nest"
		container2.IsContainer = true
		container2.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, container2)))
		defer func() { _ = delErr(repo.Delete(ctx, container2.ID, 0)) }()

		anotherItem, err := world.NewObjectWithID(ulid.Make(), "Nested Item", world.InContainer(container2.ID))
		require.NoError(t, err)
		anotherItem.Description = "In container2"
		anotherItem.CreatedAt = time.Now().UTC()
		require.NoError(t, delErr(repo.Create(ctx, anotherItem)))
		defer func() { _ = delErr(repo.Delete(ctx, anotherItem.ID, 0)) }()

		// Move container2 (with item inside) into container - would create depth 3, should fail
		err = delErr(repo.Move(ctx, container2.ID, world.Containment{ObjectID: &container.ID}, 0))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max nesting depth")
		errutil.AssertErrorCode(t, err, "NESTING_DEPTH_EXCEEDED")
	})
}

// objectDBVersion reads the stored version column directly so a test can assert
// the guard did/did not advance the row.
func objectDBVersion(ctx context.Context, t *testing.T, id ulid.ULID) int {
	t.Helper()
	var v int
	err := testPool.QueryRow(ctx, `SELECT version FROM objects WHERE id = $1`, id.String()).Scan(&v)
	require.NoError(t, err)
	return v
}

// createObjectTestLocation inserts a location for object containment and cleans it up.
func createObjectTestLocation(ctx context.Context, t *testing.T) ulid.ULID {
	t.Helper()
	locID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Obj Guard Loc', 'x', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locID.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String()) })
	return locID
}

// TestObjectRepository_UpdateVersionGuard binds MODEL-03 for object Update.
func TestObjectRepository_UpdateVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)
	locID := createObjectTestLocation(ctx, t)

	newObj := func(t *testing.T, name string) *world.Object {
		t.Helper()
		obj, err := world.NewObjectWithID(ulid.Make(), name, world.InLocation(locID))
		require.NoError(t, err)
		obj.CreatedAt = time.Now().UTC()
		return obj
	}

	t.Run("create populates version 1 and Get reads it back", func(t *testing.T) {
		obj := newObj(t, "guard-create")
		delta, err := repo.Create(ctx, obj)
		require.NoError(t, err)
		assert.Equal(t, 1, obj.Version)
		assert.Equal(t, 1, delta.Primary.AfterVersion)
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, obj.ID, 0)) })

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, got.Version)
	})

	t.Run("successful update increments version by 1 and refreshes struct", func(t *testing.T) {
		obj := newObj(t, "guard-update")
		require.NoError(t, delErr(repo.Create(ctx, obj)))
		require.Equal(t, 1, obj.Version)
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, obj.ID, 0)) })

		obj.Name = "guard-update-v2"
		delta, err := repo.Update(ctx, obj)
		require.NoError(t, err)
		assert.Equal(t, 2, obj.Version)
		assert.Equal(t, 1, delta.Primary.BeforeVersion)
		assert.Equal(t, 2, delta.Primary.AfterVersion)
		assert.Equal(t, 2, objectDBVersion(ctx, t, obj.ID))
	})

	t.Run("stale-version update returns WORLD_CONCURRENT_EDIT and does not overwrite", func(t *testing.T) {
		obj := newObj(t, "guard-stale")
		require.NoError(t, delErr(repo.Create(ctx, obj)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, obj.ID, 0)) })

		_, err := testPool.Exec(ctx, `UPDATE objects SET version = version + 1, name = $2 WHERE id = $1`,
			obj.ID.String(), "winner")
		require.NoError(t, err)

		obj.Name = "loser"
		_, err = repo.Update(ctx, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Equal(t, "winner", got.Name)
	})

	t.Run("update of an absent object returns OBJECT_NOT_FOUND", func(t *testing.T) {
		obj := newObj(t, "ghost")
		obj.Version = 4
		_, err := repo.Update(ctx, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})
}

// TestObjectRepository_MoveVersionGuard binds MODEL-03 for the object containment
// write (Move), which now carries expectedVersion (05-14 enrollment).
func TestObjectRepository_MoveVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)
	loc1 := createObjectTestLocation(ctx, t)
	loc2 := createObjectTestLocation(ctx, t)

	newObj := func(t *testing.T, name string) *world.Object {
		t.Helper()
		obj, err := world.NewObjectWithID(ulid.Make(), name, world.InLocation(loc1))
		require.NoError(t, err)
		obj.CreatedAt = time.Now().UTC()
		return obj
	}

	t.Run("successful move increments version", func(t *testing.T) {
		obj := newObj(t, "move-ok")
		require.NoError(t, delErr(repo.Create(ctx, obj)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, obj.ID, 0)) })

		delta, err := repo.Move(ctx, obj.ID, world.Containment{LocationID: &loc2}, obj.Version)
		require.NoError(t, err)
		assert.Equal(t, 1, delta.Primary.BeforeVersion)
		assert.Equal(t, 2, delta.Primary.AfterVersion)
		assert.Equal(t, 2, objectDBVersion(ctx, t, obj.ID))

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Equal(t, loc2, *got.LocationID())
	})

	t.Run("stale-version move returns WORLD_CONCURRENT_EDIT and does not move", func(t *testing.T) {
		obj := newObj(t, "move-stale")
		require.NoError(t, delErr(repo.Create(ctx, obj)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, obj.ID, 0)) })

		_, err := testPool.Exec(ctx, `UPDATE objects SET version = version + 1 WHERE id = $1`, obj.ID.String())
		require.NoError(t, err)

		_, err = repo.Move(ctx, obj.ID, world.Containment{LocationID: &loc2}, 1) // stale
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Equal(t, loc1, *got.LocationID(), "stale move must not relocate the object")
	})

	t.Run("move of an absent object returns OBJECT_NOT_FOUND", func(t *testing.T) {
		_, err := repo.Move(ctx, ulid.Make(), world.Containment{LocationID: &loc2}, 3)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})
}

// TestObjectRepository_DeleteVersionGuard binds MODEL-03 for object Delete.
func TestObjectRepository_DeleteVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)
	locID := createObjectTestLocation(ctx, t)

	newObj := func(t *testing.T, name string) *world.Object {
		t.Helper()
		obj, err := world.NewObjectWithID(ulid.Make(), name, world.InLocation(locID))
		require.NoError(t, err)
		obj.CreatedAt = time.Now().UTC()
		return obj
	}

	t.Run("stale-version delete returns WORLD_CONCURRENT_EDIT", func(t *testing.T) {
		obj := newObj(t, "del-stale")
		require.NoError(t, delErr(repo.Create(ctx, obj)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, obj.ID, 0)) })

		_, err := testPool.Exec(ctx, `UPDATE objects SET version = version + 1 WHERE id = $1`, obj.ID.String())
		require.NoError(t, err)

		_, err = repo.Delete(ctx, obj.ID, 1) // stale
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
	})

	t.Run("delete of an absent object returns OBJECT_NOT_FOUND", func(t *testing.T) {
		_, err := repo.Delete(ctx, ulid.Make(), 2)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "OBJECT_NOT_FOUND")
	})

	t.Run("version-matched delete succeeds and returns a tombstone delta", func(t *testing.T) {
		obj := newObj(t, "del-ok")
		require.NoError(t, delErr(repo.Create(ctx, obj)))

		delta, err := repo.Delete(ctx, obj.ID, obj.Version)
		require.NoError(t, err)
		assert.True(t, delta.Primary.Tombstone)
		assert.Equal(t, 1, delta.Primary.BeforeVersion)

		_, err = repo.Get(ctx, obj.ID)
		assert.ErrorIs(t, err, world.ErrNotFound)
	})
}
