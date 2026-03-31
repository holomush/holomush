// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// AutoMigrator runs database migrations.
type AutoMigrator interface {
	Up() error
	Close() error
}

// MigrationBootstrapper wraps database migration as a BootstrapPlugin.
type MigrationBootstrapper struct {
	databaseURL     string
	migratorFactory func(string) (AutoMigrator, error)
	enabled         bool
}

// Compile-time check.
var _ plugins.BootstrapPlugin = (*MigrationBootstrapper)(nil)

// NewMigrationBootstrapper creates a new MigrationBootstrapper.
func NewMigrationBootstrapper(databaseURL string, factory func(string) (AutoMigrator, error), enabled bool) *MigrationBootstrapper {
	return &MigrationBootstrapper{
		databaseURL:     databaseURL,
		migratorFactory: factory,
		enabled:         enabled,
	}
}

// Priority returns the bootstrap priority for schema initialization.
func (b *MigrationBootstrapper) Priority() int {
	return plugins.BootstrapPrioritySchema
}

// Bootstrap runs database migrations if enabled.
func (b *MigrationBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error {
	if !b.enabled {
		slog.InfoContext(ctx, "auto-migration disabled, skipping")
		return nil
	}

	slog.InfoContext(ctx, "running auto-migration")

	migrator, err := b.migratorFactory(b.databaseURL)
	if err != nil {
		return oops.Code("MIGRATION_INIT_FAILED").With("operation", "create migrator").Wrap(err)
	}

	defer func() {
		if closeErr := migrator.Close(); closeErr != nil {
			slog.WarnContext(ctx, "error closing migrator", "error", closeErr)
		}
	}()

	if err := migrator.Up(); err != nil {
		return oops.Code("AUTO_MIGRATION_FAILED").With("operation", "run migrations").Wrap(err)
	}

	slog.InfoContext(ctx, "auto-migration complete")
	return nil
}
