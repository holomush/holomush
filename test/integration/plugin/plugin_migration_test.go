// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/test/testutil"
)

// migrationsDirCoreScenes is the relative path from test/integration/plugin
// to plugins/core-scenes/migrations.
const migrationsDirCoreScenes = "../../../plugins/core-scenes/migrations"

// readPluginMigration reads the up.sql file for the given migration number
// from the plugin's migrations directory.
func readPluginMigration(t *testing.T, dir string, n int) string {
	t.Helper()
	pattern := filepath.Join(dir, fmt.Sprintf("%06d_*.up.sql", n))
	matches, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.Lenf(t, matches, 1, "expected exactly one migration matching %s, got %v", pattern, matches)
	body, err := os.ReadFile(matches[0])
	require.NoError(t, err)
	return string(body)
}

// applyPluginMigrationsUpTo applies migrations [1..n] in order, all under
// the search_path of `schema`. Each .sql file is executed in a single
// pool.Exec — adequate for the simple, single-statement migrations these
// plugin migrations contain (CREATE TABLE / ALTER TABLE).
func applyPluginMigrationsUpTo(t *testing.T, pool *pgxpool.Pool, dir, schema string, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, fmt.Sprintf("SET search_path TO %s, public", schema))
	require.NoError(t, err)
	for i := 1; i <= n; i++ {
		applyPluginMigrationN(t, pool, dir, i)
	}
}

// applyPluginMigrationN runs a single migration .up.sql file. The current
// search_path on `pool` MUST already point at the target schema.
func applyPluginMigrationN(t *testing.T, pool *pgxpool.Pool, dir string, n int) {
	t.Helper()
	body := readPluginMigration(t, dir, n)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, body)
	require.NoErrorf(t, err, "applying migration %d failed", n)
}

// pluginMigrationPool opens a pool against a fresh DB. The pool is bound
// to t.Cleanup.
func pluginMigrationPool(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, fmt.Sprintf("SET search_path TO %s, public", schema))
	require.NoError(t, err)
	return pool
}

// listScenLogColumns enumerates dek_ref / dek_version columns in the
// given schema's scene_log table.
func listSceneLogDekColumns(t *testing.T, pool *pgxpool.Pool, schema string) []string {
	t.Helper()
	rows, err := pool.Query(t.Context(), `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = 'scene_log'
		  AND column_name IN ('dek_ref', 'dek_version')
		ORDER BY column_name`, schema)
	require.NoError(t, err)
	defer rows.Close()
	var present []string
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		present = append(present, c)
	}
	require.NoError(t, rows.Err())
	return present
}

// TestPhase7PluginMigrationStandalone — INV-P7-3 + INV-P7-10. Verifies
// that migration 000005 adds dek_ref + dek_version on top of the
// pre-existing migrations 1-4 in isolation, and that re-applying it is
// a no-op (idempotency via ADD COLUMN IF NOT EXISTS).
func TestPhase7PluginMigrationStandalone(t *testing.T) {
	const schema = "plugin_core_scenes_test"
	pool := pluginMigrationPool(t, schema)

	// Apply migrations 1..4 (existing baseline).
	applyPluginMigrationsUpTo(t, pool, migrationsDirCoreScenes, schema, 4)

	// Pre-condition: dek columns NOT present after 1-4.
	preFive := listSceneLogDekColumns(t, pool, schema)
	require.Empty(t, preFive,
		"dek_ref/dek_version MUST NOT exist before migration 000005")

	// Apply migration 5 in isolation.
	applyPluginMigrationN(t, pool, migrationsDirCoreScenes, 5)

	got := listSceneLogDekColumns(t, pool, schema)
	assert.Equal(t, []string{"dek_ref", "dek_version"}, got,
		"INV-P7-3: scene_log MUST have dek_ref + dek_version after migration 5")

	// Idempotency: re-applying migration 5 is a no-op (CREATE INDEX IF
	// NOT EXISTS + ADD COLUMN IF NOT EXISTS).
	applyPluginMigrationN(t, pool, migrationsDirCoreScenes, 5)
	got = listSceneLogDekColumns(t, pool, schema)
	assert.Equal(t, []string{"dek_ref", "dek_version"}, got,
		"INV-P7-10: re-applying migration 5 MUST be idempotent")
}

// TestSceneLogHasDekColumns — production-shape assertion: after the
// FULL plugin migration sequence (1..N), scene_log carries both columns
// with the expected SQL types (BIGINT for dek_ref, INTEGER for dek_version).
func TestSceneLogHasDekColumns(t *testing.T) {
	const schema = "plugin_core_scenes_full"
	pool := pluginMigrationPool(t, schema)

	// Discover the highest migration number present and apply through it.
	matches, err := filepath.Glob(filepath.Join(migrationsDirCoreScenes, "*.up.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	sort.Strings(matches)
	highest := len(matches)
	// Sanity: filename ordering matches numeric ordering for these
	// migrations (zero-padded 6 digits).
	last := filepath.Base(matches[len(matches)-1])
	require.Truef(t, strings.HasPrefix(last, fmt.Sprintf("%06d_", highest)),
		"migration filename ordering MUST be sequential — got %s as highest of %d", last, highest)

	applyPluginMigrationsUpTo(t, pool, migrationsDirCoreScenes, schema, highest)

	var dekRefType, dekVersionType string
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT data_type FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = 'scene_log'
		  AND column_name = 'dek_ref'`, schema).Scan(&dekRefType))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT data_type FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = 'scene_log'
		  AND column_name = 'dek_version'`, schema).Scan(&dekVersionType))
	assert.Equal(t, "bigint", dekRefType)
	assert.Equal(t, "integer", dekVersionType)
}
