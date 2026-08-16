// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is the home for shared helpers used across the test/meta package's
// structural meta-tests. Relocated from inv_binding_test.go (holomush-hz0v4.14.16)
// when that file — whose per-family coverage test had already been retired in
// favour of TestEveryRegistryInvariantHasBinding — was removed.

// skipDirs are directories that meta-tests MUST NOT descend into when scanning
// the repo tree. Keeping this in sync with project layout avoids false
// positives from vendored or generated trees.
var skipDirs = map[string]struct{}{
	".git":         {},
	".jj":          {},
	".worktrees":   {},
	"vendor":       {},
	"node_modules": {},
	"bin":          {},
	"build":        {},
	"dist":         {},
}

// findRepoRoot walks upward from the test's working directory until it finds
// a directory containing go.mod, which marks the repository root.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root (no go.mod found in any parent of %q)", dir)
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// Census helpers (plan 04-08 Task 1)
//
// The three helpers below are consumed by characteraccess_routing_census_test.go
// (the routing census) and character_rpc_census_test.go (the descriptor census).
// Their own contracts are pinned by the tests at the bottom of this file, so a
// helper that stops doing what its census assumes fails here rather than turning
// a census green for the wrong reason.
//
// They are ADDITIONS. world_envelope_census_test.go keeps its own single-purpose
// serviceReceiverName and bodyReferencesSelector, and neither shipped census
// calls anything declared here.
// ---------------------------------------------------------------------------

// facadeReceiverName returns the receiver variable name for a method whose
// receiver type — after unwrapping an optional pointer — is one of accepted.
//
// Consumed by the ROUTING CENSUS (characteraccess_routing_census_test.go) for
// both halves: the facade half passes a one-member set naming the character
// facade, the web half a one-member set naming the web handler. It generalizes
// world_envelope_census_test.go's serviceReceiverName, which hard-codes a single
// type name; that function is deliberately left alone.
//
// It reports not-ok for a receiver-less function, for a receiver type outside
// accepted, and for a receiver with no variable name — the last because there is
// then no name to hand to bodyReferencesSelector, so membership could not be
// derived. The routing census's UNIVERSE collector deliberately does NOT use this
// helper: an unnamed or value receiver must stay inside that universe so it fails
// classification loudly instead of vanishing.
func facadeReceiverName(fn *ast.FuncDecl, accepted map[string]struct{}) (string, bool) {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "", false
	}
	recv := fn.Recv.List[0]
	expr := recv.Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	if _, accept := accepted[ident.Name]; !accept {
		return "", false
	}
	if len(recv.Names) == 0 {
		return "", false
	}
	return recv.Names[0].Name, true
}

// bodyReferencesIdent reports whether body references name as a BARE identifier.
//
// Consumed by the ROUTING CENSUS's web half. bodyReferencesSelector
// (world_envelope_census_test.go) matches `<recv>.<field>`, which is exactly
// right for the facade half — the shared gate is reached by method promotion, so
// every call is a selector on the handler's own receiver — and useless for the
// web half, where a proxy names the session-token header as a package-level
// constant with no qualifier at all.
//
// A same-named FIELD SELECTOR on an unrelated expression is deliberately NOT a
// match: `x.headerInjectSessionToken` is a different thing from reading the
// package constant, and counting it would let a proxy satisfy the predicate
// without ever touching the header. The walk therefore descends into a selector's
// X and skips its Sel.
func bodyReferencesIdent(body *ast.BlockStmt, name string) bool {
	if body == nil {
		return false
	}
	found := false
	var walk func(n ast.Node) bool
	walk = func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		if sel, ok := n.(*ast.SelectorExpr); ok {
			// Descend into the qualified expression only. sel.Sel is a field or
			// method name, never a bare identifier reference.
			ast.Inspect(sel.X, walk)
			return false
		}
		if ident, ok := n.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return true
	}
	ast.Inspect(body, walk)
	return found
}

