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

// MigratorIface abstracts the Migrator for testing.
type MigratorIface interface {
	Up() error
	Down() error
	Steps(n int) error
	Version() (uint, bool, error)
	Force(version int) error
	Close() error
}

// runMigrateUpLogic handles the migrate up logic with output.
//
//nolint:errcheck // CLI output errors are intentionally ignored - no recovery possible
func runMigrateUpLogic(out io.Writer, migrator MigratorIface) error {
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
		return nil
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
//nolint:errcheck // CLI output errors are intentionally ignored - no recovery possible
func runMigrateDownLogic(out io.Writer, migrator MigratorIface, all bool) error {
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
		return nil
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
//nolint:errcheck // CLI output errors are intentionally ignored - no recovery possible
func runMigrateStatusLogic(out io.Writer, migrator MigratorIface) error {
	version, dirty, err := migrator.Version()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Current version: %d\n", version)
	if dirty {
		fmt.Fprintln(out, "Status: DIRTY (migration failed, manual intervention required)")
		fmt.Fprintln(out, "Use 'holomush migrate force VERSION' to reset")
	} else {
		fmt.Fprintln(out, "Status: OK")
	}
	return nil
}

// runMigrateVersionLogic handles the migrate version logic with output.
//
//nolint:errcheck // CLI output errors are intentionally ignored - no recovery possible
func runMigrateVersionLogic(out io.Writer, migrator MigratorIface) error {
	version, _, err := migrator.Version()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, version)
	return nil
}

// runMigrateForceLogic handles the migrate force logic with output.
//
//nolint:errcheck // CLI output errors are intentionally ignored - no recovery possible
func runMigrateForceLogic(out io.Writer, migrator MigratorIface, version int) error {
	fmt.Fprintf(out, "Forcing version to %d...\n", version)
	if err := migrator.Force(version); err != nil {
		return err
	}
	fmt.Fprintln(out, "Version forced successfully")
	return nil
}

// NewMigrateCmd creates the migrate subcommand.
func NewMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration management",
		Long:  `Manage PostgreSQL database schema migrations using golang-migrate.`,
	}

	cmd.AddCommand(newMigrateUpCmd())
	cmd.AddCommand(newMigrateDownCmd())
	cmd.AddCommand(newMigrateStatusCmd())
	cmd.AddCommand(newMigrateVersionCmd())
	cmd.AddCommand(newMigrateForceCmd())

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
	return &cobra.Command{
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
					cmd.PrintErrf("Warning: failed to close migrator: %v\n", closeErr)
				}
			}()

			if err := runMigrateUpLogic(cmd.OutOrStdout(), migrator); err != nil {
				return oops.With("command", "migrate up").Wrap(err)
			}
			return nil
		},
	}
}

func newMigrateDownCmd() *cobra.Command {
	var all bool

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
					cmd.PrintErrf("Warning: failed to close migrator: %v\n", closeErr)
				}
			}()

			if err := runMigrateDownLogic(cmd.OutOrStdout(), migrator, all); err != nil {
				return oops.With("command", "migrate down").Wrap(err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Rollback all migrations")
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
					cmd.PrintErrf("Warning: failed to close migrator: %v\n", closeErr)
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
					cmd.PrintErrf("Warning: failed to close migrator: %v\n", closeErr)
				}
			}()

			if err := runMigrateVersionLogic(cmd.OutOrStdout(), migrator); err != nil {
				return oops.With("command", "migrate version").Wrap(err)
			}
			return nil
		},
	}
}

// parseForceVersion parses a version string for the migrate force command.
// Note: fmt.Sscanf stops at the first non-digit, so "3abc" parses as 3.
func parseForceVersion(arg string) (int, error) {
	var version int
	if _, err := fmt.Sscanf(arg, "%d", &version); err != nil {
		return 0, oops.Code("INVALID_VERSION").Errorf("invalid version: %s", arg)
	}
	return version, nil
}

func newMigrateForceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "force VERSION",
		Short: "Force set migration version (for dirty state recovery)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := getDatabaseURL()
			if err != nil {
				return oops.With("command", "migrate force").Wrap(err)
			}

			version, err := parseForceVersion(args[0])
			if err != nil {
				return oops.With("command", "migrate force").Wrap(err)
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return oops.With("command", "migrate force").Wrap(err)
			}
			defer func() {
				if closeErr := migrator.Close(); closeErr != nil {
					cmd.PrintErrf("Warning: failed to close migrator: %v\n", closeErr)
				}
			}()

			if err := runMigrateForceLogic(cmd.OutOrStdout(), migrator, version); err != nil {
				return oops.With("command", "migrate force").Wrap(err)
			}
			return nil
		},
	}
}
