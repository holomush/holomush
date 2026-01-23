// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/holomush/holomush/internal/store"
)

// testPool is the shared database pool for integration tests.
var testPool *pgxpool.Pool

// testCleanup is called to terminate the container after tests.
var testCleanup func()

// TestMain sets up a PostgreSQL testcontainer for integration tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("holomush"),
		postgres.WithPassword("holomush"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		panic("failed to get connection string: " + err.Error())
	}

	// Create the event store to run migrations
	eventStore, err := store.NewPostgresEventStore(ctx, connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		panic("failed to create event store: " + err.Error())
	}

	// Run all migrations (001, 002, 003)
	if err := eventStore.Migrate(ctx); err != nil {
		eventStore.Close()
		_ = container.Terminate(ctx)
		panic("failed to run migrations: " + err.Error())
	}
	eventStore.Close()

	// Create a new pool for tests
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		panic("failed to create pool: " + err.Error())
	}

	testPool = pool
	testCleanup = func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	testCleanup()

	os.Exit(code)
}
