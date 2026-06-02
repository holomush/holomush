// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package meta contains repository-wide meta-tests that enforce structural
// invariants.
//
// This file hosts shared helpers (findRepoRoot, skipDirs) used by many
// meta-tests in the package. The former Phase-3c per-family coverage test
// (TestEveryPhase3cInvariantHasAtLeastOneTestBinding) was retired when its
// INV-53..60 invariants migrated to the registry as INV-CLUSTER-1..10
// (holomush-hz0v4.14.11); the registry meta-test
// (TestEveryRegistryInvariantHasBinding) now owns that coverage. The helpers
// remain here pending relocation by holomush-hz0v4.14.16.
package meta

import (
	"os"
	"path/filepath"
	"testing"
)

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
