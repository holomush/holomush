// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// invPCRE matches any INV-PC-<1-8> token in a file.
var invPCRE = regexp.MustCompile(`INV-PC-[1-8]`)

// TestPluginConfigInvariantsHaveTestCoverage asserts that each of INV-PC-1
// through INV-PC-8 is cited by at least one *_test.go file in the repository
// tree. This ensures no invariant from the plugin-runtime-config design spec
// loses its test coverage silently.
func TestPluginConfigInvariantsHaveTestCoverage(t *testing.T) {
	want := []string{
		"INV-PC-1", "INV-PC-2", "INV-PC-3", "INV-PC-4",
		"INV-PC-5", "INV-PC-6", "INV-PC-7", "INV-PC-8",
	}
	cited := scanTreeForInvariantCitations(t, "INV-PC-")
	for _, id := range want {
		require.Contains(t, cited, id, "no test cites %s", id)
	}
}

// scanTreeForInvariantCitations walks the repository from the module root over
// all *_test.go files, collects every token matching the given prefix pattern
// (e.g. "INV-PC-"), and returns the set of distinct identifiers found.
// The root is resolved via findRepoRoot (defined in inv_binding_test.go),
// which walks upward from Getwd() until it finds a directory containing go.mod.
func scanTreeForInvariantCitations(t *testing.T, prefix string) map[string]struct{} {
	t.Helper()
	root := findRepoRoot(t)

	rootFS, err := os.OpenRoot(root)
	require.NoError(t, err, "open repo root %q", root)
	defer func() { _ = rootFS.Close() }()

	cited := make(map[string]struct{})

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		f, openErr := rootFS.Open(rel)
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(f)
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		for _, m := range invPCRE.FindAllString(string(data), -1) {
			if strings.HasPrefix(m, prefix) {
				cited[m] = struct{}{}
			}
		}
		return nil
	})
	require.NoError(t, err, "walk repo for INV-PC citations")
	return cited
}
