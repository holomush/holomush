// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/postgres"
)

func createCascadeTestLocation(ctx context.Context, t *testing.T) ulid.ULID {
	t.Helper()
	locID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Cascade Test Loc', 'Test', 'persistent', 'last:0', NOW())
	`, locID.String())
	require.NoError(t, err)
	return locID
}

func createCascadeTestObject(ctx context.Context, t *testing.T, locationID ulid.ULID) ulid.ULID {
	t.Helper()
	objID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO objects (id, location_id, name, description, created_at)
		VALUES ($1, $2, 'Cascade Test Object', 'A test object', NOW())
	`, objID.String(), locationID.String())
	require.NoError(t, err)
	return objID
}

func createCascadeTestCharacter(ctx context.Context, t *testing.T, locationID ulid.ULID) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at)
		VALUES ($1, $2, 'testhash', NOW())
	`, playerID.String(), "cascade_player_"+playerID.String())
	require.NoError(t, err)

	charID := ulid.Make()
	_, err = testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id, created_at)
		VALUES ($1, $2, 'Cascade Test Char', $3, NOW())
	`, charID.String(), playerID.String(), locationID.String())
	require.NoError(t, err)
	return charID
}

func createCascadeTestProperty(ctx context.Context, t *testing.T, parentType string, parentID ulid.ULID) ulid.ULID {
	t.Helper()
	propID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO entity_properties (id, parent_type, parent_id, name, value, owner, visibility, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'public', NOW(), NOW())
	`, propID.String(), parentType, parentID.String(), "test_prop", "test_value", "system")
	require.NoError(t, err)
	return propID
}

func countPropertiesForParent(ctx context.Context, t *testing.T, parentType string, parentID ulid.ULID) int {
	t.Helper()
	var count int
	err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM entity_properties WHERE parent_type = $1 AND parent_id = $2
	`, parentType, parentID.String()).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestCascadeDelete_Location_DeletesPropertiesInSameTransaction(t *testing.T) {
	ctx := context.Background()

	locID := createCascadeTestLocation(ctx, t)
	propID := createCascadeTestProperty(ctx, t, "location", locID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM entity_properties WHERE id = $1`, propID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String())
	})

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "location", locID), "property should exist before delete")

	locRepo := postgres.NewLocationRepository(testPool)
	propRepo := postgres.NewPropertyRepository(testPool)
	tx := postgres.NewTransactor(testPool)

	deleteFn := func(txCtx context.Context) error {
		if err := propRepo.DeleteByParent(txCtx, "location", locID); err != nil {
			return err
		}
		return locRepo.Delete(txCtx, locID)
	}

	err := tx.InTransaction(ctx, deleteFn)
	require.NoError(t, err)

	assert.Equal(t, 0, countPropertiesForParent(ctx, t, "location", locID), "properties should be deleted")

	var exists bool
	err = testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM locations WHERE id = $1)`, locID.String()).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "location should be deleted")
}

func TestCascadeDelete_Location_RollsBackPropertiesOnParentDeleteFail(t *testing.T) {
	ctx := context.Background()

	locID := createCascadeTestLocation(ctx, t)
	propID := createCascadeTestProperty(ctx, t, "location", locID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM entity_properties WHERE id = $1`, propID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String())
	})

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "location", locID), "property should exist before delete")

	locRepo := postgres.NewLocationRepository(testPool)
	propRepo := postgres.NewPropertyRepository(testPool)
	tx := postgres.NewTransactor(testPool)

	deleteFn := func(txCtx context.Context) error {
		if err := propRepo.DeleteByParent(txCtx, "location", locID); err != nil {
			return err
		}
		_ = locRepo.Delete(txCtx, locID)
		return errors.New("simulated failure after property delete")
	}

	err := tx.InTransaction(ctx, deleteFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated failure")

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "location", locID), "properties should remain after rollback")

	var exists bool
	err = testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM locations WHERE id = $1)`, locID.String()).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "location should remain after rollback")
}

