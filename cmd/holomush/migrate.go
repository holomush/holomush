package main

import (
	"context"
	"fmt"
	"os"

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
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}

	ctx := context.Background()

	cmd.Println("Connecting to database...")
	eventStore, err := store.NewPostgresEventStore(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer eventStore.Close()

	cmd.Println("Running migrations...")
	if err := eventStore.Migrate(ctx); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	cmd.Println("Migrations completed successfully")
	return nil
}
