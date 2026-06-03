// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/store"
)

// testPool creates a testcontainer-backed *pgxpool.Pool with migrations
// applied. Teardown is registered on t.Cleanup.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pgContainer, err := postgres.Run(
		ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	migrator, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	require.NoError(t, migrator.Up())
	migrator.Close()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// TestStore_ResolveIntegrity_ResolvesCrashedRotate is INV-CRYPTO-21.
// Simulates a crashed Rotate by directly inserting two unrotated rows
// (bypassing Manager.Rotate via package-internal access), then verifies
// ResolveIntegrity resolves them.
func TestStore_ResolveIntegrity_ResolvesCrashedRotate(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)

	ctxID := ContextID{Type: "scene", ID: "inv37-test"}

	// Insert v1 — unexported insert + row accessible from package dek.
	r1 := row{
		ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1,
		WrappedDEK:   []byte("fake-dek-v1"),
		WrapProvider: "test", WrapKeyID: "k1",
		Participants: []Participant{
			{PlayerID: "alice", CharacterID: "c-alice", BindingID: "bind-alice", JoinedAt: time.Now().UTC()},
		},
	}
	_, err := store.insert(context.Background(), r1)
	require.NoError(t, err)

	// Simulate crashed Rotate: insert v2 without marking v1 rotated.
	r2 := row{
		ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 2,
		WrappedDEK:   []byte("fake-dek-v2"),
		WrapProvider: "test", WrapKeyID: "k1",
		Participants: []Participant{
			{PlayerID: "bob", CharacterID: "c-bob", BindingID: "bind-bob", JoinedAt: time.Now().UTC()},
		},
	}
	_, err = store.insert(context.Background(), r2)
	require.NoError(t, err)

	// Both rows exist with rotated_at IS NULL. Verify pre-condition.
	var count int
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM crypto_keys
		 WHERE context_type = $1 AND context_id = $2
		   AND rotated_at IS NULL AND destroyed_at IS NULL`,
		ctxID.Type, ctxID.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "pre-condition: two unrotated rows simulate crashed Rotate")

	// Run the integrity check.
	err = store.ResolveIntegrity(context.Background())
	require.NoError(t, err)

	// After resolution, only v2 should remain unrotated.
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM crypto_keys
		 WHERE context_type = $1 AND context_id = $2
		   AND rotated_at IS NULL AND destroyed_at IS NULL`,
		ctxID.Type, ctxID.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "post-resolution: exactly one unrotated row")

	// v1 should be marked rotated.
	var rotatedAt *pgnanos.Time
	err = pool.QueryRow(context.Background(), `
		SELECT rotated_at FROM crypto_keys
		 WHERE context_type = $1 AND context_id = $2 AND version = 1`,
		ctxID.Type, ctxID.ID).Scan(&rotatedAt)
	require.NoError(t, err)
	assert.NotNil(t, rotatedAt, "v1 should be marked rotated by integrity check")
}
