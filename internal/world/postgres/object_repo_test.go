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

func TestObjectRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location for object containment
	locationID := ulid.Make()
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
			ID:          ulid.Make(),
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

	t.Run("create with invalid containment - no location", func(t *testing.T) {
		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Invalid Object",
			Description: "Has no containment set.",
			// No LocationID, HeldByCharacterID, or ContainedInObjectID set
			CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, obj)
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})

	t.Run("update", func(t *testing.T) {
		obj := &world.Object{
			ID:          ulid.Make(),
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

	t.Run("update with owner_id", func(t *testing.T) {
		// Create a character to be the owner
		charID := ulid.Make()
		playerID := ulid.Make()
		_, err := testPool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash, created_at)
			VALUES ($1, $2, 'testhash', NOW())
		`, playerID.String(), "player_update_"+playerID.String())
		require.NoError(t, err)
		_, err = testPool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, created_at)
			VALUES ($1, $2, 'UpdateOwnerChar', $3, NOW())
		`, charID.String(), playerID.String(), locationID.String())
		require.NoError(t, err)

		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Object For Owner Test",
			Description: "An object.",
			LocationID:  &locationID,
			IsContainer: false,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err = repo.Create(ctx, obj)
		require.NoError(t, err)

		// Update to add owner
		obj.OwnerID = &charID
		err = repo.Update(ctx, obj)
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		require.NotNil(t, got.OwnerID)
		assert.Equal(t, charID, *got.OwnerID)

		// Cleanup
		_ = repo.Delete(ctx, obj.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	t.Run("update change containment to held_by", func(t *testing.T) {
		// Create a character to hold the object
		charID := ulid.Make()
		playerID := ulid.Make()
		_, err := testPool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash, created_at)
			VALUES ($1, $2, 'testhash', NOW())
		`, playerID.String(), "player_held_"+playerID.String())
		require.NoError(t, err)
		_, err = testPool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, created_at)
			VALUES ($1, $2, 'HolderChar', $3, NOW())
		`, charID.String(), playerID.String(), locationID.String())
		require.NoError(t, err)

		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Object To Hold",
			Description: "An object.",
			LocationID:  &locationID,
			IsContainer: false,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err = repo.Create(ctx, obj)
		require.NoError(t, err)

		// Update to be held by character (move from location to inventory)
		obj.LocationID = nil
		obj.HeldByCharacterID = &charID
		err = repo.Update(ctx, obj)
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID)
		require.NotNil(t, got.HeldByCharacterID)
		assert.Equal(t, charID, *got.HeldByCharacterID)

		// Cleanup
		_ = repo.Delete(ctx, obj.ID)
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	t.Run("update change containment to container", func(t *testing.T) {
		// Create a container object
		container := &world.Object{
			ID:          ulid.Make(),
			Name:        "Container",
			Description: "A container.",
			LocationID:  &locationID,
			IsContainer: true,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		err := repo.Create(ctx, container)
		require.NoError(t, err)

		// Create an object to put in the container
		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Object To Contain",
			Description: "An object.",
			LocationID:  &locationID,
			IsContainer: false,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		err = repo.Create(ctx, obj)
		require.NoError(t, err)

		// Update to be contained in container
		obj.LocationID = nil
		obj.ContainedInObjectID = &container.ID
		err = repo.Update(ctx, obj)
		require.NoError(t, err)

		got, err := repo.Get(ctx, obj.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID)
		require.NotNil(t, got.ContainedInObjectID)
		assert.Equal(t, container.ID, *got.ContainedInObjectID)

		// Cleanup
		_ = repo.Delete(ctx, obj.ID)
		_ = repo.Delete(ctx, container.ID)
	})

	t.Run("delete", func(t *testing.T) {
		obj := &world.Object{
			ID:          ulid.Make(),
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
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("update not found", func(t *testing.T) {
		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Nonexistent",
			Description: "Does not exist.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		err := repo.Update(ctx, obj)
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("delete not found", func(t *testing.T) {
		err := repo.Delete(ctx, ulid.Make())
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("update with invalid containment - no location", func(t *testing.T) {
		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Valid Object",
			Description: "Initially valid.",
			LocationID:  &locationID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, obj)
		require.NoError(t, err)

		// Invalidate containment by clearing all locations
		obj.LocationID = nil
		obj.HeldByCharacterID = nil
		obj.ContainedInObjectID = nil
		err = repo.Update(ctx, obj)
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)

		// Cleanup
		_ = repo.Delete(ctx, obj.ID)
	})
}

func TestObjectRepository_ListAtLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create a test location
	locationID := ulid.Make()
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
		ID:          ulid.Make(),
		Name:        "Object 1",
		Description: "First object.",
		LocationID:  &locationID,
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
	obj2 := &world.Object{
		ID:          ulid.Make(),
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

func TestObjectRepository_ListAtLocation_Empty(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// Create an empty location
	emptyLocationID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Empty Location', 'No objects here', 'persistent', 'last:0', NOW())
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
		VALUES ($1, 'Char Location', 'Location for character', 'persistent', 'last:0', NOW())
	`, locationID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
	}()

	// Create a test player first with unique username
	playerID := ulid.Make()
	_, err = testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at)
		VALUES ($1, $2, 'testhash', NOW())
	`, playerID.String(), "player_"+playerID.String())
	require.NoError(t, err)
	defer func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	}()

	// Create a test character
	characterID := ulid.Make()
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
		ID:                ulid.Make(),
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
	locationID := ulid.Make()
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
		ID:          ulid.Make(),
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
		ID:                  ulid.Make(),
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
	loc1ID := ulid.Make()
	loc2ID := ulid.Make()
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
			ID:          ulid.Make(),
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
			ID:          ulid.Make(),
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
			ID:          ulid.Make(),
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
			ID:          ulid.Make(),
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
			ID:          ulid.Make(),
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
			ID:          ulid.Make(),
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
			ID:          ulid.Make(),
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
		nonExistentID := ulid.Make()
		err := repo.Move(ctx, item.ID, world.Containment{ObjectID: &nonExistentID})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "container object not found")
	})

	t.Run("move to character", func(t *testing.T) {
		// Create a test player first
		playerID := ulid.Make()
		_, err := testPool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash, created_at)
			VALUES ($1, $2, 'testhash', NOW())
		`, playerID.String(), "player_move_"+playerID.String())
		require.NoError(t, err)
		defer func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
		}()

		// Create a test character
		characterID := ulid.Make()
		_, err = testPool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, created_at)
			VALUES ($1, $2, 'Move Test Character', $3, NOW())
		`, characterID.String(), playerID.String(), loc1ID.String())
		require.NoError(t, err)
		defer func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, characterID.String())
		}()

		item := &world.Object{
			ID:          ulid.Make(),
			Name:        "Portable Item",
			Description: "Can be picked up.",
			LocationID:  &loc1ID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, item))
		defer func() {
			_ = repo.Delete(ctx, item.ID)
		}()

		// Move to character
		err = repo.Move(ctx, item.ID, world.Containment{CharacterID: &characterID})
		require.NoError(t, err)

		got, err := repo.Get(ctx, item.ID)
		require.NoError(t, err)
		assert.Nil(t, got.LocationID)
		assert.NotNil(t, got.HeldByCharacterID)
		assert.Equal(t, characterID, *got.HeldByCharacterID)
		assert.Nil(t, got.ContainedInObjectID)
	})

	t.Run("move exceeds max nesting depth fails", func(t *testing.T) {
		// Create containers nested 3 deep (at max depth)
		// level1 -> level2 -> level3 -> item (should fail to add level4)

		level1 := &world.Object{
			ID:          ulid.Make(),
			Name:        "Level1 Container",
			Description: "Top level container.",
			LocationID:  &loc1ID,
			IsContainer: true,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, level1))
		defer func() { _ = repo.Delete(ctx, level1.ID) }()

		level2 := &world.Object{
			ID:                  ulid.Make(),
			Name:                "Level2 Container",
			Description:         "Second level container.",
			ContainedInObjectID: &level1.ID,
			IsContainer:         true,
			CreatedAt:           time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, level2))
		defer func() { _ = repo.Delete(ctx, level2.ID) }()

		level3 := &world.Object{
			ID:                  ulid.Make(),
			Name:                "Level3 Container",
			Description:         "Third level container.",
			ContainedInObjectID: &level2.ID,
			IsContainer:         true,
			CreatedAt:           time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, level3))
		defer func() { _ = repo.Delete(ctx, level3.ID) }()

		// Try to add an item at level 4 - should fail (exceeds max depth of 3)
		item := &world.Object{
			ID:          ulid.Make(),
			Name:        "Deep Item",
			Description: "Too deep.",
			LocationID:  &loc1ID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, item))
		defer func() { _ = repo.Delete(ctx, item.ID) }()

		err := repo.Move(ctx, item.ID, world.Containment{ObjectID: &level3.ID})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max nesting depth")
	})

	t.Run("move container with nested items exceeds depth fails", func(t *testing.T) {
		// This tests the scenario where we move a container that already has items
		// into another container, which could exceed max depth.
		// Max depth is 3, so:
		// - Create containerA (in room) with itemA inside (depth 2)
		// - Create containerB (in room) with containerC inside (depth 2)
		// - Moving containerA into containerC would make itemA at depth 4 - should fail

		// Container A in room with an item inside
		containerA := &world.Object{
			ID:          ulid.Make(),
			Name:        "Container A with item",
			Description: "Has an item inside.",
			LocationID:  &loc1ID,
			IsContainer: true,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, containerA))
		defer func() { _ = repo.Delete(ctx, containerA.ID) }()

		itemA := &world.Object{
			ID:                  ulid.Make(),
			Name:                "Item in Container A",
			Description:         "Nested item.",
			ContainedInObjectID: &containerA.ID,
			CreatedAt:           time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, itemA))
		defer func() { _ = repo.Delete(ctx, itemA.ID) }()

		// Container B in room containing Container C
		containerB := &world.Object{
			ID:          ulid.Make(),
			Name:        "Container B",
			Description: "Top level.",
			LocationID:  &loc1ID,
			IsContainer: true,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, containerB))
		defer func() { _ = repo.Delete(ctx, containerB.ID) }()

		containerC := &world.Object{
			ID:                  ulid.Make(),
			Name:                "Container C",
			Description:         "Inside B.",
			ContainedInObjectID: &containerB.ID,
			IsContainer:         true,
			CreatedAt:           time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, containerC))
		defer func() { _ = repo.Delete(ctx, containerC.ID) }()

		// Moving containerA (which has itemA inside) into containerC would create:
		// B -> C -> A -> itemA (depth 4, exceeds max of 3)
		err := repo.Move(ctx, containerA.ID, world.Containment{ObjectID: &containerC.ID})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max nesting depth")
	})

	t.Run("move creates circular containment fails", func(t *testing.T) {
		// Create container A containing container B
		containerA := &world.Object{
			ID:          ulid.Make(),
			Name:        "Container A",
			Description: "First container.",
			LocationID:  &loc1ID,
			IsContainer: true,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, containerA))
		defer func() { _ = repo.Delete(ctx, containerA.ID) }()

		containerB := &world.Object{
			ID:                  ulid.Make(),
			Name:                "Container B",
			Description:         "Second container inside A.",
			ContainedInObjectID: &containerA.ID,
			IsContainer:         true,
			CreatedAt:           time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, containerB))
		defer func() { _ = repo.Delete(ctx, containerB.ID) }()

		// Try to move A into B - should fail (circular)
		err := repo.Move(ctx, containerA.ID, world.Containment{ObjectID: &containerB.ID})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular containment")
	})

	t.Run("move object into itself fails", func(t *testing.T) {
		container := &world.Object{
			ID:          ulid.Make(),
			Name:        "Self Container",
			Description: "Cannot contain itself.",
			LocationID:  &loc1ID,
			IsContainer: true,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, container))
		defer func() { _ = repo.Delete(ctx, container.ID) }()

		// Try to move container into itself - should fail
		err := repo.Move(ctx, container.ID, world.Containment{ObjectID: &container.ID})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular containment")
	})

	t.Run("move non-existent object fails", func(t *testing.T) {
		nonExistentID := ulid.Make()
		err := repo.Move(ctx, nonExistentID, world.Containment{LocationID: &loc1ID})
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("move with multiple containment fields fails", func(t *testing.T) {
		// Create a character for the test
		charID := ulid.Make()
		playerID := ulid.Make()
		_, err := testPool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash, created_at)
			VALUES ($1, $2, 'testhash', NOW())
		`, playerID.String(), "player_multi_"+playerID.String())
		require.NoError(t, err)
		_, err = testPool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id, created_at)
			VALUES ($1, $2, 'MultiTestChar', $3, NOW())
		`, charID.String(), playerID.String(), loc1ID.String())
		require.NoError(t, err)
		defer func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
		}()

		item := &world.Object{
			ID:          ulid.Make(),
			Name:        "Multi Containment Item",
			Description: "Item for testing invalid containment.",
			LocationID:  &loc1ID,
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
		require.NoError(t, repo.Create(ctx, item))
		defer func() { _ = repo.Delete(ctx, item.ID) }()

		// Try to move with both LocationID and CharacterID set - should fail
		err = repo.Move(ctx, item.ID, world.Containment{
			LocationID:  &loc1ID,
			CharacterID: &charID,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one")
	})
}
