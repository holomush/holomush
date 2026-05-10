//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/store"
)

// newTestPool spins up a Postgres testcontainer and returns a connected
// *pgxpool.Pool plus a cleanup function. The cleanup terminates the
// container and closes the pool.
func newTestPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	pgC, err := postgres.Run(
		ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgC.Terminate(ctx) })

	connStr, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	cleanup := func() {
		pool.Close()
		_ = pgC.Terminate(ctx)
	}
	return pool, cleanup
}

// runMigrations applies migrations 1..targetVersion against the pool's
// database. Uses store.NewMigrator (which wraps golang-migrate).
func runMigrations(ctx context.Context, pool *pgxpool.Pool, targetVersion uint) error {
	connStr := pool.Config().ConnConfig.ConnString()
	migrator, err := store.NewMigrator(connStr)
	if err != nil {
		return err
	}
	defer migrator.Close()
	return migrator.Migrate(targetVersion)
}

func TestNewTestPoolAndRunMigrationsSmoke(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 17)) // pre-w9ml state
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM events_audit`).Scan(&n))
}
