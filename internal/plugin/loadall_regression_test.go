// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Verifies: INV-PLUGIN-41
// Verifies: INV-PLUGIN-43
func TestDefaultPluginSetResolvesWithNoUnsatisfiedDeps(t *testing.T) {
	root, err := filepath.Abs("../../plugins")
	require.NoError(t, err)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	var discovered []*DiscoveredPlugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, rErr := os.ReadFile(filepath.Join(root, e.Name(), "plugin.yaml"))
		if os.IsNotExist(rErr) {
			continue
		}
		require.NoError(t, rErr)
		man, pErr := ParseManifest(data)
		require.NoError(t, pErr, "plugin %s", e.Name())
		discovered = append(discovered, &DiscoveredPlugin{Manifest: man, Dir: e.Name()})
	}

	// Host services present at resolution time on main: only WorldService.
	res, err := ResolveDependencyOrder(discovered, []string{"holomush.world.v1.WorldService"}, DefaultCapabilityVocabulary())
	require.NoError(t, err)
	require.Empty(t, res.Unsatisfied, "default plugin set MUST resolve with no unsatisfied deps")
	require.Empty(t, res.Cycles)
}
