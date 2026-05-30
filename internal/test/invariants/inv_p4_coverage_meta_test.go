// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

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

// TestINV_P4_Coverage_Meta pins INV-P4-13 (meta): every numbered INV-P4-N
// declaration in the Phase 4 design spec MUST have at least one named
// test in the Go corpus.
//
// For each INV-P4-N this test verifies that the named test referenced in
// the coverage matrix (spec §12.1) exists somewhere under the repo root
// as a top-level Test* function in a *_test.go file.
//
// If this test FAILS:
//   - Either the test was renamed/deleted without updating this table, OR
//   - A new INV-P4-N invariant was declared without a corresponding test.
//
// Fix by updating the cases slice AND the spec's §12.1 coverage matrix
// in lockstep — the two MUST agree at all times.
//
// INV-P4-13 is THIS meta-test; recursive self-inclusion would be circular,
// so it is excluded from the cases table and is self-evidently covered by
// its own execution.
//
// Ginkgo integration tests are registered under a top-level TestXxx suite
// entry (e.g. TestCoreScenesIntegration, TestScenes); the meta-test maps
// invariants to their suite entry rather than the Ginkgo Describe label.
// The Describe label remains greppable in the spec for invariant traceability.
//
// Same shape as internal/eventbus/history/phase7_boundary_meta_test.go
// (T15 of the Phase 7 plan). Uses pure-Go go/parser walk — no external
// rg dependency — so the test runs in any environment with a Go toolchain.
func TestINV_P4_Coverage_Meta(t *testing.T) {
	t.Parallel()

	cases := []struct {
		inv      string
		testName string
		note     string
	}{
		// INV-P4-1: superseded by INV-ROPS-3's repo-wide colon-stream
		// eradication scan (TestINV_ROPS_3_NoColonStreamLiterals in
		// colon_eradication_test.go). The scene-only INV-P4-1 test
		// (scene_subjects_test.go) was retired by holomush-rops.8.
		// INV-P4-2: manifest crypto.emits set == EmitTypeRegistrar set.
		// Pinned by manifest-parse unit test in plugins/core-scenes/main_test.go
		// (package main). The substrate INV-S5 integration check runs under
		// TestCoreScenesIntegration but the manifest-parse unit is the named pin.
		{
			inv:      "INV-P4-2",
			testName: "TestPlugin_CryptoEmitsMatchesRegistry",
		},
		// INV-P4-3: sensitivity matrix must match spec §2 table.
		// Pinned by manifest-parse unit in plugins/core-scenes/main_test.go.
		{
			inv:      "INV-P4-3",
			testName: "TestPlugin_SensitivityMatrix",
		},
		// INV-P4-4: GetPoseOrder MUST NOT consult the ABAC engine.
		// Two complementary pins:
		//   (a) unit test asserting PermissionDenied for non-participants
		//   (b) go/parser meta-test asserting no engine.Evaluate in function body
		{
			inv:      "INV-P4-4",
			testName: "TestGetPoseOrder_NotParticipant_PermissionDenied",
		},
		{
			inv:      "INV-P4-4",
			testName: "TestINV_P4_4_NoABACInGetPoseOrder",
			note:     "meta-test: no engine.Evaluate call in GetPoseOrder body",
		},
		// INV-P4-5: resolver MUST NOT expose pose-order metadata as attributes.
		// Two complementary pins:
		//   (a) unit test on resolver output
		//   (b) rg-based meta-test asserting no pose-metadata columns in resolver.go
		{
			inv:      "INV-P4-5",
			testName: "TestResolveResourceDoesNotLeakPoseOrderMetadata",
		},
		{
			inv:      "INV-P4-5",
			testName: "TestINV_P4_5_ResolverNoPoseOrderLeak",
			note:     "meta-test: no pose-metadata column refs in resolver.go",
		},
		// INV-P4-6: non-participants MUST NOT receive scene IC events.
		// Ginkgo integration test; entry point is TestScenes in
		// test/integration/scenes/suite_test.go. The Describe label
		// "INV-P4-6: non-participant scene IC isolation" is greppable.
		{
			inv:      "INV-P4-6",
			testName: "TestScenes",
			note:     "Ginkgo suite entry; Describe: INV-P4-6 non-participant scene IC isolation",
		},
		// INV-P4-7: per-mode pose-order computation correctness.
		// Table-driven pure-function unit test in plugins/core-scenes/poseorder_test.go.
		{
			inv:      "INV-P4-7",
			testName: "TestCompute",
		},
		// INV-P4-8: maintained metadata == SQL-recovery result.
		// Ginkgo integration test; entry point is TestCoreScenesIntegration in
		// plugins/core-scenes/core_scenes_suite_test.go. The Describe label
		// "SceneStore.InsertScenePose + pose-order rebuild" is greppable.
		{
			inv:      "INV-P4-8",
			testName: "TestCoreScenesIntegration",
			note:     "Ginkgo suite entry; Describe: pose-order metadata rebuild from scene_log",
		},
		// INV-P4-9: late-joining participants see only IC events from joined_at.
		// Ginkgo integration test; entry point is TestScenes in
		// test/integration/scenes/suite_test.go. The Describe label
		// "INV-P4-9: late-joiner temporal floor" is greppable.
		{
			inv:      "INV-P4-9",
			testName: "TestScenes",
			note:     "Ginkgo suite entry; Describe: INV-P4-9 late-joiner temporal floor",
		},
		// INV-P4-10: scene_pose audit INSERT + pose-metadata UPDATE are transactional.
		// Ginkgo integration test; entry point is TestCoreScenesIntegration in
		// plugins/core-scenes/core_scenes_suite_test.go. The Describe label
		// "SceneAuditStore.InsertScenePose" is greppable.
		{
			inv:      "INV-P4-10",
			testName: "TestCoreScenesIntegration",
			note:     "Ginkgo suite entry; Describe: SceneAuditStore.InsertScenePose (INV-P4-10)",
		},
		// INV-P4-11: emit subcommands require participant membership.
		// Pinned by unit test in plugins/core-scenes/commands_emit_test.go.
		{
			inv:      "INV-P4-11",
			testName: "TestSceneSubcommand_NonParticipant_PermissionDenied",
		},
		// INV-P4-12: scene update pose_order_mode requires scene owner.
		// Ginkgo integration test; entry point is TestCoreScenesIntegration in
		// plugins/core-scenes/core_scenes_suite_test.go. ABAC update-own-scene
		// policy enforced at Layer-2 capability pre-flight. The Describe label
		// "UpdateScene pose_order_mode non-owner rejection (INV-P4-12)" is greppable.
		{
			inv:      "INV-P4-12",
			testName: "TestCoreScenesIntegration",
			note:     "Ginkgo suite entry; Describe: UpdateScene pose_order_mode non-owner (INV-P4-12)",
		},
	}

	repoRoot := findRepoRootFromInvariants(t)
	testNames := collectTestFuncNamesFromInvariants(t, repoRoot)

	for _, tc := range cases {
		tc := tc
		name := tc.inv
		if tc.note != "" {
			name = tc.inv + "/" + tc.testName
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := testNames[tc.testName]; !ok {
				t.Fatalf("%s: named test %q NOT FOUND under %s\n  note: %s",
					tc.inv, tc.testName, repoRoot, tc.note)
			}
		})
	}
}

