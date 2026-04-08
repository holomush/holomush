// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	// Register pgx stdlib driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// TestMigration000005AuditSourceComponentAppliesCleanly applies all migrations
// against a fresh Postgres instance and asserts that the access_audit_log
// table has the expected shape after 000005 has run: renamed event_id/event_name
// columns, new source/component/message columns (NOT NULL), the old policy_id/
// policy_name columns gone, and the idx_audit_log_source_component index created.
func TestMigration000005AuditSourceComponentAppliesCleanly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pgEnv.Terminate(context.Background())
	})

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migrator.Up())
	require.NoError(t, migrator.Close())

	db, err := sql.Open("pgx", pgEnv.ConnStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Assert the renamed columns exist.
	assertColumnExists(t, db, "access_audit_log", "event_id")
	assertColumnExists(t, db, "access_audit_log", "event_name")

	// Assert the new columns exist with NOT NULL constraint.
	assertColumnExistsNotNull(t, db, "access_audit_log", "source")
	assertColumnExistsNotNull(t, db, "access_audit_log", "component")
	assertColumnExistsNotNull(t, db, "access_audit_log", "message")

	// Assert the old column names no longer exist.
	assertColumnDoesNotExist(t, db, "access_audit_log", "policy_id")
	assertColumnDoesNotExist(t, db, "access_audit_log", "policy_name")

	// Assert the source/component index was created.
	assertIndexExists(t, db, "idx_audit_log_source_component")
}

// TestMigration000005AuditSourceComponentRollbackReturnsSchemaToOriginalShape
// applies all migrations, then rolls back exactly one step via Steps(-1) and
// verifies the access_audit_log schema is back to its pre-000005 shape:
// policy_id/policy_name restored, source/component/message gone, and the
// idx_audit_log_source_component index dropped.
func TestMigration000005AuditSourceComponentRollbackReturnsSchemaToOriginalShape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pgEnv.Terminate(context.Background())
	})

	// Apply all migrations (including 000005).
	migratorUp, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migratorUp.Up())
	require.NoError(t, migratorUp.Close())

	db, err := sql.Open("pgx", pgEnv.ConnStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Sanity: after Up, the new shape is present.
	assertColumnExists(t, db, "access_audit_log", "event_id")
	assertColumnExists(t, db, "access_audit_log", "source")

	// Roll back just 000005 by stepping the migrator down once.
	migratorDown, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migratorDown.Steps(-1))
	require.NoError(t, migratorDown.Close())

	// After down, the original shape is restored.
	assertColumnExists(t, db, "access_audit_log", "policy_id")
	assertColumnExists(t, db, "access_audit_log", "policy_name")
	assertColumnDoesNotExist(t, db, "access_audit_log", "event_id")
	assertColumnDoesNotExist(t, db, "access_audit_log", "event_name")
	assertColumnDoesNotExist(t, db, "access_audit_log", "source")
	assertColumnDoesNotExist(t, db, "access_audit_log", "component")
	assertColumnDoesNotExist(t, db, "access_audit_log", "message")

	// Index should also be dropped.
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM pg_indexes WHERE indexname = $1`,
		"idx_audit_log_source_component",
	).Scan(&count))
	assert.Equal(t, 0, count, "source/component index should be dropped on rollback")
}

// TestMigration000005AuditSourceComponentBackfillsExistingRows stops migrations
// at version 4 (pre-000005), inserts a row using the old policy_id/policy_name
// schema, then applies 000005 and verifies the row was preserved under the
// renamed columns and backfilled with the migration's default source/component/
// message values.
//
// The table is partitioned by timestamp, so the test must first create a
// partition covering the row's timestamp before INSERT.
func TestMigration000005AuditSourceComponentBackfillsExistingRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pgEnv.Terminate(context.Background())
	})

	// Apply migrations up to and including 000004 (but NOT 000005).
	// The project's Migrator does not have a Migrate(version) method; it
	// exposes Steps(n), so we apply exactly 4 steps from a fresh database.
	migratorEarly, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migratorEarly.Steps(4))
	require.NoError(t, migratorEarly.Close())

	db, err := sql.Open("pgx", pgEnv.ConnStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Sanity: we should be at version 4 with the OLD schema shape.
	assertColumnExists(t, db, "access_audit_log", "policy_id")
	assertColumnExists(t, db, "access_audit_log", "policy_name")
	assertColumnDoesNotExist(t, db, "access_audit_log", "source")

	// Create a partition for the row we are about to insert. The audit
	// table is partitioned by RANGE(timestamp); production creates these
	// at bootstrap, not in migrations, so the test must do it explicitly.
	_, err = db.ExecContext(ctx, `
		CREATE TABLE access_audit_log_2026_04 PARTITION OF access_audit_log
		FOR VALUES FROM ('2026-04-01') TO ('2026-05-01')
	`)
	require.NoError(t, err)

	// Insert a row into access_audit_log using the pre-000005 schema
	// (policy_id/policy_name columns, no source/component/message).
	_, err = db.ExecContext(ctx, `
		INSERT INTO access_audit_log (
			id, timestamp, subject, action, resource, effect,
			policy_id, policy_name, attributes, duration_us
		) VALUES (
			'seed-test-id',
			'2026-04-15T12:00:00Z'::timestamptz,
			'character:01ABC', 'read', 'location:01XYZ', 'allow',
			'allow-read', 'Allow Read', '{}'::jsonb, 100
		)
	`)
	require.NoError(t, err)

	// Now apply 000005.
	migratorFinal, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migratorFinal.Up())
	require.NoError(t, migratorFinal.Close())

	// Query the existing row through the new column names — it should
	// have been preserved, and the new columns should contain the
	// migration defaults.
	var source, component, message, eventID, eventName string
	err = db.QueryRow(`
		SELECT source, component, message, event_id, event_name
		FROM access_audit_log
		WHERE id = $1
	`, "seed-test-id").Scan(&source, &component, &message, &eventID, &eventName)
	require.NoError(t, err)

	assert.Equal(t, "engine", source, "pre-existing rows should be backfilled with source='engine'")
	assert.Equal(t, "abac", component, "pre-existing rows should be backfilled with component='abac'")
	assert.Equal(t, "", message, "pre-existing rows should have empty message")
	assert.Equal(t, "allow-read", eventID, "existing policy_id value should be renamed to event_id")
	assert.Equal(t, "Allow Read", eventName, "existing policy_name value should be renamed to event_name")
}

// assertColumnExists fails the test if table.column is not present in
// information_schema.columns.
func assertColumnExists(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "expected column %s.%s to exist", table, column)
}

// assertColumnExistsNotNull fails the test if table.column is missing or is
// nullable.
func assertColumnExistsNotNull(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var isNullable string
	err := db.QueryRow(`
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&isNullable)
	require.NoError(t, err, "column %s.%s not found", table, column)
	assert.Equal(t, "NO", isNullable, "column %s.%s should be NOT NULL", table, column)
}

// assertColumnDoesNotExist fails the test if table.column IS present in
// information_schema.columns.
func assertColumnDoesNotExist(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "expected column %s.%s to NOT exist after migration", table, column)
}

// assertIndexExists fails the test if the named index is not present in
// pg_indexes.
func assertIndexExists(t *testing.T, db *sql.DB, indexName string) {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM pg_indexes
		WHERE indexname = $1
	`, indexName).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "expected index %s to exist", indexName)
}
