// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/store"
)

// CLI output functions use //nolint:errcheck because stdout/stderr write errors
// cannot be meaningfully recovered - the user won't see the error message anyway.

// migrator abstracts store.Migrator for CLI command testing.
type migrator interface {
	Up() error
	Down() error
	Steps(n int) error
	Version() (uint, bool, error)
	Close() error
	PendingMigrations() ([]uint, error)
	AppliedMigrations() ([]uint, error)
	AdoptPreview() (store.AdoptPreview, error)
}

// runMigrateUpDryRun shows what migrations would be applied without running them.
//
//nolint:errcheck // CLI output errors intentionally ignored - no recovery possible
func runMigrateUpDryRun(out io.Writer, migrator migrator) error {
	// Ask about the cutover BEFORE reading anything out of goose. Version and
	// PendingMigrations both report goose's ledger, which on a not-yet-adopted
	// database is empty — so taken at face value they describe a database with no
	// migration history at all, and the preview becomes the OPPOSITE of what
	// `migrate up` does. A pre-adopt database is precisely the one an operator
	// previews before an irreversible cutover.
	preview, err := migrator.AdoptPreview()
	if err != nil {
		return oops.With("operation", "preview adopt").Wrap(err)
	}

	currentVersion, _, err := migrator.Version()
	if err != nil {
		return oops.With("operation", "get version").Wrap(err)
	}

	pending, err := migrator.PendingMigrations()
	if err != nil {
		return oops.With("operation", "list pending migrations").Wrap(err)
	}

	if preview.Pending {
		fmt.Fprintln(out, "This database still uses golang-migrate bookkeeping (schema_migrations).")
		fmt.Fprintf(out, "Recorded version: %d\n", preview.Recorded)

		if preview.Dirty {
			fmt.Fprintln(out, "That ledger is marked dirty, so `migrate up` will REFUSE and abort;")
			fmt.Fprintln(out, "no migration would be applied and no bookkeeping would be written.")
			fmt.Fprintln(out, "Resolve the dirty version with the previous migration tooling first.")
			return nil
		}

		fmt.Fprintln(out, "`migrate up` will first adopt it into goose — a one-shot, irreversible")
		fmt.Fprintln(out, "write — recording every migration up to that version as already applied.")
		fmt.Fprintln(out)

		// Re-base the figures on the recorded version. goose reports 0 and the whole
		// corpus as pending here; what actually gets applied is only what sits above
		// what the legacy ledger already accounts for.
		currentVersion = preview.Recorded
		var afterAdopt []uint
		for _, v := range pending {
			if v > preview.Recorded {
				afterAdopt = append(afterAdopt, v)
			}
		}
		pending = afterAdopt
	}

	if len(pending) == 0 {
		fmt.Fprintf(out, "Already at latest version: %d\n", currentVersion)
		fmt.Fprintln(out, "No migrations would be applied.")
		return nil
	}

	fmt.Fprintln(out, "Dry run - the following migrations would be applied:")
	for _, v := range pending {
		name, err := store.MigrationName(v)
		if err != nil {
			return oops.With("operation", "get migration name").With("version", v).Wrap(err)
		}
		if name != "" {
			fmt.Fprintf(out, "  - %s\n", name)
		} else {
			fmt.Fprintf(out, "  - Version %d\n", v)
		}
	}
	fmt.Fprintf(out, "\nCurrent version: %d\n", currentVersion)
	fmt.Fprintf(out, "Target version: %d\n", pending[len(pending)-1])
	return nil
}

// runMigrateDownDryRun shows what migrations would be rolled back without running them.
//
//nolint:errcheck // CLI output errors intentionally ignored - no recovery possible
func runMigrateDownDryRun(out io.Writer, migrator migrator, all bool) error {
	currentVersion, _, err := migrator.Version()
	if err != nil {
		return oops.With("operation", "get version").Wrap(err)
	}

	if currentVersion == 0 {
		fmt.Fprintln(out, "Already at version 0, no migrations to roll back.")
		return nil
	}

	applied, err := migrator.AppliedMigrations()
	if err != nil {
		return oops.With("operation", "list applied migrations").Wrap(err)
	}

	if len(applied) == 0 {
		fmt.Fprintln(out, "No migrations to roll back.")
		return nil
	}

	if all {
		fmt.Fprintln(out, "Dry run - the following migrations would be rolled back:")
		// Show in reverse order (most recent first)
		for i := len(applied) - 1; i >= 0; i-- {
			v := applied[i]
			name, err := store.MigrationName(v)
			if err != nil {
				return oops.With("operation", "get migration name").With("version", v).Wrap(err)
			}
			if name != "" {
				fmt.Fprintf(out, "  - %s\n", name)
			} else {
				fmt.Fprintf(out, "  - Version %d\n", v)
			}
		}
		fmt.Fprintf(out, "\nCurrent version: %d\n", currentVersion)
		fmt.Fprintln(out, "Target version: 0")
	} else {
		fmt.Fprintln(out, "Dry run - the following migration would be rolled back:")
		name, err := store.MigrationName(currentVersion)
		if err != nil {
			return oops.With("operation", "get migration name").With("version", currentVersion).Wrap(err)
		}
		if name != "" {
			fmt.Fprintf(out, "  - %s\n", name)
		} else {
			fmt.Fprintf(out, "  - Version %d\n", currentVersion)
		}
		targetVersion := uint(0)
		if len(applied) > 1 {
			targetVersion = applied[len(applied)-2]
		}
		fmt.Fprintf(out, "\nCurrent version: %d\n", currentVersion)
		fmt.Fprintf(out, "Target version: %d\n", targetVersion)
	}
	return nil
}

