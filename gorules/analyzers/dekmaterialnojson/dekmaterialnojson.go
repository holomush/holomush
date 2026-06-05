// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dekmaterialnojson forbids passing dek.Material (or
// *dek.Material) to encoding/json sinks (json.Marshal,
// json.MarshalIndent, (*json.Encoder).Encode). Part of INV-CRYPTO-16 (Material
// non-leakage).
package dekmaterialnojson

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const sinkDescription = "encoding/json"

var sinks = []holomushlint.Sink{
	{PkgPath: "encoding/json", FuncName: "Marshal"},
	{PkgPath: "encoding/json", FuncName: "MarshalIndent"},
	{PkgPath: "encoding/json", RecvTypeName: "Encoder", MethodName: "Encode"},
}

var Analyzer = &analysis.Analyzer{
	Name:     "dekmaterialnojson",
	Doc:      "INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/json",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if !holomushlint.CallTargetsAnySink(pass, call, sinks) {
			return
		}
		for _, arg := range call.Args {
			if holomushlint.IsDEKMaterialArg(pass, arg) {
				pass.Reportf(arg.Pos(),
					"INV-CRYPTO-16: dek.Material MUST NOT be passed to %s — Material is opaque by design (see internal/eventbus/crypto/dek/material.go and bead holomush-46ya for context)",
					sinkDescription)
				return
			}
		}
	})
	return nil, nil
}
