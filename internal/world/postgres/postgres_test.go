// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://localhost:5432/holomush_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		// Skip tests if no database available
		os.Exit(0)
	}

	// Check if we can actually connect
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		os.Exit(0)
	}

	testPool = pool

	code := m.Run()
	pool.Close()
	os.Exit(code)
}
