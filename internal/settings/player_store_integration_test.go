// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package settings_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/test/testutil"
)

// newRepoPlayerStore stands up a fresh migrated database, a pgx pool, and a
// repo-backed PlayerSettings store sharing the same player repository.
func newRepoPlayerStore(t *testing.T) (*settings.PlayerSettings, *postgres.PlayerRepository) {
	t.Helper()
	ctx := context.Background()
	connStr := testutil.FreshDatabase(t, testutil.SharedPostgres(t))
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	repo := postgres.NewPlayerRepository(pool)
	return settings.NewRepoPlayerSettingsStore(repo), repo
}

// TestRepoPlayerSettingsPersistsOwnerPartitionAcrossHandles is the persist+
// readback invariant: a write under Plugin(name).SetStringSlice persists via the
// commit func (GetByID -> mutate -> Update), and a FRESH handle re-reading the
// player from the repo observes the value.
func TestRepoPlayerSettingsPersistsOwnerPartitionAcrossHandles(t *testing.T) {
	ctx := context.Background()
	st, repo := newRepoPlayerStore(t)

	player, err := auth.NewPlayer("settingsalice", nil, "hash")
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, player))

	// Write under an owner partition; the non-nil commit func must persist it.
	require.NoError(
		t,
		st.For(ctx, player.ID).
			Plugin("core-scenes").
			SetStringSlice(ctx, "content.cw_block", []string{"violence"}),
	)

	// A FRESH handle re-reads the player from the repo and must see the value.
	got, ok := st.For(ctx, player.ID).
		Plugin("core-scenes").
		StringSliceN(ctx, "content.cw_block")
	require.True(t, ok)
	assert.Equal(t, []string{"violence"}, got)

	// Typed preference fields must survive the read-modify-write cycle.
	reloaded, err := repo.GetByID(ctx, player.ID)
	require.NoError(t, err)
	assert.Equal(t, player.Username, reloaded.Username)
}

// TestRepoPlayerSettingsDoesNotLostUpdateSiblingOwners proves the commit func
// re-reads and merges, so a write to one owner partition does not clobber a
// sibling owner's partition persisted by a separate For() call.
func TestRepoPlayerSettingsDoesNotLostUpdateSiblingOwners(t *testing.T) {
	ctx := context.Background()
	st, repo := newRepoPlayerStore(t)

	player, err := auth.NewPlayer("settingsbob", nil, "hash")
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, player))

	require.NoError(
		t,
		st.For(ctx, player.ID).Plugin("owner_a").
			SetStringSlice(ctx, "k", []string{"a"}),
	)
	require.NoError(
		t,
		st.For(ctx, player.ID).Plugin("owner_b").
			SetStringSlice(ctx, "k", []string{"b"}),
	)

	gotA, okA := st.For(ctx, player.ID).Plugin("owner_a").StringSliceN(ctx, "k")
	require.True(t, okA, "owner_a partition must survive the owner_b write")
	assert.Equal(t, []string{"a"}, gotA)

	gotB, okB := st.For(ctx, player.ID).Plugin("owner_b").StringSliceN(ctx, "k")
	require.True(t, okB)
	assert.Equal(t, []string{"b"}, gotB)
}

// TestRepoPlayerSettingsConcurrentHandlesDoNotLoseUpdate is the player mirror of
// the character concurrency test: two handles opened while both owner partitions
// exist, each mutating a different owner, must both persist. Distinct from
// DoesNotLostUpdateSiblingOwners above, which writes sequentially (each fresh
// For() re-reads after the prior commit) and so cannot surface the cross-owner
// lost-update that dirty-owner tracking fixes.
func TestRepoPlayerSettingsConcurrentHandlesDoNotLoseUpdate(t *testing.T) {
	ctx := context.Background()
	st, repo := newRepoPlayerStore(t)

	player, err := auth.NewPlayer("settingscarol", nil, "hash")
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, player))

	// Pre-seed both owner partitions so each handle below loads both.
	require.NoError(t, st.For(ctx, player.ID).Plugin("owner_a").SetStringSlice(ctx, "k", []string{"a1"}))
	require.NoError(t, st.For(ctx, player.ID).Plugin("owner_b").SetStringSlice(ctx, "k", []string{"b1"}))

	// Open both handles BEFORE either commits — each loads {owner_a:[a1], owner_b:[b1]}.
	handleA := st.For(ctx, player.ID)
	handleB := st.For(ctx, player.ID)

	require.NoError(t, handleA.Plugin("owner_a").SetStringSlice(ctx, "k", []string{"a2"}))
	require.NoError(t, handleB.Plugin("owner_b").SetStringSlice(ctx, "k", []string{"b2"}))

	gotA, okA := st.For(ctx, player.ID).Plugin("owner_a").StringSliceN(ctx, "k")
	require.True(t, okA)
	assert.Equal(t, []string{"a2"}, gotA, "owner_a update must survive owner_b's concurrent commit")

	gotB, okB := st.For(ctx, player.ID).Plugin("owner_b").StringSliceN(ctx, "k")
	require.True(t, okB)
	assert.Equal(t, []string{"b2"}, gotB)
}
