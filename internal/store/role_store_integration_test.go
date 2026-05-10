//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/store"
)

func TestPlayerHasRole_ReturnsTrueForPlayerWithAdminCharacter(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 20))

	playerID := ulid.Make().String()
	charID := ulid.Make().String()
	_, err := pool.Exec(ctx, `INSERT INTO players (id, username, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())`, playerID, "alice-"+playerID[:8], "hash")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO characters (id, player_id, name)
		VALUES ($1, $2, $3)`, charID, playerID, "Alice-"+charID[:8])
	require.NoError(t, err)

	rs := store.NewPostgresRoleStore(pool)
	require.NoError(t, rs.AddRole(ctx, charID, access.RoleAdmin))

	has, err := rs.PlayerHasRole(ctx, playerID, access.RoleAdmin)
	require.NoError(t, err)
	require.True(t, has)
}

func TestPlayerHasRole_ReturnsFalseForPlayerWithoutAnyAdminCharacter(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 20))

	playerID := ulid.Make().String()
	charID := ulid.Make().String()
	_, err := pool.Exec(ctx, `INSERT INTO players (id, username, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())`, playerID, "bob-"+playerID[:8], "hash")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO characters (id, player_id, name)
		VALUES ($1, $2, $3)`, charID, playerID, "Bob-"+charID[:8])
	require.NoError(t, err)

	rs := store.NewPostgresRoleStore(pool)
	// Add and then remove to assert the negative path explicitly.
	require.NoError(t, rs.AddRole(ctx, charID, access.RoleAdmin))
	require.NoError(t, rs.RemoveRole(ctx, charID, access.RoleAdmin))

	has, err := rs.PlayerHasRole(ctx, playerID, access.RoleAdmin)
	require.NoError(t, err)
	require.False(t, has)
}

func TestPlayerHasRole_ReturnsFalseForUnknownPlayer(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newTestPool(t)
	defer cleanup()
	require.NoError(t, runMigrations(ctx, pool, 20))

	rs := store.NewPostgresRoleStore(pool)
	has, err := rs.PlayerHasRole(ctx, ulid.Make().String(), access.RoleAdmin)
	require.NoError(t, err)
	require.False(t, has)
}
