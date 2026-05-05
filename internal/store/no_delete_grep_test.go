// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoDeleteFromPluginsInCodebase enforces INV-W9ML-9: plugin rows are
// never DELETEd. SweepInactive sets gc_at instead. CI grep guards against
// future changes that would reintroduce DELETE.
//
// Uses `git grep` (not `os.ReadFile`) so the search covers the entire
// tracked tree regardless of the test's working directory.
func TestNoDeleteFromPluginsInCodebase(t *testing.T) {
	out, err := exec.Command("git", "grep", "-nE",
		`DELETE\s+FROM\s+plugins\b`,
		"--", "*.go").CombinedOutput()
	// `git grep` exits 1 when no matches found — that's the success case.
	// Exit 0 means matches were found, which violates INV-W9ML-9.
	if err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatalf("INV-W9ML-9 violation: DELETE FROM plugins in production code:\n%s", out)
	}
}
