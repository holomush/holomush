// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

// phase6Invariants enumerates the Phase 6 scene-publication invariants
// (INV-P6-1 .. INV-P6-10) defined in §14 of
// docs/superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md.
// Each MUST be cited by at least one tier-1..3 test (unit, plugin-local
// integration, or full-stack integration) via an `INV-P6-<n>` substring in a
// test name, comment, or assertion message. This is a regression lock:
// deleting the last test that exercises an invariant — or silently dropping
// its ID citation — fails this meta-test loudly.
//
// Invariant summaries (from §14 of the spec):
//   - INV-P6-1:  Vote rosters frozen at attempt creation; owner+member only, invited excluded.
//   - INV-P6-2:  Votes free to change during COLLECTING; in COOLOFF only a no-vote changes it (→ COLLECTING).
//   - INV-P6-3:  Only the scene owner may withdraw an active attempt.
//   - INV-P6-4:  Scene transitions to archived ONLY on PUBLISHED; ATTEMPT_FAILED leaves it ended.
//   - INV-P6-5:  IsParticipant gate runs before any content/vote DB query.
//   - INV-P6-6:  ABAC engine is NOT consulted in participant-gated publication RPCs.
//   - INV-P6-7:  AttributeResolverService never returns scene content under any attribute.
//   - INV-P6-8:  Public archive RPCs return opaque NOT_FOUND for any non-PUBLISHED attempt.
//   - INV-P6-9:  Privacy-boundary blocks emit WARN log + metric + span error, NO IC stream event.
//   - INV-P6-10: COOLOFF → PUBLISHED snapshot is atomic; failure → ATTEMPT_FAILED, no partial state.
var phase6Invariants = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

// invP6CitationRE matches an `INV-P6-<digits>` citation. The `\d+` is greedy,
// so "INV-P6-10" captures 10 (not 1). The Phase-6 retry sub-invariants are
// written "INV-P6-E8-N"; the non-digit "E" immediately after the prefix means
// they do NOT match this pattern and are correctly ignored.
var invP6CitationRE = regexp.MustCompile(`INV-P6-(\d+)`)

// phase6SelfFile is this meta-test's own base name. It is excluded from the
// walk so the phase6Invariants enumeration and the summaries above cannot
// self-satisfy the coverage check.
const phase6SelfFile = "scenes_phase6_invariants_test.go"

// TestPhase6InvariantsHaveTestCoverage enumerates INV-P6-1 .. INV-P6-10 and
// asserts each is cited by at least one *_test.go file (excluding this one).
// A new Phase-6 invariant added to the spec without a paired, ID-citing test —
// or the silent removal of the last test citing an existing invariant — fails
// this meta-test. Mirrors i_priv_coverage_test.go / i_pres_coverage_test.go.
func TestPhase6InvariantsHaveTestCoverage(t *testing.T) {
	root := findRepoRoot(t)

	// os.Root gives race-free, symlink-safe reads confined to the repo tree
	// (gosec G304/G122); all WalkDir paths are converted to root-relative form
	// before Root.Open. Mirrors i_priv_coverage_test.go.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open repo root %q: %v", root, err)
	}
	defer func() { _ = rootFS.Close() }()

	citations := make(map[int][]string) // INV-P6-N -> citing file paths

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") || d.Name() == phase6SelfFile {
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
		for _, m := range invP6CitationRE.FindAllSubmatch(data, -1) {
			n, convErr := strconv.Atoi(string(m[1]))
			if convErr != nil {
				continue
			}
			citations[n] = append(citations[n], rel)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk repo: %v", walkErr)
	}

	for _, inv := range phase6Invariants {
		if len(citations[inv]) == 0 {
			t.Errorf("INV-P6-%d: no citing test found (expected at least one `INV-P6-%d` substring in a tier-1..3 *_test.go file)", inv, inv)
		}
	}
}
