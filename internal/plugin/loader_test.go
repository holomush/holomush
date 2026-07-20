// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// newLoaderForTest builds a PluginLoader from package plugins_test using only
// the loader's own collaborators. There is deliberately no *Manager and no
// plugins.NewManager call anywhere in this file: that absence is the D-02 / SC2
// proof for the third ARCH-02 unit. Because NewManager is never reached,
// ErrMissingVerbRegistry (INV-EVENTBUS-11) is not in play here.
func newLoaderForTest(t *testing.T, dir string) *plugins.PluginLoader {
	t.Helper()
	return plugins.NewPluginLoader(
		plugins.LoaderConfig{PluginsDir: dir},
		plugins.NewPluginRuntime(),
		plugins.NewIdentityStore(nil, 0),
	)
}

// writeManifest drops a plugin.yaml into a per-plugin subdirectory of dir,
// mirroring the on-disk layout Discover walks.
func writeManifest(t *testing.T, dir, pluginDir, body string) string {
	t.Helper()
	full := filepath.Join(dir, pluginDir)
	require.NoError(t, os.MkdirAll(full, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(full, "plugin.yaml"), []byte(body), 0o600))
	return full
}

func TestNewPluginLoaderIsConstructibleWithoutAManager(t *testing.T) {
	loader := newLoaderForTest(t, t.TempDir())
	require.NotNil(t, loader, "loader must be usable without constructing a Manager")
}

func TestDiscoverReturnsNoPluginsForAnEmptyDirectory(t *testing.T) {
	loader := newLoaderForTest(t, t.TempDir())

	discovered, err := loader.Discover(context.Background())

	require.NoError(t, err)
	assert.Empty(t, discovered)
}

func TestDiscoverReturnsNilWithoutErrorWhenTheDirectoryIsAbsent(t *testing.T) {
	loader := newLoaderForTest(t, filepath.Join(t.TempDir(), "does-not-exist"))

	discovered, err := loader.Discover(context.Background())

	require.NoError(t, err, "a missing plugins directory is not an error")
	assert.Nil(t, discovered)
}

func TestDiscoverReturnsTheManifestForOneValidPlugin(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "alpha", "name: alpha\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua\n")
	loader := newLoaderForTest(t, dir)

	discovered, err := loader.Discover(context.Background())

	require.NoError(t, err)
	require.Len(t, discovered, 1)
	assert.Equal(t, "alpha", discovered[0].Manifest.Name)
	assert.Equal(t, filepath.Join(dir, "alpha"), discovered[0].Dir)
}

// A malformed manifest is SKIPPED with a warning rather than surfaced as an
// error — Discover's contract is "invalid plugins are logged and skipped", and
// the valid sibling in the same directory must still be discovered.
func TestDiscoverSkipsAMalformedManifestAndKeepsTheValidSibling(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "broken", "name: [unclosed\n\tversion: ??\n")
	writeManifest(t, dir, "alpha", "name: alpha\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua\n")
	loader := newLoaderForTest(t, dir)

	discovered, err := loader.Discover(context.Background())

	require.NoError(t, err, "a malformed manifest is skipped, not returned as an error")
	require.Len(t, discovered, 1)
	assert.Equal(t, "alpha", discovered[0].Manifest.Name)
}

func TestDiscoverSkipsADirectoryWithNoManifest(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "not-a-plugin"), 0o750))
	loader := newLoaderForTest(t, dir)

	discovered, err := loader.Discover(context.Background())

	require.NoError(t, err)
	assert.Empty(t, discovered)
}

// resolveLoadOrder with no ServiceRegistry configured falls back to priority
// sort. The assertion is that the order is deterministic and STABLE across
// repeated calls — the observed tie-break is whatever the current sort does,
// not a rule this test invents.
func TestResolveLoadOrderIsDeterministicAcrossRepeatedCalls(t *testing.T) {
	loader := newLoaderForTest(t, t.TempDir())
	discovered := []*plugins.DiscoveredPlugin{
		{Manifest: &plugins.Manifest{Name: "high", Priority: priorityPtr(90)}},
		{Manifest: &plugins.Manifest{Name: "low", Priority: priorityPtr(10)}},
		{Manifest: &plugins.Manifest{Name: "mid", Priority: priorityPtr(50)}},
	}

	first, err := loader.TestResolveLoadOrder(discovered)
	require.NoError(t, err)
	firstNames := namesOf(first.Ordered)
	assert.Equal(t, []string{"low", "mid", "high"}, firstNames,
		"priority sort orders lower values first")

	for i := 0; i < 3; i++ {
		again, againErr := loader.TestResolveLoadOrder(discovered)
		require.NoError(t, againErr)
		assert.Equal(t, firstNames, namesOf(again.Ordered),
			"load order must not drift across repeated calls")
	}
}

func priorityPtr(p plugins.LoadPriority) *plugins.LoadPriority { return &p }

func namesOf(ordered []*plugins.DiscoveredPlugin) []string {
	out := make([]string, 0, len(ordered))
	for _, dp := range ordered {
		out = append(out, dp.Manifest.Name)
	}
	return out
}

// computeHashes reads only from the DiscoveredPlugin's directory, so its output
// is pinnable for a fixed input. sha256("name: pinned\nversion: 1.0.0\ntype: setting\n")
// is a fixed value; asserting it directly catches any change to what is hashed.
func TestComputeHashesPinsTheManifestDigestForAFixedInput(t *testing.T) {
	dir := t.TempDir()
	const body = "name: pinned\nversion: 1.0.0\ntype: setting\n"
	pluginDir := writeManifest(t, dir, "pinned", body)
	loader := newLoaderForTest(t, dir)

	manifestHash, contentHash, err := loader.TestComputeHashes(&plugins.DiscoveredPlugin{
		Manifest: &plugins.Manifest{Name: "pinned", Version: "1.0.0", Type: plugins.TypeSetting},
		Dir:      pluginDir,
	})

	require.NoError(t, err)
	assert.Equal(t,
		"52db22d827d3301598b9593899145c6a2611be3dde799cd51c2471672a2bf6e1",
		hex.EncodeToString(manifestHash),
		"manifest hash must be sha256 of the plugin.yaml bytes, unchanged")
	assert.Nil(t, contentHash, "setting plugins have no executable artifact")
}
