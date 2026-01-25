// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"os"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/store"
)

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
				return err
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return err
			}
			defer func() { _ = migrator.Close() }() //nolint:errcheck // cleanup on exit

			cmd.Println("Applying migrations...")
			if err := migrator.Up(); err != nil {
				return err
			}

			version, _, versionErr := migrator.Version()
			if versionErr != nil {
				return versionErr
			}
			cmd.Printf("Migrations complete. Current version: %d\n", version)
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
				return err
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return err
			}
			defer func() { _ = migrator.Close() }() //nolint:errcheck // cleanup on exit

			if all {
				cmd.Println("Rolling back all migrations...")
				if err := migrator.Down(); err != nil {
					return err
				}
			} else {
				cmd.Println("Rolling back one migration...")
				if err := migrator.Steps(-1); err != nil {
					return err
				}
			}

			version, _, versionErr := migrator.Version()
			if versionErr != nil {
				return versionErr
			}
			cmd.Printf("Rollback complete. Current version: %d\n", version)
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
				return err
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return err
			}
			defer func() { _ = migrator.Close() }() //nolint:errcheck // cleanup on exit

			version, dirty, err := migrator.Version()
			if err != nil {
				return err
			}

			cmd.Printf("Current version: %d\n", version)
			if dirty {
				cmd.Println("Status: DIRTY (migration failed, manual intervention required)")
				cmd.Println("Use 'holomush migrate force VERSION' to reset")
			} else {
				cmd.Println("Status: OK")
			}
			return nil
		},
	}
}

func newMigrateVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print current schema version",
		RunE: func(_ *cobra.Command, _ []string) error {
			url, err := getDatabaseURL()
			if err != nil {
				return err
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return err
			}
			defer func() { _ = migrator.Close() }() //nolint:errcheck // cleanup on exit

			version, _, err := migrator.Version()
			if err != nil {
				return err
			}

			fmt.Println(version)
			return nil
		},
	}
}

func newMigrateForceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "force VERSION",
		Short: "Force set migration version (for dirty state recovery)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := getDatabaseURL()
			if err != nil {
				return err
			}

			var version int
			if _, scanErr := fmt.Sscanf(args[0], "%d", &version); scanErr != nil {
				return oops.Code("INVALID_VERSION").Errorf("invalid version: %s", args[0])
			}

			migrator, err := store.NewMigrator(url)
			if err != nil {
				return err
			}
			defer func() { _ = migrator.Close() }() //nolint:errcheck // cleanup on exit

			cmd.Printf("Forcing version to %d...\n", version)
			if err := migrator.Force(version); err != nil {
				return err
			}

			cmd.Println("Version forced successfully")
			return nil
		},
	}
}
