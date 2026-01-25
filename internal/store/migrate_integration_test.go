//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

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
