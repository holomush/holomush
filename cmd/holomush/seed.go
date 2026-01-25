// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
)

// Default timeout for seed command.
const defaultSeedTimeout = 30 * time.Second

// seedConfig holds configuration for the seed command.
type seedConfig struct {
	timeout time.Duration
}

// NewSeedCmd creates the seed subcommand.
func NewSeedCmd() *cobra.Command {
	cfg := &seedConfig{}

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed the world with initial data",
		Long: `Creates initial world data including a starting location.
This command is idempotent - it will not create duplicates if run multiple times.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSeed(cmd, args, cfg)
		},
	}

	cmd.Flags().DurationVar(&cfg.timeout, "timeout", defaultSeedTimeout, "timeout for database operations (e.g., 30s, 1m)")

	return cmd
}

func runSeed(cmd *cobra.Command, _ []string, cfg *seedConfig) error {
	// Get database URL from environment (config file support in later phase)
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return oops.Code("CONFIG_INVALID").Errorf("DATABASE_URL environment variable is required")
	}

	// Add timeout to prevent indefinite hangs
	// Use cmd.Context() to respect SIGINT/SIGTERM signals
	ctx, cancel := context.WithTimeout(cmd.Context(), cfg.timeout)
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
	// ULID must be exactly 26 characters (Crockford's base32 alphabet)
	startingLocID, err := ulid.Parse("01HZN3XS000000000000000000")
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
		// Check for unique constraint violation
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			cmd.Println("Starting location already exists, skipping seed")

			// Verify existing location matches expected attributes
			existing, getErr := locationRepo.Get(ctx, startingLocID)
			if getErr != nil {
				slog.Warn("Could not verify existing seed location",
					"location_id", startingLocID,
					"error", getErr)
			} else {
				// Check for attribute mismatches
				if existing.Name != startingLoc.Name {
					slog.Warn("Seed location name mismatch",
						"location_id", startingLocID,
						"expected", startingLoc.Name,
						"actual", existing.Name)
				}
				if existing.Type != startingLoc.Type {
					slog.Warn("Seed location type mismatch",
						"location_id", startingLocID,
						"expected", startingLoc.Type,
						"actual", existing.Type)
				}
				if existing.Description != startingLoc.Description {
					slog.Warn("Seed location description mismatch",
						"location_id", startingLocID,
						"expected_length", len(startingLoc.Description),
						"actual_length", len(existing.Description))
				}
			}

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
