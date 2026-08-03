// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"database/sql"
	"log/slog"
	"math"

	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
	"github.com/samber/oops"
)

// Table names involved in the one-shot cutover from golang-migrate bookkeeping to
// goose bookkeeping. These are identifiers, not values, so they are never
// parameterizable — which is precisely why they are constants here and why no
// version number is ever interpolated alongside them.
const (
	legacyVersionTable   = "schema_migrations"
	archivedVersionTable = "schema_migrations_pre_goose"
	gooseVersionTable    = "goose_db_version"
)

// probeTablesSQL reports, in one round trip, whether each bookkeeping table exists.
// to_regclass returns NULL rather than raising for an absent relation, so this is
// safe to run against a completely blank database.
const probeTablesSQL = `SELECT to_regclass('public.` + legacyVersionTable + `') IS NOT NULL,
	                           to_regclass('public.` + gooseVersionTable + `') IS NOT NULL`

// probeGooseSeededSQL reports whether goose bookkeeping already carries any row.
// Any row at all counts: adopt writes the version-0 row and the whole derived set
// inside one transaction, so a concurrent reader either sees no table or sees the
// complete committed set. There is no partially-seeded state to misread.
const probeGooseSeededSQL = `SELECT EXISTS (SELECT 1 FROM ` + gooseVersionTable + `)`

// probeLegacyStateSQL reads the single row golang-migrate maintained.
const probeLegacyStateSQL = `SELECT version, dirty FROM ` + legacyVersionTable

// archiveLegacyTableSQL makes the old bookkeeping table inert while preserving it as
// a forensic record (D-04). Renaming rather than dropping means a binary rolled back
// inside the deploy window cannot find a live table to start writing to, and the
// version cut over from — plus the dirty value at that moment — stays readable.
const archiveLegacyTableSQL = `ALTER TABLE ` + legacyVersionTable + ` RENAME TO ` + archivedVersionTable

// adoptFromGolangMigrate performs the one-shot, irreversible cutover from
// golang-migrate's schema_migrations bookkeeping to goose's goose_db_version.
//
// It is deliberately small. Adopt performs NO schema verification beyond the dirty
// check (D-06): it trusts the recorded version, and the compensating control is a
// rehearsal against restored real data (D-16), not a schema-comparison engine. The
// simplicity IS the safety property for a one-shot irreversible step, so resist
// growing this function.
//
// The write happens unattended at boot (D-02's accepted cost), which is why the
// INFO line before the commit is the entire audit trail, and why the dirty refusal
// aborts boot rather than guessing.
//
// It is called from Migrator.Up only. The read-only verbs (migrate status, migrate
// version) deliberately do NOT trigger it: an operator running a diagnostic to
// decide whether to deploy must not thereby perform the deploy's irreversible step.
// The cost of that choice is that status/version report goose's pre-adopt view
// (version 0, everything pending) against a not-yet-adopted database.
//
// Returns nil — having done nothing — for a database that has no legacy table
// (fresh installs, CI, testcontainers) or whose goose bookkeeping is already
// populated (a replica that lost the boot race).
func (m *Migrator) adoptFromGolangMigrate(ctx context.Context) error {
	// Unit tests construct a Migrator around a mock provider with no database
	// handle. There is nothing to inspect and nothing to adopt in that shape.
	if m.db == nil {
		return nil
	}

	// The advisory lock is a SESSION lock, so it must be taken and released on one
	// pinned connection — and the transaction below must run on that same
	// connection, or it would not be covered by the lock at all.
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return oops.Code("MIGRATION_ADOPT_LOCK_FAILED").With("operation", "acquire connection").Wrap(err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to return adopt connection to the pool", "error", closeErr)
		}
	}()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return oops.Code("MIGRATION_ADOPT_LOCK_FAILED").With("operation", "create session locker").Wrap(err)
	}
	if lockErr := locker.SessionLock(ctx, conn); lockErr != nil {
		return oops.Code("MIGRATION_ADOPT_LOCK_FAILED").With("operation", "acquire advisory lock").Wrap(lockErr)
	}
	defer func() {
		if unlockErr := locker.SessionUnlock(ctx, conn); unlockErr != nil {
			// Do not mask the primary error. A session advisory lock is released
			// implicitly when the session ends, including on an ungraceful
			// disconnect, so a failure here is loggable rather than fatal.
			slog.WarnContext(ctx, "failed to release adopt advisory lock", "error", unlockErr)
		}
	}()

	return m.adoptLocked(ctx, conn)
}

