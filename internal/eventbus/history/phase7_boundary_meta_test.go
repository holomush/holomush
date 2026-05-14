// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history_test

import (
	"errors"
	"os"
	"os/exec"
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
// keys (INV-P7-7b, INV-P7-C0), the test rg-greps the repo for the named
// test that pins the invariant.
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
// climb until a `go.mod` is found, and rg from there. This avoids the
// cwd-fragility of relative paths like `../../`.
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

	for _, tc := range cases {
		t.Run(tc.inv, func(t *testing.T) {
			// rg -l prints filenames containing matches, one per line.
			// `func <Name>(` anchors on the function definition so we
			// don't false-positive on call sites OR on a longer test
			// name that begins with the same prefix (e.g. renaming
			// TestFenceRefusesUnknownDekRef to
			// TestFenceRefusesUnknownDekRefRENAMED would silently still
			// match without the trailing `(`).
			cmd := exec.Command("rg", "-l", `func `+tc.testName+`\(`, repoRoot) //nolint:gosec // testName values are compile-time constants from the cases slice
			out, err := cmd.Output()
			if err != nil {
				// rg exits with code 1 when no matches are found. Any other
				// exit code (or absent rg binary) is an infrastructure
				// failure — surface it loudly rather than silently passing
				// or hanging.
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
					t.Fatalf("%s: named test %q NOT FOUND under %s", tc.inv, tc.testName, repoRoot)
				}
				t.Fatalf("%s: rg failed for %q: %v", tc.inv, tc.testName, err)
			}
			require.NotEmpty(t, strings.TrimSpace(string(out)),
				"%s: named test %q not found in tree (rg returned empty)", tc.inv, tc.testName)
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
