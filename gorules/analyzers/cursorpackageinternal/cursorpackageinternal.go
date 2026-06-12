// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cursorpackageinternal implements the lint rule that forbids
// importing or referencing symbols from
// github.com/holomush/holomush/internal/eventbus/cursor outside the
// host's natural homes (eventbus, grpc, web, plugin/goplugin,
// plugin/hostfunc, plugin/hostcap).
//
// Implementation note: walks all *ast.Ident nodes that resolve via
// pass.TypesInfo.Uses to an object whose package is the cursor
// package, and emits one diagnostic per *ast.Ident that resolves to
// a cursor-package object (so a composite literal like
// `cursor.Owner{Kind: cursor.OwnerPlugin}` produces three diagnostics
// — one per ident). This is broader than the previous ruleguard
// rule's per-symbol enumeration (it covers any future cursor symbol
// automatically) but functionally equivalent for the existing
// exported surface.
package cursorpackageinternal

import (
	"go/ast"
	"strconv"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const (
	cursorPkg = "github.com/holomush/holomush/internal/eventbus/cursor"
	message   = "internal/eventbus/cursor is host-internal — clients and plugins must not import it"
)

var allowlist = []string{
	"github.com/holomush/holomush/internal/eventbus",
	"github.com/holomush/holomush/internal/grpc",
	"github.com/holomush/holomush/internal/web",
	"github.com/holomush/holomush/internal/plugin/goplugin",
	"github.com/holomush/holomush/internal/plugin/hostfunc",
	// hostcap holds the runtime-neutral host.v1 capability servers relocated
	// from goplugin (holomush-eykuh.2); QueryStreamHistory brokers cursors at
	// the plugin host-callback boundary, exactly as goplugin did.
	"github.com/holomush/holomush/internal/plugin/hostcap",
}

var Analyzer = &analysis.Analyzer{
	Name:     "cursorpackageinternal",
	Doc:      "forbids references to internal/eventbus/cursor outside host packages",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg == nil {
		return nil, nil
	}
	if holomushlint.PackagePathMatchesAny(pass.Pkg.Path(), allowlist) {
		return nil, nil
	}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	// Catch blank/side-effect imports (`import _ "…/cursor"`) that
	// would otherwise bypass the symbol-reference walker below — those
	// imports add no entries to pass.TypesInfo.Uses.
	insp.Preorder([]ast.Node{(*ast.ImportSpec)(nil)}, func(n ast.Node) {
		spec := n.(*ast.ImportSpec)
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return
		}
		if path == cursorPkg {
			pass.Reportf(spec.Path.Pos(), "%s", message)
		}
	})
	insp.Preorder([]ast.Node{(*ast.Ident)(nil)}, func(n ast.Node) {
		ident := n.(*ast.Ident)
		obj := pass.TypesInfo.Uses[ident]
		if obj == nil {
			return
		}
		if obj.Pkg() == nil {
			return
		}
		if obj.Pkg().Path() != cursorPkg {
			return
		}
		pass.Reportf(ident.Pos(), "%s", message)
	})
	return nil, nil
}
