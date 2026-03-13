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
		"postgres:18-alpine",
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
		"postgres:18-alpine",
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
		"postgres:18-alpine",
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

// TestMigrator_Force_VersionExceedsAvailable documents the behavior when
// Force() is called with a version higher than available migrations.
//
// While Force() accepts any version value, the consequences are:
// - Version() returns the forced version (999)
// - Up() returns an error (migration file not found)
// - PendingMigrations() returns empty (thinks we're past all migrations)
//
// This is dangerous because PendingMigrations() indicates nothing is pending,
// but Up() will fail. The only recovery is to Force() back to a valid version.
//
// Force() should only be used for recovery from dirty state, never to
// artificially advance the version.
func TestMigrator_Force_VersionExceedsAvailable(t *testing.T) {
	ctx := context.Background()

	// Start PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:18-alpine",
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

	// Create migrator
	migrator, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer migrator.Close()

	// Apply all available migrations first
	err = migrator.Up()
	require.NoError(t, err)

	// Verify we're at the latest version
	latestVersion, dirty, err := migrator.Version()
	require.NoError(t, err)
	require.False(t, dirty)
	require.Greater(t, latestVersion, uint(0), "should have at least one migration")

	// DANGEROUS: Force version to a value higher than any available migration
	// This should NOT be done in production - only for documenting behavior
	err = migrator.Force(999)
	require.NoError(t, err, "Force() accepts any version, even non-existent ones")

	// Verify version is now 999
	version, dirty, err := migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(999), version, "Force() sets version to arbitrary value")
	assert.False(t, dirty, "Force() clears dirty flag")

	// Up() returns an error because there's no migration file for version 999
	// golang-migrate tries to read the current version's down migration and fails
	err = migrator.Up()
	require.Error(t, err, "Up() should fail when forced to non-existent version")
	assert.Contains(t, err.Error(), "no migration found for version 999",
		"error message should indicate missing migration")

	// PendingMigrations() returns empty because version 999 > all migrations
	// This is misleading since Up() will actually fail
	pending, err := migrator.PendingMigrations()
	require.NoError(t, err)
	assert.Empty(t, pending, "PendingMigrations() returns empty when version exceeds all migrations")

	// Version remains 999
	version, _, err = migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(999), version, "version unchanged after failed Up()")
}

// TestMigrator_ConcurrentMigrationDirtyStateHandling verifies that dirty state
// detection works correctly when multiple migrators access the same database
// and one experiences a partial failure (simulated via dirty flag).
//
// This test simulates the scenario where:
// 1. Two migrators point to the same database
// 2. A migration fails partway through (leaving dirty state)
// 3. Both migrators detect the dirty state
// 4. Force() can recover the database to a clean state
func TestMigrator_ConcurrentMigrationDirtyStateHandling(t *testing.T) {
	ctx := context.Background()

	// Start PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:18-alpine",
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

	// Apply migrations first to establish baseline
	migrator1, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer migrator1.Close()

	err = migrator1.Up()
	require.NoError(t, err)

	baseVersion, dirty, err := migrator1.Version()
	require.NoError(t, err)
	require.False(t, dirty, "database should start clean")
	require.Greater(t, baseVersion, uint(0), "should have at least one migration applied")

	// Roll back to version 0 so we can simulate concurrent migration attempts
	err = migrator1.Down()
	require.NoError(t, err)

	version, dirty, err := migrator1.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(0), version, "should be at version 0 after Down()")
	assert.False(t, dirty)

	// Now simulate a concurrent scenario where one migrator sets dirty state
	// mid-execution (as would happen if a migration fails partway through).
	// We'll:
	// 1. Start migrator2
	// 2. Manually set dirty state (simulating mid-migration crash)
	// 3. Verify both migrators detect the dirty state
	// 4. Verify Force() recovery works

	migrator2, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer migrator2.Close()

	// First, apply one migration step to create the schema_migrations table
	// and establish a version for dirty state simulation
	err = migrator1.Steps(1)
	require.NoError(t, err)

	currentVersion, dirty, err := migrator1.Version()
	require.NoError(t, err)
	require.False(t, dirty)
	require.Greater(t, currentVersion, uint(0))

	// Simulate a partial migration failure by setting dirty flag directly.
	// This simulates what happens when a migration crashes mid-execution:
	// the database is left in a dirty state at the current version.
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "UPDATE schema_migrations SET dirty = true")
	require.NoError(t, err)

	// Test: Both migrators detect the dirty state
	v1, dirty1, err := migrator1.Version()
	require.NoError(t, err)
	assert.Equal(t, currentVersion, v1, "migrator1 should report correct version")
	assert.True(t, dirty1, "migrator1 should detect dirty state")

	v2, dirty2, err := migrator2.Version()
	require.NoError(t, err)
	assert.Equal(t, currentVersion, v2, "migrator2 should report correct version")
	assert.True(t, dirty2, "migrator2 should detect dirty state")

	// Test: Up() fails for both migrators due to dirty state
	err = migrator1.Up()
	require.Error(t, err, "migrator1.Up() should fail when database is dirty")
	assert.Contains(t, err.Error(), "Dirty", "error should indicate dirty state")

	err = migrator2.Up()
	require.Error(t, err, "migrator2.Up() should fail when database is dirty")
	assert.Contains(t, err.Error(), "Dirty", "error should indicate dirty state")

	// Test: Steps() fails for both migrators due to dirty state
	err = migrator1.Steps(1)
	require.Error(t, err, "migrator1.Steps() should fail when database is dirty")
	assert.Contains(t, err.Error(), "Dirty", "error should indicate dirty state")

	err = migrator2.Steps(1)
	require.Error(t, err, "migrator2.Steps() should fail when database is dirty")
	assert.Contains(t, err.Error(), "Dirty", "error should indicate dirty state")

	// Test: Force() can recover from dirty state
	// Using migrator1 to force the version clears dirty flag
	err = migrator1.Force(int(currentVersion))
	require.NoError(t, err, "migrator1.Force() should succeed")

	// Test: Both migrators now see clean state
	v1, dirty1, err = migrator1.Version()
	require.NoError(t, err)
	assert.Equal(t, currentVersion, v1)
	assert.False(t, dirty1, "migrator1 should see dirty flag cleared after Force()")

	v2, dirty2, err = migrator2.Version()
	require.NoError(t, err)
	assert.Equal(t, currentVersion, v2)
	assert.False(t, dirty2, "migrator2 should see dirty flag cleared after Force()")

	// Test: Migrations can continue after Force() recovery
	err = migrator1.Up()
	require.NoError(t, err, "migrator1.Up() should succeed after Force() recovery")

	// Verify final state
	finalVersion, dirty, err := migrator1.Version()
	require.NoError(t, err)
	assert.Equal(t, baseVersion, finalVersion, "should reach final version after recovery")
	assert.False(t, dirty, "database should be clean after recovery and Up()")

	// migrator2 should also see the consistent final state
	v2Final, dirty2Final, err := migrator2.Version()
	require.NoError(t, err)
	assert.Equal(t, finalVersion, v2Final, "migrator2 should see same final version")
	assert.False(t, dirty2Final, "migrator2 should see clean state")
}
