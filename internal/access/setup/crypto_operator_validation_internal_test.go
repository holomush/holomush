//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/test/testutil"
)

// freshPool returns a *pgxpool.Pool bound to a fresh test database from
// the shared Postgres container. testutil.FreshDatabase already templates
// a pre-migrated schema (see test/testutil/postgres.go's ensureTemplate),
// so no separate migration call is needed. Caller's t.Cleanup closes the
// pool.
func freshPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	env := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, env)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// seedPlayer inserts a minimal player row and returns the ULID string.
func seedPlayer(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	id := ulid.Make().String()
	// Username uses the full ULID for uniqueness — ULIDs created in the
	// same millisecond share their timestamp prefix, so a short prefix
	// is not collision-safe.
	// INV-STORE-1: players.created_at is BIGINT-ns; deterministic fixture so the
	// seed value survives the round-trip unambiguously and the test is reproducible.
	createdAt := time.Date(2026, 5, 22, 12, 0, 0, 123456789, time.UTC).UnixNano()
	_, err := pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $4)`,
		id, "test_player_"+id, "hash", createdAt)
	require.NoError(t, err)
	return id
}

func TestCryptoOperatorValidation_AllKnown(t *testing.T) {
	ctx := context.Background()
	pool := freshPool(t)
	p1 := seedPlayer(t, ctx, pool)
	p2 := seedPlayer(t, ctx, pool)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	set, err := validateCryptoOperators(ctx, pool, []string{p1, p2}, logger)
	require.NoError(t, err)
	assert.Len(t, set, 2)
	assert.NotContains(t, logBuf.String(), "crypto.operator references unknown player")
}

func TestCryptoOperatorValidation_SomeUnknown(t *testing.T) {
	ctx := context.Background()
	pool := freshPool(t)
	p1 := seedPlayer(t, ctx, pool)
	unknown := ulid.Make().String()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	set, err := validateCryptoOperators(ctx, pool, []string{p1, unknown}, logger)
	require.NoError(t, err)
	assert.Len(t, set, 2, "set keeps full configured list (lax+warn semantics)")
	assert.Contains(t, set, p1)
	assert.Contains(t, set, unknown)

	output := logBuf.String()
	assert.Contains(t, output, "crypto.operator references unknown player")
	assert.Contains(t, output, unknown)
}

// TestCryptoOperatorValidation_AllUnknown asserts INV-B5: all configured
// operator IDs unknown to the players table → server WARNS but does NOT
// fail startup.
func TestCryptoOperatorValidation_AllUnknown(t *testing.T) {
	ctx := context.Background()
	pool := freshPool(t)
	u1 := ulid.Make().String()
	u2 := ulid.Make().String()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	set, err := validateCryptoOperators(ctx, pool, []string{u1, u2}, logger)
	require.NoError(t, err, "lax+warn must NOT fail-closed even when all IDs unknown (INV-B5)")
	assert.Len(t, set, 2)
	output := logBuf.String()
	assert.Contains(t, output, u1)
	assert.Contains(t, output, u2)
}

// TestCryptoOperatorValidation_EmptyConfig asserts INV-B7: empty operator
// config → no error, no warning, empty set.
func TestCryptoOperatorValidation_EmptyConfig(t *testing.T) {
	ctx := context.Background()
	pool := freshPool(t)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	set, err := validateCryptoOperators(ctx, pool, nil, logger)
	require.NoError(t, err)
	assert.Empty(t, set)
	assert.Empty(t, logBuf.String(), "empty config must not query DB and must not warn")
}

func TestCryptoOperatorValidation_DuplicatesInConfig(t *testing.T) {
	ctx := context.Background()
	pool := freshPool(t)
	p1 := seedPlayer(t, ctx, pool)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	set, err := validateCryptoOperators(ctx, pool, []string{p1, p1}, logger)
	require.NoError(t, err)
	assert.Len(t, set, 1, "duplicates must dedupe")
	assert.Empty(t, logBuf.String())
}

// TestCryptoOperatorValidation_QueryFails asserts the lax+warn behaviour
// for transient PG failures: validation logs a warning but does NOT gate
// startup. The configured set is returned as-is.
func TestCryptoOperatorValidation_QueryFails(t *testing.T) {
	pool := freshPool(t)
	pool.Close() // force a query failure (closed pool)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	operatorID := ulid.Make().String()
	set, err := validateCryptoOperators(context.Background(), pool, []string{operatorID}, logger)
	require.NoError(t, err, "query failure must NOT gate startup")
	assert.Contains(t, set, operatorID)
	assert.Contains(t, logBuf.String(), "crypto.operator validation skipped")
}
