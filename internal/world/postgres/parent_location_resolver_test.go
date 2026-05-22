// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

// helpers (insertCharacterAt, insertLocation, insertObjectAtLocation,
// insertContainerAtLocation, insertObjectHeldBy, insertObjectContainedIn,
// newTestPool) are defined at the bottom of this file.

func TestParentLocationResolver_LocationParent_ReturnsParentIDDirectly(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	locID := ulid.Make()

	// No DB insert: location parent_type short-circuits without query.
	got, err := resolver.ResolveParentLocation(context.Background(), "location", locID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_CharacterParent_JoinsLocationID(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "TestLoc")
	charID := insertCharacterAt(t, pool, "TestChar", &locID)

	got, err := resolver.ResolveParentLocation(ctx, "character", charID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_CharacterParent_NullLocation_ReturnsNil(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	charID := insertCharacterAt(t, pool, "Wanderer", nil)

	got, err := resolver.ResolveParentLocation(ctx, "character", charID)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestParentLocationResolver_ObjectParent_DirectLocation(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "Library")
	objID := insertObjectAtLocation(t, pool, "Book", locID)

	got, err := resolver.ResolveParentLocation(ctx, "object", objID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_ObjectParent_HeldByCharacter(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "Town")
	charID := insertCharacterAt(t, pool, "Holder", &locID)
	objID := insertObjectHeldBy(t, pool, "Note", charID)

	got, err := resolver.ResolveParentLocation(ctx, "object", objID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_ObjectParent_ContainedRecursive(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "Vault")
	chest := insertContainerAtLocation(t, pool, "Chest", locID)
	coin := insertObjectContainedIn(t, pool, "Coin", chest)

	got, err := resolver.ResolveParentLocation(ctx, "object", coin)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, locID, *got)
}

func TestParentLocationResolver_ObjectParent_Cycle_ReturnsNil(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	// Construct a cycle by direct UPDATE (bypasses ObjectRepo.Move's guard).
	locID := insertLocation(t, pool, "Anchor")
	a := insertContainerAtLocation(t, pool, "A", locID)
	b := insertObjectContainedIn(t, pool, "B", a)
	// Now make A point at B (cycle): A.contained_in=B, B.contained_in=A.
	_, err := pool.Exec(ctx, `UPDATE objects SET location_id = NULL, contained_in_object_id = $1 WHERE id = $2`,
		b.String(), a.String())
	require.NoError(t, err)

	got, err := resolver.ResolveParentLocation(ctx, "object", a)
	require.NoError(t, err)
	assert.Nil(t, got, "cycle MUST terminate via depth bound and return nil")
}

func TestParentLocationResolver_ObjectParent_MaxDepthExceeded_ReturnsNil(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	ctx := context.Background()

	locID := insertLocation(t, pool, "Bottom")
	// Build a chain of 21 objects: obj0 in locID, obj1 in obj0, obj2 in obj1, ..., obj20 in obj19.
	// The resolver's depth cap is 20 — resolving obj20's location MUST exhaust depth and return nil.
	prev := insertContainerAtLocation(t, pool, "obj0", locID)
	for i := 1; i <= 20; i++ {
		prev = insertObjectContainedIn(t, pool, fmt.Sprintf("obj%d", i), prev)
	}
	// prev is obj20. Walking from obj20 needs to traverse 20 hops up the chain to reach obj0
	// which has the location_id. With maxParentChainDepth=20, the CTE stops before reaching it.

	got, err := resolver.ResolveParentLocation(ctx, "object", prev)
	require.NoError(t, err)
	assert.Nil(t, got, "depth bound (20) MUST be enforced — chain of 21 returns nil")
}

func TestParentLocationResolver_UnknownParentType_ReturnsNil(t *testing.T) {
	pool := newTestPool(t)
	resolver := worldpg.NewParentLocationResolver(pool)
	got, err := resolver.ResolveParentLocation(context.Background(), "exit", ulid.Make())
	require.NoError(t, err)
	assert.Nil(t, got)
}

// ----- helpers below -----

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func insertLocation(t *testing.T, pool *pgxpool.Pool, name string) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO locations (id, name, description, type, replay_policy)
		VALUES ($1, $2, '', 'persistent', 'last:0')`,
		id.String(), name)
	require.NoError(t, err)
	return id
}

func insertCharacterAt(t *testing.T, pool *pgxpool.Pool, name string, locID *ulid.ULID) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'hash')`,
		playerID.String(), "p_"+playerID.String())
	require.NoError(t, err)
	charID := ulid.Make()
	if locID != nil {
		_, err = pool.Exec(context.Background(), `
			INSERT INTO characters (id, player_id, name, location_id)
			VALUES ($1, $2, $3, $4)`,
			charID.String(), playerID.String(), name, locID.String())
	} else {
		_, err = pool.Exec(context.Background(), `
			INSERT INTO characters (id, player_id, name, location_id)
			VALUES ($1, $2, $3, NULL)`,
			charID.String(), playerID.String(), name)
	}
	require.NoError(t, err)
	return charID
}

func insertObjectAtLocation(t *testing.T, pool *pgxpool.Pool, name string, locID ulid.ULID) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO objects (id, name, description, location_id, is_container)
		VALUES ($1, $2, '', $3, false)`,
		id.String(), name, locID.String())
	require.NoError(t, err)
	return id
}

func insertContainerAtLocation(t *testing.T, pool *pgxpool.Pool, name string, locID ulid.ULID) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO objects (id, name, description, location_id, is_container)
		VALUES ($1, $2, '', $3, true)`,
		id.String(), name, locID.String())
	require.NoError(t, err)
	return id
}

func insertObjectHeldBy(t *testing.T, pool *pgxpool.Pool, name string, charID ulid.ULID) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO objects (id, name, description, held_by_character_id, is_container)
		VALUES ($1, $2, '', $3, false)`,
		id.String(), name, charID.String())
	require.NoError(t, err)
	return id
}

func insertObjectContainedIn(t *testing.T, pool *pgxpool.Pool, name string, containerID ulid.ULID) ulid.ULID {
	t.Helper()
	id := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
		VALUES ($1, $2, '', $3, false)`,
		id.String(), name, containerID.String())
	require.NoError(t, err)
	return id
}
