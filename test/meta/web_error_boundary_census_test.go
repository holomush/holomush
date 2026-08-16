// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io/fs"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// webRoutesDir is the SvelteKit route tree, relative to the repo root. It is the
// walk root rather than web/src, because SvelteKit resolves an error boundary by
// route nesting and a `+error.svelte` outside the route tree is inert.
const webRoutesDir = "web/src/routes"

// errorBoundaryFileName is the SvelteKit filename that DECLARES an error
// boundary. Membership is decided by base-name EQUALITY against this constant.
//
// Equality, not a leading-substring relation. Any sibling file whose name starts
// with the boundary's — a companion test, a snapshot, a backup — would be
// counted as a second boundary by a looser predicate, and this census would then
// be red against its own correct implementation. SvelteKit's own reserved-name
// rule keeps most such siblings out of the route tree (it refuses to build when
// a `+`-prefixed file is not a route file it recognizes), but that rule is a
// property of the framework version, not of this test.
const errorBoundaryFileName = "+error.svelte"

// TestExactlyOneSvelteKitErrorBoundaryExistsUnderWebRoutes asserts that the
// SvelteKit error-boundary RESOLUTION POINT under the route tree is unique, and
// that the one boundary sits at the route root.
//
// # What uniqueness buys
//
// SvelteKit renders the NEAREST `+error.svelte` above the failing route. With
// one boundary at the root, every kind of miss — an unknown route, an /admin
// deep link the viewer may not see, an /admin/* denial, an unknown /c/[id] —
// resolves to the same component, so none of them can be told apart by which
// page came back. A second boundary re-partitions that: an admin miss would
// render one component and an anonymous miss another, and the difference is an
// authorization oracle. The regression arrives as a one-file PR that reads as
// harmless, which is why the count is pinned mechanically rather than reviewed.
//
// # What this census does NOT cover, stated so nobody infers coverage it lacks
//
// It is a guard over the FILESYSTEM, not over RENDERED OUTPUT. It says nothing
// about what the single boundary renders, whether it echoes the requested path,
// which viewer tier its destination list reflects, or whether two viewers see
// the same bytes. A boundary that is unique and also leaks is green here.
// Uniqueness is a PRECONDITION for per-viewer indistinguishability, not a proof
// of it; the rendered half is asserted by
// web/src/routes/error-boundary.svelte.test.ts and at the page level by this
// phase's later plans.
//
// Verifies: INV-PRIVACY-14
func TestExactlyOneSvelteKitErrorBoundaryExistsUnderWebRoutes(t *testing.T) {
	root := findRepoRoot(t)
	walkRoot := filepath.Join(root, webRoutesDir)

	// A moved or renamed route tree must fail loudly. Without this the walk
	// yields an empty slice and the count assertion reports "0 boundaries",
	// which reads as a deleted boundary rather than as a bad walk root.
	require.DirExists(t, walkRoot,
		"the SvelteKit route tree is the walk root; if it moved, repoint webRoutesDir")

	var found []string
	err := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != errorBoundaryFileName {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err, "walk %s", webRoutesDir)
	sort.Strings(found)

	require.Len(t, found, 1,
		"expected exactly one SvelteKit error boundary under %s, found %d: %v — "+
			"a second boundary re-partitions which component a miss resolves to, "+
			"and that partition is readable from outside",
		webRoutesDir, len(found), found)

	require.Equal(t, webRoutesDir+"/"+errorBoundaryFileName, found[0],
		"the single boundary must sit at the ROUTE ROOT; a boundary nested under a "+
			"route subtree covers only that subtree and leaves the rest to a different one")
}
