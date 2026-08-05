// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver, mirroring goose's own connection shape
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/world/postgres"
)

// BackfillCharacterIdentity is exercised through a database/sql handle on
// purpose: its only production caller is plan 02-12's goose Go migration, which
// runs on a *sql.Tx. Driving it through the pgx pool would test a shape the
// migration never uses.
func backfillDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", testConnStr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedRawCharacter inserts a character by direct SQL with NO identity columns —
// the pre-backfill shape every row in a stock database has.
func seedRawCharacter(ctx context.Context, t *testing.T, name string) ulid.ULID {
	t.Helper()
	playerID := createTestPlayer(ctx, t)
	id := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, description, created_at)
		VALUES ($1, $2, $3, '', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, id.String(), playerID.String(), name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, id.String())
	})
	return id
}

func TestBackfillCharacterIdentityComputesTheThreeDerivedColumnsAndWritesNoName(t *testing.T) {
	ctx := context.Background()
	name := charFixtureName("backfill subject")
	id := seedRawCharacter(ctx, t, name)

	_, err := postgres.BackfillCharacterIdentity(ctx, backfillDB(t))
	require.NoError(t, err)

	key, skeleton, version := characterDBIdentity(ctx, t, id)
	normalized, nErr := charname.Normalize(name)
	require.NoError(t, nErr)
	assert.Equal(t, normalized.Key, key)
	assert.Equal(t, charname.Skeleton(normalized.Key), skeleton)
	assert.Equal(t, charname.UnicodeVersion, version)

	var storedName string
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT name FROM characters WHERE id = $1`, id.String()).Scan(&storedName))
	assert.Equal(t, name, storedName, "the backfill must never write characters.name")
}

func TestBackfillCharacterIdentityIsIdempotent(t *testing.T) {
	ctx := context.Background()
	id := seedRawCharacter(ctx, t, charFixtureName("idempotent"))
	db := backfillDB(t)

	_, err := postgres.BackfillCharacterIdentity(ctx, db)
	require.NoError(t, err)
	firstKey, firstSkeleton, firstVersion := characterDBIdentity(ctx, t, id)

	_, err = postgres.BackfillCharacterIdentity(ctx, db)
	require.NoError(t, err)
	secondKey, secondSkeleton, secondVersion := characterDBIdentity(ctx, t, id)

	assert.Equal(t, firstKey, secondKey)
	assert.Equal(t, firstSkeleton, secondSkeleton)
	assert.Equal(t, firstVersion, secondVersion)
}

func TestBackfillCharacterIdentityReportsNormalizedNameCollisions(t *testing.T) {
	ctx := context.Background()
	base := charFixtureName("collider")
	first := seedRawCharacter(ctx, t, base)
	second := seedRawCharacter(ctx, t, upperASCII(base))

	sets, err := postgres.BackfillCharacterIdentity(ctx, backfillDB(t))
	require.NoError(t, err)

	found := findCollisionSet(sets, postgres.CollisionNormalizedName, memberIDs(first, second))
	require.NotNil(t, found, "two rows differing only in case share a normalized key and must be reported")
	assert.Len(t, found.Members, 2)
	for _, m := range found.Members {
		assert.NotEmpty(t, m.Name)
		assert.NotEmpty(t, m.PlayerID)
		assert.NotZero(t, m.CreatedAt)
	}
}

// TestBackfillCharacterIdentityReportsSkeletonCollisionsWithDifferentNormalizedNames
// is D-30 part 3: the case a normalized_name-only scan can NEVER reach. NFKC
// deliberately does not collapse cross-script confusables, so a pre-existing
// confusable pair has DIFFERENT normalized names by construction — and without
// this scan nothing in this phase detects it and 000056's unique index passes
// straight over it.
func TestBackfillCharacterIdentityReportsSkeletonCollisionsWithDifferentNormalizedNames(t *testing.T) {
	ctx := context.Background()
	latinName, cyrillicName := confusablePair(t)
	latin := seedRawCharacter(ctx, t, latinName)
	cyrillic := seedRawCharacter(ctx, t, cyrillicName)

	sets, err := postgres.BackfillCharacterIdentity(ctx, backfillDB(t))
	require.NoError(t, err)

	found := findCollisionSet(sets, postgres.CollisionSkeleton, memberIDs(latin, cyrillic))
	require.NotNil(t, found,
		"a whole-script homoglyph pair must be reported as a SKELETON collision")
	assert.Len(t, found.Members, 2)

	// The paired negative: the same rows are NOT a normalized-name collision,
	// which is exactly why the second scan has to exist.
	assert.Nil(t, findCollisionSet(sets, postgres.CollisionNormalizedName, memberIDs(latin, cyrillic)),
		"the pair's normalized names differ — a normalized_name-only scan cannot see it")
}

func TestBackfillCharacterIdentityRunsNoGateAndRejectsNothing(t *testing.T) {
	ctx := context.Background()
	// A name the gate WOULD refuse today (it collides with a seeded skeleton)
	// must still be backfilled: these are names already admitted under the old
	// rules, and per D-17 the backfill detects and reports, never resolves.
	latinName, cyrillicName := confusablePair(t)
	latin := seedRawCharacter(ctx, t, latinName)
	cyrillic := seedRawCharacter(ctx, t, cyrillicName)

	_, err := postgres.BackfillCharacterIdentity(ctx, backfillDB(t))
	require.NoError(t, err, "the backfill must not reject a pre-existing confusable pair")

	for _, id := range []ulid.ULID{latin, cyrillic} {
		key, skeleton, version := characterDBIdentity(ctx, t, id)
		assert.NotEmpty(t, key)
		assert.NotEmpty(t, skeleton)
		assert.Equal(t, charname.UnicodeVersion, version)
	}
}

func memberIDs(ids ...ulid.ULID) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		out[id.String()] = true
	}
	return out
}

// findCollisionSet returns the set of the given kind whose members are exactly
// the expected ids, or nil. Matching on the ids rather than the key keeps the
// assertion honest in a shared database other tests have also seeded.
func findCollisionSet(sets []postgres.IdentityCollisionSet, kind postgres.IdentityCollisionKind, want map[string]bool) *postgres.IdentityCollisionSet {
	for i := range sets {
		if sets[i].Kind != kind || len(sets[i].Members) != len(want) {
			continue
		}
		all := true
		for _, m := range sets[i].Members {
			if !want[m.ID] {
				all = false
				break
			}
		}
		if all {
			return &sets[i]
		}
	}
	return nil
}
