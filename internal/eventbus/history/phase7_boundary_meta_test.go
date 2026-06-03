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
	"unicode"
	"unicode/utf8"

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
		// INV-EVENTBUS-25 was carried by func TestSceneLogHasDekColumns until the
		// 1hq.26 testify+ginkgo migration converted the spec to a Ginkgo
		// Describe registered under the suite entry TestBinaryPlugin (see
		// test/integration/plugin/plugin_migration_test.go). The spec name
		// "Scene log has DEK columns (INV-EVENTBUS-25)" remains greppable inside
		// that file for invariant traceability.
		{"INV-EVENTBUS-25", "TestBinaryPlugin"},
		{"INV-EVENTBUS-26", "TestAuditRowStructMirrorsProto"},
		{"INV-P7-5", "TestAuditRowRoundTripPreservesAllFields"},
		// INV-P7-6 was carried by func TestSceneLogPreservesCiphertextAndAuditHeaders
		// until the holomush-cz4s testify+ginkgo migration converted the spec to a
		// Ginkgo Describe registered under the suite entry TestEventbusE2E (see
		// test/integration/eventbus_e2e/plugin_audit_round_trip_test.go). The spec
		// name "Scene log preserves ciphertext and audit headers (INV-P7-6, INV-P7-12)"
		// remains greppable inside that file for invariant traceability.
		{"INV-P7-6", "TestEventbusE2E"},
		{"INV-P7-7", "TestFenceRefusesIdentityForAlwaysSensitiveType"},
		// INV-P7-7b — per-row, NOT stream-fatal (the corrected v3 design).
		{"INV-P7-7b", "TestFenceContinuesStreamAfterRefusal"},
		{"INV-P7-8", "TestFenceSetBuiltOnceAtBoot"},
		// INV-P7-C0 — Phase C.0 substrate (auditRow stamp + accessor).
		{"INV-P7-C0", "TestAuditRowOfStampedByRouter"},
		// INV-P7-9 was carried by func TestDispatcherAndHotTierShareSelector until
		// the holomush-cz4s testify+ginkgo migration converted the spec to a Ginkgo
		// Describe registered under the suite entry TestEventbusE2E (see
		// test/integration/eventbus_e2e/dispatcher_selector_identity_test.go). The
		// spec name "Dispatcher and hot tier share selector (INV-P7-9)" remains
		// greppable inside that file for invariant traceability.
		{"INV-P7-9", "TestEventbusE2E"},
		// INV-EVENTBUS-27 was carried by func TestDowngradeAttackerMaliciousPathRefuses until
		// the holomush-cz4s testify+ginkgo migration converted the spec to a Ginkgo
		// Describe registered under the suite entry TestEventbusE2E (see
		// test/integration/eventbus_e2e/plugin_downgrade_attacker_test.go). The spec
		// name "Downgrade attacker malicious path refuses (INV-EVENTBUS-27)" remains
		// greppable inside that file for invariant traceability.
		{"INV-EVENTBUS-27", "TestEventbusE2E"},
		{"INV-P7-11", "TestDispatchDoesNotDecryptBeforeForward"},
		// INV-P7-12 shares its named Ginkgo spec with INV-P7-6 (one round-trip
		// covers both cleartext-vs-ciphertext invariants). Was carried by func
		// TestSceneLogPreservesCiphertextAndAuditHeaders until the holomush-cz4s
		// testify+ginkgo migration; now registered under TestEventbusE2E (see
		// test/integration/eventbus_e2e/plugin_audit_round_trip_test.go).
		{"INV-P7-12", "TestEventbusE2E"},
		// INV-P7-13 was carried by func TestPluginRoleCannotWriteHostTables
		// until the 1hq.26 testify+ginkgo migration converted the spec to a
		// Ginkgo Describe registered under the suite entry TestBinaryPlugin
		// (see test/integration/plugin/plugin_role_permissions_test.go).
		// The meta-test maps to the suite entry — the spec name "Plugin role
		// cannot write host tables (INV-P7-13)" remains greppable inside
		// that file for invariant traceability.
		{"INV-P7-13", "TestBinaryPlugin"},
		{"INV-P7-15", "TestFenceRefusesUnknownDekRef"},
		// INV-P7-16 was superseded by INV-STORE-5 (ADR holomush-f5h0); the
		// carrier test was renamed from TestRoundTripProducesByteEqualAAD
		// to TestRoundTripPreservesAADWithSubMicrosecondNanos as part of
		// gfo6 Phase 1 (ns-precise timestamps).
		{"INV-P7-16", "TestRoundTripPreservesAADWithSubMicrosecondNanos"},
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
			if !isRunnableGoTest(fd) {
				continue
			}
			names[fd.Name.Name] = struct{}{}
		}
		return nil
	})
	require.NoError(t, err, "failed walking %s", repoRoot)
	return names
}

// isRunnableGoTest reports whether fd matches the signature `go test` will
// actually execute: top-level (no receiver), name `TestXxx` with a capital
// letter after `Test`, exactly one `*testing.T` parameter, no return values,
// non-variadic. Without this filter a misshapen signature like
// `func TestFoo(t *testing.T) error` or `func TestFoo()` would be picked up
// by the meta-test as a "named test exists" hit but never run under
// `go test` — silently masking drift the meta-test exists to catch.
//
// Mirrors the rules in cmd/go/internal/test/test.go (Go's own test discovery).
func isRunnableGoTest(fd *ast.FuncDecl) bool {
	if fd.Recv != nil || fd.Type == nil {
		return false
	}
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		return false
	}
	name := fd.Name.Name
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	// Plain `Test` (no suffix) is not a runnable test, and a lowercase letter
	// after `Test` (e.g. `Testify`) is not either — Go requires the next rune
	// to be an uppercase letter or non-letter.
	if len(name) == 4 {
		return false
	}
	// Decode the full rune (not just the first byte) so non-ASCII names are
	// classified correctly — e.g. capital epsilon TestΕxample's leading
	// byte 0xCE looks like a non-letter under rune(name[4]) but the actual
	// rune Ε is uppercase and Go would happily run that test.
	r, _ := utf8.DecodeRuneInString(name[4:])
	if unicode.IsLetter(r) && unicode.IsLower(r) {
		return false
	}
	if fd.Type.Params == nil || len(fd.Type.Params.List) != 1 {
		return false
	}
	param := fd.Type.Params.List[0]
	// AST groups same-typed params into one Field with multiple Names —
	// e.g. `func TestFoo(t, u *testing.T)` is one Field with len(Names)==2.
	// Go test discovery requires exactly one parameter total; reject the
	// multi-name field even though its Type matches.
	if len(param.Names) > 1 {
		return false
	}
	star, ok := param.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "T" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing"
}
