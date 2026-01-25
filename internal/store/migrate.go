// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"embed"
	"errors"

	"github.com/golang-migrate/migrate/v4"
	// Register pgx/v5 database driver for golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/samber/oops"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrator wraps golang-migrate for database schema management.
type Migrator struct {
	m *migrate.Migrate
}

// NewMigrator creates a new Migrator instance.
// The databaseURL should be a PostgreSQL connection string.
func NewMigrator(databaseURL string) (*Migrator, error) {
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, oops.Code("MIGRATION_SOURCE_FAILED").Wrap(err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return nil, oops.Code("MIGRATION_INIT_FAILED").Wrap(err)
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

// Down rolls back all migrations to version 0.
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
