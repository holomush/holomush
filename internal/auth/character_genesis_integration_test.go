// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// genesisPool returns a fresh, fully migrated database pool for a test.
func genesisPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// seedGenesisPlayer inserts a player row (the character's player_id FK target)
// and returns its id.
func seedGenesisPlayer(t *testing.T, pool *pgxpool.Pool) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'hash')`,
		playerID.String(), "p_"+playerID.String())
	require.NoError(t, err)
	return playerID
}

func newGenesisService(t *testing.T, pool *pgxpool.Pool) *auth.CharacterGenesisService {
	t.Helper()
	svc, err := auth.NewCharacterGenesisService(
		worldpostgres.NewCharacterRepository(pool),
		worldpostgres.NewTransactor(pool),
		worldpostgres.NewBindingRepository(pool),
		worldpostgres.NewOutboxStore(pool),
		worldpostgres.NewReapingGuard(pool),
	)
	require.NoError(t, err)
	return svc
}

func countCharacter(t *testing.T, pool *pgxpool.Pool, id ulid.ULID) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM characters WHERE id = $1`, id.String()).Scan(&n))
	return n
}

func countBinding(t *testing.T, pool *pgxpool.Pool, charID ulid.ULID) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM player_character_bindings WHERE character_id = $1 AND ended_at IS NULL`,
		charID.String()).Scan(&n))
	return n
}

func countGenesisEnvelope(t *testing.T, pool *pgxpool.Pool, charID ulid.ULID) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM outbox WHERE aggregate_id = $1 AND kind = 'character_genesis'`,
		charID.String()).Scan(&n))
	return n
}

func genesisChar(t *testing.T, playerID ulid.ULID, name string) *world.Character {
	t.Helper()
	char, err := world.NewCharacter(playerID, name)
	require.NoError(t, err)
	return char
}

// Create commits character + binding + exactly one genesis envelope together.
func TestCharacterGenesisCreateCommitsCharacterBindingAndEnvelope(t *testing.T) {
	ctx := context.Background()
	pool := genesisPool(t)
	svc := newGenesisService(t, pool)
	playerID := seedGenesisPlayer(t, pool)
	char := genesisChar(t, playerID, "Atomic One")

	require.NoError(t, svc.Create(ctx, char, "initial_bind"))

	assert.Equal(t, 1, countCharacter(t, pool, char.ID))
	assert.Equal(t, 1, countBinding(t, pool, char.ID))
	assert.Equal(t, 1, countGenesisEnvelope(t, pool, char.ID))
}

// A player marked reaping is rejected by the genesis service (round-6 R6-2): the
// reaping-reject guard's SELECT reaping_at ... FOR UPDATE fires at the start of
// the creation tx, so no character row is inserted for a reaping player.
func TestCharacterGenesisCreateRejectsReapingPlayerAgainstDB(t *testing.T) {
	ctx := context.Background()
	pool := genesisPool(t)
	svc := newGenesisService(t, pool)
	playerID := seedGenesisPlayer(t, pool)

	// Mark the player reaping directly (the MarkReaping equivalent).
	_, err := pool.Exec(ctx,
		`UPDATE players SET reaping_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $1`,
		playerID.String())
	require.NoError(t, err)

	char := genesisChar(t, playerID, "Reaping Reject")
	err = svc.Create(ctx, char, "initial_bind_guest")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLAYER_REAPING")

	// No character row was inserted for the reaping player.
	assert.Equal(t, 0, countCharacter(t, pool, char.ID))
	assert.Equal(t, 0, countGenesisEnvelope(t, pool, char.ID))
}

// bindReason "" emits the envelope with NO binding (bootstrap-admin mode).
func TestCharacterGenesisCreateNoBindingEmitsEnvelope(t *testing.T) {
	ctx := context.Background()
	pool := genesisPool(t)
	svc := newGenesisService(t, pool)
	playerID := seedGenesisPlayer(t, pool)
	char := genesisChar(t, playerID, "No Bind Admin")

	require.NoError(t, svc.Create(ctx, char, ""))

	assert.Equal(t, 1, countCharacter(t, pool, char.ID))
	assert.Equal(t, 0, countBinding(t, pool, char.ID))
	assert.Equal(t, 1, countGenesisEnvelope(t, pool, char.ID))
}

// With an ambient transaction, the genesis service ENROLLS (re-entrant, no second
// Begin): rolling back the OUTER transaction removes the character, the binding,
// AND the envelope together.
func TestCharacterGenesisEnrollsInAmbientTxAndRollsBackTogether(t *testing.T) {
	ctx := context.Background()
	pool := genesisPool(t)
	svc := newGenesisService(t, pool)
	transactor := worldpostgres.NewTransactor(pool)
	playerID := seedGenesisPlayer(t, pool)
	char := genesisChar(t, playerID, "Rollback Hero")

	sentinel := errors.New("force outer rollback")
	err := transactor.InTransaction(ctx, func(txCtx context.Context) error {
		if cErr := svc.Create(txCtx, char, "initial_bind"); cErr != nil {
			return cErr
		}
		return sentinel // force the OUTER transaction to roll back
	})
	require.ErrorIs(t, err, sentinel)

	// All three rolled back together — re-entrant enrollment, one transaction.
	assert.Equal(t, 0, countCharacter(t, pool, char.ID))
	assert.Equal(t, 0, countBinding(t, pool, char.ID))
	assert.Equal(t, 0, countGenesisEnvelope(t, pool, char.ID))
}

// A failed step (character insert against a missing player FK) rolls the whole
// creation back — no partial character/binding/envelope state.
func TestCharacterGenesisCreateRollsBackOnFailedInsert(t *testing.T) {
	ctx := context.Background()
	pool := genesisPool(t)
	svc := newGenesisService(t, pool)
	// player_id references a player that does NOT exist -> character insert fails.
	char := genesisChar(t, ulid.Make(), "Orphan FK")

	err := svc.Create(ctx, char, "initial_bind")
	require.Error(t, err)

	assert.Equal(t, 0, countCharacter(t, pool, char.ID))
	assert.Equal(t, 0, countBinding(t, pool, char.ID))
	assert.Equal(t, 0, countGenesisEnvelope(t, pool, char.ID))
}
