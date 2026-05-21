// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package meta contains repository-wide meta-tests that enforce structural
// invariants — for example, that every I-PRES-N invariant declared in the
// presence-snapshot design spec has at least one concrete test binding via a
// `// Verifies: I-PRES-N` annotation (or, for I-PRES-8, a frontend test).
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

// iPresInvariants enumerates the I-PRES invariants (I-PRES-1..9) that MUST
// each have at least one test binding (or, for I-PRES-8, a frontend vitest
// file). Update this list when a new I-PRES invariant is added to
// docs/superpowers/specs/2026-05-19-presence-snapshot-design.md.
//
// Invariant summaries (from §7 of the spec):
//   - I-PRES-1: Snapshot returns only Active sessions; Detached/Expired excluded.
//   - I-PRES-2: Snapshot exempt from I-PRIV-1 temporal floor (timeless current state).
//   - I-PRES-3: Ownership failures collapse to SESSION_NOT_FOUND (enumeration-safe).
//   - I-PRES-4: RPC ABAC-gated by action="list_presence" on resource="location:<id>".
//   - I-PRES-5: Non-empty FocusMemberships → UNIMPLEMENTED; no silent fallback.
//   - I-PRES-6: Caller's own session included when status+location qualify.
//   - I-PRES-7: PresenceEntry has exactly 3 fields: character_id, character_name, state.
//   - I-PRES-8: Client presence map keyed by character_id; idempotent add/remove.
//   - I-PRES-9: Response deduplicates by character_id (defense-in-depth).
var iPresInvariants = []int{1, 2, 3, 4, 5, 6, 7, 8, 9}

// iPresExternalBinding lists invariants whose binding lives outside Go test
// files. I-PRES-8 is enforced by the frontend PresenceStore vitest suite at
// web/src/lib/presence/ (store.test.ts, mirror.test.ts). The meta-test
// verifies the path exists so drift (rename, deletion) causes an immediate
// failure rather than a silent pass.
var iPresExternalBinding = map[int]string{
	8: filepath.Join("web", "src", "lib", "presence"),
}

// iPresVerifiesRE matches `// Verifies: I-PRES-<digits>` annotations in
// test files. Anchored on whitespace so it tolerates both `// Verifies:`
// and `//  Verifies:` forms consistently with the precedent in
// inv_binding_test.go.
var iPresVerifiesRE = regexp.MustCompile(`//\s*Verifies:\s*I-PRES-(\d+)`)

func TestEveryIPRESInvariantHasAtLeastOneTestBinding(t *testing.T) {
	root := findRepoRoot(t)

	// Use os.Root for race-free, symlink-safe file reads inside the repo
	// tree (gosec G122). All paths produced by WalkDir below are converted
	// to root-relative form before being opened via Root.Open.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open repo root %q: %v", root, err)
	}
	defer func() { _ = rootFS.Close() }()

	// Verify external-binding paths exist before scanning for Go bindings.
	// This prevents a renamed/deleted frontend directory from causing a
	// silent pass when the invariant is marked external-only. Use os.Stat
	// rather than rootFS.Open so we don't leak file descriptors — mirrors
	// the path-existence pattern in inv_binding_test.go:112.
	for inv, rel := range iPresExternalBinding {
		if _, statErr := os.Stat(filepath.Join(root, rel)); statErr != nil {
			t.Errorf("I-PRES-%d: external-binding path %q does not exist: %v (was the frontend directory renamed or deleted?)", inv, rel, statErr)
		}
	}

	bindings := make(map[int][]string) // I-PRES-N -> list of file paths

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
		matches := iPresVerifiesRE.FindAllSubmatch(data, -1)
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

	for _, inv := range iPresInvariants {
		if _, external := iPresExternalBinding[inv]; external {
			// Binding is a frontend test (path existence verified above).
			continue
		}
		if len(bindings[inv]) == 0 {
			t.Errorf("I-PRES-%d: no test binding found (expected at least one `// Verifies: I-PRES-%d` comment in a *_test.go file)", inv, inv)
		}
	}
}
