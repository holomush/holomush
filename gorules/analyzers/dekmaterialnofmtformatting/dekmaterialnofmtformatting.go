// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dekmaterialnofmtformatting forbids passing dek.Material (or
// *dek.Material) to fmt formatting sinks (Sprint, Sprintf, Sprintln,
// Print, Printf, Println, Fprint, Fprintf, Fprintln, Errorf). Part of
// INV-CRYPTO-16 (Material non-leakage). See bead holomush-46ya for context.
package dekmaterialnofmtformatting

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const sinkDescription = "fmt formatting"

var sinks = []holomushlint.Sink{
	{PkgPath: "fmt", FuncName: "Sprint"},
	{PkgPath: "fmt", FuncName: "Sprintf"},
	{PkgPath: "fmt", FuncName: "Sprintln"},
	{PkgPath: "fmt", FuncName: "Print"},
	{PkgPath: "fmt", FuncName: "Printf"},
	{PkgPath: "fmt", FuncName: "Println"},
	{PkgPath: "fmt", FuncName: "Fprint"},
	{PkgPath: "fmt", FuncName: "Fprintf"},
	{PkgPath: "fmt", FuncName: "Fprintln"},
	{PkgPath: "fmt", FuncName: "Errorf"},
}

var Analyzer = &analysis.Analyzer{
	Name:     "dekmaterialnofmtformatting",
	Doc:      "INV-CRYPTO-16: dek.Material MUST NOT be passed to fmt formatting",
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
