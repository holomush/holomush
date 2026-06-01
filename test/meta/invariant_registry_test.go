// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// registryEntry mirrors one invariant in invariants.yaml.
type registryEntry struct {
	ID         string   `yaml:"id"`
	Legacy     []string `yaml:"legacy"`
	Summary    string   `yaml:"summary"`
	Severity   string   `yaml:"severity"`
	Status     string   `yaml:"status"`
	AssertedBy []string `yaml:"asserted_by"`
	External   bool     `yaml:"external"`
	Binding    string   `yaml:"binding"`
}

type registryDoc struct {
	Scopes []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Boundary    string `yaml:"boundary"`
	} `yaml:"scopes"`
	Invariants []registryEntry `yaml:"invariants"`
}

// registryVerifiesRE matches `// Verifies: INV-<SCOPE>-<N>` annotations in test files.
var registryVerifiesRE = regexp.MustCompile(`//\s*Verifies:\s*(INV-[A-Z]+-\d+)`)

// registryInvRefRE matches invariant IDs referenced in spec prose but not via Verifies.
// Used for the orphan-detection pass.
var registryInvRefRE = regexp.MustCompile(`\b(INV-[A-Z]+-\d+)\b`)

// TestEveryRegistryInvariantHasBinding asserts that every invariant in
// docs/architecture/invariants.yaml has at least one test binding, or is
// explicitly marked binding: pending (tolerated — verification backfill tracked
// separately) or external: true.
func TestEveryRegistryInvariantHasBinding(t *testing.T) {
	root := findRepoRoot(t)

	// 1. Parse the YAML registry.
	data, err := os.ReadFile(filepath.Join(root, "docs", "architecture", "invariants.yaml"))
	if err != nil {
		t.Fatalf("read invariants.yaml: %v", err)
	}
	var reg registryDoc
	if err = yaml.Unmarshal(data, &reg); err != nil {
		t.Fatalf("parse invariants.yaml: %v", err)
	}
	if len(reg.Invariants) == 0 {
		// Scaffolding phase: the registry is populated per-scope during the
		// holomush-hz0v4.14 migration. Until the first scope lands, an empty
		// registry is expected — skip rather than fail so the scaffold can land
		// green. Once any invariant exists, the binding assertions below enforce.
		// TEMPORARY: this skip MUST be removed once the registry is populated —
		// tracked by holomush-hz0v4.14.18 (gates final verification .14.17), so a
		// later regression that empties the registry fails loudly instead of skipping.
		t.Skip("invariants.yaml has no entries yet — populated per-scope by the holomush-hz0v4.14 migration (skip removed by holomush-hz0v4.14.18)")
	}

	// Index by ID for fast lookup.
	byID := make(map[string]registryEntry, len(reg.Invariants))
	seenScopes := make(map[string]bool)
	for _, e := range reg.Invariants {
		if _, dup := byID[e.ID]; dup {
			t.Errorf("duplicate ID in registry: %s", e.ID)
		}
		byID[e.ID] = e
		// Extract scope from ID for scope-existence check.
		parts := strings.SplitN(e.ID, "-", 3)
		if len(parts) == 3 {
			seenScopes[parts[0]+"-"+parts[1]] = true
		}
	}

	// Cross-check: every scope declared in the YAML has at least one invariant.
	for _, sc := range reg.Scopes {
		if !seenScopes[sc.Name] {
			t.Errorf("scope %s declared in YAML scopes but has no invariants", sc.Name)
		}
	}

	// 2. Walk the repo for // Verifies: annotations.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open repo root: %v", err)
	}
	defer func() { _ = rootFS.Close() }()

	bindings := make(map[string][]string) // INV-ID -> list of file paths

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
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		matches := registryVerifiesRE.FindAllSubmatch(data, -1)
		for _, m := range matches {
			id := string(m[1])
			bindings[id] = append(bindings[id], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	// 3. Assert every registry entry has a binding or external path.
	pendingCount := 0
	for _, e := range reg.Invariants {
		if e.External {
			for _, p := range e.AssertedBy {
				absPath := filepath.Join(root, p)
				if _, statErr := os.Stat(absPath); statErr != nil {
					t.Errorf("%s: external path %q does not exist: %v", e.ID, p, statErr)
				}
			}
			continue
		}
		if e.Binding == "pending" {
			if len(e.AssertedBy) > 0 {
				t.Errorf("%s: binding: pending entries MUST NOT carry asserted_by (no fabricated bindings)", e.ID)
			}
			pendingCount++
			continue
		}
		if len(bindings[e.ID]) == 0 {
			t.Errorf("%s: no test binding found (expected at least one `// Verifies: %s` comment in a *_test.go file)", e.ID, e.ID)
		}
	}
	t.Logf("registry: %d invariant(s) marked binding: pending (verification backfill tracked separately)", pendingCount)

	// 4. Orphan detection: scan specs/ for INV-<SCOPE>-<N> references not in registry.
	orphans := make(map[string]int)
	err = filepath.WalkDir(filepath.Join(root, "docs", "superpowers", "specs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // G122: meta-test walker reads in-repo doc files for invariant-ref grep; no symlink concern
		if readErr != nil {
			return readErr
		}
		matches := registryInvRefRE.FindAllSubmatch(data, -1)
		for _, m := range matches {
			id := string(m[1])
			if _, known := byID[id]; !known {
				orphans[id]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk specs: %v", err)
	}
	for id, n := range orphans {
		t.Errorf("orphan invariant %s referenced %d time(s) in specs/ but not in invariants.yaml", id, n)
	}
}
