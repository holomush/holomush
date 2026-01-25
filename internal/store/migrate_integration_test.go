//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/holomush/holomush/internal/store"
)

func TestMigrator_FullCycle(t *testing.T) {
	ctx := context.Background()

	// Start PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2)),
	)
	require.NoError(t, err)
	defer pgContainer.Terminate(ctx)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Test: Create migrator
	migrator, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer migrator.Close()

	// Test: Initial version is 0
	version, dirty, err := migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(0), version)
	assert.False(t, dirty)

	// Test: Apply all migrations
	err = migrator.Up()
	require.NoError(t, err)

	version, dirty, err = migrator.Version()
	require.NoError(t, err)
	assert.Greater(t, version, uint(0), "Up() should apply at least one migration")
	assert.False(t, dirty)
	latestVersion := version // Save for relative assertions below

	// Test: Rollback one
	err = migrator.Steps(-1)
	require.NoError(t, err)

	version, _, err = migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, latestVersion-1, version, "Steps(-1) should rollback one version")

	// Test: Apply one
	err = migrator.Steps(1)
	require.NoError(t, err)

	version, _, err = migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, latestVersion, version, "Steps(1) should restore to latest version")

	// Test: Down() rolls back all migrations
	err = migrator.Down()
	require.NoError(t, err)

	version, dirty, err = migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(0), version, "Down() should rollback to version 0")
	assert.False(t, dirty)

	// Test: Re-apply all for Force() test
	err = migrator.Up()
	require.NoError(t, err)

	// Test: Force() sets version without running migrations
	err = migrator.Force(3)
	require.NoError(t, err)

	version, dirty, err = migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(3), version, "Force() should set version to 3")
	assert.False(t, dirty, "Force() should clear dirty flag")
}

func TestMigrator_DirtyStateRecovery(t *testing.T) {
	ctx := context.Background()

	// Start PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2)),
	)
	require.NoError(t, err)
	defer pgContainer.Terminate(ctx)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Create migrator and apply migrations
	migrator, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer migrator.Close()

	err = migrator.Up()
	require.NoError(t, err)

	// Capture current version before simulating dirty state
	currentVersion, dirty, err := migrator.Version()
	require.NoError(t, err)
	require.False(t, dirty, "database should start clean")
	require.Greater(t, currentVersion, uint(0), "should have at least one migration applied")

	// Simulate dirty state using raw SQL
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "UPDATE schema_migrations SET dirty = true")
	require.NoError(t, err)

	// Verify dirty state is reflected
	version, dirty, err := migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, currentVersion, version)
	assert.True(t, dirty, "database should be dirty after manual update")

	// Verify Up() fails when database is dirty
	// golang-migrate returns ErrDirty when trying to run migrations on a dirty database
	err = migrator.Up()
	require.Error(t, err, "Up() should fail when database is dirty")
	assert.Contains(t, err.Error(), "Dirty", "error should indicate dirty state")

	// Verify Steps() also fails when dirty
	err = migrator.Steps(1)
	require.Error(t, err, "Steps() should fail when database is dirty")
	assert.Contains(t, err.Error(), "Dirty", "error should indicate dirty state")

	// Use Force() to recover from dirty state
	err = migrator.Force(int(currentVersion))
	require.NoError(t, err, "Force() should succeed and clear dirty flag")

	// Verify dirty flag is cleared
	version, dirty, err = migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, currentVersion, version, "version should remain unchanged after Force()")
	assert.False(t, dirty, "dirty flag should be cleared after Force()")

	// Verify migrations can continue - Up() should succeed (or return no change)
	err = migrator.Up()
	require.NoError(t, err, "Up() should succeed after Force() clears dirty state")
}

func TestMigrator_ConcurrentUp(t *testing.T) {
	ctx := context.Background()

	// Start PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2)),
	)
	require.NoError(t, err)
	defer pgContainer.Terminate(ctx)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Create two migrators pointing to the same database
	migrator1, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer migrator1.Close()

	migrator2, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer migrator2.Close()

	// Run migrations concurrently
	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		err1 = migrator1.Up()
	}()
	go func() {
		defer wg.Done()
		err2 = migrator2.Up()
	}()
	wg.Wait()

	// At least one should succeed (or both if sequential lock acquisition)
	// ErrNoChange is acceptable (means migrations already applied)
	// Both succeeding is fine due to lock serialization
	successCount := 0
	if err1 == nil {
		successCount++
	}
	if err2 == nil {
		successCount++
	}
	assert.GreaterOrEqual(t, successCount, 1, "at least one migration should succeed")

	// Verify consistent final state using a fresh migrator
	verifier, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer verifier.Close()

	version, dirty, err := verifier.Version()
	require.NoError(t, err)
	assert.Greater(t, version, uint(0), "migrations should have been applied")
	assert.False(t, dirty, "database should not be in dirty state")

	// Verify both migrators report same version
	v1, dirty1, err := migrator1.Version()
	require.NoError(t, err)
	assert.Equal(t, version, v1, "migrator1 should report same version")
	assert.False(t, dirty1)

	v2, dirty2, err := migrator2.Version()
	require.NoError(t, err)
	assert.Equal(t, version, v2, "migrator2 should report same version")
	assert.False(t, dirty2)
}
