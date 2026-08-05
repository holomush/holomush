// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	// Register the pgx/v5 database/sql driver under the name "pgx".
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/samber/oops"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// pingTimeout bounds the connectivity check in NewMigrator so a bad DSN or an
// unreachable host fails fast with MIGRATION_INIT_FAILED rather than hanging.
const pingTimeout = 5 * time.Second

// Cached migration versions - computed once since embedded FS is immutable.
var (
	cachedVersionsOnce sync.Once
	cachedVersions     []uint
	cachedVersionsErr  error
)

// Cached migration names - computed once since embedded FS is immutable.
var (
	cachedNamesOnce sync.Once
	cachedNames     map[uint]string // version -> full name (e.g., "000001_initial")
	cachedNamesErr  error
)

// provIface abstracts the subset of the goose *Provider method set this package
// uses, so unit tests can exercise Migrator without a database. It is the direct
// successor to the golang-migrate-era migrateIface seam and exists for the same
// reason: a real provider requires a live connection, which makes unit tests slow
// and brittle.
type provIface interface {
	Up(ctx context.Context) ([]*goose.MigrationResult, error)
	UpByOne(ctx context.Context) (*goose.MigrationResult, error)
	UpTo(ctx context.Context, version int64) ([]*goose.MigrationResult, error)
	Down(ctx context.Context) (*goose.MigrationResult, error)
	DownTo(ctx context.Context, version int64) ([]*goose.MigrationResult, error)
	GetDBVersion(ctx context.Context) (int64, error)
	Status(ctx context.Context) ([]*goose.MigrationStatus, error)
	Close() error
}

// Migrator wraps goose for database schema management.
//
// IMPORTANT: Migrator is NOT safe for concurrent use. Each instance
// should be used from a single goroutine and must not be copied.
// For concurrent scenarios, create separate Migrator instances.
type Migrator struct {
	m provIface

	// db is the same handle the goose provider was built on. It is held so the
	// adopt gate can take a session advisory lock and run its transaction on a
	// pinned connection. Unit tests construct a Migrator around a mock provider
	// and leave this nil, in which case adopt has nothing to inspect and no-ops.
	db *sql.DB
}

// NewMigrator creates a new Migrator instance.
//
// The databaseURL is a PostgreSQL connection string; both postgres:// and
// postgresql:// are accepted natively by the pgx/v5 driver. The golang-migrate-era
// rewrite to a pgx5:// scheme is gone — goose talks to a plain *sql.DB.
//
// Cross-process serialization is provided by goose's PostgreSQL session-advisory-lock
// locker, so two binaries starting at once cannot interleave migrations.
func NewMigrator(databaseURL string) (*Migrator, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, oops.Code("MIGRATION_INIT_FAILED").With("operation", "open database").Wrap(err)
	}

	// sql.Open is lazy: it validates nothing. Ping so an unusable DSN fails here,
	// where callers already expect MIGRATION_INIT_FAILED, rather than at first use.
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close() //nolint:errcheck // cleanup on the init error path; init error takes precedence
		return nil, oops.Code("MIGRATION_INIT_FAILED").With("operation", "connect to database").Wrap(pingErr)
	}

	// goose globs *.sql at the ROOT of the fsys it is handed and does not recurse,
	// so the embedded FS must be re-rooted at migrations/. Handing it migrationsFS
	// directly yields a silent ErrNoMigrations.
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		_ = db.Close() //nolint:errcheck // cleanup on the init error path; init error takes precedence
		return nil, oops.Code("MIGRATION_SOURCE_FAILED").With("operation", "sub migrations fs").Wrap(err)
	}

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		_ = db.Close() //nolint:errcheck // cleanup on the init error path; init error takes precedence
		return nil, oops.Code("MIGRATION_INIT_FAILED").With("operation", "create session locker").Wrap(err)
	}

	// WithVerbose(true) is required alongside WithSlog: Provider.logf returns
	// immediately when verbose is false, so the logger would never be called.
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		sub,
		goose.WithSessionLocker(locker),
		goose.WithSlog(slog.Default()),
		goose.WithVerbose(true),
	)
	if err != nil {
		_ = db.Close() //nolint:errcheck // cleanup on the init error path; init error takes precedence
		return nil, oops.Code("MIGRATION_INIT_FAILED").With("operation", "initialize migrator").Wrap(err)
	}

	return &Migrator{m: provider, db: db}, nil
}

