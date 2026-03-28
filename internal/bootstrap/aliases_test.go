// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
)

// fakeAliasRepo implements AliasSeeder for testing.
type fakeAliasRepo struct {
	aliases  map[string]string
	setCalls int
}

func newFakeAliasRepo() *fakeAliasRepo {
	return &fakeAliasRepo{aliases: make(map[string]string)}
}

func (f *fakeAliasRepo) GetSystemAliases(_ context.Context) (map[string]string, error) {
	cp := make(map[string]string, len(f.aliases))
	for k, v := range f.aliases {
		cp[k] = v
	}
	return cp, nil
}

func (f *fakeAliasRepo) SetSystemAlias(_ context.Context, alias, cmd, _ string) error {
	f.aliases[alias] = cmd
	f.setCalls++
	return nil
}

func TestSeedSystemAliases(t *testing.T) {
	tests := []struct {
		name         string
		preExisting  map[string]string
		wantSetCalls int
		wantAliases  map[string]string
	}{
		{
			name:         "seeds all three on empty repo",
			preExisting:  nil,
			wantSetCalls: 3,
			wantAliases:  map[string]string{`"`: "say", ":": "pose", ";": "pose"},
		},
		{
			name:         "skips existing aliases",
			preExisting:  map[string]string{`"`: "say"},
			wantSetCalls: 2,
			wantAliases:  map[string]string{`"`: "say", ":": "pose", ";": "pose"},
		},
		{
			name:         "all pre-existing seeds nothing",
			preExisting:  map[string]string{`"`: "say", ":": "pose", ";": "pose"},
			wantSetCalls: 0,
			wantAliases:  map[string]string{`"`: "say", ":": "pose", ";": "pose"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeAliasRepo()
			for k, v := range tt.preExisting {
				repo.aliases[k] = v
			}
			cache := command.NewAliasCache()

			err := SeedSystemAliases(context.Background(), repo, cache)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSetCalls, repo.setCalls)
			for alias, cmd := range tt.wantAliases {
				assert.Equal(t, cmd, repo.aliases[alias], "alias %q", alias)
			}
		})
	}
}

func TestSeedSystemAliases_Idempotent(t *testing.T) {
	repo := newFakeAliasRepo()
	cache := command.NewAliasCache()

	err := SeedSystemAliases(context.Background(), repo, cache)
	require.NoError(t, err)
	assert.Equal(t, 3, repo.setCalls)

	// Second call should seed nothing new.
	err = SeedSystemAliases(context.Background(), repo, cache)
	require.NoError(t, err)
	assert.Equal(t, 3, repo.setCalls)
}

func TestSeedSystemAliases_AlwaysLoadsCache(t *testing.T) {
	repo := newFakeAliasRepo()
	repo.aliases[`"`] = "say"
	repo.aliases[":"] = "pose"
	repo.aliases[";"] = "pose"
	cache := command.NewAliasCache()

	err := SeedSystemAliases(context.Background(), repo, cache)
	require.NoError(t, err)
	assert.Equal(t, 0, repo.setCalls)

	// Cache should still have all aliases loaded.
	tests := []struct {
		alias   string
		wantCmd string
	}{
		{`"`, "say"},
		{":", "pose"},
		{";", "pose"},
	}
	for _, tt := range tests {
		cmd, ok := cache.GetSystemAlias(tt.alias)
		assert.True(t, ok, "alias %q should be in cache", tt.alias)
		assert.Equal(t, tt.wantCmd, cmd, "alias %q", tt.alias)
	}
}
