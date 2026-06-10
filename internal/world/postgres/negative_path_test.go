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

// --------------------------------------------------------------------------
// LocationRepository negative-path tests
// --------------------------------------------------------------------------

// TestLocationRepository_CreateWithNonExistentOwner verifies that creating a
// location whose owner_id references a non-existent character fails with a
// foreign-key violation.
func TestLocationRepository_CreateWithNonExistentOwner(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	ghost := ulid.Make() // no character with this ID exists
	loc := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypeScene,
		Name:         "Ghost Owner Scene",
		Description:  "References a non-existent owner.",
		OwnerID:      &ghost,
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC(),
	}

	err := repo.Create(ctx, loc)
	require.Error(t, err, "creating a location with a non-existent owner_id must fail")
}

// TestLocationRepository_CreateWithNonExistentShadowsID verifies that creating
// a location whose shadows_id references a non-existent location fails with a
// foreign-key violation.
func TestLocationRepository_CreateWithNonExistentShadowsID(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	ghost := ulid.Make() // no location with this ID exists
	loc := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypeScene,
		Name:         "Ghost Shadow Scene",
		Description:  "References a non-existent shadows_id.",
		ShadowsID:    &ghost,
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC(),
	}

	err := repo.Create(ctx, loc)
	require.Error(t, err, "creating a location with a non-existent shadows_id must fail")
}

// TestLocationRepository_ConcurrentUpdateConflict verifies that a row-level
// lock blocks a concurrent Update until the lock is released.
func TestLocationRepository_ConcurrentUpdateConflict(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	loc := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypePersistent,
		Name:         "Concurrent Lock Test",
		Description:  "Location for concurrent-update test.",
		ReplayPolicy: "last:0",
		CreatedAt:    time.Now().UTC(),
	}
	require.NoError(t, repo.Create(ctx, loc))
	t.Cleanup(func() { _ = repo.Delete(ctx, loc.ID) })

	// Lock the row in an open transaction.
	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `SELECT id FROM locations WHERE id = $1 FOR UPDATE`, loc.ID.String())
	require.NoError(t, err)

	// A concurrent update with a short deadline must block and time out.
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	loc.Name = "Should Not Land"
	err = repo.Update(shortCtx, loc)
	require.Error(t, err, "update should time out while the row is locked")
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Release the lock; the update must now succeed.
	require.NoError(t, tx.Rollback(ctx))

	loc.Name = "Updated After Lock"
	require.NoError(t, repo.Update(ctx, loc))

	got, err := repo.Get(ctx, loc.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated After Lock", got.Name)
}

// --------------------------------------------------------------------------
// ObjectRepository negative-path tests
// --------------------------------------------------------------------------

// TestObjectRepository_CreateWithNonExistentLocation verifies that creating an
// object whose location_id references a non-existent location fails with a
// foreign-key violation wrapped as OBJECT_NOT_FOUND or a generic error —
// the important invariant is that the operation fails, not silently succeeds.
func TestObjectRepository_CreateWithNonExistentLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	ghostLoc := ulid.Make()
	obj, err := world.NewObjectWithID(ulid.Make(), "Ghost Location Object", world.InLocation(ghostLoc))
	require.NoError(t, err)
	obj.Description = "References a non-existent location."
	obj.CreatedAt = time.Now().UTC()

	err = repo.Create(ctx, obj)
	require.Error(t, err, "creating an object with a non-existent location_id must fail")
}

// TestObjectRepository_CreateWithNonExistentOwner verifies that setting
// owner_id to a non-existent character ID fails with a FK violation.
func TestObjectRepository_CreateWithNonExistentOwner(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	// We need a valid location for the object's containment.
	locID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'FK Owner Test Loc', 'temp', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locID.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String()) })

	ghostOwner := ulid.Make()
	obj, err := world.NewObjectWithID(ulid.Make(), "Ghost Owner Object", world.InLocation(locID))
	require.NoError(t, err)
	obj.Description = "Has a non-existent owner."
	obj.OwnerID = &ghostOwner
	obj.CreatedAt = time.Now().UTC()

	err = repo.Create(ctx, obj)
	require.Error(t, err, "creating an object with a non-existent owner_id must fail")
}

