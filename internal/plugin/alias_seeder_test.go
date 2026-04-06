// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakeAliasSeeder implements AliasSeeder for testing. GetSystemAliases can be
// configured to fail on the Nth call (1-indexed) via failOnGetCall to exercise
// the initial fetch and cache reload error paths independently.
type fakeAliasSeeder struct {
	existing      map[string]string
	creators      map[string]string
	sources       map[string]string
	setErr        error
	getCallCount  int
	failOnGetCall int   // 1-indexed; 0 disables
	getErr        error // returned when failOnGetCall matches
}

func newFakeAliasSeeder() *fakeAliasSeeder {
	return &fakeAliasSeeder{
		existing: make(map[string]string),
		creators: make(map[string]string),
		sources:  make(map[string]string),
	}
}

func (f *fakeAliasSeeder) GetSystemAliases(_ context.Context) (map[string]string, error) {
	f.getCallCount++
	if f.failOnGetCall > 0 && f.getCallCount == f.failOnGetCall {
		return nil, f.getErr
	}
	cp := make(map[string]string, len(f.existing))
	for k, v := range f.existing {
		cp[k] = v
	}
	return cp, nil
}

func (f *fakeAliasSeeder) SetSystemAlias(_ context.Context, alias, cmd, createdBy, source string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.existing[alias] = cmd
	f.creators[alias] = createdBy
	f.sources[alias] = source
	return nil
}

func TestCollectManifestAliasesGathersFromMultiplePlugins(t *testing.T) {
	loaded := []*DiscoveredPlugin{
		{
			Manifest: &Manifest{
				Name: "comms",
				Commands: []CommandSpec{
					{Name: "say", Aliases: []string{`"`}},
					{Name: "pose", Aliases: []string{":"}},
				},
			},
		},
		{
			Manifest: &Manifest{
				Name: "nav",
				Commands: []CommandSpec{
					{Name: "look", Aliases: []string{"l"}},
					{Name: "north", Aliases: []string{"n"}},
				},
			},
		},
	}

	got := CollectManifestAliases(loaded)

	assert.Len(t, got, 4)
	assert.Equal(t, ManifestAlias{Alias: `"`, Command: "say", Plugin: "comms"}, got[0])
	assert.Equal(t, ManifestAlias{Alias: ":", Command: "pose", Plugin: "comms"}, got[1])
	assert.Equal(t, ManifestAlias{Alias: "l", Command: "look", Plugin: "nav"}, got[2])
	assert.Equal(t, ManifestAlias{Alias: "n", Command: "north", Plugin: "nav"}, got[3])
}

func TestCollectManifestAliasesSkipsDuplicateAcrossPlugins(t *testing.T) {
	loaded := []*DiscoveredPlugin{
		{
			Manifest: &Manifest{
				Name: "comms",
				Commands: []CommandSpec{
					{Name: "say", Aliases: []string{`"`}},
				},
			},
		},
		{
			Manifest: &Manifest{
				Name: "other",
				Commands: []CommandSpec{
					{Name: "shout", Aliases: []string{`"`}},
				},
			},
		},
	}

	got := CollectManifestAliases(loaded)

	assert.Len(t, got, 1)
	assert.Equal(t, ManifestAlias{Alias: `"`, Command: "say", Plugin: "comms"}, got[0])
}

func TestCollectManifestAliasesReturnsEmptyWhenNoAliases(t *testing.T) {
	loaded := []*DiscoveredPlugin{
		{
			Manifest: &Manifest{
				Name: "comms",
				Commands: []CommandSpec{
					{Name: "say"},
				},
			},
		},
	}

	got := CollectManifestAliases(loaded)
	assert.Empty(t, got)
}

func TestSeedManifestAliasesSeedsNewAndLoadsCache(t *testing.T) {
	repo := newFakeAliasSeeder()
	cache := command.NewAliasCache()
	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "comms"},
		{Alias: ":", Command: "pose", Plugin: "comms"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	assert.Equal(t, "say", repo.existing[`"`])
	assert.Equal(t, "pose", repo.existing[":"])

	cached := cache.ListSystemAliases()
	assert.Equal(t, "say", cached[`"`])
	assert.Equal(t, "pose", cached[":"])
}

func TestSeedManifestAliasesSkipsExistingAliases(t *testing.T) {
	repo := newFakeAliasSeeder()
	repo.existing[`"`] = "shout"

	cache := command.NewAliasCache()
	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "comms"},
		{Alias: ":", Command: "pose", Plugin: "comms"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	// Existing alias is NOT overwritten.
	assert.Equal(t, "shout", repo.existing[`"`])
	// New alias is seeded.
	assert.Equal(t, "pose", repo.existing[":"])
}

func TestSeedManifestAliasesContinuesOnSetError(t *testing.T) {
	repo := newFakeAliasSeeder()
	repo.setErr = errors.New("db write failed")

	cache := command.NewAliasCache()
	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "comms"},
		{Alias: ":", Command: "pose", Plugin: "comms"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	// Neither alias was stored because repo.setErr is non-nil.
	assert.Empty(t, repo.existing)
}

func TestSeedManifestAliasesUsesNullCreatedByForManifestSeeding(t *testing.T) {
	repo := newFakeAliasSeeder()
	cache := command.NewAliasCache()
	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "comms"},
		{Alias: "l", Command: "look", Plugin: "nav"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	// Manifest-seeded aliases use empty createdBy (NULL in DB) because
	// the created_by column references players(id) and plugin names are
	// not valid player IDs.
	assert.Equal(t, "", repo.creators[`"`])
	assert.Equal(t, "", repo.creators["l"])
}

func TestSeedManifestAliasesRecordsPluginNameAsSource(t *testing.T) {
	repo := newFakeAliasSeeder()
	cache := command.NewAliasCache()
	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "comms"},
		{Alias: "l", Command: "look", Plugin: "nav"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	assert.Equal(t, "comms", repo.sources[`"`])
	assert.Equal(t, "nav", repo.sources["l"])
}

func TestSeedManifestAliasesReturnsErrorOnInitialFetchFailure(t *testing.T) {
	repo := newFakeAliasSeeder()
	repo.failOnGetCall = 1
	repo.getErr = errors.New("db down")
	cache := command.NewAliasCache()

	err := SeedManifestAliases(context.Background(),
		[]ManifestAlias{{Alias: `"`, Command: "say", Plugin: "comms"}},
		repo, cache)

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ALIAS_SEED_FETCH_FAILED")
}

func TestSeedManifestAliasesReturnsErrorOnCacheReloadFailure(t *testing.T) {
	repo := newFakeAliasSeeder()
	// Succeed on first call (initial fetch), fail on second (cache reload).
	repo.failOnGetCall = 2
	repo.getErr = errors.New("connection lost")
	cache := command.NewAliasCache()

	err := SeedManifestAliases(context.Background(),
		[]ManifestAlias{{Alias: `"`, Command: "say", Plugin: "comms"}},
		repo, cache)

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ALIAS_CACHE_RELOAD_FAILED")
}

func TestSeedManifestAliasesSkipsInBatchDuplicates(t *testing.T) {
	repo := newFakeAliasSeeder()
	cache := command.NewAliasCache()

	// Two entries with the same alias — second should be skipped so it
	// cannot upsert over the first's plugin attribution.
	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "comms"},
		{Alias: `"`, Command: "shout", Plugin: "other"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	// First entry wins — source stays "comms", command stays "say".
	assert.Equal(t, "say", repo.existing[`"`])
	assert.Equal(t, "comms", repo.sources[`"`])
}
