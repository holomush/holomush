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
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
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

// applyPluginMigrationsUpTo applies migrations [1..n] in order under the
// search_path of `schema`, pinned to a SINGLE acquired connection. The
// pool.Exec("SET search_path") path is connection-scoped — pool may
// hand out a different connection on the next Exec, dropping the
// search_path setting and silently creating tables in `public` instead.
// Acquiring once + routing all DDL through that connection guarantees
// the migration ran in the intended schema.
func applyPluginMigrationsUpTo(t *testing.T, pool *pgxpool.Pool, dir, schema string, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema))
	require.NoError(t, err)
	_, err = conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s, public", schema))
	require.NoError(t, err)
	for i := 1; i <= n; i++ {
		body := readPluginMigration(t, dir, i)
		_, err := conn.Exec(ctx, body)
		require.NoErrorf(t, err, "applying migration %d failed", i)
	}
}

// applyPluginMigrationN runs a single migration .up.sql file pinned to
// a single acquired connection from the pool with search_path set to
// `schema`. Stand-alone variant of the loop in applyPluginMigrationsUpTo
// for tests that exercise one migration in isolation.
func applyPluginMigrationN(t *testing.T, pool *pgxpool.Pool, dir, schema string, n int) {
	t.Helper()
	body := readPluginMigration(t, dir, n)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s, public", schema))
	require.NoError(t, err)
	_, err = conn.Exec(ctx, body)
	require.NoErrorf(t, err, "applying migration %d failed", n)
}

// pluginMigrationPool opens a pool against a fresh DB and creates the
// target schema. Per-connection search_path setting is moved to the
// individual migration helpers so the schema is established at the same
// connection where the DDL runs (not lost across pool-connection
// hand-offs). The pool is bound to t.Cleanup.
func pluginMigrationPool(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	connStr := testutil.FreshDatabase(t, sharedPG)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema))
	require.NoError(t, err)
	return pool
}

// listSceneLogDekColumns enumerates dek_ref / dek_version columns in the
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

// Phase 7 plugin migration standalone — INV-EVENTBUS-25 + INV-EVENTBUS-27. Verifies
// that migration 000005 adds dek_ref + dek_version on top of the
// pre-existing migrations 1-4 in isolation, and that re-applying it is
// a no-op (idempotency via ADD COLUMN IF NOT EXISTS).
var _ = Describe("Phase 7 plugin migration standalone (INV-EVENTBUS-25, INV-EVENTBUS-27)", func() {
	It("adds dek_ref + dek_version on migration 5 and is idempotent", func() {
		const schema = "plugin_core_scenes_test"
		pool := pluginMigrationPool(suiteT, schema)

		// Apply migrations 1..4 (existing baseline).
		applyPluginMigrationsUpTo(suiteT, pool, migrationsDirCoreScenes, schema, 4)

		// Pre-condition: dek columns NOT present after 1-4.
		preFive := listSceneLogDekColumns(suiteT, pool, schema)
		Expect(preFive).To(BeEmpty(),
			"dek_ref/dek_version MUST NOT exist before migration 000005")

		// Apply migration 5 in isolation.
		applyPluginMigrationN(suiteT, pool, migrationsDirCoreScenes, schema, 5)

		got := listSceneLogDekColumns(suiteT, pool, schema)
		Expect(got).To(Equal([]string{"dek_ref", "dek_version"}),
			"INV-EVENTBUS-25: scene_log MUST have dek_ref + dek_version after migration 5")

		// Idempotency: re-applying migration 5 is a no-op (CREATE INDEX IF
		// NOT EXISTS + ADD COLUMN IF NOT EXISTS).
		applyPluginMigrationN(suiteT, pool, migrationsDirCoreScenes, schema, 5)
		got = listSceneLogDekColumns(suiteT, pool, schema)
		Expect(got).To(Equal([]string{"dek_ref", "dek_version"}),
			"INV-EVENTBUS-27: re-applying migration 5 MUST be idempotent")
	})
})

// Scene log dek columns — production-shape assertion: after the FULL
// plugin migration sequence (1..N), scene_log carries both columns with
// the expected SQL types (BIGINT for dek_ref, INTEGER for dek_version).
//
// INV-EVENTBUS-25 cross-reference: this spec is the named carrier for the
// invariant; the phase7_boundary_meta_test.go drift detector maps the
// invariant to the suite entry TestBinaryPlugin (Ginkgo Describes are
// not top-level *testing.T funcs).
var _ = Describe("Scene log has DEK columns (INV-EVENTBUS-25)", func() {
	It("has bigint dek_ref + integer dek_version after full migration sequence", func() {
		const schema = "plugin_core_scenes_full"
		pool := pluginMigrationPool(suiteT, schema)

		// Discover the highest migration number present and apply through it.
		matches, err := filepath.Glob(filepath.Join(migrationsDirCoreScenes, "*.up.sql"))
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).NotTo(BeEmpty())
		sort.Strings(matches)
		highest := len(matches)
		// Sanity: filename ordering matches numeric ordering for these
		// migrations (zero-padded 6 digits).
		last := filepath.Base(matches[len(matches)-1])
		Expect(strings.HasPrefix(last, fmt.Sprintf("%06d_", highest))).To(BeTrue(),
			"migration filename ordering MUST be sequential — got %s as highest of %d", last, highest)

		applyPluginMigrationsUpTo(suiteT, pool, migrationsDirCoreScenes, schema, highest)

		var dekRefType, dekVersionType string
		Expect(pool.QueryRow(suiteT.Context(), `
			SELECT data_type FROM information_schema.columns
			WHERE table_schema = $1
			  AND table_name = 'scene_log'
			  AND column_name = 'dek_ref'`, schema).Scan(&dekRefType)).NotTo(HaveOccurred())
		Expect(pool.QueryRow(suiteT.Context(), `
			SELECT data_type FROM information_schema.columns
			WHERE table_schema = $1
			  AND table_name = 'scene_log'
			  AND column_name = 'dek_version'`, schema).Scan(&dekVersionType)).NotTo(HaveOccurred())
		Expect(dekRefType).To(Equal("bigint"))
		Expect(dekVersionType).To(Equal("integer"))
	})
})