// adoptLocked runs the cutover with the advisory lock already held on conn.
//
// Every probe here runs INSIDE the lock, which is the load-bearing part of the
// concurrency story (D-03). Serializing two booting replicas does not by itself make
// the second one correct — it only makes it later. The second replica has to look
// again, after it wins the lock, and find the work already done.
func (m *Migrator) adoptLocked(ctx context.Context, conn *sql.Conn) error {
	var legacyExists, gooseExists bool
	if err := conn.QueryRowContext(ctx, probeTablesSQL).Scan(&legacyExists, &gooseExists); err != nil {
		return oops.Code("MIGRATION_ADOPT_PROBE_FAILED").With("operation", "probe bookkeeping tables").Wrap(err)
	}

	// No legacy table: a fresh database. This is the branch that keeps the
	// irreversible path from ever firing in CI, testcontainers, and new deploys.
	if !legacyExists {
		return nil
	}

	if gooseExists {
		var seeded bool
		if err := conn.QueryRowContext(ctx, probeGooseSeededSQL).Scan(&seeded); err != nil {
			return oops.Code("MIGRATION_ADOPT_PROBE_FAILED").With("operation", "probe goose bookkeeping").Wrap(err)
		}
		if seeded {
			slog.InfoContext(ctx, "goose bookkeeping already populated, skipping adopt",
				"legacy_table", legacyVersionTable,
				"goose_table", gooseVersionTable)
			return nil
		}
	}

	recorded, dirty, err := readLegacyVersion(ctx, conn)
	if err != nil {
		return err
	}

	// A dirty flag means the recorded version describes a schema that was never
	// fully applied. Seeding from it would record partially-applied migrations as
	// applied, and goose has no way to detect that afterward. Refuse and abort boot
	// (D-05) — this matches the repo's default-deny posture.
	if dirty {
		return errAdoptRefusedDirty(recorded)
	}

	all, err := allMigrationVersions()
	if err != nil {
		return oops.Code("MIGRATION_ADOPT_SEED_FAILED").With("operation", "list embedded versions").Wrap(err)
	}
	seed := derivedSeedVersions(recorded, all)

	return m.seedAndArchive(ctx, conn, recorded, seed, gooseExists)
}

// readLegacyVersion reads golang-migrate's single bookkeeping row.
func readLegacyVersion(ctx context.Context, conn *sql.Conn) (recorded uint, dirty bool, err error) {
	var version int64
	scanErr := conn.QueryRowContext(ctx, probeLegacyStateSQL).Scan(&version, &dirty)
	if scanErr != nil {
		return 0, false, oops.Code("MIGRATION_ADOPT_PROBE_FAILED").
			With("operation", "read legacy version").
			With("legacy_table", legacyVersionTable).
			Wrap(scanErr)
	}
	if version < 0 {
		return 0, dirty, nil
	}
	return uint(version), dirty, nil
}

