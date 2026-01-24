// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
)

// NewSeedCmd creates the seed subcommand.
func NewSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Seed the world with initial data",
		Long: `Creates initial world data including a starting location.
This command is idempotent - it will not create duplicates if run multiple times.`,
		RunE: runSeed,
	}
}

func runSeed(cmd *cobra.Command, _ []string) error {
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
	if migrateErr := eventStore.Migrate(ctx); migrateErr != nil {
		return oops.Code("MIGRATION_FAILED").With("operation", "run migrations").Wrap(migrateErr)
	}

	// Create a separate pool for the location repository
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return oops.Code("DB_POOL_FAILED").With("operation", "create pool").Wrap(err)
	}
	defer pool.Close()

	locationRepo := postgres.NewLocationRepository(pool)

	// Seed starting location
	startingLoc := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypePersistent,
		Name:         "The Nexus",
		Description:  "A swirling vortex of energy marks the center of the multiverse. Paths branch off in every direction, leading to countless worlds and possibilities. This is where all journeys begin.",
		ReplayPolicy: "last:0",
		CreatedAt:    time.Now().UTC(),
	}

	// Check if any persistent locations exist (idempotent check)
	existing, err := locationRepo.ListByType(ctx, world.LocationTypePersistent)
	if err != nil {
		return oops.Code("SEED_CHECK_FAILED").With("operation", "check existing locations").Wrap(err)
	}

	if len(existing) > 0 {
		cmd.Println("Starting location already exists, skipping seed")
		slog.Info("World already seeded", "existing_locations", len(existing))
		return nil
	}

	if err := locationRepo.Create(ctx, startingLoc); err != nil {
		return oops.Code("SEED_FAILED").With("operation", "create starting location").Wrap(err)
	}

	cmd.Println("Created starting location: The Nexus")
	slog.Info("Created starting location", "id", startingLoc.ID, "name", startingLoc.Name)

	cmd.Println("World seeding complete!")
	return nil
}
