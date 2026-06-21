//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// freshMigratedPool returns a *pgxpool.Pool on a fresh database cloned from the
// process-wide, pre-migrated template (schema at the latest migration). Use for
// functional tests that need the full current schema.
//
// It uses the single shared Postgres testcontainer (testutil.SharedPostgres) and
// a fast CREATE DATABASE ... TEMPLATE clone (testutil.FreshDatabase) — NOT a
// per-test container and NOT a per-test migration run. The previous helper spun
// up a brand-new postgres:18-alpine container per test; with t.Parallel and
// `task test:int` running ./... under -race, 15+ such containers burst at once
// and starved constrained CI runners, so the tests whose context budget was
// charged with that startup time hit "context deadline exceeded" (holomush-gf6tp,
// holomush-qoruw). The shared container + template clone removes both costs.
func freshMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	env := testutil.SharedPostgres(t)
	return newPoolFromConnStr(t, testutil.FreshDatabase(t, env))
}

// rawPool returns a *pgxpool.Pool on a blank database (no migrations applied) on
// the shared Postgres testcontainer. Use for migration-process tests that drive
// runMigrations to specific versions (up and down) themselves and therefore need
// to start from an empty schema. Unlike freshMigratedPool (holomush-role creds),
// testutil.RawDatabase returns a postgres-superuser connection.
func rawPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	env := testutil.SharedPostgres(t)
	return newPoolFromConnStr(t, testutil.RawDatabase(t, env))
}

// newPoolFromConnStr opens a *pgxpool.Pool for connStr and registers its close
// via t.Cleanup. Shared by freshMigratedPool and rawPool so pool construction
// has a single home.
func newPoolFromConnStr(t *testing.T, connStr string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// runMigrations applies migrations up/down to targetVersion against the pool's
// database via store.NewMigrator (golang-migrate). The ctx parameter is accepted
// for call-site symmetry; golang-migrate's Migrate does not take a context.
func runMigrations(ctx context.Context, pool *pgxpool.Pool, targetVersion uint) error {
	_ = ctx
	connStr := pool.Config().ConnConfig.ConnString()
	migrator, err := store.NewMigrator(connStr)
	if err != nil {
		return err
	}
	defer migrator.Close()
	return migrator.Migrate(targetVersion)
}

func TestRawPoolAndRunMigrationsSmoke(t *testing.T) {
	ctx := context.Background()
	pool := rawPool(t)

	require.NoError(t, runMigrations(ctx, pool, 17)) // pre-w9ml state
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM events_audit`).Scan(&n))
}
