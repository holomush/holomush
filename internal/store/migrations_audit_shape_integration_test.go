// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"time"

	// Register pgx stdlib driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// assertColumnExists fails the spec if table.column is not present in
// information_schema.columns.
func assertColumnExists(db *sql.DB, table, column string) {
	GinkgoHelper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&count)
	Expect(err).NotTo(HaveOccurred())
	Expect(count).To(Equal(1), "expected column %s.%s to exist", table, column)
}

// assertColumnExistsNotNull fails the spec if table.column is missing or is
// nullable.
func assertColumnExistsNotNull(db *sql.DB, table, column string) {
	GinkgoHelper()
	var isNullable string
	err := db.QueryRow(`
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&isNullable)
	Expect(err).NotTo(HaveOccurred(), "column %s.%s not found", table, column)
	Expect(isNullable).To(Equal("NO"), "column %s.%s should be NOT NULL", table, column)
}

// assertColumnDoesNotExist fails the spec if table.column IS present in
// information_schema.columns.
func assertColumnDoesNotExist(db *sql.DB, table, column string) {
	GinkgoHelper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = $1 AND column_name = $2
	`, table, column).Scan(&count)
	Expect(err).NotTo(HaveOccurred())
	Expect(count).To(Equal(0), "expected column %s.%s to NOT exist after migration", table, column)
}

// assertIndexExists fails the spec if the named index is not present in
// pg_indexes.
func assertIndexExists(db *sql.DB, indexName string) {
	GinkgoHelper()
	var count int
	err := db.QueryRow(`
		SELECT count(*) FROM pg_indexes
		WHERE indexname = $1
	`, indexName).Scan(&count)
	Expect(err).NotTo(HaveOccurred())
	Expect(count).To(Equal(1), "expected index %s to exist", indexName)
}

var _ = Describe("Migration 000005 audit source/component", func() {
	// TestMigration000005AuditSourceComponentAppliesCleanly applies all migrations
	// against a fresh Postgres instance and asserts that the access_audit_log
	// table has the expected shape after 000005 has run: renamed event_id/event_name
	// columns, new source/component/message columns (NOT NULL), the old policy_id/
	// policy_name columns gone, and the idx_audit_log_source_component index created.
	It("applies cleanly and produces expected access_audit_log shape", func() {
		connStr := testutil.RawDatabase(suiteT, sharedPG)

		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		Expect(migrator.Close()).To(Succeed())

		db, err := sql.Open("pgx", connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)

		// Assert the renamed columns exist.
		assertColumnExists(db, "access_audit_log", "event_id")
		assertColumnExists(db, "access_audit_log", "event_name")

		// Assert the new columns exist with NOT NULL constraint.
		assertColumnExistsNotNull(db, "access_audit_log", "source")
		assertColumnExistsNotNull(db, "access_audit_log", "component")
		assertColumnExistsNotNull(db, "access_audit_log", "message")

		// Assert the old column names no longer exist.
		assertColumnDoesNotExist(db, "access_audit_log", "policy_id")
		assertColumnDoesNotExist(db, "access_audit_log", "policy_name")

		// Assert the source/component index was created.
		assertIndexExists(db, "idx_audit_log_source_component")
	})

	// TestMigration000005AuditSourceComponentRollbackReturnsSchemaToOriginalShape
	// applies all migrations, then rolls back exactly one step via Steps(-1) and
	// verifies the access_audit_log schema is back to its pre-000005 shape:
	// policy_id/policy_name restored, source/component/message gone, and the
	// idx_audit_log_source_component index dropped.
	It("rollback returns access_audit_log schema to original pre-000005 shape", func() {
		connStr := testutil.RawDatabase(suiteT, sharedPG)

		// Apply all migrations (including 000005).
		migratorUp, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorUp.Up()).To(Succeed())
		Expect(migratorUp.Close()).To(Succeed())

		db, err := sql.Open("pgx", connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)

		// Sanity: after Up, 000005 is applied and the new shape is present.
		migratorCheck, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		version, dirty, err := migratorCheck.Version()
		Expect(err).NotTo(HaveOccurred())
		Expect(version).To(BeNumerically(">=", uint(5)), "000005 must be applied before testing rollback")
		Expect(dirty).To(BeFalse())
		Expect(migratorCheck.Close()).To(Succeed())
		assertColumnExists(db, "access_audit_log", "event_id")
		assertColumnExists(db, "access_audit_log", "source")

		// Migrate down to version 4 (just before 000005_audit_source_component)
		// to restore the pre-000005 audit schema shape. Using Migrate(4) instead
		// of Steps(-N) so the test doesn't break when new migrations are added.
		migratorDown, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorDown.Migrate(4)).To(Succeed())
		Expect(migratorDown.Close()).To(Succeed())

		// After down, the original shape is restored.
		assertColumnExists(db, "access_audit_log", "policy_id")
		assertColumnExists(db, "access_audit_log", "policy_name")
		assertColumnDoesNotExist(db, "access_audit_log", "event_id")
		assertColumnDoesNotExist(db, "access_audit_log", "event_name")
		assertColumnDoesNotExist(db, "access_audit_log", "source")
		assertColumnDoesNotExist(db, "access_audit_log", "component")
		assertColumnDoesNotExist(db, "access_audit_log", "message")

		// Index should also be dropped.
		var count int
		Expect(db.QueryRow(
			`SELECT count(*) FROM pg_indexes WHERE indexname = $1`,
			"idx_audit_log_source_component",
		).Scan(&count)).To(Succeed())
		Expect(count).To(Equal(0), "source/component index should be dropped on rollback")
	})

	// TestMigration000005AuditSourceComponentBackfillsExistingRows stops migrations
	// at version 4 (pre-000005), inserts a row using the old policy_id/policy_name
	// schema, then applies 000005 and verifies the row was preserved under the
	// renamed columns and backfilled with the migration's default source/component/
	// message values.
	//
	// The table is partitioned by timestamp, so the test must first create a
	// partition covering the row's timestamp before INSERT.
	It("backfills existing rows with default source/component/message values", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		connStr := testutil.RawDatabase(suiteT, sharedPG)

		// Apply migrations up to and including 000004 (but NOT 000005).
		// The project's Migrator does not have a Migrate(version) method; it
		// exposes Steps(n), so we apply exactly 4 steps from a fresh database.
		migratorEarly, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorEarly.Steps(4)).To(Succeed())
		Expect(migratorEarly.Close()).To(Succeed())

		db, err := sql.Open("pgx", connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)

		// Sanity: we should be at version 4 with the OLD schema shape.
		assertColumnExists(db, "access_audit_log", "policy_id")
		assertColumnExists(db, "access_audit_log", "policy_name")
		assertColumnDoesNotExist(db, "access_audit_log", "source")

		// Create a partition for the row we are about to insert. The audit
		// table is partitioned by RANGE(timestamp); production creates these
		// at bootstrap, not in migrations, so the test must do it explicitly.
		_, err = db.ExecContext(ctx, `
			CREATE TABLE access_audit_log_2026_04 PARTITION OF access_audit_log
			FOR VALUES FROM ('2026-04-01') TO ('2026-05-01')
		`)
		Expect(err).NotTo(HaveOccurred())

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
		Expect(err).NotTo(HaveOccurred())

		// Now apply 000005.
		migratorFinal, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorFinal.Up()).To(Succeed())
		Expect(migratorFinal.Close()).To(Succeed())

		// Query the existing row through the new column names — it should
		// have been preserved, and the new columns should contain the
		// migration defaults.
		var source, component, message, eventID, eventName string
		err = db.QueryRow(`
			SELECT source, component, message, event_id, event_name
			FROM access_audit_log
			WHERE id = $1
		`, "seed-test-id").Scan(&source, &component, &message, &eventID, &eventName)
		Expect(err).NotTo(HaveOccurred())

		Expect(source).To(Equal("engine"), "pre-existing rows should be backfilled with source='engine'")
		Expect(component).To(Equal("abac"), "pre-existing rows should be backfilled with component='abac'")
		Expect(message).To(Equal(""), "pre-existing rows should have empty message")
		Expect(eventID).To(Equal("allow-read"), "existing policy_id value should be renamed to event_id")
		Expect(eventName).To(Equal("Allow Read"), "existing policy_name value should be renamed to event_name")
	})
})

