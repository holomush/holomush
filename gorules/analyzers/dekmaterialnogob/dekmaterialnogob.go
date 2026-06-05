// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dekmaterialnogob forbids passing dek.Material (or
// *dek.Material) to encoding/gob sinks ((*gob.Encoder).Encode,
// gob.Register). Part of INV-CRYPTO-16 (Material non-leakage).
package dekmaterialnogob

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const sinkDescription = "encoding/gob"

var sinks = []holomushlint.Sink{
	{PkgPath: "encoding/gob", RecvTypeName: "Encoder", MethodName: "Encode"},
	{PkgPath: "encoding/gob", FuncName: "Register"},
}

var Analyzer = &analysis.Analyzer{
	Name:     "dekmaterialnogob",
	Doc:      "INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/gob",
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
