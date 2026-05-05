//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000018CreatesPluginsTable(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 18))

	// Assert each expected column is present...
	var matched int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'plugins'
		  AND column_name IN ('id','name','display_name','version',
		                      'manifest_hash','content_hash',
		                      'first_seen_at','last_seen_at','gc_at')
	`).Scan(&matched))
	assert.Equal(t, 9, matched, "plugins must have all 9 expected columns")
	// ...and assert there are NO extras (catches contract drift if a future
	// migration adds a column without updating the named-column whitelist).
	var total int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'plugins'
	`).Scan(&total))
	assert.Equal(t, 9, total, "plugins MUST have exactly 9 columns (no extras)")

	var indexExists bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM pg_indexes
		    WHERE indexname = 'plugins_name_active'
		      AND indexdef LIKE '%WHERE (gc_at IS NULL)%'
		)
	`).Scan(&indexExists))
	assert.True(t, indexExists, "partial UNIQUE index plugins_name_active must exist")
}

func TestMigration000018TruncatesEventsAudit(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()

	require.NoError(t, runMigrations(ctx, pool, 17))
	_, err := pool.Exec(ctx, `
		INSERT INTO events_audit (id, subject, type, timestamp, actor_kind,
		                         envelope, schema_ver, codec, js_seq, rendering)
		VALUES ($1, 'test', 'test', now(), 'plugin', '\x00', 1, 'identity', 1, '{}'::jsonb)
	`, []byte("0123456789abcdef"))
	require.NoError(t, err)

	require.NoError(t, runMigrations(ctx, pool, 18))

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM events_audit`).Scan(&n))
	assert.Equal(t, 0, n, "events_audit MUST be empty after migration 000018")
}
