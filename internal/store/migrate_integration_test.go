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
	assert.Equal(t, uint(7), version)
	assert.False(t, dirty)

	// Test: Rollback one
	err = migrator.Steps(-1)
	require.NoError(t, err)

	version, _, err = migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(6), version)

	// Test: Apply one
	err = migrator.Steps(1)
	require.NoError(t, err)

	version, _, err = migrator.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(7), version)
}
