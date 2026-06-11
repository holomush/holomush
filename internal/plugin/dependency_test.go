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

// Verifies: INV-PLUGIN-41
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

// Verifies: INV-PLUGIN-42
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

// Verifies: INV-PLUGIN-41
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

func TestResolveResultReportsVersionUnsatisfied(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "consumer", Requires: []Dependency{{Kind: DependencyService, Name: "svc-a", Version: ">=2.0.0"}}}},
		{Manifest: &Manifest{Name: "provider", Version: "1.0.0", Provides: []string{"svc-a"}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	require.Len(t, res.Unsatisfied, 1)
	assert.Equal(t, "VERSION_UNSATISFIED", res.Unsatisfied[0].Reason)
}

func TestResolveResultOptionalVersionMismatchIsSkipped(t *testing.T) {
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "consumer", Requires: []Dependency{{Kind: DependencyService, Name: "svc-a", Version: ">=2.0.0", Optional: true}}}},
		{Manifest: &Manifest{Name: "provider", Version: "1.0.0", Provides: []string{"svc-a"}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	assert.Empty(t, res.Unsatisfied, "optional dependency with version mismatch should not be reported")
	assert.Len(t, res.Ordered, 2)
}

// Verifies: INV-PLUGIN-41
func TestResolveResultReportsUnknownDependencyKind(t *testing.T) {
	// A Go-constructed required dependency with a zero-value Kind must be
	// reported, never silently dropped.
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "c", Requires: []Dependency{{Name: "x"}}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	require.Len(t, res.Unsatisfied, 1)
	assert.Equal(t, "UNKNOWN_DEPENDENCY_KIND", res.Unsatisfied[0].Reason)
	assert.Equal(t, "x", res.Unsatisfied[0].Entry.Name)
}

// Verifies: INV-PLUGIN-41
func TestResolveResultReportsMissingNamedDependency(t *testing.T) {
	// A named manifest dependency on an undiscovered plugin must be reported,
	// never silently dropped by the edge-build loop.
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "dependent", Dependencies: map[string]string{"absent-base": ">= 1.0.0"}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	require.Len(t, res.Unsatisfied, 1)
	assert.Equal(t, "UNSATISFIED_DEPENDENCY", res.Unsatisfied[0].Reason)
	assert.Equal(t, "absent-base", res.Unsatisfied[0].Entry.Name)
}

func TestResolveResultMisdeclaredCapabilityReportedEvenWhenOptional(t *testing.T) {
	// A capability entry naming a plugin-provided service is a kind/provider
	// mismatch (INV-PLUGIN-42) — reported regardless of optional, since optional
	// would otherwise silence it AND skip the required ordering edge.
	plugins := []*DiscoveredPlugin{
		{Manifest: &Manifest{Name: "consumer", Requires: []Dependency{{Kind: DependencyCapability, Name: "holomush.scene.v1.SceneService", Optional: true}}}},
		{Manifest: &Manifest{Name: "provider", Provides: []string{"holomush.scene.v1.SceneService"}}},
	}
	res, err := ResolveDependencyOrder(plugins, nil, NewCapabilityVocabulary())
	require.NoError(t, err)
	require.Len(t, res.Unsatisfied, 1)
	assert.Equal(t, "MISDECLARED_DEPENDENCY", res.Unsatisfied[0].Reason)
}
