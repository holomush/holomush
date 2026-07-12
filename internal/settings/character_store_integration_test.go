// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package settings_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// newWorldPrefsWriter builds a production-shaped world.Service over pool — the
// world boundary the character-settings write routes through after the round-4
// C5 / D-05 fold-in. It wires the guarded character writer + the same-tx outbox +
// the re-entrant transactor so world.Service.UpdateCharacterPreferences performs
// the version-guarded UPDATE characters SET preferences and emits its envelope.
// The engine is permissive because UpdateCharacterPreferences runs no checkAccess
// (settings persistence primitive; authorization happens at the command layer).
func newWorldPrefsWriter(t *testing.T, pool *pgxpool.Pool) *world.Service {
	t.Helper()
	return world.NewService(world.ServiceConfig{
		CharacterRepo: worldpostgres.NewCharacterRepository(pool),
		Transactor:    worldpostgres.NewTransactor(pool),
		OutboxWriter:  worldpostgres.NewOutboxStore(pool),
		Engine:        policytest.AllowAllEngine(),
		GameID:        "main",
	})
}

// newRepoCharacterStore stands up a fresh migrated database, a pgx pool, and a
// repo-backed CharacterSettings store whose preferences WRITE routes through the
// world boundary (round-4 C5 / D-05) and whose READ is a direct pool read.
func newRepoCharacterStore(t *testing.T) (*settings.CharacterSettings, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	connStr := testutil.FreshDatabase(t, testutil.SharedPostgres(t))
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	repo := store.NewCharacterSettingsRepository(pool, newWorldPrefsWriter(t, pool))
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
// readback invariant: a write under Plugin(name).SetStringSlice persists via the
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
			Plugin("core-scenes").
			SetStringSlice(ctx, "content.cw_block", []string{"spiders", "gore"}),
	)

	// A FRESH handle re-reads the character from the repo and must see the value.
	got, ok := st.For(ctx, characterID).
		Plugin("core-scenes").
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
		st.For(ctx, characterID).Plugin("owner_a").
			SetStringSlice(ctx, "k", []string{"a"}),
	)
	require.NoError(
		t,
		st.For(ctx, characterID).Plugin("owner_b").
			SetStringSlice(ctx, "k", []string{"b"}),
	)

	gotA, okA := st.For(ctx, characterID).Plugin("owner_a").StringSliceN(ctx, "k")
	require.True(t, okA, "owner_a partition must survive the owner_b write")
	assert.Equal(t, []string{"a"}, gotA)

	gotB, okB := st.For(ctx, characterID).Plugin("owner_b").StringSliceN(ctx, "k")
	require.True(t, okB)
	assert.Equal(t, []string{"b"}, gotB)
}

// TestRepoCharacterSettingsReturnsEmptyViewForUnprovisionedCharacter proves an
// unknown character reads back as empty (no error, all keys unset), matching the
// Settings reads-never-error contract.
func TestRepoCharacterSettingsReturnsEmptyViewForUnprovisionedCharacter(t *testing.T) {
	ctx := context.Background()
	st, _ := newRepoCharacterStore(t)

	_, ok := st.For(ctx, idgen.New()).Plugin("core-scenes").StringSliceN(ctx, "content.cw_block")
	assert.False(t, ok)
}

