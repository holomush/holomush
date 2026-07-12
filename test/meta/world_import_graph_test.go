// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/build"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// modulePath is the Go module path — the prefix of every in-repo import.
const modulePath = "github.com/holomush/holomush"

// worldPkgImports returns the PRODUCTION import list of the package rooted at rel
// (relative to the repo root). build.Package.Imports excludes _test.go files
// (those are TestImports/XTestImports) — so this guards production imports only,
// per round-3 MEDIUM (test files may legitimately hold concrete fixtures).
func worldPkgImports(t *testing.T, root, rel string) []string {
	t.Helper()
	bctx := build.Default
	bctx.CgoEnabled = false
	pkg, err := bctx.ImportDir(filepath.Join(root, rel), 0)
	require.NoErrorf(t, err, "import package %s", rel)
	return pkg.Imports
}

// TestWorldImportGraphForbiddenEdges asserts the FULL adjacency matrix the
// MODEL-04 design promises (round-3 MEDIUM — the round-2 guard checked only 3
// edges). Any of the eight forbidden edges would either recreate an import cycle
// or breach the writer/relay boundary.
func TestWorldImportGraphForbiddenEdges(t *testing.T) {
	root := findRepoRoot(t)

	const (
		world    = "internal/world"
		outbox   = "internal/world/outbox"
		postgres = "internal/world/postgres"
		wmodel   = "internal/world/wmodel"
	)

	forbidden := []struct{ fromRel, toRel string }{
		{world, outbox},    // world must not import the relay package
		{world, postgres},  // world must not import the concrete writer package
		{outbox, postgres}, // round-2 second cycle: outbox -> postgres -> outbox
		{outbox, world},    // round-3 forbidden edge
		{postgres, outbox}, // writer boundary: postgres must not import the relay
		{wmodel, world},    // wmodel is a leaf; wmodel -> world recreates a cycle
		{wmodel, postgres}, // leaf must not import the writer
		{wmodel, outbox},   // leaf must not import the relay
	}

	for _, e := range forbidden {
		imports := worldPkgImports(t, root, e.fromRel)
		toPath := modulePath + "/" + e.toRel
		require.NotContainsf(t, imports, toPath,
			"forbidden import edge: %s must NOT import %s (production imports only)", e.fromRel, e.toRel)
	}
}

// TestWorldPostgresCompositionAllowlist asserts that ONLY composition/test
// packages import internal/world/postgres, constraining concrete world-writer
// construction (round-3 blocker #5 pairing). A new production package cannot hold
// a concrete writer repo.
func TestWorldPostgresCompositionAllowlist(t *testing.T) {
	root := findRepoRoot(t)
	target := modulePath + "/internal/world/postgres"

	// Allowed importer package-path prefixes (repo-relative, slash-separated).
	allowed := []string{
		"cmd/holomush",
		"internal/world/setup",
		"internal/bootstrap/setup",
		"internal/access/setup",
		"internal/testsupport", // the whole testsupport tree
		"internal/world/postgres",
	}
	isAllowed := func(rel string) bool {
		for _, a := range allowed {
			if rel == a || strings.HasPrefix(rel, a+"/") {
				return true
			}
		}
		return false
	}

	skipDirs := map[string]struct{}{
		".git": {}, "vendor": {}, "node_modules": {}, "web": {}, "site": {},
		"gorules": {}, "testdata": {}, "dist": {}, ".planning": {},
	}

	bctx := build.Default
	bctx.CgoEnabled = false

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, skip := skipDirs[d.Name()]; skip {
			return filepath.SkipDir
		}
		pkg, ierr := bctx.ImportDir(path, 0)
		if ierr != nil {
			// Not a buildable Go package for the default context — skip.
			return nil //nolint:nilerr // intentional: non-package dirs are skipped
		}
		if !slices.Contains(pkg.Imports, target) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		require.NoError(t, rerr)
		rel = filepath.ToSlash(rel)
		require.Truef(t, isAllowed(rel),
			"package %q imports internal/world/postgres but is not in the composition allowlist "+
				"(concrete world-writer construction is constrained to composition/test packages)", rel)
		return nil
	})
	require.NoError(t, err)
}
