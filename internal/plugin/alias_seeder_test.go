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
)

// fakeAliasSeeder implements AliasSeeder for testing.
type fakeAliasSeeder struct {
	existing map[string]string
	creators map[string]string
	setErr   error
}

func newFakeAliasSeeder() *fakeAliasSeeder {
	return &fakeAliasSeeder{
		existing: make(map[string]string),
		creators: make(map[string]string),
	}
}

func (f *fakeAliasSeeder) GetSystemAliases(_ context.Context) (map[string]string, error) {
	cp := make(map[string]string, len(f.existing))
	for k, v := range f.existing {
		cp[k] = v
	}
	return cp, nil
}

func (f *fakeAliasSeeder) SetSystemAlias(_ context.Context, alias, cmd, createdBy string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.existing[alias] = cmd
	f.creators[alias] = createdBy
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

	got, err := CollectManifestAliases(loaded)
	require.NoError(t, err)

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

	got, err := CollectManifestAliases(loaded)
	require.NoError(t, err)

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

	got, err := CollectManifestAliases(loaded)
	require.NoError(t, err)
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

func TestSeedManifestAliasesSetsCreatedByToPluginName(t *testing.T) {
	repo := newFakeAliasSeeder()
	cache := command.NewAliasCache()
	aliases := []ManifestAlias{
		{Alias: `"`, Command: "say", Plugin: "comms"},
		{Alias: "l", Command: "look", Plugin: "nav"},
	}

	err := SeedManifestAliases(context.Background(), aliases, repo, cache)
	require.NoError(t, err)

	assert.Equal(t, "comms", repo.creators[`"`])
	assert.Equal(t, "nav", repo.creators["l"])
}