// setSymmetricDifference compares a derived set against an expected set and
// returns the two difference groups plus a rendered message naming both.
//
// Consumed by BOTH censuses. 01-SPEC §2.6 mandates a set-equality assertion
// producing a symmetric-difference diff on failure, so a RED census names which
// member is extra and which is missing. The shipped precedent
// (world_envelope_census_test.go:187-207) has set-equality SEMANTICS but renders
// per-element, one assertion per member; this renders the two groups together, so
// a reader sees the whole shape of the disagreement in one line.
//
// Both groups are sorted, so the rendered message is stable across map iteration
// order and two runs over the same sets are byte-identical.
func setSymmetricDifference(derived, expected map[string]struct{}) (extra, missing []string, message string) {
	extra = []string{}
	missing = []string{}
	for name := range derived {
		if _, ok := expected[name]; !ok {
			extra = append(extra, name)
		}
	}
	for name := range expected {
		if _, ok := derived[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	message = "extra (derived but not expected): [" + strings.Join(extra, " ") +
		"]; missing (expected but not derived): [" + strings.Join(missing, " ") + "]"
	return extra, missing, message
}

// parseMetaHelperFixture parses a Go source fixture and returns its declarations.
// The census helpers above are AST predicates, so their contracts are pinned
// against parsed fixtures rather than against live repository files — a fixture
// can carry the negative shapes (a same-named field selector, a receiver on the
// wrong type) that no production file is allowed to contain.
func parseMetaHelperFixture(t *testing.T, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, 0)
	require.NoError(t, err, "parse fixture")
	return file
}

// fixtureFuncDecl returns the named function or method declaration from a parsed
// fixture.
func fixtureFuncDecl(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("fixture declares no function named %q", name)
	return nil
}

const facadeReceiverFixture = `package fixture

func (s *Accepted) PointerReceiver() {}

func (s *Rejected) ReceiverTypeOutsideTheSet() {}

func ReceiverlessFunction() {}

func (*Accepted) ReceiverWithNoVariableName() {}
`

func TestFacadeReceiverNameAcceptsOnlyAcceptedNamedReceivers(t *testing.T) {
	file := parseMetaHelperFixture(t, facadeReceiverFixture)
	accepted := map[string]struct{}{"Accepted": {}}

	name, ok := facadeReceiverName(fixtureFuncDecl(t, file, "PointerReceiver"), accepted)
	assert.True(t, ok, "a pointer receiver whose type name is in the accepted set is accepted")
	assert.Equal(t, "s", name, "the receiver VARIABLE name is returned, so the caller can build a selector predicate")

	_, ok = facadeReceiverName(fixtureFuncDecl(t, file, "ReceiverTypeOutsideTheSet"), accepted)
	assert.False(t, ok, "a method on a type outside the accepted set is rejected")

	_, ok = facadeReceiverName(fixtureFuncDecl(t, file, "ReceiverlessFunction"), accepted)
	assert.False(t, ok, "a receiver-less function is rejected")

	_, ok = facadeReceiverName(fixtureFuncDecl(t, file, "ReceiverWithNoVariableName"), accepted)
	assert.False(t, ok, "a receiver with no variable name is rejected — there is no name to build a selector against")
}

const bodyReferencesIdentFixture = `package fixture

func UsesTheBareIdentifier() {
	read(headerInjectSessionToken)
}

func UsesASameNamedFieldSelector() {
	read(other.headerInjectSessionToken)
}
`

func TestBodyReferencesIdentMatchesABareIdentifierAndNotASameNamedFieldSelector(t *testing.T) {
	file := parseMetaHelperFixture(t, bodyReferencesIdentFixture)

	assert.True(t,
		bodyReferencesIdent(fixtureFuncDecl(t, file, "UsesTheBareIdentifier").Body, "headerInjectSessionToken"),
		"a bare identifier used as a call argument is a match")

	assert.False(t,
		bodyReferencesIdent(fixtureFuncDecl(t, file, "UsesASameNamedFieldSelector").Body, "headerInjectSessionToken"),
		"a same-named FIELD SELECTOR on an unrelated expression is NOT a match — reading some struct's field of that name is not reading the package constant")
}

func TestSetSymmetricDifferenceReportsBothDirectionsAndRendersStably(t *testing.T) {
	equal := map[string]struct{}{"a": {}, "b": {}}
	extra, missing, _ := setSymmetricDifference(equal, map[string]struct{}{"a": {}, "b": {}})
	assert.Empty(t, extra, "equal sets report no extra member")
	assert.Empty(t, missing, "equal sets report no missing member")

	derived := map[string]struct{}{"kept": {}, "unexpected": {}}
	expected := map[string]struct{}{"kept": {}, "stale": {}}
	extra, missing, message := setSymmetricDifference(derived, expected)
	assert.Equal(t, []string{"unexpected"}, extra, "a member the expected set never named is EXTRA")
	assert.Equal(t, []string{"stale"}, missing, "a member the derived set no longer carries is MISSING")
	assert.Contains(t, message, "extra", "the rendered message carries a distinct label for the extra group")
	assert.Contains(t, message, "missing", "the rendered message carries a distinct label for the missing group")
	assert.Contains(t, message, "unexpected")
	assert.Contains(t, message, "stale")

	// Stable ordering: the same sets built in a different insertion order render
	// byte-identically, so a RED census message does not churn between runs.
	reorderedDerived := map[string]struct{}{}
	for _, n := range []string{"unexpected", "kept"} {
		reorderedDerived[n] = struct{}{}
	}
	reorderedExpected := map[string]struct{}{}
	for _, n := range []string{"stale", "kept"} {
		reorderedExpected[n] = struct{}{}
	}
	_, _, reorderedMessage := setSymmetricDifference(reorderedDerived, reorderedExpected)
	assert.Equal(t, message, reorderedMessage, "each group is sorted before rendering, so insertion order cannot change the message")

	multi, _, multiMessage := setSymmetricDifference(
		map[string]struct{}{"zeta": {}, "alpha": {}, "mu": {}},
		map[string]struct{}{},
	)
	assert.Equal(t, []string{"alpha", "mu", "zeta"}, multi, "the extra group is sorted")
	assert.Equal(t,
		"extra (derived but not expected): [alpha mu zeta]; missing (expected but not derived): []",
		multiMessage,
		"the rendered shape is exact, so a census failure message is reviewable rather than incidental")
}
