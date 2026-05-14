// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPhase7InvariantsHaveNamedTests is the drift detector for the
// invariants table in
// docs/superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md
// §2. For each INV-P7-N (1..16) plus the two plan-internal substrate
// keys (INV-P7-7b, INV-P7-C0), the test verifies the named test that
// pins the invariant exists somewhere in the repo's *_test.go corpus.
//
// If this test FAILS:
//   - Either an invariant was removed without updating this table, OR
//   - A named test was renamed/removed without updating this table.
//
// Fix by adjusting the cases slice below AND the spec's invariant table
// in lockstep — the two MUST agree at all times.
//
// INV-P7-2 and INV-P7-14 are intentionally excluded from this table:
// INV-P7-14 is THIS meta-test (the table protects everything except
// itself; recursive coverage would be circular). INV-P7-2 is a
// build-time invariant covered by `task plugin:build-all` rather than a
// named test in the Go corpus.
//
// Implementation: walk the test file's location via runtime.Caller(0),
// climb until a `go.mod` is found, then walk the tree with go/parser
// to enumerate top-level Test* function names. Pure-Go (no external
// `rg` dependency) so the test runs in any environment with a Go
// toolchain — including stripped-down CI runners.
func TestPhase7InvariantsHaveNamedTests(t *testing.T) {
	cases := []struct {
		inv      string
		testName string
	}{
		{"INV-P7-1", "TestDispatchForwardsCiphertextByteEqual"},
		{"INV-P7-3", "TestSceneLogHasDekColumns"},
		{"INV-P7-4", "TestAuditRowStructMirrorsProto"},
		{"INV-P7-5", "TestAuditRowRoundTripPreservesAllFields"},
		{"INV-P7-6", "TestSceneLogPreservesCiphertextAndAuditHeaders"},
		{"INV-P7-7", "TestFenceRefusesIdentityForAlwaysSensitiveType"},
		// INV-P7-7b — per-row, NOT stream-fatal (the corrected v3 design).
		{"INV-P7-7b", "TestFenceContinuesStreamAfterRefusal"},
		{"INV-P7-8", "TestFenceSetBuiltOnceAtBoot"},
		// INV-P7-C0 — Phase C.0 substrate (auditRow stamp + accessor).
		{"INV-P7-C0", "TestAuditRowOfStampedByRouter"},
		{"INV-P7-9", "TestDispatcherAndHotTierShareSelector"},
		{"INV-P7-10", "TestDowngradeAttackerMaliciousPathRefuses"},
		{"INV-P7-11", "TestDispatchDoesNotDecryptBeforeForward"},
		// INV-P7-12 shares its named test with INV-P7-6 (one round-trip
		// covers both cleartext-vs-ciphertext invariants).
		{"INV-P7-12", "TestSceneLogPreservesCiphertextAndAuditHeaders"},
		{"INV-P7-13", "TestPluginRoleCannotWriteHostTables"},
		{"INV-P7-15", "TestFenceRefusesUnknownDekRef"},
		{"INV-P7-16", "TestRoundTripProducesByteEqualAAD"},
	}

	repoRoot := findRepoRoot(t)
	testNames := collectTestFuncNames(t, repoRoot)

	for _, tc := range cases {
		t.Run(tc.inv, func(t *testing.T) {
			if _, ok := testNames[tc.testName]; !ok {
				t.Fatalf("%s: named test %q NOT FOUND under %s", tc.inv, tc.testName, repoRoot)
			}
		})
	}
}

// findRepoRoot walks up from this test file's directory until a go.mod
// is found. Deterministic regardless of the test's cwd at invocation
// time — gotestsum, go test, IDE runners, and CI all set cwd
// inconsistently.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller(0) MUST resolve this test file's path")

	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("findRepoRoot: walked to filesystem root from %s without finding go.mod", filepath.Dir(thisFile))
		}
		dir = parent
	}
}

// collectTestFuncNames walks repoRoot, parses every *_test.go file with
// go/parser, and returns the set of top-level Test* function names found.
// Skipped directories: vendor/, node_modules/, build/, and any dot-prefixed
// directory (covers .git/, .jj/, .beads/, .claude/, .svelte-kit/, etc.).
//
// A single malformed/generated test file does not fail the whole walk —
// the parse error is logged and the file is skipped. Drift detection still
// works as long as the test corpus is parseable.
func collectTestFuncNames(t *testing.T, repoRoot string) map[string]struct{} {
	t.Helper()
	names := make(map[string]struct{})
	fset := token.NewFileSet()

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "node_modules" || name == "build" {
				return filepath.SkipDir
			}
			// Skip any dot-prefixed directory (.git, .jj, .beads, .claude,
			// .svelte-kit, etc.) — these never hold load-bearing Go test
			// files. The repoRoot itself never matches because filepath.Dir
			// strips the trailing slash from a non-dot input path.
			if strings.HasPrefix(name, ".") && path != repoRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// Tolerate a single malformed file (generated stubs, intentionally
			// broken fixtures) — drift detection survives partial corpus.
			t.Logf("parse %s: %v (skipping)", path, err)
			return nil
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			// Top-level (no receiver) Test* function.
			if fd.Recv != nil {
				continue
			}
			if !strings.HasPrefix(fd.Name.Name, "Test") {
				continue
			}
			names[fd.Name.Name] = struct{}{}
		}
		return nil
	})
	require.NoError(t, err, "failed walking %s", repoRoot)
	return names
}
