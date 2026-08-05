// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	worldpg "github.com/holomush/holomush/internal/world/postgres"
)

// insertPlayerWithCharacters seeds one player owning len(names) characters and
// returns the player id plus the character ids in the order given.
//
// It deliberately does NOT reuse insertCharacterAt (parent_location_resolver_test.go),
// which mints a FRESH player per character — the whole point of these fixtures is
// several characters under ONE player.
func insertPlayerWithCharacters(t *testing.T, pool *pgxpool.Pool, names ...string) (ulid.ULID, []ulid.ULID) {
	t.Helper()
	ctx := context.Background()

	playerID := ulid.Make()
	_, err := pool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'hash')`,
		playerID.String(), "p_"+playerID.String())
	require.NoError(t, err)

	charIDs := make([]ulid.ULID, 0, len(names))
	for _, name := range names {
		charID := ulid.Make()
		_, err := pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name)
			VALUES ($1, $2, $3)`,
			charID.String(), playerID.String(), name+"_"+charID.String())
		require.NoError(t, err)
		charIDs = append(charIDs, charID)
	}
	return playerID, charIDs
}

// insertOrphanCharacter seeds a character row with a NULL player_id.
func insertOrphanCharacter(t *testing.T, pool *pgxpool.Pool, name string) ulid.ULID {
	t.Helper()
	charID := ulid.Make()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO characters (id, player_id, name)
		VALUES ($1, NULL, $2)`,
		charID.String(), name+"_"+charID.String())
	require.NoError(t, err)
	return charID
}

func strs(ids []ulid.ULID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// TestCharacterOwnerResolverReturnsEachOwningPlayersCompleteCharacterSet is the
// load-bearing shape assertion for D-27's permit-side ALL direction.
//
// The resolver MUST return each owning player's COMPLETE character set, not
// merely the members that appeared in the input. "Every character of this player
// appears in the row's field" cannot be evaluated from a flat list of players who
// happen to own one of the listed characters — a flat []string is the shape a
// plain UNION needs, and returning it would make the union the only implementable
// rule regardless of what the maintainer decided.
func TestCharacterOwnerResolverReturnsEachOwningPlayersCompleteCharacterSet(t *testing.T) {
	pool := newTestPool(t)
	r := worldpg.NewCharacterOwnerResolver(pool)

	// Alice holds two characters; only ONE of them is named in the input.
	alice, aliceChars := insertPlayerWithCharacters(t, pool, "AliceOne", "AliceTwo")
	// Bob holds exactly one character, which is named.
	bob, bobChars := insertPlayerWithCharacters(t, pool, "BobOnly")

	got, err := r.ResolveOwnerScopes(context.Background(), []string{
		aliceChars[0].String(),
		bobChars[0].String(),
	})
	require.NoError(t, err)

	require.Len(t, got, 2, "one entry per OWNING player")
	assert.ElementsMatch(t, strs(aliceChars), got[alice.String()],
		"Alice's COMPLETE set must come back, including the character NOT in the input")
	assert.ElementsMatch(t, strs(bobChars), got[bob.String()])
}

func TestCharacterOwnerResolverSkipsUnknownCharacterIDs(t *testing.T) {
	pool := newTestPool(t)
	r := worldpg.NewCharacterOwnerResolver(pool)

	bob, bobChars := insertPlayerWithCharacters(t, pool, "BobOnly")

	got, err := r.ResolveOwnerScopes(context.Background(), []string{
		bobChars[0].String(),
		ulid.Make().String(), // no such character row
		"not-even-a-ulid",    // not an id shape at all
	})
	require.NoError(t, err, "an unknown character id is not an error — it simply owns nothing")
	require.Len(t, got, 1)
	assert.ElementsMatch(t, strs(bobChars), got[bob.String()])
}

func TestCharacterOwnerResolverSkipsCharactersWithANullPlayerID(t *testing.T) {
	pool := newTestPool(t)
	r := worldpg.NewCharacterOwnerResolver(pool)

	orphan := insertOrphanCharacter(t, pool, "Orphan")

	got, err := r.ResolveOwnerScopes(context.Background(), []string{orphan.String()})
	require.NoError(t, err)
	assert.Empty(t, got, "a NULL player_id yields no player entry — never an empty-string key")
}

func TestCharacterOwnerResolverIssuesNoQueryForAnEmptyInput(t *testing.T) {
	// A nil pool proves the short-circuit: any query attempt panics.
	r := worldpg.NewCharacterOwnerResolver(nil)

	got, err := r.ResolveOwnerScopes(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = r.ResolveOwnerScopes(context.Background(), []string{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestCharacterOwnerResolverExcludesAnOrphanCharacterFromAnOwningPlayersScope
// pins that a player's returned scope set carries only THEIR characters — a
// leaked row would silently break the ALL direction in the safe-looking way
// (an extra member makes "every character appears" false, denying a permit that
// should have been granted) and the ANY direction in the unsafe way.
func TestCharacterOwnerResolverExcludesAnOrphanCharacterFromAnOwningPlayersScope(t *testing.T) {
	pool := newTestPool(t)
	r := worldpg.NewCharacterOwnerResolver(pool)

	alice, aliceChars := insertPlayerWithCharacters(t, pool, "AliceOne")
	_ = insertOrphanCharacter(t, pool, "Orphan")
	_, _ = insertPlayerWithCharacters(t, pool, "StrangerOne", "StrangerTwo")

	got, err := r.ResolveOwnerScopes(context.Background(), []string{aliceChars[0].String()})
	require.NoError(t, err)
	require.Len(t, got, 1, "only the owning player is returned")
	assert.Equal(t, strs(aliceChars), got[alice.String()])
}
