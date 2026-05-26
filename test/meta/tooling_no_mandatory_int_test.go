// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestToolingDoesNotMandateLocalIntE2E enforces INV-6: no .claude/ tooling
// artifact may assert that local int/e2e is mandatory before push. The old
// "to full completion before push" phrasing encoded exactly that rule.
func TestToolingDoesNotMandateLocalIntE2E(t *testing.T) {
	root := findRepoRoot(t)
	const banned = "to full completion before push"
	claudeDir := filepath.Join(root, ".claude")

	var offenders []string
	err := filepath.WalkDir(claudeDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// agent-memory holds review logs/learnings that legitimately QUOTE
			// the banned phrase descriptively (and reports/ is gitignored, so
			// this filesystem walk would otherwise read ephemeral session
			// artifacts). It is not a tooling artifact that asserts the rule.
			if d.Name() == "agent-memory" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip this guard's own source so its literal doesn't self-trip.
		if strings.HasSuffix(path, "tooling_no_mandatory_int_test.go") {
			return nil
		}
		f, openErr := os.Open(path) //nolint:gosec // path from controlled WalkDir under .claude/
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), banned) {
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	require.NoError(t, err, "walk .claude/")
	require.Empty(t, offenders,
		"these .claude/ artifacts still assert the old full-gate-before-push rule (INV-6): %v", offenders)
}
