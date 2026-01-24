// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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

	// Add timeout to prevent indefinite hangs
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	// Reuse the event store's pool for the location repository
	pool := eventStore.Pool()
	if pool == nil {
		return oops.Code("DB_POOL_FAILED").Errorf("failed to get database pool from event store")
	}
	locationRepo := postgres.NewLocationRepository(pool)

	// Seed starting location using a well-known ID for idempotency
	// Using a fixed ID means duplicate inserts will fail with a constraint violation
	startingLocID, err := ulid.Parse("01NEXUS00000000000000000000")
	if err != nil {
		return oops.Code("SEED_FAILED").With("operation", "parse seed location ID").Wrap(err)
	}

	startingLoc := &world.Location{
		ID:           startingLocID,
		Type:         world.LocationTypePersistent,
		Name:         "The Nexus",
		Description:  "A swirling vortex of energy marks the center of the multiverse. Paths branch off in every direction, leading to countless worlds and possibilities. This is where all journeys begin.",
		ReplayPolicy: "last:0",
		CreatedAt:    time.Now().UTC(),
	}

	// Attempt to create the location; handle duplicate gracefully
	if err := locationRepo.Create(ctx, startingLoc); err != nil {
		// Check for unique constraint violation (PostgreSQL error code 23505)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			cmd.Println("Starting location already exists, skipping seed")
			slog.Info("World already seeded", "location_id", startingLocID)
			return nil
		}
		return oops.Code("SEED_FAILED").With("operation", "create starting location").Wrap(err)
	}

	cmd.Println("Created starting location: The Nexus")
	slog.Info("Created starting location", "id", startingLoc.ID, "name", startingLoc.Name)

	cmd.Println("World seeding complete!")
	return nil
}
