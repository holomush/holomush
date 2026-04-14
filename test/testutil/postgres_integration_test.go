// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package testutil_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/test/testutil"
)

func TestSharedPostgresReturnsSameInstanceAcrossCalls(t *testing.T) {
	env1 := testutil.SharedPostgres(t)
	env2 := testutil.SharedPostgres(t)
	assert.Same(t, env1, env2, "SharedPostgres must return the same pointer")
}

func TestSharedPostgresHasAdminConnStr(t *testing.T) {
	env := testutil.SharedPostgres(t)
	require.NotEmpty(t, env.AdminConnStr, "AdminConnStr must be populated")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, env.AdminConnStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	var user string
	err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
	require.NoError(t, err)
	assert.Equal(t, "postgres", user, "AdminConnStr must connect as postgres superuser")
}

func TestFreshDatabaseReturnsMigratedDatabase(t *testing.T) {
	env := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, env)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	var user string
	err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
	require.NoError(t, err)
	assert.Equal(t, "holomush", user)

	var exists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'players'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "players table should exist after migration")
}

func TestFreshDatabaseReturnsIsolatedDatabases(t *testing.T) {
	env := testutil.SharedPostgres(t)
	connStr1 := testutil.FreshDatabase(t, env)
	connStr2 := testutil.FreshDatabase(t, env)

	assert.NotEqual(t, connStr1, connStr2, "each call must return a different database")

	ctx := context.Background()

	// Capture baseline count (migrations seed data into the template).
	conn2, err := pgx.Connect(ctx, connStr2)
	require.NoError(t, err)
	defer conn2.Close(ctx)
	var baselineCount int
	err = conn2.QueryRow(ctx, "SELECT count(*) FROM players").Scan(&baselineCount)
	require.NoError(t, err)

	// Insert a row into db1.
	conn1, err := pgx.Connect(ctx, connStr1)
	require.NoError(t, err)
	defer conn1.Close(ctx)
	_, err = conn1.Exec(ctx, "INSERT INTO players (id, username, password_hash) VALUES ('test-id-1', 'isolation_probe', 'hash1')")
	require.NoError(t, err)

	// Verify db2 still has only baseline rows — the insert must not leak.
	var afterCount int
	err = conn2.QueryRow(ctx, "SELECT count(*) FROM players").Scan(&afterCount)
	require.NoError(t, err)
	assert.Equal(t, baselineCount, afterCount, "databases must be isolated: insert in db1 must not appear in db2")
}

func TestRawDatabaseReturnsSuperuserConnectionToBlankDB(t *testing.T) {
	env := testutil.SharedPostgres(t)
	connStr := testutil.RawDatabase(t, env)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	var user string
	err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
	require.NoError(t, err)
	assert.Equal(t, "postgres", user)

	var exists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'players'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "raw database should have no migrations")
}
