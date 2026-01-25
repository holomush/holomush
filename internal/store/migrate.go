// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	// Register pgx/v5 database driver for golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/samber/oops"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateIface abstracts golang-migrate for testing.
type migrateIface interface {
	Up() error
	Down() error
	Steps(n int) error
	Version() (version uint, dirty bool, err error)
	Force(version int) error
	Close() (source error, database error)
}

// Migrator wraps golang-migrate for database schema management.
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

	// Convert postgres:// to pgx5:// for golang-migrate pgx/v5 driver.
	// The pgx/v5 driver expects the pgx5:// scheme.
	migrateURL := databaseURL
	if rest, found := strings.CutPrefix(databaseURL, "postgres://"); found {
		migrateURL = "pgx5://" + rest
	} else if rest, found := strings.CutPrefix(databaseURL, "postgresql://"); found {
		migrateURL = "pgx5://" + rest
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, migrateURL)
	if err != nil {
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
// WARNING: Setting an incorrect version can corrupt database state tracking.
func (m *Migrator) Force(version int) error {
	if err := m.m.Force(version); err != nil {
		return oops.Code("MIGRATION_FORCE_FAILED").With("version", version).Wrap(err)
	}
	return nil
}

// Close releases resources.
func (m *Migrator) Close() error {
	srcErr, dbErr := m.m.Close()
	if srcErr != nil {
		return oops.Code("MIGRATION_CLOSE_FAILED").With("component", "source").Wrap(srcErr)
	}
	if dbErr != nil {
		return oops.Code("MIGRATION_CLOSE_FAILED").With("component", "database").Wrap(dbErr)
	}
	return nil
}

// allMigrationVersions reads the embedded migrations and returns all available versions.
// Versions are returned sorted in ascending order.
func allMigrationVersions() ([]uint, error) {
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

// PendingMigrations returns the list of migration versions that would be applied
// when running Up(). Returns versions sorted in ascending order.
func (m *Migrator) PendingMigrations() ([]uint, error) {
	currentVersion, _, err := m.Version()
	if err != nil {
		return nil, err
	}

	allVersions, err := allMigrationVersions()
	if err != nil {
		return nil, err
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
		return nil, err
	}

	if currentVersion == 0 {
		return nil, nil
	}

	allVersions, err := allMigrationVersions()
	if err != nil {
		return nil, err
	}

	var applied []uint
	for _, v := range allVersions {
		if v <= currentVersion {
			applied = append(applied, v)
		}
	}
	return applied, nil
}
