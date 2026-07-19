// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// transitiveInternalClosure loads pkgPattern and walks its transitive import
// graph (packages.NeedDeps), returning the set of every
// github.com/holomush/holomush/internal/... package reached, directly or
// transitively, from any of the packages pkgPattern expands to.
//
// The packages.Config below deliberately leaves the Tests option at its zero
// value (false). A package's *test* import closure legitimately differs from
// its build closure, and the direct-import gate
// (TestGatewayImportsAreOnlyProtocolTranslation in gateway_imports_test.go,
// which DOES opt into loading test files) already covers gateway-side test
// files.
func transitiveInternalClosure(t *testing.T, pkgPattern string) map[string]struct{} {
	t.Helper()

	pkgs, err := packages.Load(
		&packages.Config{
			Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps,
		},
		pkgPattern,
	)
	require.NoError(t, err)
	require.Empty(t, packages.PrintErrors(pkgs))

	const internalPrefix = "github.com/holomush/holomush/internal/"
	closure := make(map[string]struct{})
	visited := make(map[string]struct{})

	var walk func(pkg *packages.Package)
	walk = func(pkg *packages.Package) {
		if _, ok := visited[pkg.PkgPath]; ok {
			return
		}
		visited[pkg.PkgPath] = struct{}{}
		if strings.HasPrefix(pkg.PkgPath, internalPrefix) {
			closure[pkg.PkgPath] = struct{}{}
		}
		for _, imp := range pkg.Imports {
			walk(imp)
		}
	}

	for _, pkg := range pkgs {
		walk(pkg)
	}
	return closure
}

// closureContainsPackage reports whether closure contains bad, either as an
// exact PkgPath match or as a sub-package of bad. Mirrors checkFile's match
// semantics (gateway_imports_test.go) so the direct-import gate and this
// closure gate agree on what "in" means.
func closureContainsPackage(closure map[string]struct{}, bad string) bool {
	for pkgPath := range closure {
		if pkgPath == bad || strings.HasPrefix(pkgPath, bad+"/") {
			return true
		}
	}
	return false
}

// TestGatewayTransitiveClosureExcludesDomainPackages is INV-EVENTBUS-1's
// transitive half. TestGatewayImportsAreOnlyProtocolTranslation
// (gateway_imports_test.go) proves gateway files hold no DIRECT import of a
// forbidden package; it cannot see a forbidden package reached through an
// innocuous-looking intermediate. This test computes the full transitive
// closure of each gateway tree and asserts it contains none of
// gatewayForbiddenPackages.
//
// Verifies: INV-EVENTBUS-1
func TestGatewayTransitiveClosureExcludesDomainPackages(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
	}{
		{"telnet", "github.com/holomush/holomush/internal/telnet/..."},
		{"web", "github.com/holomush/holomush/internal/web/..."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			closure := transitiveInternalClosure(t, tc.pattern)
			for _, bad := range gatewayForbiddenPackages {
				if closureContainsPackage(closure, bad) {
					t.Errorf("%s's transitive closure reaches forbidden package %s", tc.pattern, bad)
				}
			}
		})
	}
}

// TestClosureOracleDetectsStoreInsideInternalGrpc is a POSITIVE CONTROL.
// TestGatewayTransitiveClosureExcludesDomainPackages passes trivially (a false
// green) if transitiveInternalClosure's walk is vacuous — e.g. if it always
// returns an empty set. This test proves the oracle is not vacuous by
// asserting it detects a REAL, currently-true violation: internal/grpc's
// transitive closure reaches internal/store (verified live:
// `go list -deps ./internal/grpc | rg -c 'holomush/internal/store$'` is
// non-zero). internal/grpc is not itself gateway code — it's a stand-in
// package whose closure is known to contain a forbidden package, used only to
// exercise the walk.
//
// If this test ever goes red because internal/grpc legitimately stops
// reaching internal/store, do NOT delete it and do NOT weaken it to a
// tautology — REPLACE the control with another package pair whose closure
// genuinely contains a forbidden package (find one with
// `go list -deps <candidate-package>` against gatewayForbiddenPackages). A
// stale failure here is loud and safe; a silently vacated control is not.
//
// Verifies: INV-EVENTBUS-1
func TestClosureOracleDetectsStoreInsideInternalGrpc(t *testing.T) {
	closure := transitiveInternalClosure(t, "github.com/holomush/holomush/internal/grpc")
	const store = "github.com/holomush/holomush/internal/store"
	if !closureContainsPackage(closure, store) {
		t.Fatalf("expected internal/grpc's transitive closure to contain %s (positive control failed — "+
			"the closure oracle may be vacuous; see this test's doc comment before touching gatewayForbiddenPackages)", store)
	}
}

// TestClosureOracleWalksTransitivelyNotVacuously is the oracle self-check:
// it proves transitiveInternalClosure actually descends into the import
// graph rather than returning an empty or single-element set. internal/telnet
// imports internal/telnet/gamenotice directly (gateway_handler.go), so a
// correct walk of the single package "internal/telnet" (no "/..." expansion)
// must surface it.
//
// Verifies: INV-EVENTBUS-1
func TestClosureOracleWalksTransitivelyNotVacuously(t *testing.T) {
	closure := transitiveInternalClosure(t, "github.com/holomush/holomush/internal/telnet")
	require.NotEmpty(t, closure, "transitiveInternalClosure returned an empty set for internal/telnet — the walk is vacuous")

	const gamenotice = "github.com/holomush/holomush/internal/telnet/gamenotice"
	if _, ok := closure[gamenotice]; !ok {
		t.Fatalf("expected internal/telnet's closure to contain %s (oracle self-check failed — the walk did not descend)", gamenotice)
	}
}
