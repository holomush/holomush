// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestINV_Meta_AllInvariantsReferenced walks *_test.go files under
// internal/telemetry (.) and internal/logging (../logging) and fails if any
// of INV-L1…INV-L8 has zero references. This ensures every invariant from
// the spec is locked by at least one test. (INV-META)
func TestINV_Meta_AllInvariantsReferenced(t *testing.T) {
	want := []string{"INV-L1", "INV-L2", "INV-L3", "INV-L4", "INV-L5", "INV-L6", "INV-L7", "INV-L8"}
	// Paths are relative to this file's package directory (internal/telemetry).
	dirs := []string{".", "../logging"}

	found := map[string]bool{}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err, "reading dir %s", dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			content, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
			require.NoError(t, readErr, "reading %s/%s", dir, e.Name())
			for _, id := range want {
				if strings.Contains(string(content), id) {
					found[id] = true
				}
			}
		}
	}

	for _, id := range want {
		require.True(t, found[id], "no test references %s — add an // %s comment to the relevant test", id, id)
	}
}
