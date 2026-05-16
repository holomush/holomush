// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package eventbus_e2e_test hosts the end-to-end test matrix from the
// JetStream event-log design spec §8 (docs/superpowers/specs/
// 2026-04-18-jetstream-event-log-design.md). Each file in the package
// covers one row of the matrix.
//
// Suite conventions:
//
//   - Every test uses eventbustest.New(t) for the embedded JetStream bus
//     and testutil.SharedPostgres/FreshDatabase for a migrated PG database.
//   - No time.Sleep anywhere — synchronization goes through
//     eventbustest.Await* helpers or channel/ctx barriers.
//   - Tests that depend on infrastructure not yet implemented (drift
//     detector, audit-backfill CLI) are skeletons with t.Skip referencing
//     the follow-up bead that must land before they can pass.
package eventbus_e2e_test

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/test/testutil"
)

// newPool opens a pgxpool against a caller-supplied connection string.
// t.Cleanup handles Close — callers do not.
func newPool(t *testing.T, connStr string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// freshPool spins up a migrated PG database and returns a ready pool.
// Convenience wrapper for the common test setup pattern.
func freshPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	return newPool(t, connStr)
}

// mintEvent builds a well-formed Event on the given subject. Payload is
// small JSON by default so tests don't thrash on bytes.
func mintEvent(subject eventbus.Subject, etype eventbus.Type, body string) eventbus.Event {
	return eventbus.Event{
		ID:        ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader),
		Subject:   subject,
		Type:      etype,
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(body),
	}
}

// freshSessionID mints a ULID-shaped session identifier. Different from
// the package-level testEntropy in internal/eventbus/*_test.go because
// suite tests are parallel-safe with fresh entropy each call.
func freshSessionID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader).String()
}

// ensurePluginSchema creates the plugin's schema + table shape directly
// via SQL. In production the schema provisioner runs the plugin's
// migrations; for these e2e tests we inline the DDL so the suite doesn't
// depend on the plugin loader.
func ensurePluginSchema(ctx context.Context, t *testing.T, pool *pgxpool.Pool, schema, ddl string) {
	t.Helper()
	_, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, fmt.Sprintf("SET search_path TO %s, public", schema))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, ddl)
	require.NoError(t, err)
}

// waitForRowInSceneLog polls plugin_core_scenes.scene_log for a row with
// the given event id (raw 16-byte ULID) until it appears or the timeout
// fires. Used by the Phase 7 round-trip + plugin-isolation tests
// (B.5.0 helper per the implementation plan). Uses require.Eventually
// to honour the suite's no-time.Sleep convention.
func waitForRowInSceneLog(t *testing.T, pool *pgxpool.Pool, eventID []byte, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		var found int
		err := pool.QueryRow(t.Context(),
			`SELECT 1 FROM plugin_core_scenes.scene_log WHERE id = $1`, eventID).Scan(&found)
		return err == nil && found == 1
	}, timeout, 10*time.Millisecond, "scene_log row %x not present after %s", eventID, timeout)
}
