// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// testPool is the shared database pool for integration tests.
var testPool *pgxpool.Pool

// testCleanup is called to terminate the container after tests.
var testCleanup func()

// TestMain sets up a PostgreSQL testcontainer for integration tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	// Run all migrations using the new Migrator
	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("failed to create migrator: " + err.Error())
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = pgEnv.Terminate(ctx)
		panic("failed to run migrations: " + err.Error())
	}
	_ = migrator.Close()

	// Create a new pool for tests
	pool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("failed to create pool: " + err.Error())
	}

	testPool = pool
	testCleanup = func() {
		pool.Close()
		_ = pgEnv.Terminate(ctx)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	testCleanup()

	os.Exit(code)
}
