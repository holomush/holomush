//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
)

func TestPluginRepoUpsertInsertsNewRow(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))

	repo := store.NewPostgresPluginRepo(pool)
	id, drift, err := repo.Upsert(ctx, store.PluginUpsertInput{
		Name: "core-scenes", DisplayName: "Core Scenes", Version: "1.0.0",
		ManifestHash: []byte{0x01, 0x02, 0x03}, ContentHash: []byte{0x04, 0x05},
	})
	require.NoError(t, err)
	assert.Nil(t, drift)
	_, parseErr := ulid.Parse(id.String())
	assert.NoError(t, parseErr)
}

func TestPluginRepoUpsertUpdatesLastSeenWithoutDrift(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	in := store.PluginUpsertInput{
		Name: "core-scenes", DisplayName: "Core", Version: "1.0.0",
		ManifestHash: []byte{0x01}, ContentHash: []byte{0x04},
	}
	id1, _, err := repo.Upsert(ctx, in)
	require.NoError(t, err)
	id2, drift, err := repo.Upsert(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)
	assert.Nil(t, drift)
}

func TestPluginRepoUpsertReportsDriftOnHashChange(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	in1 := store.PluginUpsertInput{
		Name: "core-scenes", DisplayName: "Core", Version: "1.0.0",
		ManifestHash: []byte{0x01}, ContentHash: []byte{0x04},
	}
	id1, _, err := repo.Upsert(ctx, in1)
	require.NoError(t, err)

	in2 := in1
	in2.ManifestHash = []byte{0xAA, 0xBB}
	in2.Version = "1.1.0"
	id2, drift, err := repo.Upsert(ctx, in2)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)
	require.NotNil(t, drift)
	assert.Equal(t, []byte{0x01}, drift.OldManifestHash)
	assert.Equal(t, []byte{0xAA, 0xBB}, drift.NewManifestHash)
	assert.Equal(t, "1.0.0", drift.VersionBefore)
	assert.Equal(t, "1.1.0", drift.VersionAfter)
}

func TestPluginRepoListAllReturnsActiveAndDeactivated(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	_, _, err := repo.Upsert(ctx, store.PluginUpsertInput{Name: "active", DisplayName: "A", Version: "1", ManifestHash: []byte{0x01}})
	require.NoError(t, err)
	_, _, err = repo.Upsert(ctx, store.PluginUpsertInput{Name: "stale", DisplayName: "S", Version: "1", ManifestHash: []byte{0x02}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE plugins SET last_seen_at = now() - interval '99 days' WHERE name = 'stale'`)
	require.NoError(t, err)
	_, err = repo.SweepInactive(ctx, 1)
	require.NoError(t, err)

	rows, err := repo.ListAll(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	var active, deactivated int
	for _, r := range rows {
		if r.GcAt == nil {
			active++
		} else {
			deactivated++
		}
	}
	assert.Equal(t, 1, active)
	assert.Equal(t, 1, deactivated)
}

func TestPluginRepoSweepInactiveDeactivatesStaleRowsOnly(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	_, _, _ = repo.Upsert(ctx, store.PluginUpsertInput{Name: "fresh", DisplayName: "F", Version: "1", ManifestHash: []byte{0x01}})
	_, _, _ = repo.Upsert(ctx, store.PluginUpsertInput{Name: "stale", DisplayName: "S", Version: "1", ManifestHash: []byte{0x02}})
	_, err := pool.Exec(ctx, `UPDATE plugins SET last_seen_at = now() - interval '5 days' WHERE name = 'stale'`)
	require.NoError(t, err)

	swept, err := repo.SweepInactive(ctx, 3)
	require.NoError(t, err)
	require.Len(t, swept, 1)
	assert.Equal(t, "stale", swept[0].Name)
}

func TestPluginRepoSweepNeverDeletesRows(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 18))
	repo := store.NewPostgresPluginRepo(pool)

	_, _, _ = repo.Upsert(ctx, store.PluginUpsertInput{Name: "p", DisplayName: "P", Version: "1", ManifestHash: []byte{0x01}})
	_, err := pool.Exec(ctx, `UPDATE plugins SET last_seen_at = now() - interval '99 days'`)
	require.NoError(t, err)
	_, err = repo.SweepInactive(ctx, 1)
	require.NoError(t, err)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM plugins`).Scan(&n))
	assert.Equal(t, 1, n, "SweepInactive MUST NOT delete; only set gc_at")
}