// seedAndArchive writes the derived bookkeeping and retires the legacy table in ONE
// transaction, so a failure anywhere leaves the database exactly as it was found.
func (m *Migrator) seedAndArchive(
	ctx context.Context,
	conn *sql.Conn,
	recorded uint,
	seed []uint,
	gooseExists bool,
) error {
	// goose owns the shape of its bookkeeping table and has changed it — the id
	// identity column is absent from older documentation. Ask goose for the DDL
	// rather than copying a literal that would silently rot.
	versionStore, err := database.NewStore(database.DialectPostgres, gooseVersionTable)
	if err != nil {
		return oops.Code("MIGRATION_ADOPT_SEED_FAILED").With("operation", "create version store").Wrap(err)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return oops.Code("MIGRATION_ADOPT_SEED_FAILED").With("operation", "begin transaction").Wrap(err)
	}
	defer func() {
		// Rollback after a successful Commit is a no-op returning ErrTxDone.
		_ = tx.Rollback() //nolint:errcheck // best-effort cleanup; the operation error takes precedence
	}()

	if !gooseExists {
		if createErr := versionStore.CreateVersionTable(ctx, tx); createErr != nil {
			return oops.Code("MIGRATION_ADOPT_SEED_FAILED").
				With("operation", "create version table").
				Wrap(createErr)
		}
	}

	// goose writes a version-0 row when it creates the table, and both Provider.up
	// and Provider.down return errMissingZeroVersion against a table that lacks it.
	if insertErr := versionStore.Insert(ctx, tx, database.InsertRequest{Version: 0}); insertErr != nil {
		return oops.Code("MIGRATION_ADOPT_SEED_FAILED").With("operation", "insert bootstrap row").Wrap(insertErr)
	}

	// ASCENDING, one row at a time, straight from the ascending slice.
	//
	// This loop is the single most consequential detail in the cutover.
	// goose_db_version.id is GENERATED BY DEFAULT AS IDENTITY and goose's
	// ListMigrations is ORDER BY id DESC — insertion order, not version order. So
	// the order rows go in here IS the order a later `migrate down` rolls them back.
	// A descending seed produces a ledger that satisfies every count, every
	// set-equality, and every min/max assertion while rolling back in the wrong
	// order. Do not sort, reverse, or route this through a map on the way to SQL.
	for _, v := range seed {
		if v > math.MaxInt64 {
			return oops.Code("MIGRATION_ADOPT_SEED_FAILED").
				With("version", v).
				Errorf("migration version %d exceeds the maximum representable version", v)
		}
		if insertErr := versionStore.Insert(ctx, tx, database.InsertRequest{Version: int64(v)}); insertErr != nil {
			return oops.Code("MIGRATION_ADOPT_SEED_FAILED").
				With("operation", "insert derived version").
				With("version", v).
				Wrap(insertErr)
		}
	}

	if _, renameErr := tx.ExecContext(ctx, archiveLegacyTableSQL); renameErr != nil {
		return oops.Code("MIGRATION_ADOPT_RENAME_FAILED").
			With("legacy_table", legacyVersionTable).
			With("archived_table", archivedVersionTable).
			Wrap(renameErr)
	}

	// Log BEFORE the commit. Nobody is watching this boot, so this line is the
	// entire audit trail for the phase's one irreversible write.
	slog.InfoContext(ctx, "adopting golang-migrate bookkeeping into goose",
		"recorded_version", recorded,
		"seeded_versions", len(seed),
		"archived_table", archivedVersionTable)

	if commitErr := tx.Commit(); commitErr != nil {
		return oops.Code("MIGRATION_ADOPT_SEED_FAILED").With("operation", "commit").Wrap(commitErr)
	}
	return nil
}

// derivedSeedVersions returns every embedded version at or below recorded, in the
// ascending order it received them.
//
// Why the whole set and not just the recorded version: golang-migrate recorded a
// single current version, never the applied set, so the set must be inferred and
// there is no other source for it. The inference is not symmetric.
// gooseutil.UpVersions treats any filesystem version below the database's maximum
// that is not recorded as MISSING — a hard error without -allow-missing, and a
// re-application with it. Seeding only the recorded version would therefore offer to
// re-run every lower migration against a database where they are already applied.
// Over-seeding is comparatively harmless: versions present in the database but
// absent from the filesystem are silently ignored. The danger is one-directional.
//
// Why ascending: the caller inserts in this order, goose_db_version.id is an
// identity column, and ListMigrations orders by id DESC — so this order becomes
// rollback order. See INV-STORE-10.
//
// The version gap in the embedded chain is handled by construction: only versions
// that actually exist in the filesystem are ever returned, so a recorded version
// sitting inside the gap yields the versions below the gap and nothing invented.
//
// all MUST already be ascending; allMigrationVersions sorts before returning.
func derivedSeedVersions(recorded uint, all []uint) []uint {
	var seed []uint
	for _, v := range all {
		if v <= recorded {
			seed = append(seed, v)
		}
	}
	return seed
}

// errAdoptRefusedDirty builds D-05's refusal. The version travels in the oops
// context under a named key, not only in the message, so it can be filtered on
// downstream rather than parsed out of prose.
func errAdoptRefusedDirty(version uint) error {
	return oops.Code("MIGRATION_ADOPT_REFUSED_DIRTY").
		With("dirty_version", version).
		With("legacy_table", legacyVersionTable).
		Errorf(
			"refusing to adopt golang-migrate bookkeeping: %s is marked dirty at version %d, "+
				"which means that migration was never fully applied; resolve it with the previous "+
				"migration tooling before starting this binary",
			legacyVersionTable, version,
		)
}