// Up applies all pending migrations.
//
// Up fires the one-shot cutover from golang-migrate bookkeeping to goose
// bookkeeping, as does every other entry point that migrates UPWARD (Steps with a
// positive n, and Migrate). The read-only verbs deliberately do not: a diagnostic
// run to decide whether to deploy must not itself perform the deploy's
// irreversible step. See adoptFromGolangMigrate.
func (m *Migrator) Up() error {
	ctx := context.Background()

	// Returned unwrapped on purpose. The adopt errors already carry their own
	// MIGRATION_ADOPT_* codes, and re-wrapping in MIGRATION_UP_FAILED would replace
	// the top-level code that callers and tests assert on with a generic one.
	if err := m.adoptFromGolangMigrate(ctx); err != nil {
		return err
	}

	if _, err := m.m.Up(ctx); err != nil {
		return oops.Code("MIGRATION_UP_FAILED").Wrap(err)
	}
	return nil
}

// Down rolls back all migrations to version 0, effectively removing all schema objects.
// WARNING: This is a destructive operation that drops all tables and data.
func (m *Migrator) Down() error {
	if _, err := m.m.DownTo(context.Background(), 0); err != nil {
		return oops.Code("MIGRATION_DOWN_FAILED").Wrap(err)
	}
	return nil
}

// Steps applies n migrations. Positive n migrates up, negative n migrates down.
// Steps(0) is a no-op.
//
// goose's ErrNoNextVersion (nothing left to apply or roll back) is the analogue of
// golang-migrate's ErrNoChange, which this wrapper has always treated as success —
// the CLI's `migrate down` relies on it to print "already at version N" rather than
// failing when the database is already at zero.
//
// A POSITIVE n fires the adopt gate first, exactly as Up does: it is an upward
// migration path, not a diagnostic, and against a pre-adopt database goose would
// otherwise read its empty ledger as version 0 and start re-applying migration 1
// over a schema that is already fully present. A NEGATIVE n does not: rolling a
// pre-adopt database back is not a supported operation, and firing an irreversible
// forward cutover from a rollback request would be a surprise. See
// adoptFromGolangMigrate.
func (m *Migrator) Steps(n int) error {
	if n == 0 {
		return nil
	}
	ctx := context.Background()

	if n > 0 {
		// Returned unwrapped, matching Up: the adopt errors carry their own
		// MIGRATION_ADOPT_* codes and must not be masked by MIGRATION_STEPS_FAILED.
		if err := m.adoptFromGolangMigrate(ctx); err != nil {
			return err
		}
	}

	count := n
	if count < 0 {
		count = -count
	}
	for range count {
		var stepErr error
		if n > 0 {
			_, stepErr = m.m.UpByOne(ctx)
		} else {
			_, stepErr = m.m.Down(ctx)
		}
		if stepErr != nil {
			if errors.Is(stepErr, goose.ErrNoNextVersion) {
				return nil
			}
			return oops.Code("MIGRATION_STEPS_FAILED").With("steps", n).Wrap(stepErr)
		}
	}
	return nil
}

