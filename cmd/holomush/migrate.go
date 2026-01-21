// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"os"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/store"
)

// NewMigrateCmd creates the migrate subcommand.
func NewMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations",
		Long:  `Run all pending database migrations against the PostgreSQL database.`,
		RunE:  runMigrate,
	}
}

func runMigrate(cmd *cobra.Command, _ []string) error {
	// Get database URL from environment (config file support in later phase)
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return oops.Code("CONFIG_INVALID").Errorf("DATABASE_URL environment variable is required")
	}

	ctx := context.Background()

	cmd.Println("Connecting to database...")
	eventStore, err := store.NewPostgresEventStore(ctx, databaseURL)
	if err != nil {
		return oops.Code("DB_CONNECT_FAILED").With("operation", "connect to database").Wrap(err)
	}
	defer eventStore.Close()

	cmd.Println("Running migrations...")
	if err := eventStore.Migrate(ctx); err != nil {
		return oops.Code("MIGRATION_FAILED").With("operation", "run migrations").Wrap(err)
	}

	cmd.Println("Migrations completed successfully")
	return nil
}
