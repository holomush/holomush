// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHostDoesNotReferencePluginConfigKeys verifies INV-PLUGIN-1: the host treats
// plugin config opaquely — it MUST NOT contain literals of any plugin's config
// keys (it understands only generic types, never specific plugin semantics).
// This pins the boundary against a future host edit that special-cases a plugin
// key such as "vote_window" or "cooloff_window".
//
// INV-PLUGIN-1: host opacity — host code MUST NOT reference plugin config key
// literals; keys are the exclusive concern of each plugin's manifest.
//
// Verifies: INV-PLUGIN-1
func TestHostDoesNotReferencePluginConfigKeys(t *testing.T) {
	bannedKeys := []string{"vote_window", "cooloff_window", "scheduler_interval"}
	// Scan each immediate host package directory (non-recursive — subpackages
	// like goplugin/ are scanned by their own entry in this list).
	pkgs := []string{".", "setup", "goplugin", "hostfunc", "lua"}
	for _, pkg := range pkgs {
		dir := filepath.Join(".", pkg)
		matches := grepGoLiterals(t, dir, bannedKeys)
		require.Empty(t, matches,
			"host pkg %s must not reference plugin config key literals: %v", pkg, matches)
	}
}

// grepGoLiterals reads every non-test .go file in dir (non-recursively) and
// returns a list of "file:key" strings for any file that contains a banned key
// as a substring. Test files are excluded because the keys legitimately appear
// in test fixtures and in this test's own bannedKeys slice.
func grepGoLiterals(t *testing.T, dir string, keys []string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "read dir %s", dir)

	var hits []string
	for _, e := range entries {
		if e.IsDir() {
			continue // non-recursive: skip subdirectories
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, readErr, "read file %s/%s", dir, name)
		content := string(data)
		for _, k := range keys {
			if strings.Contains(content, k) {
				hits = append(hits, name+":"+k)
			}
		}
	}
	return hits
}
