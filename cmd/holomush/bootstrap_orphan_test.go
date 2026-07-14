//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

// applyAllMigrations runs the embedded migration set against the given pool's
// database. Uses the pool's connection config to derive the connStr.
func applyAllMigrations(t *testing.T, connStr string) {
	t.Helper()
	migrator, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	defer func() { _ = migrator.Close() }()
	require.NoError(t, migrator.Up())
}

func TestBootstrapPassesWithCleanEventsAudit(t *testing.T) {
	connStr, cleanup := startPostgresContainer(t)
	defer cleanup()

	applyAllMigrations(t, connStr)

	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, runBootstrapOrphanCheck(context.Background(), pool))
}

func TestBootstrapFailsWithSyntheticOrphan(t *testing.T) {
	connStr, cleanup := startPostgresContainer(t)
	defer cleanup()

	applyAllMigrations(t, connStr)

	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	defer pool.Close()

	// Insert a synthetic plugin-kind event with NULL actor_id to simulate
	// a legacy orphan that survived a w9ml migration mis-step. Use a REAL ULID
	// (not an arbitrary 16-byte string) so event_ms — the 000052 partition key —
	// is well-defined; a now()-based ULID lands in the current-month partition.
	orphanID := ulid.Make()
	eventMS := int64(orphanID.Time()) * int64(time.Millisecond)
	_, execErr := pool.Exec(context.Background(), `
		INSERT INTO events_audit (id, subject, type, timestamp, actor_kind,
		                         actor_id, envelope, schema_ver, codec, js_seq, rendering, event_ms)
		VALUES ($1, 'test', 'test', $3, $2, NULL, '\x00', 1, 'identity', 1, '{}'::jsonb, $4)
	`, orphanID[:], eventbus.ActorKindPlugin.String(), pgnanos.From(time.Now()), eventMS)
	require.NoError(t, execErr)

	err = runBootstrapOrphanCheck(context.Background(), pool)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_ACTOR_ORPHAN_DETECTED")
}
