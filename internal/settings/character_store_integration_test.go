// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package settings_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// newRepoCharacterStore stands up a fresh migrated database, a pgx pool, and a
// repo-backed CharacterSettings store over the characters.preferences column.
func newRepoCharacterStore(t *testing.T) (*settings.CharacterSettings, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	connStr := testutil.FreshDatabase(t, testutil.SharedPostgres(t))
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	repo := store.NewCharacterSettingsRepository(pool)
	return settings.NewRepoCharacterSettingsStore(repo), pool
}

// seedCharacter inserts a player and a character (with default empty
// preferences) and returns the character ID.
func seedCharacter(t *testing.T, ctx context.Context, pool *pgxpool.Pool) ulid.ULID {
	t.Helper()
	playerID := idgen.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, $3)`,
		playerID.String(), "charsettings-"+playerID.String(), "hash")
	require.NoError(t, err)

	characterID := idgen.New()
	_, err = pool.Exec(ctx,
		`INSERT INTO characters (id, player_id, name) VALUES ($1, $2, $3)`,
		characterID.String(), playerID.String(), "Hero")
	require.NoError(t, err)
	return characterID
}

// TestRepoCharacterSettingsPersistsOwnerPartitionAcrossHandles is the persist+
// readback invariant: a write under Owner(name).SetStringSlice persists via the
// commit func (GetPreferences -> mutate -> SetPreferences), and a FRESH handle
// re-reading the character from the repo observes the value.
func TestRepoCharacterSettingsPersistsOwnerPartitionAcrossHandles(t *testing.T) {
	ctx := context.Background()
	st, pool := newRepoCharacterStore(t)
	characterID := seedCharacter(t, ctx, pool)

	// Write under an owner partition; the non-nil commit func must persist it.
	require.NoError(
		t,
		st.For(ctx, characterID).
			Owner("core-scenes").
			SetStringSlice(ctx, "content.cw_block", []string{"spiders", "gore"}),
	)

	// A FRESH handle re-reads the character from the repo and must see the value.
	got, ok := st.For(ctx, characterID).
		Owner("core-scenes").
		StringSliceN(ctx, "content.cw_block")
	require.True(t, ok)
	assert.Equal(t, []string{"spiders", "gore"}, got)
}

// TestRepoCharacterSettingsDoesNotLostUpdateSiblingOwners proves the commit func
// re-reads and merges, so a write to one owner partition does not clobber a
// sibling owner's partition persisted by a separate For() call.
func TestRepoCharacterSettingsDoesNotLostUpdateSiblingOwners(t *testing.T) {
	ctx := context.Background()
	st, pool := newRepoCharacterStore(t)
	characterID := seedCharacter(t, ctx, pool)

	require.NoError(
		t,
		st.For(ctx, characterID).Owner("owner_a").
			SetStringSlice(ctx, "k", []string{"a"}),
	)
	require.NoError(
		t,
		st.For(ctx, characterID).Owner("owner_b").
			SetStringSlice(ctx, "k", []string{"b"}),
	)

	gotA, okA := st.For(ctx, characterID).Owner("owner_a").StringSliceN(ctx, "k")
	require.True(t, okA, "owner_a partition must survive the owner_b write")
	assert.Equal(t, []string{"a"}, gotA)

	gotB, okB := st.For(ctx, characterID).Owner("owner_b").StringSliceN(ctx, "k")
	require.True(t, okB)
	assert.Equal(t, []string{"b"}, gotB)
}

// TestRepoCharacterSettingsReturnsEmptyViewForUnprovisionedCharacter proves an
// unknown character reads back as empty (no error, all keys unset), matching the
// Settings reads-never-error contract.
func TestRepoCharacterSettingsReturnsEmptyViewForUnprovisionedCharacter(t *testing.T) {
	ctx := context.Background()
	st, _ := newRepoCharacterStore(t)

	_, ok := st.For(ctx, idgen.New()).Owner("core-scenes").StringSliceN(ctx, "content.cw_block")
	assert.False(t, ok)
}

// TestCharacterSettingsRepoErrorsWritingMissingCharacter proves the repo's
// SetPreferences fails loudly (CHARACTER_NOT_FOUND) when the target character
// row does not exist, rather than silently discarding the write. The UPDATE
// affects zero rows for an absent id; without the RowsAffected guard pgx
// reports success and the write is lost.
func TestCharacterSettingsRepoErrorsWritingMissingCharacter(t *testing.T) {
	ctx := context.Background()
	connStr := testutil.FreshDatabase(t, testutil.SharedPostgres(t))
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	repo := store.NewCharacterSettingsRepository(pool)

	err = repo.SetPreferences(ctx, idgen.New(), settings.CharacterPreferences{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
}

// TestRepoCharacterSettingsConcurrentHandlesDoNotLoseUpdate proves the dirty-
// owner tracking fix. Two For() handles are opened against the same character
// while BOTH owner partitions already exist (so both handles load both owners);
// each handle then mutates a DIFFERENT owner. Both updates must survive. Before
// the fix, each commit re-serialized every loaded owner, so the second commit
// wrote its stale copy of the first handle's owner back, losing that update.
func TestRepoCharacterSettingsConcurrentHandlesDoNotLoseUpdate(t *testing.T) {
	ctx := context.Background()
	st, pool := newRepoCharacterStore(t)
	characterID := seedCharacter(t, ctx, pool)

	// Pre-seed both owner partitions so each handle below loads both.
	require.NoError(t, st.For(ctx, characterID).Owner("owner_a").SetStringSlice(ctx, "k", []string{"a1"}))
	require.NoError(t, st.For(ctx, characterID).Owner("owner_b").SetStringSlice(ctx, "k", []string{"b1"}))

	// Open both handles BEFORE either commits — each loads {owner_a:[a1], owner_b:[b1]}.
	handleA := st.For(ctx, characterID)
	handleB := st.For(ctx, characterID)

	// Each handle mutates a different owner and commits.
	require.NoError(t, handleA.Owner("owner_a").SetStringSlice(ctx, "k", []string{"a2"}))
	require.NoError(t, handleB.Owner("owner_b").SetStringSlice(ctx, "k", []string{"b2"}))

	// Both updates must survive: dirty tracking serializes only the mutated owner,
	// so handleB's commit does not rewrite owner_a with its stale loaded copy.
	gotA, okA := st.For(ctx, characterID).Owner("owner_a").StringSliceN(ctx, "k")
	require.True(t, okA)
	assert.Equal(t, []string{"a2"}, gotA, "owner_a update must survive owner_b's concurrent commit")

	gotB, okB := st.For(ctx, characterID).Owner("owner_b").StringSliceN(ctx, "k")
	require.True(t, okB)
	assert.Equal(t, []string{"b2"}, gotB)
}
