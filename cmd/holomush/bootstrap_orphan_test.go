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

// TestBootstrapFailsWithOrphanInUnpartitioned proves the check also scans the
// legacy events_audit_unpartitioned table (finding 5): after 000052 renamed
// history off events_audit, a restore-from-old-backup orphan sits in the
// unpartitioned table, and the pre-Start gate must still catch it.
func TestBootstrapFailsWithOrphanInUnpartitioned(t *testing.T) {
	connStr, cleanup := startPostgresContainer(t)
	defer cleanup()

	applyAllMigrations(t, connStr)

	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	defer pool.Close()

	// The legacy table exists after 000052 (empty). Seed a plugin-actor orphan
	// into it (old schema: no event_ms column).
	orphanID := ulid.Make()
	_, execErr := pool.Exec(context.Background(), `
		INSERT INTO events_audit_unpartitioned (id, subject, type, timestamp, actor_kind,
		                         actor_id, envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, 'test', 'test', $3, $2, NULL, '\x00', 1, 'identity', 1, '{}'::jsonb)
	`, orphanID[:], eventbus.ActorKindPlugin.String(), pgnanos.From(time.Now()))
	require.NoError(t, execErr)

	err = runBootstrapOrphanCheck(context.Background(), pool)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_ACTOR_ORPHAN_DETECTED")
}

// TestBootstrapPassesWhenUnpartitionedAbsent proves a clean install (no
// events_audit_unpartitioned) does not error with "relation does not exist" and
// passes when there are no orphans — the to_regclass guard keeps the legacy
// table out of the probe when it is absent.
func TestBootstrapPassesWhenUnpartitionedAbsent(t *testing.T) {
	connStr, cleanup := startPostgresContainer(t)
	defer cleanup()

	applyAllMigrations(t, connStr)

	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	defer pool.Close()

	// Simulate a clean install / post-Backfill state: the legacy table is gone.
	_, dropErr := pool.Exec(context.Background(), `DROP TABLE IF EXISTS events_audit_unpartitioned`)
	require.NoError(t, dropErr)

	require.NoError(t, runBootstrapOrphanCheck(context.Background(), pool))
}