// Migrate migrates to the specified version. If the current version is
// higher, it runs down migrations; if lower, up migrations. This is more
// stable than Steps(-N) for targeting a specific schema state because it
// does not depend on knowing how many migrations exist above the target.
//
// Migrate fires the adopt gate BEFORE reading the current version, and the
// ordering is the point: the direction is decided from that read. Against a
// pre-adopt database an ungated read returns 0, so a request to move DOWN to a
// target is mistaken for a request to move UP to it — the up branch then either
// silently does nothing or re-applies migration 1 over a schema that already
// carries it. Unlike Steps, Migrate cannot know it is a rollback until it has
// looked, so the gate cannot be scoped to the upward branch. See
// adoptFromGolangMigrate.
func (m *Migrator) Migrate(version uint) error {
	ctx := context.Background()

	// Returned unwrapped, matching Up: the adopt errors carry their own
	// MIGRATION_ADOPT_* codes and must not be masked by MIGRATION_MIGRATE_FAILED.
	if err := m.adoptFromGolangMigrate(ctx); err != nil {
		return err
	}

	// uint is 64-bit on every platform this builds for, so a caller could in
	// principle hand over a value with no int64 representation. Reject it rather
	// than wrapping around into a negative target version, which DownTo would
	// then reject with a far less obvious message.
	if version > math.MaxInt64 {
		return oops.Code("MIGRATION_MIGRATE_FAILED").
			With("target_version", version).
			Errorf("target version %d exceeds the maximum representable migration version", version)
	}
	target := int64(version)

	current, err := m.m.GetDBVersion(ctx)
	if err != nil {
		return oops.Code("MIGRATION_MIGRATE_FAILED").With("target_version", version).Wrap(err)
	}

	switch {
	case current < target:
		if _, err := m.m.UpTo(ctx, target); err != nil {
			return oops.Code("MIGRATION_MIGRATE_FAILED").With("target_version", version).Wrap(err)
		}
	case current > target:
		if _, err := m.m.DownTo(ctx, target); err != nil {
			return oops.Code("MIGRATION_MIGRATE_FAILED").With("target_version", version).Wrap(err)
		}
	}
	return nil
}

// Version returns the current migration version.
//
// The second return value is always false: goose has no dirty state. It commits a
// migration's body and its version row in one transaction, so the partially-applied
// condition golang-migrate's dirty flag reported cannot arise. The value is retained
// this release so the ~25 existing call sites and the CLI keep compiling; callers
// should not branch on it.
//
// Returns version 0 when no migrations have been applied.
func (m *Migrator) Version() (version uint, dirty bool, err error) {
	v, err := m.m.GetDBVersion(context.Background())
	if err != nil {
		return 0, false, oops.Code("MIGRATION_VERSION_FAILED").Wrap(err)
	}
	if v < 0 {
		return 0, false, nil
	}
	return uint(v), false, nil
}

// Close releases the database connection held by the migrator.
// It is safe to call more than once.
func (m *Migrator) Close() error {
	if err := m.m.Close(); err != nil {
		return oops.Code("MIGRATION_CLOSE_FAILED").With("component", "database").Wrap(err)
	}
	return nil
}

// allMigrationVersions returns all available migration versions from the embedded FS.
// Results are cached and a defensive copy is returned.
func allMigrationVersions() ([]uint, error) {
	cachedVersionsOnce.Do(func() {
		cachedVersions, cachedVersionsErr = loadMigrationVersions()
	})
	if cachedVersionsErr != nil {
		return nil, cachedVersionsErr
	}
	// Return a copy to prevent callers from mutating the cache.
	result := make([]uint, len(cachedVersions))
	copy(result, cachedVersions)
	return result, nil
}

