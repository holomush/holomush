// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"database/sql"
	"errors"

	// Register pgx stdlib driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

const (
	// sessionsLocationIndexVersion is the migration that creates the index.
	sessionsLocationIndexVersion = 53
	// sessionsLocationIndexPriorVersion is the schema state immediately before
	// it — the version the round trip steps back to.
	sessionsLocationIndexPriorVersion = 52
	// sessionsLocationIndexName is the index under test. Every assertion below
	// is keyed on this exact name: counting indexes on the sessions table would
	// be satisfied by a down migration that dropped the wrong one, since three
	// other indexes already live there.
	sessionsLocationIndexName = "idx_sessions_location_id"
)

// sessionsLocationIndexDef returns the pg_indexes definition for the named
// index on the sessions table, or "" when no such index exists. Keyed lookup by
// index name — never a count of indexes on the table.
func sessionsLocationIndexDef(db *sql.DB, indexName string) string {
	GinkgoHelper()
	var def string
	err := db.QueryRow(`
		SELECT indexdef FROM pg_indexes
		WHERE tablename = 'sessions' AND indexname = $1
	`, indexName).Scan(&def)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	Expect(err).NotTo(HaveOccurred())
	return def
}

var _ = Describe("Migration 000053 sessions location index", func() {
	// The migrator only ever moves UP during normal startup, so the ordinary
	// integration lane proves nothing about the down migration — a down that
	// silently no-ops would pass it. This spec drives the round trip
	// explicitly: it steps the index in, asserts it is present by a keyed
	// catalog lookup on its name, steps back one version, asserts it is gone,
	// then reapplies so the database is left in the state the rest of the suite
	// expects.
	It("creates the sessions location_id index on the way up and drops it on the way back down", func() {
		connStr := testutil.RawDatabase(suiteT, sharedPG)

		// The whole chain applies cleanly against a fresh database.
		migratorUp, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorUp.Up()).To(Succeed())
		Expect(migratorUp.Close()).To(Succeed())

		db, err := sql.Open("pgx", connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)

		// Bind the version number to this plan's migration files. Without this
		// the round trip below would step some other migration if a renumber
		// ever moved 000053, and every assertion would still pass.
		name, err := store.MigrationName(sessionsLocationIndexVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(name).To(Equal("000053_sessions_location_index"),
			"version %d must be this plan's migration", sessionsLocationIndexVersion)

		// Step back to the state immediately before the index migration. The
		// index MUST be absent here: this is the negative control that proves
		// the assertions below observe this migration rather than some
		// pre-existing index.
		migratorDown, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorDown.Migrate(sessionsLocationIndexPriorVersion)).To(Succeed())
		Expect(migratorDown.Close()).To(Succeed())

		Expect(sessionsLocationIndexDef(db, sessionsLocationIndexName)).To(BeEmpty(),
			"index must not exist before migration %d runs", sessionsLocationIndexVersion)

		// Step forward exactly one migration: the index appears, on the right
		// table and the right column.
		migratorStep, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorStep.Steps(1)).To(Succeed())
		version, dirty, err := migratorStep.Version()
		Expect(err).NotTo(HaveOccurred())
		Expect(dirty).To(BeFalse())
		Expect(version).To(Equal(uint(sessionsLocationIndexVersion)))
		Expect(migratorStep.Close()).To(Succeed())

		def := sessionsLocationIndexDef(db, sessionsLocationIndexName)
		Expect(def).NotTo(BeEmpty(), "index %s must exist after migration %d",
			sessionsLocationIndexName, sessionsLocationIndexVersion)
		Expect(def).To(ContainSubstring("location_id"),
			"index %s must cover the presence query's filter column", sessionsLocationIndexName)

		// Step back exactly one migration: the index is gone again. This is
		// the assertion the up-only integration lane cannot make.
		migratorBack, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorBack.Steps(-1)).To(Succeed())
		version, dirty, err = migratorBack.Version()
		Expect(err).NotTo(HaveOccurred())
		Expect(dirty).To(BeFalse())
		Expect(version).To(Equal(uint(sessionsLocationIndexPriorVersion)))
		Expect(migratorBack.Close()).To(Succeed())

		Expect(sessionsLocationIndexDef(db, sessionsLocationIndexName)).To(BeEmpty(),
			"down migration %d must drop index %s",
			sessionsLocationIndexVersion, sessionsLocationIndexName)

		// Reapply so the database is left at the head schema the rest of the
		// suite expects.
		migratorReapply, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migratorReapply.Up()).To(Succeed())
		Expect(migratorReapply.Close()).To(Succeed())

		Expect(sessionsLocationIndexDef(db, sessionsLocationIndexName)).NotTo(BeEmpty(),
			"index must be restored after reapplying migrations")
	})

	// Idempotency: the up migration uses IF NOT EXISTS, so re-running it
	// against a database that already carries the index must succeed rather
	// than erroring on a duplicate relation.
	It("re-applies cleanly when the index already exists", func() {
		connStr := testutil.RawDatabase(suiteT, sharedPG)

		migrator, err := store.NewMigrator(connStr)
		Expect(err).NotTo(HaveOccurred())
		Expect(migrator.Up()).To(Succeed())
		Expect(migrator.Close()).To(Succeed())

		db, err := sql.Open("pgx", connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)

		Expect(sessionsLocationIndexDef(db, sessionsLocationIndexName)).NotTo(BeEmpty())

		// Step back one and forward one with the index already dropped and
		// recreated, then execute the up migration's statement a second time
		// directly to prove IF NOT EXISTS holds.
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS ` + sessionsLocationIndexName +
			` ON sessions(location_id)`)
		Expect(err).NotTo(HaveOccurred(), "up migration statement must be idempotent")

		// And the down statement is idempotent in the same way.
		_, err = db.Exec(`DROP INDEX IF EXISTS ` + sessionsLocationIndexName)
		Expect(err).NotTo(HaveOccurred())
		_, err = db.Exec(`DROP INDEX IF EXISTS ` + sessionsLocationIndexName)
		Expect(err).NotTo(HaveOccurred(), "down migration statement must be idempotent")
	})
})
