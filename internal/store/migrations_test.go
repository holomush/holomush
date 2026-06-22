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
	pool := rawPool(t)

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
	// Note: initialized_at is still TIMESTAMPTZ at migration 30; migration 000043 converts it to BIGINT.
	_, err = pool.Exec(ctx,
		`INSERT INTO bootstrap_metadata(chain_name, scope_key)
		 VALUES ('test.chain', 'scope1')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO bootstrap_metadata(chain_name, scope_key)
		 VALUES ('test.chain', 'scope1')`)
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

// TestMigration_000031_CryptoRekeyCheckpoints verifies the crypto_rekey_checkpoints
// migration DDL:
//   - UNIQUE partial index rejects a second non-terminal row for the same
//     (context_type, context_id) pair.
//   - CHECK constraint rejects status='complete' without a completed_at timestamp.
//
// Depends on migration 000030 (replace_bootstrap_metadata) and all earlier E-chain
// migrations being present. Runs against a fresh isolated Postgres instance.
func TestMigration_000031_CryptoRekeyCheckpoints(t *testing.T) {
	ctx := context.Background()
	pool := rawPool(t)

	// Apply all migrations up through 000031.
	require.NoError(t, runMigrations(ctx, pool, 31))

	// Seed a crypto_keys row so that old_dek_id FK constraint is satisfied.
	var dekID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO crypto_keys
         (context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants)
         VALUES ('scene', '01ABC', 1, $1, 'test-provider', 'test-key', '[]'::jsonb)
         RETURNING id`,
		make([]byte, 32)).Scan(&dekID)
	require.NoError(t, err)

	// Insert an active checkpoint.
	// reqID and reqID2 must be distinct 16-byte primary keys.
	reqID := []byte{0x01, 0x48, 0x58, 0x59, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
	_, err = pool.Exec(ctx,
		`INSERT INTO crypto_rekey_checkpoints
         (request_id, context_type, context_id, op_args_hash, policy_hash,
          primary_player_id, status, old_dek_id)
         VALUES ($1, 'scene', '01ABC', $2, $3, '01PRIM', 'phase1_auth', $4)`,
		reqID, make([]byte, 32), make([]byte, 32), dekID)
	require.NoError(t, err)

	// UNIQUE partial index rejects a second active checkpoint on same context.
	reqID2 := []byte{0x01, 0x48, 0x58, 0x59, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}
	_, err = pool.Exec(ctx,
		`INSERT INTO crypto_rekey_checkpoints
         (request_id, context_type, context_id, op_args_hash, policy_hash,
          primary_player_id, status, old_dek_id)
         VALUES ($1, 'scene', '01ABC', $2, $3, '01OTHER', 'phase1_auth', $4)`,
		reqID2, make([]byte, 32), make([]byte, 32), dekID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "crypto_rekey_checkpoints_one_active_per_context")

	// Mark first complete; second insert now succeeds.
	// Note: completed_at is still TIMESTAMPTZ at migration 31; migration 000043 converts it to BIGINT.
	_, err = pool.Exec(ctx,
		`UPDATE crypto_rekey_checkpoints SET status='complete', completed_at=now() WHERE request_id=$1`,
		reqID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx,
		`INSERT INTO crypto_rekey_checkpoints
         (request_id, context_type, context_id, op_args_hash, policy_hash,
          primary_player_id, status, old_dek_id)
         VALUES ($1, 'scene', '01ABC', $2, $3, '01OTHER', 'phase1_auth', $4)`,
		reqID2, make([]byte, 32), make([]byte, 32), dekID)
	require.NoError(t, err, "after first is terminal, second can claim the slot")

	// CHECK constraint rejects status='complete' without completed_at.
	_, err = pool.Exec(ctx,
		`UPDATE crypto_rekey_checkpoints SET status='complete' WHERE request_id=$1`,
		reqID2)
	require.Error(t, err, "CHECK constraint must reject complete-without-timestamp")
	require.Contains(t, err.Error(), "crypto_rekey_checkpoints_terminal_consistency")
}

// TestMigration_000032_CreateSettingBootstrapState verifies the setting_bootstrap_state
// migration DDL:
//   - Up: creates setting_bootstrap_state(key, value, updated_at) with key as primary key.
//   - INSERT ... ON CONFLICT (key) DO UPDATE performs an upsert correctly.
//   - Down: drops the table cleanly.
//
// This table is the key-value store for SettingBootstrapper (content/setting bootstrap),
// kept separate from bootstrap_metadata which is now owned by the auditchain primitive.
func TestMigration_000032_CreateSettingBootstrapState(t *testing.T) {
	ctx := context.Background()
	pool := rawPool(t)

	// Apply all migrations up through 000032.
	require.NoError(t, runMigrations(ctx, pool, 32))

	// Verify table exists with expected columns.
	var colCount int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'setting_bootstrap_state'
		  AND column_name IN ('key', 'value', 'updated_at')
	`).Scan(&colCount)
	require.NoError(t, err)
	require.Equal(t, 3, colCount, "setting_bootstrap_state must have key, value, updated_at columns")

	// INSERT a row.
	_, err = pool.Exec(ctx,
		`INSERT INTO setting_bootstrap_state(key, value) VALUES ('active_setting', 'crossroads')`)
	require.NoError(t, err)

	// Verify the row round-trips.
	var value string
	err = pool.QueryRow(ctx,
		`SELECT value FROM setting_bootstrap_state WHERE key = $1`, "active_setting").Scan(&value)
	require.NoError(t, err)
	require.Equal(t, "crossroads", value)

	// Upsert via ON CONFLICT updates the value.
	// Note: updated_at is still TIMESTAMPTZ at migration 32; migration 000043 converts it to BIGINT.
	_, err = pool.Exec(ctx,
		`INSERT INTO setting_bootstrap_state(key, value, updated_at) VALUES ('active_setting', 'tavern', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`)
	require.NoError(t, err)

	err = pool.QueryRow(ctx,
		`SELECT value FROM setting_bootstrap_state WHERE key = $1`, "active_setting").Scan(&value)
	require.NoError(t, err)
	require.Equal(t, "tavern", value, "upsert must update existing row")

	// Duplicate primary key without ON CONFLICT is rejected.
	_, err = pool.Exec(ctx,
		`INSERT INTO setting_bootstrap_state(key, value) VALUES ('active_setting', 'duplicate')`)
	require.Error(t, err, "duplicate key must be rejected by primary key")

	// Migrate down: table must be dropped.
	require.NoError(t, runMigrations(ctx, pool, 31))

	var tableExists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS(
		    SELECT 1 FROM information_schema.tables
		    WHERE table_name = 'setting_bootstrap_state'
		)
	`).Scan(&tableExists)
	require.NoError(t, err)
	require.False(t, tableExists, "down migration must drop setting_bootstrap_state")
}
