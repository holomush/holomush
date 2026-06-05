// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"testing"
)

// This file is the home for shared helpers used across the test/meta package's
// structural meta-tests. Relocated from inv_binding_test.go (holomush-hz0v4.14.16)
// when that file — whose per-family coverage test had already been retired in
// favour of TestEveryRegistryInvariantHasBinding — was removed.

// skipDirs are directories that meta-tests MUST NOT descend into when scanning
// the repo tree. Keeping this in sync with project layout avoids false
// positives from vendored or generated trees.
var skipDirs = map[string]struct{}{
	".git":         {},
	".jj":          {},
	".worktrees":   {},
	"vendor":       {},
	"node_modules": {},
	"bin":          {},
	"build":        {},
	"dist":         {},
}

// findRepoRoot walks upward from the test's working directory until it finds
// a directory containing go.mod, which marks the repository root.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root (no go.mod found in any parent of %q)", dir)
		}
		dir = parent
	}
}
