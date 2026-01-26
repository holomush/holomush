// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/golang-migrate/migrate/v4"
	// Register pgx/v5 database driver for golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/samber/oops"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

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

// migrateIface abstracts golang-migrate for testing. The real golang-migrate
// library requires a database connection, making unit tests slow and brittle.
// This interface allows mocking migration operations without a database.
type migrateIface interface {
	Up() error
	Down() error
	Steps(n int) error
	Version() (version uint, dirty bool, err error)
	Force(version int) error
	Close() (source error, database error)
}

// Migrator wraps golang-migrate for database schema management.
//
// IMPORTANT: Migrator is NOT safe for concurrent use. Each instance
// should be used from a single goroutine and must not be copied.
// For concurrent scenarios, create separate Migrator instances.
type Migrator struct {
	m migrateIface
}

// NewMigrator creates a new Migrator instance.
// The databaseURL should be a PostgreSQL connection string with either
// postgres:// or pgx5:// scheme. The function automatically converts
// postgres:// to pgx5:// for golang-migrate compatibility.
func NewMigrator(databaseURL string) (*Migrator, error) {
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, oops.Code("MIGRATION_SOURCE_FAILED").With("operation", "create migration source").Wrap(err)
	}

	// Convert postgres:// or postgresql:// to pgx5:// for golang-migrate pgx/v5 driver.
	// The pgx/v5 driver expects the pgx5:// scheme.
	migrateURL := databaseURL
	if rest, found := strings.CutPrefix(databaseURL, "postgres://"); found {
		migrateURL = "pgx5://" + rest
	} else if rest, found := strings.CutPrefix(databaseURL, "postgresql://"); found {
		migrateURL = "pgx5://" + rest
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, migrateURL)
	if err != nil {
		_ = source.Close() //nolint:errcheck // cleanup for embedded FS; init error takes precedence
		return nil, oops.Code("MIGRATION_INIT_FAILED").With("operation", "initialize migrator").Wrap(err)
	}

	return &Migrator{m: m}, nil
}

// Up applies all pending migrations.
func (m *Migrator) Up() error {
	if err := m.m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return oops.Code("MIGRATION_UP_FAILED").Wrap(err)
	}
	return nil
}

// Down rolls back all migrations to version 0, effectively removing all schema objects.
// WARNING: This is a destructive operation that drops all tables and data.
func (m *Migrator) Down() error {
	if err := m.m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return oops.Code("MIGRATION_DOWN_FAILED").Wrap(err)
	}
	return nil
}

// Steps applies n migrations. Positive n migrates up, negative n migrates down.
func (m *Migrator) Steps(n int) error {
	if err := m.m.Steps(n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return oops.Code("MIGRATION_STEPS_FAILED").With("steps", n).Wrap(err)
	}
	return nil
}

// Version returns the current migration version and dirty state.
// A dirty state indicates a migration failed partway through and requires manual intervention.
// Returns version 0 with dirty=false if no migrations have been applied.
func (m *Migrator) Version() (version uint, dirty bool, err error) {
	version, dirty, err = m.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, oops.Code("MIGRATION_VERSION_FAILED").Wrap(err)
	}
	return version, dirty, nil
}

// Force sets the migration version without running migrations.
// Use only for recovering from a dirty state after manually fixing the database.
// Version must be non-negative; negative values are rejected with INVALID_VERSION error.
// WARNING: Setting an incorrect version causes the migrator to skip migrations
// (if too high) or re-run already-applied migrations (if too low), potentially
// causing data loss or duplicate data.
//
// NOTE: The CLI also validates version (defense-in-depth). This store-layer check is
// authoritative and ensures the invariant holds regardless of entry point.
func (m *Migrator) Force(version int) error {
	// Defense-in-depth: CLI also validates, but we enforce here as the authoritative layer.
	if version < 0 {
		return oops.Code("INVALID_VERSION").Errorf("version must be non-negative, got %d", version)
	}
	if err := m.m.Force(version); err != nil {
		return oops.Code("MIGRATION_FORCE_FAILED").With("version", version).Wrap(err)
	}
	return nil
}

// Close releases resources.
func (m *Migrator) Close() error {
	srcErr, dbErr := m.m.Close()
	if srcErr != nil && dbErr != nil {
		// Both failed - combine errors so neither is lost in logs
		return oops.Code("MIGRATION_CLOSE_FAILED").
			With("component", "both").
			Errorf("source: %v; database: %v", srcErr, dbErr)
	}
	if srcErr != nil {
		return oops.Code("MIGRATION_CLOSE_FAILED").With("component", "source").Wrap(srcErr)
	}
	if dbErr != nil {
		return oops.Code("MIGRATION_CLOSE_FAILED").With("component", "database").Wrap(dbErr)
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
// are validated at compile time by TestMigrationsFS_EmbeddedFiles in migrate_embed_test.go,
// which ensures all files follow the NNNNNN_name.(up|down).sql pattern.
func loadMigrationVersions() ([]uint, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, oops.Code("MIGRATION_LIST_FAILED").With("operation", "read migrations dir").Wrap(err)
	}

	versionSet := make(map[uint]struct{})
	for _, entry := range entries {
		name := entry.Name()
		// Parse version from filename (e.g., "000003_foo.up.sql" -> 3)
		if !strings.HasSuffix(name, ".up.sql") {
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
				"expected_format", "NNNNNN_name.up.sql",
				"error", err)
			continue
		}
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
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		var version uint
		if _, err := fmt.Sscanf(name, "%06d", &version); err != nil {
			continue // Skip malformed files (same behavior as loadMigrationVersions)
		}
		// Store name without .up.sql suffix
		names[version] = strings.TrimSuffix(name, ".up.sql")
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
