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

// iPrivInvariants enumerates the I-PRIV invariants that MUST each have at
// least one test binding via a `// Verifies: I-PRIV-N` annotation. Source:
// docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md §9.
//
// I-PRIV-7 is included here because the placeholder Describe at
// test/integration/privacy/privacy_test.go:30 explicitly claims its
// satisfaction (per spec §8: "I-PRIV-7 is satisfied either by the
// integration test above (when non-skipped) OR by the meta-test
// enumerating zero plugins with `history_scope:` declared (vacuously
// true)"). The placeholder MUST carry the annotation so the meta-test
// fails loudly if the Describe is ever deleted without replacement.
//
// Invariant summaries (from §9 of the spec):
//   - I-PRIV-1: Per-session-row temporal floor on stream history reads.
//   - I-PRIV-2: Guest sessions get an additional MAX(guest_character.CreatedAt) bound.
//   - I-PRIV-3: ReattachCAS / SelectCharacter reattach preserve LocationArrivedAt.
//   - I-PRIV-4: Idle status change does NOT advance LocationArrivedAt.
//   - I-PRIV-5: All denial paths collapse to STREAM_ACCESS_DENIED on the wire.
//   - I-PRIV-6: ABAC staff override bypasses hard-gate only, not temporal floor.
//   - I-PRIV-7: Plugins with divergent history-replay semantics MUST declare them
//     via manifest `history_scope:`. Vacuous when zero plugins declare it.
//   - I-PRIV-8: Subscribe.OpenSession and SetFilters MUST query the existing
//     durable consumer before CreateOrUpdateConsumer (NATS-source-of-truth).
var iPrivInvariants = []int{1, 2, 3, 4, 5, 6, 7, 8}

// iPrivVerifiesRE matches `// Verifies: I-PRIV-<digits>` annotations in
// test files. Same anchoring rules as the other per-family coverage tests —
// consistent tolerance of `// Verifies:` and `//  Verifies:` forms.
var iPrivVerifiesRE = regexp.MustCompile(`//\s*Verifies:\s*I-PRIV-(\d+)`)

// TestEveryIPRIVInvariantHasAtLeastOneTestBinding asserts the I-PRIV-N
// invariant family each has a paired test binding. (The analogous PRESENCE
// per-family test was retired once INV-PRESENCE migrated to the registry,
// whose TestEveryRegistryInvariantHasBinding now owns that coverage —
// holomush-hz0v4.14.5; PRIVACY follows when it migrates.) A new I-PRIV-N
// invariant added to the spec without a paired test binding fails this test.
func TestEveryIPRIVInvariantHasAtLeastOneTestBinding(t *testing.T) {
	root := findRepoRoot(t)

	// Use os.Root for race-free, symlink-safe file reads inside the repo
	// tree (gosec G122). All paths produced by WalkDir below are converted
	// to root-relative form before being opened via Root.Open.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open repo root %q: %v", root, err)
	}
	defer func() { _ = rootFS.Close() }()

	bindings := make(map[int][]string) // I-PRIV-N -> list of file paths

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
		matches := iPrivVerifiesRE.FindAllSubmatch(data, -1)
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

	for _, inv := range iPrivInvariants {
		if len(bindings[inv]) == 0 {
			t.Errorf("I-PRIV-%d: no test binding found (expected at least one `// Verifies: I-PRIV-%d` comment in a *_test.go file)", inv, inv)
		}
	}
}