// runMigrateUpLogic handles the migrate up logic with output.
//
//nolint:errcheck // CLI output errors intentionally ignored - no recovery possible
func runMigrateUpLogic(out io.Writer, migrator migrator) error {
	// Get version before
	beforeVersion, _, err := migrator.Version()
	if err != nil {
		return oops.With("operation", "get version before").Wrap(err)
	}

	fmt.Fprintln(out, "Applying migrations...")
	if upErr := migrator.Up(); upErr != nil {
		return upErr
	}

	// Get version after
	afterVersion, _, err := migrator.Version()
	if err != nil {
		fmt.Fprintf(out, "Warning: migrations applied but failed to get version: %v\n", err)
		fmt.Fprintln(out, "Check status with 'holomush migrate status'")
		return oops.Code("MIGRATION_VERSION_CHECK_FAILED").With("operation", "verify migration result").Wrap(err)
	}

	if beforeVersion == afterVersion {
		fmt.Fprintf(out, "Already at latest version: %d\n", afterVersion)
	} else {
		fmt.Fprintf(out, "Migrated from version %d to %d\n", beforeVersion, afterVersion)
	}
	return nil
}

// runMigrateDownLogic handles the migrate down logic with output.
//
//nolint:errcheck // CLI output errors intentionally ignored - no recovery possible
func runMigrateDownLogic(out io.Writer, migrator migrator, all bool) error {
	// Get version before
	beforeVersion, _, err := migrator.Version()
	if err != nil {
		return oops.With("operation", "get version before").Wrap(err)
	}

	if all {
		fmt.Fprintln(out, "Rolling back all migrations...")
		if downErr := migrator.Down(); downErr != nil {
			return downErr
		}
	} else {
		fmt.Fprintln(out, "Rolling back one migration...")
		if stepsErr := migrator.Steps(-1); stepsErr != nil {
			return stepsErr
		}
	}

	// Get version after
	afterVersion, _, err := migrator.Version()
	if err != nil {
		fmt.Fprintf(out, "Warning: rollback applied but failed to get version: %v\n", err)
		fmt.Fprintln(out, "Check status with 'holomush migrate status'")
		return oops.Code("MIGRATION_VERSION_CHECK_FAILED").With("operation", "verify rollback result").Wrap(err)
	}

	if beforeVersion == afterVersion {
		fmt.Fprintf(out, "Already at version %d, no migrations to roll back\n", afterVersion)
	} else {
		fmt.Fprintf(out, "Rolled back from version %d to %d\n", beforeVersion, afterVersion)
	}
	return nil
}

// runMigrateStatusLogic handles the migrate status logic with output.
//
// goose commits each migration's body and its version row in a single transaction,
// so the partially-applied "dirty" condition golang-migrate reported cannot arise.
// There is consequently no DIRTY branch and no recovery subcommand to point at.
//
//nolint:errcheck // CLI output errors intentionally ignored - no recovery possible
func runMigrateStatusLogic(out io.Writer, migrator migrator) error {
	version, _, err := migrator.Version()
	if err != nil {
		return oops.With("operation", "get version").Wrap(err)
	}

	fmt.Fprintf(out, "Current version: %d\n", version)
	fmt.Fprintln(out, "Status: OK")
	return nil
}

// runMigrateVersionLogic handles the migrate version logic with output.
//
//nolint:errcheck // CLI output errors intentionally ignored - no recovery possible
func runMigrateVersionLogic(out io.Writer, migrator migrator) error {
	version, _, err := migrator.Version()
	if err != nil {
		return oops.With("operation", "get version").Wrap(err)
	}
	fmt.Fprintln(out, version)
	return nil
}

