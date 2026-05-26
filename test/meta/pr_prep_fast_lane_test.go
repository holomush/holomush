// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// taskBlock returns the Taskfile body of `<name>:` up to the next 2-space task key.
func taskBlock(t *testing.T, tf, name string) string {
	t.Helper()
	loc := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `:[ \t]*$`).FindStringIndex(tf)
	require.NotNil(t, loc, "%s target not found in Taskfile.yaml", name)
	after := tf[loc[1]:]
	if next := regexp.MustCompile(`(?m)^  \S`).FindStringIndex(after); next != nil {
		return after[:next[0]]
	}
	return after
}

// TestPrPrepFastLaneExcludesHeavyTiers enforces INV-4: the mandatory pr-prep
// (non-full) lane must not run test:int/test:e2e and must not flock; the
// heavy tiers live only in pr-prep:full.
func TestPrPrepFastLaneExcludesHeavyTiers(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err, "read Taskfile.yaml")
	tf := string(data)

	fast := taskBlock(t, tf, "pr-prep")
	require.NotContains(t, fast, "test:int", "pr-prep (fast) must not run integration tests (INV-4)")
	require.NotContains(t, fast, "test:e2e", "pr-prep (fast) must not run E2E tests (INV-4)")
	require.NotContains(t, fast, "flock", "pr-prep (fast) must not acquire the flock (INV-4)")

	full := taskBlock(t, tf, "pr-prep:full")
	require.Contains(t, full, "flock", "pr-prep:full must keep the flock")
}
