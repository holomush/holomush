// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"fmt"
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
	timeout  time.Duration
	noStrict bool
}

// seedLocation holds seed data attributes for comparison.
type seedLocation struct {
	Name        string
	Type        string
	Description string
}

// NewSeedCmd creates the seed subcommand.
func NewSeedCmd() *cobra.Command {
	cfg := &seedConfig{}

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed the world with initial data",
		Long: `Creates initial world data including a starting location.
This command is idempotent - it will not create duplicates if run multiple times.

When re-running on an existing database, the command verifies that seed data
attributes match. Use --no-strict to allow attribute mismatches (warns instead
of failing). Note that verification failures due to database errors always fail
and cannot be suppressed by --no-strict.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSeed(cmd, args, cfg)
		},
	}

	cmd.Flags().DurationVar(&cfg.timeout, "timeout", defaultSeedTimeout, "timeout for database operations (e.g., 30s, 1m)")
	cmd.Flags().BoolVar(&cfg.noStrict, "no-strict", false, "allow seed attribute mismatches (warn instead of fail)")

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
				return handleVerificationFailure(cmd, startingLocID, getErr)
			}

			// Collect and check for attribute mismatches
			expected := seedLocation{
				Name:        startingLoc.Name,
				Type:        string(startingLoc.Type),
				Description: startingLoc.Description,
			}
			actual := seedLocation{
				Name:        existing.Name,
				Type:        string(existing.Type),
				Description: existing.Description,
			}
			mismatches := collectMismatches(startingLocID, expected, actual)

			if mismatchErr := checkSeedMismatches(cmd, mismatches, cfg.noStrict); mismatchErr != nil {
				return mismatchErr
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

// collectMismatches compares expected and actual seed location attributes
// and returns a list of human-readable mismatch descriptions.
func collectMismatches(id ulid.ULID, expected, actual seedLocation) []string {
	var mismatches []string

	if expected.Name != actual.Name {
		mismatches = append(mismatches, fmt.Sprintf(
			"location %s name mismatch: expected %q, got %q",
			id, expected.Name, actual.Name))
	}
	if expected.Type != actual.Type {
		mismatches = append(mismatches, fmt.Sprintf(
			"location %s type mismatch: expected %q, got %q",
			id, expected.Type, actual.Type))
	}
	if expected.Description != actual.Description {
		mismatches = append(mismatches, fmt.Sprintf(
			"location %s description mismatch: expected length %d, got length %d",
			id, len(expected.Description), len(actual.Description)))
	}

	return mismatches
}

// logVerificationFailure logs when we can't verify existing seed location attributes.
// This is logged at ERROR level (not WARN) because verification failure indicates
// a potentially serious issue that operators should investigate.
func logVerificationFailure(cmd *cobra.Command, locationID ulid.ULID, err error) {
	slog.Error("Could not verify existing seed location",
		"location_id", locationID,
		"error", err)
	cmd.PrintErrln("WARNING: Could not verify existing seed location attributes")
}

// handleVerificationFailure returns an error when we cannot verify existing seed location.
// This error is NOT suppressible by --no-strict because verification failure indicates
// a database error (not a data mismatch), which requires investigation.
func handleVerificationFailure(cmd *cobra.Command, locationID ulid.ULID, err error) error {
	logVerificationFailure(cmd, locationID, err)
	return oops.Code("SEED_VERIFY_FAILED").
		With("location_id", locationID.String()).
		Wrapf(err, "seed location exists but could not verify attributes")
}

// checkSeedMismatches prints warnings for mismatches and returns an error in strict mode.
func checkSeedMismatches(cmd *cobra.Command, mismatches []string, noStrict bool) error {
	if len(mismatches) == 0 {
		return nil
	}

	// Print all mismatches to stderr
	for _, m := range mismatches {
		cmd.PrintErrln("WARNING:", m)
	}

	// In strict mode (default), fail with error
	if !noStrict {
		return oops.Code("SEED_MISMATCH").Errorf(
			"seed data has %d attribute mismatch(es); use --no-strict to allow",
			len(mismatches))
	}

	return nil
}
