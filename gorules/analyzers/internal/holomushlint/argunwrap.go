// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// IsDEKMaterialArg reports whether expr is, or unwraps to, an
// expression whose type is dek.Material or *dek.Material. Unwraps
// type-conversion CallExprs (e.g. `any(m)`, `MyAlias(m)`),
// ParenExprs, and TypeAssertExprs so bypasses like
//
//	json.Marshal(any(material))
//	json.Marshal((any)(material))
//	slog.Info("k", "v", any(material))
//
// are still caught — without unwrapping, pass.TypesInfo.TypeOf(arg)
// would see the conversion's outer type (e.g. `any`) and miss the
// inner `*dek.Material`.
//
// Returns true on the first level of the chain whose type matches
// dek.Material; otherwise walks down to the next layer.
func IsDEKMaterialArg(pass *analysis.Pass, expr ast.Expr) bool {
	for expr != nil {
		if IsDEKMaterial(pass.TypesInfo.TypeOf(expr)) {
			return true
		}
		switch e := expr.(type) {
		case *ast.ParenExpr:
			expr = e.X
		case *ast.TypeAssertExpr:
			expr = e.X
		case *ast.CallExpr:
			// Type-conversion CallExpr: Fun is a type, not a function.
			// Covers any(m), MyAlias(m), (T)(m).
			if isTypeConversion(pass, e) && len(e.Args) == 1 {
				expr = e.Args[0]
				continue
			}
			return false
		default:
			return false
		}
	}
	return false
}

// isTypeConversion reports whether call is a type conversion expression
// (Fun resolves to a type via pass.TypesInfo.Types).
func isTypeConversion(pass *analysis.Pass, call *ast.CallExpr) bool {
	tv, ok := pass.TypesInfo.Types[call.Fun]
	if !ok {
		return false
	}
	return tv.IsType()
}
