// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package meta contains repository-wide meta-tests that enforce structural
// invariants. This file gates the 12 session-liveness invariants declared in
// docs/superpowers/specs/2026-05-30-session-liveness-and-gateway-survival-design.md
// §297-329. It asserts a BIJECTION: every invariant in the catalog maps to ≥1
// covering test that carries a `// Verifies: <id>` annotation, AND every such
// annotation names a real invariant from the catalog.
//
// # Invariant namespace
//
// The liveness family uses these annotation ids:
//
//	I-LIVE-1 .. I-LIVE-5
//	I-LIVENESS-PRES-1   (see Note below)
//	I-SURV-1 .. I-SURV-5
//	I-SEC-1
//
// # Note on presence label collision
//
// The presence-snapshot invariant "Snapshot returns only Active sessions;
// Detached/Expired excluded" (presence-snapshot design spec §7) migrated to
// the registry as INV-PRESENCE-1 (holomush-hz0v4.14.5); the invariant registry
// meta-test (TestEveryRegistryInvariantHasBinding) now owns its coverage.
// The liveness catalog's presence invariant ("location presence roster
// filtered to grid_present") is a DIFFERENT invariant that historically shared
// the legacy "I-PRES-1" label.
//
// To avoid two independent meta-tests both claiming the same label with
// different semantics, this gate uses "I-LIVENESS-PRES-1" for the liveness
// family's presence invariant. Covering tests that satisfy both invariants
// simultaneously carry both annotations.
package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// livenessInvariantCatalog is the authoritative 12-item set of session-liveness
// invariant ids. Source: design spec §297-329.
//
//	I-LIVE-1 (refresh): while a transport is open, the gateway refreshes the lease.
//	I-LIVE-2 (expiry): a connection past the lease cutoff is swept.
//	I-LIVE-3 (derivation): session.active ⇔ ≥1 live connection; grid_present derived.
//	I-LIVE-4 (boot grace): lease sweep MUST NOT reap within the boot-grace window.
//	I-LIVE-5 (single source of liveness): only sanctioned writers transition liveness state.
//	I-LIVENESS-PRES-1 (grid roster): location presence roster filtered to grid_present.
//	I-SURV-1 (cause distinction): gateway distinguishes transient gRPC error from terminal SESSION_NOT_FOUND.
//	I-SURV-2 (gap-free resume): on reconnect, re-Subscribe resumes without dup/missing events.
//	I-SURV-3 (reattach continuity): if session detached during the gap, reattach restores it.
//	I-SURV-4 (bounded retry): reconnect bounded by a ceiling; exceeding it closes + Disconnects.
//	I-SURV-5 (transport symmetry): web and telnet implement identical survival semantics.
//	I-SEC-1 (ownership): RefreshConnection validates connection ownership; enumeration-safe.
var livenessInvariantCatalog = []string{
	"I-LIVE-1",
	"I-LIVE-2",
	"I-LIVE-3",
	"I-LIVE-4",
	"I-LIVE-5",
	"I-LIVENESS-PRES-1",
	"I-SURV-1",
	"I-SURV-2",
	"I-SURV-3",
	"I-SURV-4",
	"I-SURV-5",
	"I-SEC-1",
}

// livenessVerifiesRE matches `// Verifies: <liveness-id>` annotations in test
// files, capturing the invariant id. The pattern covers all liveness-family
// prefixes: I-LIVE-N, I-LIVENESS-PRES-N, I-SURV-N, I-SEC-N.
var livenessVerifiesRE = regexp.MustCompile(
	`//\s*Verifies:\s*(I-LIVE-\d+|I-LIVENESS-PRES-\d+|I-SURV-\d+|I-SEC-\d+)`,
)

// TestEveryLivenessInvariantHasACoveringTest enforces a strict bijection between
// the 12-item liveness invariant catalog and the `// Verifies: <id>` annotations
// found in test files across the repository:
//
//  1. Every catalog invariant has ≥1 covering test (no uncovered invariants).
//  2. Every annotation names a catalog invariant (no stale / mis-typed ids).
//
// The test does NOT re-run the covered tests; it only asserts the mapping is
// complete and accurate by scanning the test tree.
func TestEveryLivenessInvariantHasACoveringTest(t *testing.T) {
	root := findRepoRoot(t)

	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open repo root %q: %v", root, err)
	}
	defer func() { _ = rootFS.Close() }()

	// annotated maps invariant id → files that carry a Verifies annotation.
	annotated := make(map[string][]string)

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
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
		for _, m := range livenessVerifiesRE.FindAllSubmatch(data, -1) {
			id := string(m[1])
			annotated[id] = append(annotated[id], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	// Build catalog set for O(1) lookup.
	catalog := make(map[string]struct{}, len(livenessInvariantCatalog))
	for _, id := range livenessInvariantCatalog {
		catalog[id] = struct{}{}
	}

	// Direction 1: every catalog invariant has ≥1 covering test.
	for _, id := range livenessInvariantCatalog {
		if len(annotated[id]) == 0 {
			t.Errorf("liveness invariant %s has no covering test "+
				"(add `// Verifies: %s` to a test that exercises it)", id, id)
		}
	}

	// Direction 2: every annotation names a real catalog invariant.
	// Collect annotated ids in sorted order for a deterministic diff.
	var extra []string
	for id := range annotated {
		if _, ok := catalog[id]; !ok {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	for _, id := range extra {
		t.Errorf("annotation `// Verifies: %s` found in %v but %q is not in the "+
			"liveness invariant catalog — typo or stale id?", id, annotated[id], id)
	}

	// Provide a full bijection summary on failure for easy debugging. The
	// catalog is already in declared order; only the annotated set needs sorting.
	if t.Failed() {
		annotatedSorted := make([]string, 0, len(annotated))
		for id := range annotated {
			annotatedSorted = append(annotatedSorted, id)
		}
		sort.Strings(annotatedSorted)
		t.Logf("catalog:   %v", livenessInvariantCatalog)
		t.Logf("annotated: %v", annotatedSorted)
	}
}

// TestLivenessInvariantCatalogIsComplete asserts that the in-code catalog
// slice has exactly the expected 12 invariants and no duplicates. This is a
// sanity-check on the catalog itself (guards against copy-paste errors).
func TestLivenessInvariantCatalogIsComplete(t *testing.T) {
	want := 12
	require.Len(t, livenessInvariantCatalog, want,
		"liveness invariant catalog must have exactly %d entries", want)

	seen := make(map[string]struct{}, len(livenessInvariantCatalog))
	for _, id := range livenessInvariantCatalog {
		require.NotContains(t, seen, id,
			"duplicate invariant id %q in livenessInvariantCatalog", id)
		seen[id] = struct{}{}
	}
}