// findRepoRootFromInvariants walks up from this test file's directory until
// a go.mod is found. Deterministic regardless of the test's cwd at invocation
// time — gotestsum, go test, IDE runners, and CI all set cwd inconsistently.
//
// Named distinctly from the Phase 7 meta-test's helper to avoid a collision
// when the two test files happen to end up in the same test binary (they live
// in different packages, but the distinct name documents intent clearly).
func findRepoRootFromInvariants(t *testing.T) string {
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
			t.Fatalf("findRepoRootFromInvariants: walked to filesystem root from %s without finding go.mod",
				filepath.Dir(thisFile))
		}
		dir = parent
	}
}

// collectTestFuncNamesFromInvariants walks repoRoot, parses every *_test.go
// file with go/parser, and returns the set of top-level Test* function names.
// Skips vendor/, node_modules/, build/, and any dot-prefixed directory.
//
// A single malformed/generated test file does not fail the whole walk —
// the parse error is logged and the file is skipped. Drift detection still
// works as long as the test corpus is parseable.
func collectTestFuncNamesFromInvariants(t *testing.T, repoRoot string) map[string]struct{} {
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
			// Skip dot-prefixed directories (.git, .jj, .beads, .claude,
			// .svelte-kit, etc.) — these never hold load-bearing Go test files.
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
			if !isRunnableGoTestInvariants(fd) {
				continue
			}
			names[fd.Name.Name] = struct{}{}
		}
		return nil
	})
	require.NoError(t, err, "failed walking %s", repoRoot)
	return names
}

// isRunnableGoTestInvariants reports whether fd matches the signature that
// `go test` will actually execute: top-level (no receiver), name TestXxx
// with an upper-case letter after Test, exactly one *testing.T parameter,
// no return values, non-variadic.
//
// Mirrors cmd/go/internal/test/test.go rules. Named with the Invariants
// suffix to avoid a symbol collision with the identically-structured helper
// that may be linked from another file in the same package.
func isRunnableGoTestInvariants(fd *ast.FuncDecl) bool {
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
	if len(name) == 4 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name[4:])
	if unicode.IsLetter(r) && unicode.IsLower(r) {
		return false
	}
	if fd.Type.Params == nil || len(fd.Type.Params.List) != 1 {
		return false
	}
	param := fd.Type.Params.List[0]
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
