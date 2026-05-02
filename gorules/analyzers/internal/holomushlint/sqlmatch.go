// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint

import (
	"go/ast"
	"go/constant"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// ExtractStringConst attempts to recover a string-constant value from
// expr. Three shapes are supported, matching the SceneOps SQL rule's
// design (spec §4.4.3):
//
//  1. *ast.BasicLit of kind STRING (raw or interpreted).
//  2. *ast.BinaryExpr chain of `+`-joined string literals (any depth).
//  3. *ast.Ident resolving to a constant.Value of kind String via
//     pass.TypesInfo.Types[expr].Value.
//
// Returns (s, true) on success, ("", false) otherwise. Callers handling
// the false case should not emit a diagnostic — failure to recover a
// string is not itself a violation, only an inability to inspect the
// SQL.
func ExtractStringConst(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	// Shape 3 first: types.Info covers idents, qualified idents
	// (other.Const), and constant-folded BasicLits all at once.
	if tv, ok := pass.TypesInfo.Types[expr]; ok {
		if tv.Value != nil && tv.Value.Kind() == constant.String {
			return constant.StringVal(tv.Value), true
		}
	}
	// Shapes 1 and 2 fall through if types.Info doesn't have a folded value
	// (the package may not have been type-checked, or expr may be nil-typed).
	return extractStringFromAST(expr)
}

func extractStringFromAST(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, ok := extractStringFromAST(e.X)
		if !ok {
			return "", false
		}
		r, ok := extractStringFromAST(e.Y)
		if !ok {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return extractStringFromAST(e.X)
	}
	return "", false
}