// TestCharacterSettingsRepoErrorsWritingMissingCharacter proves the repo's
// SetPreferences fails loudly (CHARACTER_NOT_FOUND) when the target character
// row does not exist, rather than silently discarding the write. Routed through
// the world boundary (round-4 C5 / D-05), the version-guarded UPDATE affects zero
// rows for an absent id and the locked follow-up read classifies it as
// CHARACTER_NOT_FOUND — the write is never silently lost.
func TestCharacterSettingsRepoErrorsWritingMissingCharacter(t *testing.T) {
	ctx := context.Background()
	connStr := testutil.FreshDatabase(t, testutil.SharedPostgres(t))
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	repo := store.NewCharacterSettingsRepository(pool, newWorldPrefsWriter(t, pool))

	err = repo.SetPreferences(ctx, idgen.New(), settings.CharacterPreferences{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_NOT_FOUND")
}

// TestRepoCharacterSettingsConcurrentWritesSurfaceConflict is the round-6 grok
// MEDIUM guard for the RMW race the fold-in introduces (round-4 C5 / D-05): the
// character-settings write is now an internal read-version-then-CAS through the
// world boundary. Two genuinely concurrent SetPreferences against the same
// character race the CAS; exactly ONE must win and the other must surface the
// typed WORLD_CONCURRENT_EDIT (MODEL-03) — never a silent lost update. The
// settings caller RECEIVES the typed error and surfaces it (D-02 — no auto-retry).
//
// The conflict is a genuine race, so a single concurrent pair MAY serialize
// (both succeed with no loss). We retry the concurrent pair until the guard is
// observed to fire, bounded — a correct guard manifests the conflict quickly; a
// broken guard (no CAS) would never conflict and the test fails at the cap.
func TestRepoCharacterSettingsConcurrentWritesSurfaceConflict(t *testing.T) {
	ctx := context.Background()
	st, pool := newRepoCharacterStore(t)
	characterID := seedCharacter(t, ctx, pool)

	const maxAttempts = 200
	var sawConflict bool
	for attempt := 0; attempt < maxAttempts && !sawConflict; attempt++ {
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		values := []string{"racer-a", "racer-b"}
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-start // release both writers together to force the read-then-CAS race
				errs[idx] = st.For(ctx, characterID).
					Plugin("racer").
					SetStringSlice(ctx, "k", []string{values[idx]})
			}(i)
		}
		close(start)
		wg.Wait()

		conflicts := 0
		for _, e := range errs {
			if e == nil {
				continue
			}
			// The only tolerated failure is the typed conflict — never a silent
			// lost update, never any other error.
			require.ErrorIs(t, e, world.ErrConcurrentEdit,
				"a concurrent settings write may only fail with WORLD_CONCURRENT_EDIT")
			errutil.AssertErrorCode(t, e, world.CodeConcurrentEdit)
			conflicts++
		}
		if conflicts > 0 {
			// When a conflict fired, exactly one writer must have failed (the other won).
			require.Equal(t, 1, conflicts,
				"exactly one concurrent writer surfaces WORLD_CONCURRENT_EDIT; the other succeeds")
			sawConflict = true
		}

		// Whatever the outcome, the row is never silently lost: the persisted value
		// is one of the two racers (the winner), never absent or corrupted.
		got, ok := st.For(ctx, characterID).Plugin("racer").StringSliceN(ctx, "k")
		require.True(t, ok, "the winning write must persist")
		require.Len(t, got, 1)
		assert.Contains(t, values, got[0])
	}

	require.True(t, sawConflict,
		"the version guard must surface WORLD_CONCURRENT_EDIT under concurrent settings writes within %d attempts", maxAttempts)
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
	require.NoError(t, st.For(ctx, characterID).Plugin("owner_a").SetStringSlice(ctx, "k", []string{"a1"}))
	require.NoError(t, st.For(ctx, characterID).Plugin("owner_b").SetStringSlice(ctx, "k", []string{"b1"}))

	// Open both handles BEFORE either commits — each loads {owner_a:[a1], owner_b:[b1]}.
	handleA := st.For(ctx, characterID)
	handleB := st.For(ctx, characterID)

	// Each handle mutates a different owner and commits.
	require.NoError(t, handleA.Plugin("owner_a").SetStringSlice(ctx, "k", []string{"a2"}))
	require.NoError(t, handleB.Plugin("owner_b").SetStringSlice(ctx, "k", []string{"b2"}))

	// Both updates must survive: dirty tracking serializes only the mutated owner,
	// so handleB's commit does not rewrite owner_a with its stale loaded copy.
	gotA, okA := st.For(ctx, characterID).Plugin("owner_a").StringSliceN(ctx, "k")
	require.True(t, okA)
	assert.Equal(t, []string{"a2"}, gotA, "owner_a update must survive owner_b's concurrent commit")

	gotB, okB := st.For(ctx, characterID).Plugin("owner_b").StringSliceN(ctx, "k")
	require.True(t, okB)
	assert.Equal(t, []string{"b2"}, gotB)
}