// TestObjectRepository_ConcurrentUpdateConflict verifies that a row lock held
// by an open transaction blocks a concurrent Update until released.
func TestObjectRepository_ConcurrentUpdateConflict(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewObjectRepository(testPool)

	locID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Obj Concurrent Lock Loc', 'temp', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locID.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String()) })

	obj, err := world.NewObjectWithID(ulid.Make(), "Concurrent Lock Object", world.InLocation(locID))
	require.NoError(t, err)
	obj.Description = "For concurrent update test."
	obj.CreatedAt = time.Now().UTC()
	require.NoError(t, repo.Create(ctx, obj))
	t.Cleanup(func() { _ = repo.Delete(ctx, obj.ID) })

	// Lock the row in an open transaction.
	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `SELECT id FROM objects WHERE id = $1 FOR UPDATE`, obj.ID.String())
	require.NoError(t, err)

	// Concurrent update with short deadline must block and time out.
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	obj.Name = "Should Not Land"
	err = repo.Update(shortCtx, obj)
	require.Error(t, err, "update should time out while the row is locked")
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Release the lock; the update must now succeed.
	require.NoError(t, tx.Rollback(ctx))

	obj.Name = "Updated After Lock"
	require.NoError(t, repo.Update(ctx, obj))

	got, err := repo.Get(ctx, obj.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated After Lock", got.Name)
}

// --------------------------------------------------------------------------
// CharacterRepository negative-path tests
// --------------------------------------------------------------------------

// TestCharacterRepository_CreateWithNonExistentPlayer verifies that creating a
// character whose player_id references a non-existent player fails with
// CHARACTER_CREATE_FAILED (FK violation).
func TestCharacterRepository_CreateWithNonExistentPlayer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	locationID := createTestLocation(ctx, t)

	char := &world.Character{
		ID:          ulid.Make(),
		PlayerID:    ulid.Make(), // no player with this ID exists
		Name:        "FKViolationHero",
		Description: "Orphaned character.",
		LocationID:  &locationID,
		CreatedAt:   time.Now().UTC(),
	}

	err := repo.Create(ctx, char)
	require.Error(t, err, "creating a character for a non-existent player must fail")
	errutil.AssertErrorCode(t, err, "CHARACTER_CREATE_FAILED")
}

// TestCharacterRepository_CreateWithNonExistentLocation verifies that creating a
// character whose location_id references a non-existent location fails with
// CHARACTER_CREATE_FAILED (FK violation).
func TestCharacterRepository_CreateWithNonExistentLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewCharacterRepository(testPool)
	playerID := createTestPlayer(ctx, t)

	ghostLoc := ulid.Make()
	char := &world.Character{
		ID:          ulid.Make(),
		PlayerID:    playerID,
		Name:        "FKLocationHero",
		Description: "Character with non-existent location.",
		LocationID:  &ghostLoc,
		CreatedAt:   time.Now().UTC(),
	}

	err := repo.Create(ctx, char)
	require.Error(t, err, "creating a character with a non-existent location_id must fail")
	errutil.AssertErrorCode(t, err, "CHARACTER_CREATE_FAILED")
}

// --------------------------------------------------------------------------
// ExitRepository negative-path tests
// --------------------------------------------------------------------------

// TestExitRepository_CreateWithNonExistentFromLocation verifies that creating
// an exit whose from_location_id references a non-existent location fails
// with a FK violation.
func TestExitRepository_CreateWithNonExistentFromLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)

	toLocID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Exit FK To Loc', 'target', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, toLocID.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, toLocID.String()) })

	ghostFrom := ulid.Make()
	exit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: ghostFrom, // does not exist
		ToLocationID:   toLocID,
		Name:           "north",
	}

	err = repo.Create(ctx, exit)
	require.Error(t, err, "creating an exit from a non-existent location must fail")
}

// TestExitRepository_UniqueNamePerLocation verifies that inserting two exits
// with the same (from_location_id, name) pair violates the unique constraint.
func TestExitRepository_UniqueNamePerLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)

	fromLocID := ulid.Make()
	toLocID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Exit Unique From', 'src', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT),
		       ($2, 'Exit Unique To',   'dst', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, fromLocID.String(), toLocID.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM exits WHERE from_location_id = $1`, fromLocID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, fromLocID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, toLocID.String())
	})

	exit1 := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: fromLocID,
		ToLocationID:   toLocID,
		Name:           "north",
	}
	require.NoError(t, repo.Create(ctx, exit1))

	// Second exit with the same from_location_id + name must fail.
	exit2 := &world.Exit{
		ID:             ulid.Make(), // distinct ID
		FromLocationID: fromLocID,
		ToLocationID:   toLocID,
		Name:           "north", // same direction — unique constraint violation
	}
	err = repo.Create(ctx, exit2)
	require.Error(t, err, "inserting a duplicate (from_location_id, name) exit must fail")
}