// NewMigrateCmd creates the migrate subcommand.
// When invoked without a subcommand, it defaults to running "migrate up".
//
// Design note: Each subcommand has similar boilerplate (getDatabaseURL, NewMigrator,
// defer Close). This was intentionally not extracted into a helper because:
// 1. Explicit code is more readable and each command is self-contained
// 2. Error wrapping includes command-specific context
// 3. Commands have subtle differences (dry-run, all flag) that complicate extraction
// 4. ~15 lines of duplication is acceptable for CLI commands
func NewMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration management",
		Long:  `Manage PostgreSQL database schema migrations using goose.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Default behavior: run migrate up
			url, err := getDatabaseURL()
			if err != nil {
				return oops.With("command", "migrate").Wrap(err)
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return oops.With("command", "migrate").Wrap(err)
			}
			defer func() {
				if closeErr := migrator.Close(); closeErr != nil {
					cmd.PrintErrf("Warning: failed to close migrator (connection may leak): %v\n", closeErr)
				}
			}()

			if err := runMigrateUpLogic(cmd.OutOrStdout(), migrator); err != nil {
				return oops.With("command", "migrate").Wrap(err)
			}
			return nil
		},
	}

	cmd.AddCommand(newMigrateUpCmd())
	cmd.AddCommand(newMigrateDownCmd())
	cmd.AddCommand(newMigrateStatusCmd())
	cmd.AddCommand(newMigrateVersionCmd())

	return cmd
}

func getDatabaseURL() (string, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return "", oops.Code("CONFIG_INVALID").Errorf("DATABASE_URL environment variable is required")
	}
	return url, nil
}

func newMigrateUpCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := getDatabaseURL()
			if err != nil {
				return oops.With("command", "migrate up").Wrap(err)
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return oops.With("command", "migrate up").Wrap(err)
			}
			defer func() {
				if closeErr := migrator.Close(); closeErr != nil {
					cmd.PrintErrf("Warning: failed to close migrator (connection may leak): %v\n", closeErr)
				}
			}()

			if dryRun {
				if err := runMigrateUpDryRun(cmd.OutOrStdout(), migrator); err != nil {
					return oops.With("command", "migrate up --dry-run").Wrap(err)
				}
				return nil
			}

			if err := runMigrateUpLogic(cmd.OutOrStdout(), migrator); err != nil {
				return oops.With("command", "migrate up").Wrap(err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what migrations would be applied without running them")
	return cmd
}

func newMigrateDownCmd() *cobra.Command {
	var all bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Rollback migrations",
		Long:  `Rollback one migration, or all migrations with --all flag.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := getDatabaseURL()
			if err != nil {
				return oops.With("command", "migrate down").Wrap(err)
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return oops.With("command", "migrate down").Wrap(err)
			}
			defer func() {
				if closeErr := migrator.Close(); closeErr != nil {
					cmd.PrintErrf("Warning: failed to close migrator (connection may leak): %v\n", closeErr)
				}
			}()

			if dryRun {
				if err := runMigrateDownDryRun(cmd.OutOrStdout(), migrator, all); err != nil {
					return oops.With("command", "migrate down --dry-run").Wrap(err)
				}
				return nil
			}

			if err := runMigrateDownLogic(cmd.OutOrStdout(), migrator, all); err != nil {
				return oops.With("command", "migrate down").Wrap(err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Rollback all migrations")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what migrations would be rolled back without running them")
	return cmd
}

func newMigrateStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := getDatabaseURL()
			if err != nil {
				return oops.With("command", "migrate status").Wrap(err)
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return oops.With("command", "migrate status").Wrap(err)
			}
			defer func() {
				if closeErr := migrator.Close(); closeErr != nil {
					cmd.PrintErrf("Warning: failed to close migrator (connection may leak): %v\n", closeErr)
				}
			}()

			if err := runMigrateStatusLogic(cmd.OutOrStdout(), migrator); err != nil {
				return oops.With("command", "migrate status").Wrap(err)
			}
			return nil
		},
	}
}

func newMigrateVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print current schema version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, err := getDatabaseURL()
			if err != nil {
				return oops.With("command", "migrate version").Wrap(err)
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return oops.With("command", "migrate version").Wrap(err)
			}
			defer func() {
				if closeErr := migrator.Close(); closeErr != nil {
					cmd.PrintErrf("Warning: failed to close migrator (connection may leak): %v\n", closeErr)
				}
			}()

			if err := runMigrateVersionLogic(cmd.OutOrStdout(), migrator); err != nil {
				return oops.With("command", "migrate version").Wrap(err)
			}
			return nil
		},
	}
}
