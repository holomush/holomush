// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCIRequiredJobNamesPresent enforces INV-5: both the real CI workflow and
// the docs-skip workflow must define jobs named "Integration Test" and
// "E2E Test" so the required checks resolve (incl. on docs-only PRs).
func TestCIRequiredJobNamesPresent(t *testing.T) {
	root := findRepoRoot(t)
	for _, wf := range []string{
		filepath.Join(".github", "workflows", "ci.yaml"),
		filepath.Join(".github", "workflows", "ci-docs-skip.yaml"),
	} {
		data, err := os.ReadFile(filepath.Join(root, wf))
		require.NoError(t, err, "read %s", wf)
		body := string(data)
		require.Contains(t, body, "name: Integration Test", "%s missing 'Integration Test' job (INV-5)", wf)
		require.Contains(t, body, "name: E2E Test", "%s missing 'E2E Test' job (INV-5)", wf)
	}
}