func TestCascadeDelete_Object_DeletesPropertiesInSameTransaction(t *testing.T) {
	ctx := context.Background()

	locID := createCascadeTestLocation(ctx, t)
	objID := createCascadeTestObject(ctx, t, locID)
	propID := createCascadeTestProperty(ctx, t, "object", objID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM entity_properties WHERE id = $1`, propID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM objects WHERE id = $1`, objID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String())
	})

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "object", objID), "property should exist before delete")

	objRepo := postgres.NewObjectRepository(testPool)
	propRepo := postgres.NewPropertyRepository(testPool)
	tx := postgres.NewTransactor(testPool)

	deleteFn := func(txCtx context.Context) error {
		if err := propRepo.DeleteByParent(txCtx, "object", objID); err != nil {
			return err
		}
		return objRepo.Delete(txCtx, objID)
	}

	err := tx.InTransaction(ctx, deleteFn)
	require.NoError(t, err)

	assert.Equal(t, 0, countPropertiesForParent(ctx, t, "object", objID), "properties should be deleted")

	var exists bool
	err = testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM objects WHERE id = $1)`, objID.String()).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "object should be deleted")
}

func TestCascadeDelete_Object_RollsBackPropertiesOnParentDeleteFail(t *testing.T) {
	ctx := context.Background()

	locID := createCascadeTestLocation(ctx, t)
	objID := createCascadeTestObject(ctx, t, locID)
	propID := createCascadeTestProperty(ctx, t, "object", objID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM entity_properties WHERE id = $1`, propID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM objects WHERE id = $1`, objID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String())
	})

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "object", objID), "property should exist before delete")

	objRepo := postgres.NewObjectRepository(testPool)
	propRepo := postgres.NewPropertyRepository(testPool)
	tx := postgres.NewTransactor(testPool)

	deleteFn := func(txCtx context.Context) error {
		if err := propRepo.DeleteByParent(txCtx, "object", objID); err != nil {
			return err
		}
		_ = objRepo.Delete(txCtx, objID)
		return errors.New("simulated failure after property delete")
	}

	err := tx.InTransaction(ctx, deleteFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated failure")

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "object", objID), "properties should remain after rollback")

	var exists bool
	err = testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM objects WHERE id = $1)`, objID.String()).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "object should remain after rollback")
}

func TestCascadeDelete_Character_DeletesPropertiesInSameTransaction(t *testing.T) {
	ctx := context.Background()

	locID := createCascadeTestLocation(ctx, t)
	charID := createCascadeTestCharacter(ctx, t, locID)
	propID := createCascadeTestProperty(ctx, t, "character", charID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM entity_properties WHERE id = $1`, propID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String())
	})

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "character", charID), "property should exist before delete")

	charRepo := postgres.NewCharacterRepository(testPool)
	propRepo := postgres.NewPropertyRepository(testPool)
	tx := postgres.NewTransactor(testPool)

	deleteFn := func(txCtx context.Context) error {
		if err := propRepo.DeleteByParent(txCtx, "character", charID); err != nil {
			return err
		}
		return charRepo.Delete(txCtx, charID)
	}

	err := tx.InTransaction(ctx, deleteFn)
	require.NoError(t, err)

	assert.Equal(t, 0, countPropertiesForParent(ctx, t, "character", charID), "properties should be deleted")

	var exists bool
	err = testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM characters WHERE id = $1)`, charID.String()).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "character should be deleted")
}

func TestCascadeDelete_Character_RollsBackPropertiesOnParentDeleteFail(t *testing.T) {
	ctx := context.Background()

	locID := createCascadeTestLocation(ctx, t)
	charID := createCascadeTestCharacter(ctx, t, locID)
	propID := createCascadeTestProperty(ctx, t, "character", charID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM entity_properties WHERE id = $1`, propID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String())
	})

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "character", charID), "property should exist before delete")

	charRepo := postgres.NewCharacterRepository(testPool)
	propRepo := postgres.NewPropertyRepository(testPool)
	tx := postgres.NewTransactor(testPool)

	deleteFn := func(txCtx context.Context) error {
		if err := propRepo.DeleteByParent(txCtx, "character", charID); err != nil {
			return err
		}
		_ = charRepo.Delete(txCtx, charID)
		return errors.New("simulated failure after property delete")
	}

	err := tx.InTransaction(ctx, deleteFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated failure")

	assert.Equal(t, 1, countPropertiesForParent(ctx, t, "character", charID), "properties should remain after rollback")

	var exists bool
	err = testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM characters WHERE id = $1)`, charID.String()).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "character should remain after rollback")
}
