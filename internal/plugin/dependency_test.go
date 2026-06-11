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

		order, err := ResolveDependencyOrder(plugins, nil)
		require.NoError(t, err)
		assert.Equal(t, "provider", order[0].Manifest.Name)
		assert.Equal(t, "consumer", order[1].Manifest.Name)
	})

	t.Run("allows requires satisfied by server services", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "consumer", Requires: RequireServices("holomush.world.v1.WorldService")}},
		}
		serverServices := []string{"holomush.world.v1.WorldService"}

		order, err := ResolveDependencyOrder(plugins, serverServices)
		require.NoError(t, err)
		assert.Len(t, order, 1)
	})

	t.Run("detects circular dependency", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "a", Requires: RequireServices("svc-b"), Provides: []string{"svc-a"}}},
			{Manifest: &Manifest{Name: "b", Requires: RequireServices("svc-a"), Provides: []string{"svc-b"}}},
		}
		_, err := ResolveDependencyOrder(plugins, nil)
		assert.Error(t, err)
	})

	t.Run("returns error for unsatisfied requires", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "consumer", Requires: RequireServices("svc-missing")}},
		}
		_, err := ResolveDependencyOrder(plugins, nil)
		assert.Error(t, err)
	})

	t.Run("handles plugins with no requires or provides", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "standalone"}},
			{Manifest: &Manifest{Name: "provider", Provides: []string{"svc-a"}}},
		}
		order, err := ResolveDependencyOrder(plugins, nil)
		require.NoError(t, err)
		assert.Len(t, order, 2)
	})

	t.Run("respects manifest dependencies in addition to service graph", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "dependent", Dependencies: map[string]string{"base": ">= 1.0.0"}}},
			{Manifest: &Manifest{Name: "base", Version: "1.0.0"}},
		}
		order, err := ResolveDependencyOrder(plugins, nil)
		require.NoError(t, err)
		assert.Equal(t, "base", order[0].Manifest.Name)
		assert.Equal(t, "dependent", order[1].Manifest.Name)
	})

	t.Run("detects duplicate service providers", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "provider-a", Provides: []string{"svc-x"}}},
			{Manifest: &Manifest{Name: "provider-b", Provides: []string{"svc-x"}}},
		}
		_, err := ResolveDependencyOrder(plugins, nil)
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
		order, err := ResolveDependencyOrder(plugins, nil)
		require.NoError(t, err)
		assert.Len(t, order, 4)
		// a must be first
		assert.Equal(t, "a", order[0].Manifest.Name)
		// d must be last
		assert.Equal(t, "d", order[3].Manifest.Name)
	})
}