var _ = Describe("Migration 000014 events_audit DEK columns", func() {
	// TestEventsAuditHasDEKColumnsAfterMigration014 applies all migrations against
	// a fresh Postgres instance and asserts the events_audit table has the
	// dek_ref BIGINT and dek_version INTEGER columns (both nullable) plus the
	// partial index events_audit_dek_ref ON (dek_ref) WHERE dek_ref IS NOT NULL
	// after migration 000014 has run.
	It("has dek_ref BIGINT, dek_version INTEGER (both nullable), and partial index after migration 000014", func() {
		connStr := testutil.RawDatabase(suiteT, sharedPG)

		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		Expect(migrator.Close()).To(Succeed())

		db, err := sql.Open("pgx", connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)

		// dek_ref column — BIGINT, nullable.
		var dataType, isNullable string
		err = db.QueryRow(`
			SELECT data_type, is_nullable
			  FROM information_schema.columns
			 WHERE table_name = 'events_audit' AND column_name = 'dek_ref'
		`).Scan(&dataType, &isNullable)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataType).To(Equal("bigint"))
		Expect(isNullable).To(Equal("YES"))

		// dek_version column — INTEGER, nullable.
		err = db.QueryRow(`
			SELECT data_type, is_nullable
			  FROM information_schema.columns
			 WHERE table_name = 'events_audit' AND column_name = 'dek_version'
		`).Scan(&dataType, &isNullable)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataType).To(Equal("integer"))
		Expect(isNullable).To(Equal("YES"))

		// Partial index on dek_ref.
		var indexCount int
		err = db.QueryRow(`
			SELECT count(*)
			  FROM pg_indexes
			 WHERE tablename = 'events_audit' AND indexname = 'events_audit_dek_ref'
		`).Scan(&indexCount)
		Expect(err).NotTo(HaveOccurred())
		Expect(indexCount).To(Equal(1))
	})
})
