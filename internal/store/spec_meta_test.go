// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNanosecondTimestampsInvariantsHaveNamedTests is the drift detector
// for the invariants table in
// docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md §5.
// For each INV-TS-N (1..7), the test verifies the named test that pins
// the invariant exists somewhere in the repo's *_test.go corpus.
//
// If this test FAILS:
//   - Either an invariant was removed without updating this table, OR
//   - A named test was renamed/removed without updating this table.
//
// Fix by adjusting the cases slice below AND the spec's invariant table
// in lockstep — the two MUST agree at all times.
//
// INV-TS-META (this test itself) is intentionally excluded; the table
// protects everything except itself.
//
// Implementation: walk the test file's location via runtime.Caller(0),
// climb until a `go.mod` is found, then walk the tree with go/parser
// to enumerate top-level Test* function names. Pure-Go (no external `rg`
// dependency) so the test runs in any environment with a Go toolchain.
func TestNanosecondTimestampsInvariantsHaveNamedTests(t *testing.T) {
	cases := []struct {
		inv      string
		testName string
	}{
		// INV-TS-1: lint + integration meta-test
		{"INV-TS-1", "TestNoTimestamptzColumnsAfterMigration"},
		// INV-TS-2: pgnanos round-trip test
		{"INV-TS-2", "TestRoundTripPreservesSubMicrosecondNanoseconds"},
		// INV-TS-3: enforced by lint:no-microsecond-truncate; meta-test asserts the lint passes
		{"INV-TS-3", "TestLintNoMicrosecondTruncatePasses"},
		// INV-TS-4: publisher preserves ns
		{"INV-TS-4", "TestPublisherPreservesNanoseconds"},
		// INV-TS-5: AAD round-trip preserves ns
		{"INV-TS-5", "TestRoundTripPreservesAADWithSubMicrosecondNanos"},
		// INV-TS-6: floor drops sub-floor-ns events
		{"INV-TS-6", "TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival"},
		// INV-TS-7: floor includes exact-floor-ns events
		{"INV-TS-7", "TestDispatchDeliveryIncludesEventAtExactFloorNanosecond"},
	}

	repoRoot := findRepoRoot(t)
	testNames := collectTestFuncNames(t, repoRoot)

	// Phase-5-deferred subtests: TestNoTimestamptzColumnsAfterMigration
	// (INV-TS-1) and TestLintNoMicrosecondTruncatePasses (INV-TS-3) are
	// created in Phase 5 alongside the lint guards they exercise. Skipping
	// them keeps task pr-prep green for Phase 2-4 PRs. Once Phase 5 merges
	// and both tests exist, the skip becomes a no-op (the lookup succeeds)
	// and CAN be removed safely as the last step of Phase 5 (Task 22).
	phaseFiveDeferred := map[string]struct{}{
		"INV-TS-1": {},
		"INV-TS-3": {},
	}

	for _, tc := range cases {
		t.Run(tc.inv, func(t *testing.T) {
			if _, deferred := phaseFiveDeferred[tc.inv]; deferred {
				if _, ok := testNames[tc.testName]; !ok {
					t.Skipf("Phase-5-deferred: %s names test %q which lands in Phase 5; "+
						"remove this skip-guard once Phase 5 merges (see plan Task 22 Step 6)",
						tc.inv, tc.testName)
					return
				}
				// Test exists — fail loudly so the cleanup obligation
				// cannot be missed. Once Phase 5 lands and both tests
				// exist, this branch fires and breaks `task pr-prep`
				// until Task 22 Step 6 removes the guard. This is
				// preferable to a `t.Logf` (silently suppressed by
				// gotestsum compact mode) because the failure forces
				// the cleanup into the same PR that lands the tests.
				t.Errorf(
					"phase-5-deferred guard fires for %s test %q (which now exists). "+
						"Remove the entry from phaseFiveDeferred (plan Task 22 Step 6) "+
						"to restore drift detection. This failure is the cleanup obligation.",
					tc.inv, tc.testName,
				)
				return
			}
			if _, ok := testNames[tc.testName]; !ok {
				t.Errorf("spec invariant %s names test %q, but no such Test* function exists anywhere in the repo",
					tc.inv, tc.testName)
			}
		})
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	dir := filepath.Dir(here)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found in any ancestor of test file")
		}
		dir = parent
	}
}

func collectTestFuncNames(t *testing.T, root string) map[string]struct{} {
	t.Helper()
	names := make(map[string]struct{})
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendor, build, .git, node_modules, etc.
			name := info.Name()
			if name == "vendor" || name == "node_modules" || name == ".git" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		// Parse with comments off; we only need top-level decls.
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// Skip files that don't parse cleanly (e.g., generated stubs).
			// The meta-test must not fail just because some unrelated _test.go
			// has a parse error; the missing test name will be reported
			// separately by the per-INV assertion.
			return nil //nolint:nilerr // intentional skip per docstring
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil {
				continue
			}
			n := fd.Name.Name
			if strings.HasPrefix(n, "Test") {
				names[n] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("collecting test func names: %v", err)
	}
	return names
}
