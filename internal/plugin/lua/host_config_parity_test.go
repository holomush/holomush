// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

// Internal (white-box) parity tests for INV-PLUGIN-3: the host builds one merged
// value map per plugin that is stashed in h.mergedConfigs, ensuring the Lua
// delivery path reads exactly the canonical MergePluginConfig output rather than
// recomputing it independently.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// TestLuaHostStashesCanonicalMergedConfigAfterLoad asserts that after Load, the
// Lua host's internal mergedConfigs map for the named plugin is exactly equal to
// the result of plugins.MergePluginConfig called with the same (schema, override)
// inputs. This proves INV-PLUGIN-3 for the Lua delivery path: the host does not
// re-derive the config per runtime but threads through the shared MergePluginConfig
// computation.
//
// The schema has both a default-bearing key and an overridable key so a fork that
// ignored either defaults or overrides would produce a different map and fail the
// assertion.
//
// Verifies: INV-PLUGIN-3
func TestLuaHostStashesCanonicalMergedConfigAfterLoad(t *testing.T) {
	dir := t.TempDir()
	// Write a minimal Lua plugin that the host can load.
	err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte("function on_event(e) return nil end"), 0o600)
	require.NoError(t, err)

	schema := map[string]plugins.ConfigParam{
		"vote_window":    {Type: "duration", Default: "168h", Required: true},
		"cooloff_window": {Type: "duration", Default: "30m"},
	}
	override := map[string]string{"cooloff_window": "5s"}

	// Compute the canonical expected output via MergePluginConfig directly.
	want, err := plugins.MergePluginConfig(schema, override)
	require.NoError(t, err, "MergePluginConfig must not error on valid inputs")

	host := NewHost(WithPluginConfigOverrides(map[string]map[string]string{
		"parity-plugin": override,
	}))
	defer func() { _ = host.Close(context.Background()) }()

	manifest := &plugins.Manifest{
		Name:      "parity-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		Config:    schema,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
	require.NoError(t, host.Load(context.Background(), manifest, dir))

	// Access the unexported mergedConfigs field directly (white-box, same package).
	host.mu.RLock()
	got, ok := host.mergedConfigs[manifest.Name]
	host.mu.RUnlock()

	require.True(t, ok, "mergedConfigs must contain an entry for the loaded plugin")
	require.Equal(t, want, got,
		"Lua host's stashed merged config must equal the canonical MergePluginConfig output "+
			"(INV-PLUGIN-3: no per-runtime config fork)")
}
