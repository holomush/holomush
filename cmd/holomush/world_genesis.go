// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"
	"log/slog"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/world/outbox"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// NewWorldCmd returns the `holomush world` parent command: operator affordances
// for the world-change feed lifecycle (MODEL-04, 05-11). Its `genesis` subcommand
// emits the cutover snapshot (one envelope per pre-existing aggregate,
// checkpoint-idempotent); its `epoch-reset` subcommand advances the persistent
// feed epoch to restart the feed cleanly after a DB restore/backfill. It is the
// REAL operator entry point (round-4 A3) — genesis/epoch are not library-only code
// with no caller. It is off crypto/abac surfaces (no internal/access or crypto
// import).
func NewWorldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "world",
		Short: "World-change feed lifecycle operator commands (Postgres)",
	}
	cmd.AddCommand(newWorldGenesisCmd())
	cmd.AddCommand(newWorldEpochResetCmd())
	return cmd
}

// newWorldGenesisCmd returns `holomush world genesis --game <id>`.
func newWorldGenesisCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "genesis",
		Short: "Emit the cutover genesis snapshot (one envelope per existing aggregate)",
		Long: `Give the world-change feed a defined origin at cutover.

'genesis' emits exactly one genesis envelope per existing location, exit,
character, and object at the current feed epoch. It is idempotent: each envelope
inserts a durable world_genesis_checkpoint row in the same transaction, so a
re-run at the same epoch emits nothing (no duplicate, no position gap). After an
epoch-reset it re-emits at the new epoch.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorldGenesis(cmd)
		},
	}
	cmd.Flags().String("game", "main", "game id whose feed to snapshot")
	return cmd
}

// newWorldEpochResetCmd returns `holomush world epoch-reset --game <id>`.
func newWorldEpochResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epoch-reset",
		Short: "Advance the persistent feed epoch (restart the feed after a DB restore/backfill)",
		Long: `Restart the world-change feed cleanly for a DB restore/backfill.

'epoch-reset' is one locked operation: under the per-game feed-counter lock it
quarantines any unpublished old-epoch outbox row (so the relay never publishes a
stale-epoch position), increments the epoch, resets the feed position to the
origin (positions restart, they do not inherit the old counter), and wakes the
relay. Follow it with 'world genesis' to re-emit the snapshot at the new epoch.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorldEpochReset(cmd)
		},
	}
	cmd.Flags().String("game", "main", "game id whose feed epoch to advance")
	return cmd
}

// runWorldGenesis constructs the postgres GenesisStore, injects it into the
// outbox GenesisService, and emits the cutover snapshot.
func runWorldGenesis(cmd *cobra.Command) error {
	game, _ := cmd.Flags().GetString("game") //nolint:errcheck // flag defined above

	pool, err := openOutboxPool(cmd.Context())
	if err != nil {
		return err
	}
	defer pool.Close()

	svc := outbox.NewGenesisService(worldpostgres.NewGenesisStore(pool), game, slog.Default())
	res, err := svc.EmitSnapshot(cmd.Context())
	if err != nil {
		return oops.Code("WORLD_GENESIS_CMD_FAILED").With("game", game).Wrap(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), //nolint:errcheck // display output
		"genesis snapshot: game=%s epoch=%d emitted=%d skipped=%d\n",
		game, res.Epoch, res.Emitted, res.Skipped)
	return nil
}

// runWorldEpochReset constructs the postgres GenesisStore, injects it into the
// outbox GenesisService, and advances the feed epoch.
func runWorldEpochReset(cmd *cobra.Command) error {
	game, _ := cmd.Flags().GetString("game") //nolint:errcheck // flag defined above

	pool, err := openOutboxPool(cmd.Context())
	if err != nil {
		return err
	}
	defer pool.Close()

	svc := outbox.NewGenesisService(worldpostgres.NewGenesisStore(pool), game, slog.Default())
	res, err := svc.ResetEpoch(cmd.Context())
	if err != nil {
		return oops.Code("WORLD_EPOCH_RESET_CMD_FAILED").With("game", game).Wrap(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), //nolint:errcheck // display output
		"epoch reset: game=%s previous_epoch=%d new_epoch=%d quarantined=%d origin_position=%d\n",
		game, res.PreviousEpoch, res.NewEpoch, res.Quarantined, res.OriginPosition)
	return nil
}
