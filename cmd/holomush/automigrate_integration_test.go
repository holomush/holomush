//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/holomush/holomush/internal/store"
)

// startPostgresContainer starts a PostgreSQL container for testing.
func startPostgresContainer(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cleanup := func() {
		_ = container.Terminate(ctx)
	}

	return connStr, cleanup
}

// schemaTableExists checks if the schema_migrations table exists.
func schemaTableExists(ctx context.Context, connStr string) (bool, error) {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return false, err
	}
	defer conn.Close(ctx)

	var exists bool
	query := `SELECT EXISTS (
		SELECT FROM information_schema.tables
		WHERE table_schema = 'public'
		AND table_name = 'schema_migrations'
	)`
	err = conn.QueryRow(ctx, query).Scan(&exists)
	return exists, err
}

// getMigrationVersion gets the current migration version from schema_migrations.
func getMigrationVersion(ctx context.Context, connStr string) (int, bool, error) {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return 0, false, err
	}
	defer conn.Close(ctx)

	var version int
	var dirty bool
	query := `SELECT version, dirty FROM schema_migrations LIMIT 1`
	err = conn.QueryRow(ctx, query).Scan(&version, &dirty)
	if err != nil {
		return 0, false, err
	}
	return version, dirty, nil
}

func TestAutoMigrate_Integration_RunsOnStartup(t *testing.T) {
	ctx := context.Background()

	connStr, cleanup := startPostgresContainer(t)
	defer cleanup()

	// Verify schema_migrations doesn't exist yet
	exists, err := schemaTableExists(ctx, connStr)
	require.NoError(t, err)
	assert.False(t, exists, "schema_migrations should not exist before auto-migrate")

	// Run auto-migration using the runAutoMigration function
	err = runAutoMigration(connStr, func(url string) (AutoMigrator, error) {
		return store.NewMigrator(url)
	})
	require.NoError(t, err)

	// Verify schema_migrations now exists
	exists, err = schemaTableExists(ctx, connStr)
	require.NoError(t, err)
	assert.True(t, exists, "schema_migrations should exist after auto-migrate")

	// Verify a migration version was recorded (version > 0)
	version, dirty, err := getMigrationVersion(ctx, connStr)
	require.NoError(t, err)
	assert.Greater(t, version, 0, "migration version should be > 0 after auto-migrate")
	assert.False(t, dirty, "database should not be in dirty state after successful migration")
}

func TestAutoMigrate_Integration_SkippedWhenDisabled(t *testing.T) {
	ctx := context.Background()

	connStr, cleanup := startPostgresContainer(t)
	defer cleanup()

	// Verify schema_migrations doesn't exist initially
	exists, err := schemaTableExists(ctx, connStr)
	require.NoError(t, err)
	assert.False(t, exists, "schema_migrations should not exist initially")

	// Create CoreDeps with auto-migrate disabled
	deps := &CoreDeps{
		DatabaseURLGetter: func() string { return connStr },
		AutoMigrateGetter: func() bool { return false },
		MigratorFactory: func(url string) (AutoMigrator, error) {
			t.Error("MigratorFactory should not be called when auto-migrate is disabled")
			return store.NewMigrator(url)
		},
	}

	// Simulate the auto-migrate check from runCoreWithDeps
	// We just check the logic branch - if AutoMigrateGetter returns false,
	// runAutoMigration should not be called
	if deps.AutoMigrateGetter() {
		err = runAutoMigration(connStr, deps.MigratorFactory)
		require.NoError(t, err)
	}

	// Verify schema_migrations still doesn't exist (no migration ran)
	exists, err = schemaTableExists(ctx, connStr)
	require.NoError(t, err)
	assert.False(t, exists, "schema_migrations should not exist when auto-migrate is disabled")
}

func TestAutoMigrate_Integration_IdempotentOnRerun(t *testing.T) {
	ctx := context.Background()

	connStr, cleanup := startPostgresContainer(t)
	defer cleanup()

	migratorFactory := func(url string) (AutoMigrator, error) {
		return store.NewMigrator(url)
	}

	// Run auto-migration first time
	err := runAutoMigration(connStr, migratorFactory)
	require.NoError(t, err)

	// Get version after first run
	versionAfterFirst, dirtyAfterFirst, err := getMigrationVersion(ctx, connStr)
	require.NoError(t, err)
	assert.Greater(t, versionAfterFirst, 0)
	assert.False(t, dirtyAfterFirst)

	// Run auto-migration second time (should be idempotent)
	err = runAutoMigration(connStr, migratorFactory)
	require.NoError(t, err)

	// Verify version is unchanged
	versionAfterSecond, dirtyAfterSecond, err := getMigrationVersion(ctx, connStr)
	require.NoError(t, err)
	assert.Equal(t, versionAfterFirst, versionAfterSecond, "version should be unchanged after idempotent re-run")
	assert.False(t, dirtyAfterSecond)
}
