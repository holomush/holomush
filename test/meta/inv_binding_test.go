// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package meta contains repository-wide meta-tests that enforce structural
// invariants — for example, that every Phase 3c invariant declared in the
// crypto master spec has at least one concrete test binding via a
// `// Verifies: INV-N` annotation, or (for INV-58, the no-remote-clock-compare
// rule) an enforcing static analyzer.
package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// phase3cInvariants enumerates the Phase 3c invariants (INV-53..60) that
// MUST each have at least one test binding (or, for INV-58, an enforcing
// lint analyzer). Update this list when a new Phase 3c invariant is added
// to docs/superpowers/specs/2026-04-26-crypto-master-spec.md.
var phase3cInvariants = []int{53, 54, 55, 56, 57, 58, 59, 60}

// invLintEnforced lists invariants whose binding is the existence of an
// enforcing static analyzer rather than a runtime test annotation. INV-58
// (no remote-clock comparisons) is enforced by the noremoteclockcompare
// analyzer in gorules/analyzers/noremoteclockcompare/.
var invLintEnforced = map[int]string{
	58: filepath.Join("gorules", "analyzers", "noremoteclockcompare", "noremoteclockcompare.go"),
}

// verifiesRE matches `// Verifies: INV-<digits>` annotations in test files.
var verifiesRE = regexp.MustCompile(`//\s*Verifies:\s*INV-(\d+)`)

// skipDirs are directories that the meta-test MUST NOT descend into when
// scanning for test bindings. Keeping this in sync with project layout
// avoids false positives from vendored or generated trees.
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

func TestEveryPhase3cInvariantHasAtLeastOneTestBinding(t *testing.T) {
	root := findRepoRoot(t)

	// Use os.Root for race-free, symlink-safe file reads inside the repo
	// tree (gosec G122). All paths produced by WalkDir below are converted
	// to root-relative form before being opened via Root.Open.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open repo root %q: %v", root, err)
	}
	defer func() { _ = rootFS.Close() }()

	bindings := make(map[int][]string) // INV-N -> list of file paths

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		f, openErr := rootFS.Open(rel)
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			return readErr
		}
		matches := verifiesRE.FindAllSubmatch(data, -1)
		for _, m := range matches {
			n, convErr := strconv.Atoi(string(m[1]))
			if convErr != nil {
				continue
			}
			bindings[n] = append(bindings[n], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	for _, inv := range phase3cInvariants {
		if rel, lintEnforced := invLintEnforced[inv]; lintEnforced {
			if _, statErr := os.Stat(filepath.Join(root, rel)); statErr != nil {
				t.Errorf("INV-%d: lint-enforced invariant missing analyzer at %s: %v", inv, rel, statErr)
			}
			continue
		}
		if len(bindings[inv]) == 0 {
			t.Errorf("INV-%d: no test binding found (expected at least one `// Verifies: INV-%d` comment in a *_test.go file)", inv, inv)
		}
	}
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
