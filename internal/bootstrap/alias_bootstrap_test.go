// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestAliasBootstrapper_Priority(t *testing.T) {
	repo := newFakeAliasRepo()
	cache := command.NewAliasCache()
	ab := NewAliasBootstrapper(repo, cache)
	assert.Equal(t, plugins.BootstrapPriorityAlias, ab.Priority())
}

func TestAliasBootstrapper_Bootstrap(t *testing.T) {
	tests := []struct {
		name     string
		preExist map[string]string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "seeds all aliases on empty repo",
			preExist: nil,
			wantErr:  false,
		},
		{
			name:     "skips existing aliases",
			preExist: map[string]string{`"`: "say"},
			wantErr:  false,
		},
		{
			name:     "all pre-existing seeds nothing",
			preExist: map[string]string{`"`: "say", ":": "pose", ";": "pose"},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeAliasRepo()
			for k, v := range tt.preExist {
				repo.aliases[k] = v
			}
			cache := command.NewAliasCache()

			ab := NewAliasBootstrapper(repo, cache)
			err := ab.Bootstrap(context.Background(), nil, "")

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAliasBootstrapper_BootstrapLoadsCache(t *testing.T) {
	repo := newFakeAliasRepo()
	repo.aliases[`"`] = "say"
	repo.aliases[":"] = "pose"
	repo.aliases[";"] = "pose"
	cache := command.NewAliasCache()

	ab := NewAliasBootstrapper(repo, cache)
	err := ab.Bootstrap(context.Background(), nil, "")
	require.NoError(t, err)

	// Verify cache was loaded
	cmd, ok := cache.GetSystemAlias(`"`)
	assert.True(t, ok)
	assert.Equal(t, "say", cmd)
}

func TestAliasBootstrapper_BootstrapPropagatesError(t *testing.T) {
	repo := &fakeAliasRepoWithError{}
	cache := command.NewAliasCache()

	ab := NewAliasBootstrapper(repo, cache)
	err := ab.Bootstrap(context.Background(), nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test error")
}

func TestAliasBootstrapper_ImplementsBootstrapPlugin(_ *testing.T) {
	var _ plugins.BootstrapPlugin = (*AliasBootstrapper)(nil)
}

// fakeAliasRepoWithError is used for testing error propagation.
type fakeAliasRepoWithError struct{}

func (f *fakeAliasRepoWithError) GetSystemAliases(_ context.Context) (map[string]string, error) {
	return nil, errors.New("test error")
}

func (f *fakeAliasRepoWithError) SetSystemAlias(_ context.Context, _, _, _ string) error {
	return nil
}
