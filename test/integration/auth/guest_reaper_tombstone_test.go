// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

// --- shared D-06 reaping test harness (also used by guest_reaper_race_test.go) ---

// reaperPool returns a fresh, fully migrated database pool for a reaping test.
func reaperPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// seedReapGuest inserts a guest player (is_guest=true) and returns its id.
func seedReapGuest(t *testing.T, pool *pgxpool.Pool) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO players (id, username, password_hash, is_guest) VALUES ($1, $2, '', true)`,
		playerID.String(), "reapguest_"+playerID.String())
	require.NoError(t, err)
	return playerID
}

// newReapGenesis builds the genesis service (with the real reaping-reject guard).
func newReapGenesis(t *testing.T, pool *pgxpool.Pool) *auth.CharacterGenesisService {
	t.Helper()
	svc, err := auth.NewCharacterGenesisService(
		worldpg.NewCharacterRepository(pool),
		worldpg.NewTransactor(pool),
		worldpg.NewBindingRepository(pool),
		worldpg.NewOutboxStore(pool),
		worldpg.NewReapingGuard(pool),
	)
	require.NoError(t, err)
	return svc
}

// newReapService builds the tombstone-emitting CharacterReapingService — the
// guest reaper's injected cleaner.
func newReapService(t *testing.T, pool *pgxpool.Pool) *auth.CharacterReapingService {
	t.Helper()
	charRepo := worldpg.NewCharacterRepository(pool)
	playerRepo := authpg.NewPlayerRepository(pool)
	svc, err := auth.NewCharacterReapingService(
		charRepo, charRepo,
		worldpg.NewPropertyRepository(pool),
		worldpg.NewBindingRepository(pool),
		worldpg.NewTransactor(pool),
		worldpg.NewOutboxStore(pool),
		playerRepo, playerRepo,
	)
	require.NoError(t, err)
	return svc
}

func reapCharFor(t *testing.T, playerID ulid.ULID, name string) *world.Character {
	t.Helper()
	char, err := world.NewCharacter(playerID, name)
	require.NoError(t, err)
	return char
}

func outboxKindCount(t *testing.T, pool *pgxpool.Pool, aggID ulid.ULID, kind string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM outbox WHERE aggregate_id = $1 AND kind = $2`,
		aggID.String(), kind).Scan(&n))
	return n
}

func rowCount(t *testing.T, pool *pgxpool.Pool, query string, arg string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), query, arg).Scan(&n))
	return n
}

func seedCharacterProperty(t *testing.T, pool *pgxpool.Pool, charID ulid.ULID) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO entity_properties (id, parent_type, parent_id, name, value)
		 VALUES ($1, 'character', $2, 'mood', 'curious')`,
		ulid.Make().String(), charID.String())
	require.NoError(t, err)
}

// --- the D-06 regression gate ---

// Verifies: INV-WORLD-4
//
// A guest is created (genesis emits a character create envelope), then reaped
// through the tombstone-emitting reaping service (the guest reaper's cleaner):
// the feed holds BOTH the create genesis envelope AND the character_deleted
// tombstone for that character (no genesis-without-tombstone feed history), the
// character + its entity_properties rows are gone (R6-3 cascade parity), the
// player row is gone, and the player-delete FK cascade removed no un-tombstoned
// character.
func TestGuestReaperEmitsCharacterTombstoneClosingD06(t *testing.T) {
	ctx := context.Background()
	pool := reaperPool(t)
	genesis := newReapGenesis(t, pool)
	reaping := newReapService(t, pool)

	playerID := seedReapGuest(t, pool)
	char := reapCharFor(t, playerID, "Reaped Guest")
	require.NoError(t, genesis.Create(ctx, char, "initial_bind_guest"))

	// Genesis create envelope is on the feed at creation.
	require.Equal(t, 1, outboxKindCount(t, pool, char.ID, "character_genesis"),
		"guest creation must emit exactly one character_genesis envelope")
	// The character carries a property, to prove the cascade (R6-3).
	seedCharacterProperty(t, pool, char.ID)

	// Reap the guest through the reaping service (the reaper's GuestCleaner).
	require.NoError(t, reaping.DeleteGuestPlayer(ctx, playerID))

	// The feed now ALSO holds exactly one character_deleted tombstone for the
	// character — no genesis-without-tombstone history.
	assert.Equal(t, 1, outboxKindCount(t, pool, char.ID, "character_deleted"),
		"reaping must emit exactly one character_deleted tombstone")
	// Genesis create envelope is still present (feed completeness: create + delete).
	assert.Equal(t, 1, outboxKindCount(t, pool, char.ID, "character_genesis"))

	// The tombstone commits at a LATER feed position than the create (feed order).
	var genesisPos, tombstonePos int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT feed_position FROM outbox WHERE aggregate_id = $1 AND kind = 'character_genesis'`,
		char.ID.String()).Scan(&genesisPos))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT feed_position FROM outbox WHERE aggregate_id = $1 AND kind = 'character_deleted'`,
		char.ID.String()).Scan(&tombstonePos))
	assert.Greater(t, tombstonePos, genesisPos, "tombstone must follow the genesis in feed order")

	// Character, its properties, and the player row are all gone; the FK cascade
	// removed no un-tombstoned character (there is exactly one tombstone above).
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM characters WHERE id = $1`, char.ID.String()))
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM entity_properties WHERE parent_id = $1`, char.ID.String()))
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM players WHERE id = $1`, playerID.String()))
}

// A guest with MULTIPLE characters gets one tombstone per character, and the
// player is deleted only after all are tombstoned.
func TestGuestReaperTombstonesEveryCharacter(t *testing.T) {
	ctx := context.Background()
	pool := reaperPool(t)
	genesis := newReapGenesis(t, pool)
	reaping := newReapService(t, pool)

	playerID := seedReapGuest(t, pool)
	c1 := reapCharFor(t, playerID, "Guest Alpha")
	c2 := reapCharFor(t, playerID, "Guest Beta")
	require.NoError(t, genesis.Create(ctx, c1, "initial_bind_guest"))
	require.NoError(t, genesis.Create(ctx, c2, "initial_bind_guest"))

	require.NoError(t, reaping.DeleteGuestPlayer(ctx, playerID))

	assert.Equal(t, 1, outboxKindCount(t, pool, c1.ID, "character_deleted"))
	assert.Equal(t, 1, outboxKindCount(t, pool, c2.ID, "character_deleted"))
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM characters WHERE player_id = $1`, playerID.String()))
	assert.Zero(t, rowCount(t, pool, `SELECT COUNT(*) FROM players WHERE id = $1`, playerID.String()))
}
