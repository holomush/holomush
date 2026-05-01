// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package codeckeybytesallowlist forbids reading codec.Key.Bytes
// outside the codec/ and crypto/ package trees. Reads in composite
// literals, field assignments, and other write contexts are NOT
// flagged — the rule targets read-side leakage only.
//
// Implementation: walks *ast.SelectorExpr nodes whose resolved
// Selection.Obj() equals codec.Key's Bytes field. Composite-literal
// keys (e.g., codec.Key{Bytes: x}) are *ast.Ident, not *ast.SelectorExpr,
// so they are not visited by this walker. Field assignments
// (k.Bytes = x) ARE *ast.SelectorExpr; we filter them out by checking
// whether the parent node is an *ast.AssignStmt with the SelectorExpr
// in the Lhs.
package codeckeybytesallowlist

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const (
	codecPkg = "github.com/holomush/holomush/internal/eventbus/codec"
	keyType  = "Key"
	field    = "Bytes"
	message  = "INV-27 (residual defense): codec.Key.Bytes reads are restricted to internal/eventbus/codec/... and internal/eventbus/crypto/... — Material exposure goes through dek.Material.AsCodecKey only"
)

var allowlist = []string{
	"github.com/holomush/holomush/internal/eventbus/codec",
	"github.com/holomush/holomush/internal/eventbus/crypto",
}

// Analyzer forbids reads of codec.Key.Bytes outside the codec/ and
// crypto/ package trees.
var Analyzer = &analysis.Analyzer{
	Name:     "codeckeybytesallowlist",
	Doc:      "INV-27 residual defense: codec.Key.Bytes reads are restricted",
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
	insp.WithStack([]ast.Node{(*ast.SelectorExpr)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
		if !push {
			return false
		}
		sel := n.(*ast.SelectorExpr)
		if sel.Sel.Name != field {
			return true
		}
		selection, ok := pass.TypesInfo.Selections[sel]
		if !ok {
			return true
		}
		if selection.Kind() != types.FieldVal {
			return true
		}
		recv := selection.Recv()
		if ptr, ok := recv.(*types.Pointer); ok {
			recv = ptr.Elem()
		}
		named, ok := recv.(*types.Named)
		if !ok {
			return true
		}
		if named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != codecPkg {
			return true
		}
		if named.Obj().Name() != keyType {
			return true
		}
		// Skip writes: SelectorExpr appears as the LHS of an AssignStmt.
		if isWriteContext(stack) {
			return true
		}
		pass.Reportf(sel.Pos(), "%s", message)
		return true
	})
	return nil, nil
}

// isWriteContext returns true when the SelectorExpr at the top of the
// stack is on the LHS of an assignment.
func isWriteContext(stack []ast.Node) bool {
	if len(stack) < 2 {
		return false
	}
	parent := stack[len(stack)-2]
	assign, ok := parent.(*ast.AssignStmt)
	if !ok {
		return false
	}
	target := stack[len(stack)-1]
	for _, lhs := range assign.Lhs {
		if lhs == target {
			return true
		}
	}
	return false
}
