// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// holomush-cutover is the one-time deploy step for holomush-w9ml.
// Runs PG TRUNCATE events_audit + JetStream PurgeStream so no
// pre-cutover encrypted plugin-actor messages remain to fail AEAD
// post-cutover (their AAD bytes were sealed with the pre-w9ml proto
// shape).
//
// Usage:
//
//	DATABASE_URL=postgres://... NATS_URL=nats://... task migrate:plugin-actors-cutover
//
// NATS_STREAM_NAME defaults to "EVENTS" (the canonical stream name for
// this project; see internal/eventbus.StreamName).
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx := context.Background()

	// ── PostgreSQL: TRUNCATE events_audit ─────────────────────────────────
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("DATABASE_URL must be set")
		return 2
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		slog.Error("pg connect failed", "err", err)
		return 1
	}
	defer pool.Close()

	if _, execErr := pool.Exec(ctx, `TRUNCATE events_audit`); execErr != nil {
		slog.Error("TRUNCATE events_audit failed", "err", execErr)
		return 1
	}
	slog.Info("events_audit truncated")

	// ── JetStream: PurgeStream ────────────────────────────────────────────
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		slog.Error("NATS_URL must be set")
		return 2
	}

	streamName := os.Getenv("NATS_STREAM_NAME")
	if streamName == "" {
		// "EVENTS" is the canonical stream name; see internal/eventbus.StreamName.
		streamName = "EVENTS"
	}

	nc, err := nats.Connect(natsURL)
	if err != nil {
		slog.Error("nats connect failed", "err", err)
		return 1
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		slog.Error("jetstream client failed", "err", err)
		return 1
	}

	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		slog.Error("stream lookup failed", "err", err, "stream", streamName)
		return 1
	}

	if err = stream.Purge(ctx); err != nil {
		slog.Error("stream purge failed", "err", err, "stream", streamName)
		return 1
	}
	slog.Info("jetstream stream purged", "stream", streamName)

	return 0
}
