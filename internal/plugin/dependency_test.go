// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveDependencyOrder(t *testing.T) {
	t.Run("sorts plugins so providers load before consumers", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "consumer", Requires: RequireServices("svc-a")}},
			{Manifest: &Manifest{Name: "provider", Provides: []string{"svc-a"}}},
		}

		res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
		require.NoError(t, err)
		assert.Equal(t, "provider", res.Ordered[0].Manifest.Name)
		assert.Equal(t, "consumer", res.Ordered[1].Manifest.Name)
	})

	t.Run("allows requires satisfied by server services", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "consumer", Requires: RequireServices("holomush.world.v1.WorldService")}},
		}
		serverServices := []string{"holomush.world.v1.WorldService"}

		res, err := ResolveDependencyOrder(plugins, serverServices, NewCapabilityVocabulary())
		require.NoError(t, err)
		assert.Len(t, res.Ordered, 1)
	})

	t.Run("detects circular dependency", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "a", Requires: RequireServices("svc-b"), Provides: []string{"svc-a"}}},
			{Manifest: &Manifest{Name: "b", Requires: RequireServices("svc-a"), Provides: []string{"svc-b"}}},
		}
		res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
		require.NoError(t, err)
		assert.NotEmpty(t, res.Cycles)
	})

	t.Run("returns error for unsatisfied requires", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "consumer", Requires: RequireServices("svc-missing")}},
		}
		res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
		require.NoError(t, err)
		assert.NotEmpty(t, res.Unsatisfied)
	})

	t.Run("handles plugins with no requires or provides", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "standalone"}},
			{Manifest: &Manifest{Name: "provider", Provides: []string{"svc-a"}}},
		}
		res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
		require.NoError(t, err)
		assert.Len(t, res.Ordered, 2)
	})

	t.Run("respects manifest dependencies in addition to service graph", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "dependent", Dependencies: map[string]string{"base": ">= 1.0.0"}}},
			{Manifest: &Manifest{Name: "base", Version: "1.0.0"}},
		}
		res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
		require.NoError(t, err)
		assert.Equal(t, "base", res.Ordered[0].Manifest.Name)
		assert.Equal(t, "dependent", res.Ordered[1].Manifest.Name)
	})

	t.Run("detects duplicate service providers", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "provider-a", Provides: []string{"svc-x"}}},
			{Manifest: &Manifest{Name: "provider-b", Provides: []string{"svc-x"}}},
		}
		_, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
		assert.Error(t, err)
	})

	t.Run("handles diamond dependency without error", func(t *testing.T) {
		// A provides svc-a, B and C require svc-a, D requires svc-b and svc-c
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "d", Requires: RequireServices("svc-b", "svc-c")}},
			{Manifest: &Manifest{Name: "b", Requires: RequireServices("svc-a"), Provides: []string{"svc-b"}}},
			{Manifest: &Manifest{Name: "c", Requires: RequireServices("svc-a"), Provides: []string{"svc-c"}}},
			{Manifest: &Manifest{Name: "a", Provides: []string{"svc-a"}}},
		}
		res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
		require.NoError(t, err)
		assert.Len(t, res.Ordered, 4)
		// a must be first
		assert.Equal(t, "a", res.Ordered[0].Manifest.Name)
		// d must be last
		assert.Equal(t, "d", res.Ordered[3].Manifest.Name)
	})
}

func TestResolveResultReportsUnsatisfiedCapability(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyCapability, Name: "world.query"}}}},
	}
	vocab := NewCapabilityVocabulary() // empty — world.query unknown
	res, err := ResolveDependencyOrder(plugins, nil, vocab)
	require.NoError(t, err) // structured result, not a Go error
	require.Len(t, res.Unsatisfied, 1)
	assert.Equal(t, "world.query", res.Unsatisfied[0].Entry.Name)
}

func TestResolveResultSatisfiesRegisteredCapability(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyCapability, Name: "session"}}}},
	}
	vocab := DefaultCapabilityVocabulary()
	res, err := ResolveDependencyOrder(plugins, nil, vocab)
	require.NoError(t, err)
	assert.Empty(t, res.Unsatisfied)
	assert.Len(t, res.Ordered, 1)
}

func TestResolveResultMisdeclaredCapabilityThatIsPluginProvided(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "consumer", Requires: []Dependency{{Kind: DependencyCapability, Name: "holomush.scene.v1.SceneService"}}}},
		{Manifest: &Manifest{Name: "provider", Provides: []string{"holomush.scene.v1.SceneService"}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	require.Len(t, res.Unsatisfied, 1)
	assert.Equal(t, "MISDECLARED_DEPENDENCY", res.Unsatisfied[0].Reason)
}

func TestResolveResultOptionalUnsatisfiedIsSkipped(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Kind: DependencyService, Name: "holomush.absent.v1.X", Optional: true}}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	assert.Empty(t, res.Unsatisfied)
	assert.Len(t, res.Ordered, 1)
}

func TestResolveResultServiceEdgeOrdersProviderFirst(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "consumer", Requires: []Dependency{{Kind: DependencyService, Name: "svc-a"}}}},
		{Manifest: &Manifest{Name: "provider", Provides: []string{"svc-a"}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	require.Empty(t, res.Unsatisfied)
	assert.Equal(t, "provider", res.Ordered[0].Manifest.Name)
}