// loadMigrationVersions reads the embedded migrations directory and parses version numbers.
//
// Design note: Malformed filenames are logged and skipped rather than causing failures.
// This is intentional - we don't want to fail on unexpected files in the embedded FS
// (e.g., .gitkeep, editor temp files that might slip through). The embedded migrations
// are validated at compile time by the filename meta-test in migrate_embed_test.go,
// which ensures all files follow the NNNNNN_name.sql pattern.
func loadMigrationVersions() ([]uint, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, oops.Code("MIGRATION_LIST_FAILED").With("operation", "read migrations dir").Wrap(err)
	}

	versionSet := make(map[uint]struct{})
	for _, entry := range entries {
		name := entry.Name()
		// Parse version from filename (e.g., "000003_foo.sql" -> 3)
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		var version uint
		if _, err := fmt.Sscanf(name, "%06d", &version); err != nil {
			// Intentionally skip malformed files rather than failing - see function doc above.
			// Note: Using global slog is acceptable here because:
			// 1. This runs once at init time via sync.Once (not per-request)
			// 2. Malformed files are prevented by compile-time test validation
			// 3. Adding logger DI would require threading it through multiple layers
			slog.Warn("migration file name doesn't match expected format, skipping",
				"filename", name,
				"expected_format", "NNNNNN_name.sql",
				"error", err)
			continue
		}
		versionSet[version] = struct{}{}
	}

	// Go migrations are NOT in the embedded FS (the glob is `.sql`-only), so
	// they are merged in from the literal census. Omitting them makes the adopt
	// gate seed a ledger with a hole in it. See go_migration_census.go.
	for version := range goMigrationNames {
		versionSet[version] = struct{}{}
	}

	versions := make([]uint, 0, len(versionSet))
	for v := range versionSet {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions, nil
}

// loadMigrationNames reads the embedded migrations directory and builds a version-to-name map.
func loadMigrationNames() (map[uint]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, oops.Code("MIGRATION_READ_FAILED").With("operation", "read migrations dir").Wrap(err)
	}

	names := make(map[uint]string)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		var version uint
		if _, err := fmt.Sscanf(name, "%06d", &version); err != nil {
			continue // Skip malformed files (same behavior as loadMigrationVersions)
		}
		// Store name without the .sql suffix
		names[version] = strings.TrimSuffix(name, ".sql")
	}
	// Go migrations, which the `.sql`-only embed glob cannot see.
	for version, name := range goMigrationNames {
		names[version] = name
	}
	return names, nil
}

// MigrationName returns the name of a migration by version number.
// Results are cached since the embedded filesystem is immutable at runtime.
//
// Returns:
//   - (name, nil) when version is found
//   - ("", nil) when version is not found (expected behavior)
//   - ("", error) when embedded FS cannot be read (indicates corruption)
func MigrationName(version uint) (string, error) {
	cachedNamesOnce.Do(func() {
		cachedNames, cachedNamesErr = loadMigrationNames()
	})
	if cachedNamesErr != nil {
		return "", cachedNamesErr
	}

	// O(1) lookup from cache
	name := cachedNames[version]
	return name, nil
}

// PendingMigrations returns the list of migration versions that would be applied
// when running Up(). Returns versions sorted in ascending order.
func (m *Migrator) PendingMigrations() ([]uint, error) {
	currentVersion, _, err := m.Version()
	if err != nil {
		return nil, oops.With("operation", "get pending migrations").Wrap(err)
	}

	allVersions, err := allMigrationVersions()
	if err != nil {
		return nil, oops.With("operation", "get pending migrations").Wrap(err)
	}

	var pending []uint
	for _, v := range allVersions {
		if v > currentVersion {
			pending = append(pending, v)
		}
	}
	return pending, nil
}

// AppliedMigrations returns the list of migration versions that have been applied.
// Returns versions sorted in ascending order.
func (m *Migrator) AppliedMigrations() ([]uint, error) {
	currentVersion, _, err := m.Version()
	if err != nil {
		return nil, oops.With("operation", "get applied migrations").Wrap(err)
	}

	if currentVersion == 0 {
		return nil, nil
	}

	allVersions, err := allMigrationVersions()
	if err != nil {
		return nil, oops.With("operation", "get applied migrations").Wrap(err)
	}

	var applied []uint
	for _, v := range allVersions {
		if v <= currentVersion {
			applied = append(applied, v)
		}
	}
	return applied, nil
}
