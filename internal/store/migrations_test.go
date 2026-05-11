// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMigration_000030_BootstrapMetadataReplacement verifies the schema
// replacement of bootstrap_metadata from D's (key, value) shape to the
// auditchain primitive's (chain_name, scope_key) shape.
//
// Checks:
//   - Pre-30 schema supports (key, value) inserts.
//   - Migration 30 up: DROP+CREATE yields (chain_name, scope_key) primary key.
//   - Partial unique index rejects duplicate (chain_name, scope_key).
//   - Migration 30 down: restores legacy (key) column.
func TestMigration_000030_BootstrapMetadataReplacement(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()

	// Apply migrations 1-20 to reach the pre-30 schema.
	// bootstrap_metadata at this point has (key TEXT PRIMARY KEY, value TEXT NOT NULL).
	require.NoError(t, runMigrations(ctx, pool, 20))

	// Pre-30 schema has key='crypto.policy_chain_initialized.<policy_name>' rows.
	_, err := pool.Exec(ctx,
		`INSERT INTO bootstrap_metadata(key, value) VALUES ('crypto.policy_chain_initialized.dual_control_required', 'true')`)
	require.NoError(t, err)

	// Migrate up to 30: DROP+CREATE bootstrap_metadata.
	require.NoError(t, runMigrations(ctx, pool, 30))

	// New schema has (chain_name, scope_key) primary key; old rows are gone.
	var count int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM bootstrap_metadata WHERE chain_name = $1`,
		"any_chain").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "fresh table after DROP+CREATE replacement")

	// Verify primary key enforces unique (chain_name, scope_key) by attempting duplicate insert.
	_, err = pool.Exec(ctx,
		`INSERT INTO bootstrap_metadata(chain_name, scope_key, initialized_at)
		 VALUES ('test.chain', 'scope1', now())`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO bootstrap_metadata(chain_name, scope_key, initialized_at)
		 VALUES ('test.chain', 'scope1', now())`)
	require.Error(t, err, "duplicate (chain_name, scope_key) must be rejected by primary key")

	// Migrate down to 20: D's schema returns (key TEXT PRIMARY KEY).
	// Version 29 does not exist; migrating to 20 is the correct pre-30 state.
	require.NoError(t, runMigrations(ctx, pool, 20))

	var hasKey bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		              WHERE table_name='bootstrap_metadata' AND column_name='key')`).Scan(&hasKey)
	require.NoError(t, err)
	require.True(t, hasKey, "down migration restores legacy `key` column")
}
