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
	"strconv"
	"strings"
	"testing"
)

// TestNanosecondTimestampsInvariantsHaveNamedTests is the drift detector
// for the invariants table in
// docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md §5.
// For each INV-STORE-N (1..7 and 9; there is no INV-STORE-8 — the legacy
// INV-TS-8 was dropped, see the §7 future-work table), the test verifies the named test that pins the
// invariant exists somewhere in the repo's *_test.go corpus.
//
// "Named test" matches against two surfaces collected from the source AST:
//   - Top-level `func Test*(t *testing.T)` declarations (idiomatic
//     testing+testify entry points).
//   - First-argument string literals of Ginkgo container/spec calls
//     `Describe`, `Context`, `When`, `It` (so a suite-registered spec
//     pins an invariant via its description string).
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
// climb until a `go.mod` is found, then walk the tree with go/parser to
// enumerate top-level Test* function names AND Ginkgo Describe/Context/
// When/It string-literal first arguments. Pure-Go (no external `rg`
// dependency) so the test runs in any environment with a Go toolchain.
func TestNanosecondTimestampsInvariantsHaveNamedTests(t *testing.T) {
	cases := []struct {
		inv      string
		testName string
	}{
		// INV-STORE-1: lint + Ginkgo integration spec (Describe string is
		// the pinned identifier — the test is suite-registered under
		// store_suite_test.go::TestStore, not a standalone func Test*).
		{"INV-STORE-1", "INV-STORE-1: no TIMESTAMPTZ columns after migration"},
		// INV-STORE-2: pgnanos round-trip test
		{"INV-STORE-2", "TestRoundTripPreservesSubMicrosecondNanoseconds"},
		// INV-STORE-3: enforced by lint:no-microsecond-truncate; meta-test asserts the lint passes
		{"INV-STORE-3", "TestLintNoMicrosecondTruncatePasses"},
		// INV-STORE-4: publisher preserves ns
		{"INV-STORE-4", "TestPublisherPreservesNanoseconds"},
		// INV-STORE-5: AAD round-trip preserves ns
		{"INV-STORE-5", "TestRoundTripPreservesAADWithSubMicrosecondNanos"},
		// INV-STORE-6: floor drops sub-floor-ns events
		{"INV-STORE-6", "TestDispatchDeliveryDropsEventEmittedInSameNanosecondAsArrival"},
		// INV-STORE-7: floor includes exact-floor-ns events
		{"INV-STORE-7", "TestDispatchDeliveryIncludesEventAtExactFloorNanosecond"},
		// INV-STORE-9: conversion migrations saturate out-of-range/infinity to
		// int64-ns bounds, preserve NULL (Describe string is the pinned
		// identifier — suite-registered under TestStore). The legacy INV-TS-8
		// was dropped (former wire-format invariant; see §7 future-work table),
		// so there is no INV-STORE-8 — the number is intentionally skipped.
		{"INV-STORE-9", "INV-STORE-9: TIMESTAMPTZ→BIGINT conversion saturates out-of-range and infinity to int64-ns bounds and preserves NULL"},
	}

	repoRoot := findRepoRoot(t)
	testNames := collectTestNames(t, repoRoot)

	for _, tc := range cases {
		t.Run(tc.inv, func(t *testing.T) {
			if _, ok := testNames[tc.testName]; !ok {
				t.Errorf("spec invariant %s names test %q, but no such Test* function or Ginkgo Describe/Context/When/It string literal exists anywhere in the repo",
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

// ginkgoContainerNames are the Ginkgo v2 container/spec functions whose
// first string-literal argument is treated as a test-name surface for
// INV-TS-META lookup. Listed in the order Ginkgo specs introduce nesting:
// outer containers first, then leaf specs.
var ginkgoContainerNames = map[string]struct{}{
	"Describe": {},
	"Context":  {},
	"When":     {},
	"It":       {},
}

func collectTestNames(t *testing.T, root string) map[string]struct{} {
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
		// Parse with comments off; we only need top-level decls + call exprs.
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// Skip files that don't parse cleanly (e.g., generated stubs).
			// The meta-test must not fail just because some unrelated _test.go
			// has a parse error; the missing test name will be reported
			// separately by the per-INV assertion.
			return nil //nolint:nilerr // intentional skip per docstring
		}
		// Top-level Test* func decls — idiomatic testing+testify entry points.
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
		// Ginkgo container/spec first-arg string literals — suite-registered
		// specs identified by their Describe/Context/When/It description.
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if _, isContainer := ginkgoContainerNames[ident.Name]; !isContainer {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// Unquote the Go string literal. Skip silently on malformed
			// literals (parser should have rejected them above anyway).
			unquoted, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			names[unquoted] = struct{}{}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("collecting test names: %v", err)
	}
	return names
}
