// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/world/outbox"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	worldsetup "github.com/holomush/holomush/internal/world/setup"
)

// NewOutboxCmd returns the `holomush outbox` parent command: operator affordances
// for the world-change transactional-outbox relay (MODEL-04, 05-07). Its `skip`
// subcommand clears a poison halt without raw SQL by driving the SkipService,
// which owns BOTH a database connection AND the JetStream publisher (round-3
// blocker #3 — a DB-only CLI cannot publish the same-position marker).
func NewOutboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outbox",
		Short: "World-change outbox relay operator commands (Postgres + NATS)",
	}
	cmd.AddCommand(newOutboxSkipCmd())
	return cmd
}

// newOutboxSkipCmd returns `holomush outbox skip --game <id> --position <n>`.
func newOutboxSkipCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skip",
		Short: "Publish an operator skip marker for a halted poison row and resolve it",
		Long: `Clear a halted world-change feed without raw SQL.

When the relay halts on a poison (permanently-unpublishable) envelope, the feed
stops in strict position order. 'skip' drives the SkipService: it acquires the
fenced lease, validates the halted row at --position, persists-or-reuses a stable
skip-marker event id, PUBLISHES an operator-authorized marker at that same
feed_position (preserving gap-free wire order), and only after PubAck marks the
poison row resolved so the relay resumes. It never silently bumps published_at.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOutboxSkip(cmd)
		},
	}
	cmd.Flags().String("game", "main", "game id whose feed is halted")
	cmd.Flags().Int64("position", 0, "feed_position of the poison row to skip (the current feed blocker)")
	return cmd
}

// runOutboxSkip constructs the SkipService with BOTH a DB pool AND a JetStream
// publisher and invokes it. It MUST NOT call any raw store method directly — it
// drives the SERVICE, which owns both dependencies.
func runOutboxSkip(cmd *cobra.Command) error {
	game, _ := cmd.Flags().GetString("game")        //nolint:errcheck // flag defined above
	position, _ := cmd.Flags().GetInt64("position") //nolint:errcheck // flag defined above
	if position <= 0 {
		return oops.Code("EX_USAGE").
			Errorf("--position must be a positive feed_position (the halted poison row)")
	}

	pool, err := openOutboxPool(cmd.Context())
	if err != nil {
		return err
	}
	defer pool.Close()

	cfg, err := loadEventBusConfig(cmd)
	if err != nil {
		return err
	}
	conn, js, err := dialAuditJetStream(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	publisher := eventbus.NewJetStreamPublisher(js, cfg)
	outboxStore := worldsetup.NewOutboxStore(worldpostgres.NewOutboxStore(pool))
	// The CLI drives the SERVICE (owning the leased store AND the publisher) — it
	// never calls a raw store method (round-3 blocker #3).
	skip := outbox.NewSkipService(outboxStore, publisher, game, slog.Default())

	if err := skip.Skip(cmd.Context(), position); err != nil {
		return oops.Code("WORLD_OUTBOX_SKIP_CMD_FAILED").
			With("game", game).With("position", position).Wrap(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "skipped poison row at game=%s position=%d\n", game, position) //nolint:errcheck // display output
	return nil
}

// openOutboxPool opens a pgxpool against DATABASE_URL — the durable outbox the
// SkipService's lease reads/writes.
func openOutboxPool(ctx context.Context) (*pgxpool.Pool, error) {
	url, err := getDatabaseURL()
	if err != nil {
		return nil, oops.Code("WORLD_OUTBOX_SKIP_DATABASE_URL_MISSING").Wrap(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, oops.Code("WORLD_OUTBOX_SKIP_POOL_FAILED").Wrap(err)
	}
	return pool, nil
}
