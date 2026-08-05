// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// uniqueCharName suffixes a fixture's display name with part of the character
// id that will carry it.
//
// Migration 000056 makes characters.normalized_name UNIQUE, so two fixture rows
// sharing a literal display name now collide where they previously did not. It
// is DETERMINISTIC on purpose — unlike charFixtureName, which draws fresh
// randomness — so a call site may compute the name and its identity columns in
// two separate expressions and get a matching pair.
func uniqueCharName(base string, id ulid.ULID) string {
	// The SUFFIX, never a prefix: a ULID string is 10 characters of timestamp
	// followed by 16 of randomness, so id.String()[:6] is identical for every
	// id minted in the same millisecond — which is exactly what a fixture does.
	return base + " " + id.String()[20:]
}

// testPool is the shared database pool for integration tests.
var testPool *pgxpool.Pool

// testCleanup is called to terminate the container after tests.
var testCleanup func()

// testConnStr is the container's connection string. It exists so a spec can
// open a database/sql handle — BackfillCharacterIdentity's only production
// caller is a goose Go migration running on a *sql.Tx, so driving it through
// the pgx pool would exercise a shape the migration never uses.
var testConnStr string

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
	testConnStr = pgEnv.ConnStr
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
