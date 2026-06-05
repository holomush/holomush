// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dekmaterialnolog forbids passing dek.Material (or
// *dek.Material) to standard log package sinks (Print, Printf, Println,
// Fatal, Fatalf, Fatalln, Panic, Panicf, Panicln; both package-level
// functions and *log.Logger methods). Part of INV-CRYPTO-16 (Material
// non-leakage). See bead holomush-46ya for context.
package dekmaterialnolog

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const sinkDescription = "log"

var sinks = []holomushlint.Sink{
	// free functions
	{PkgPath: "log", FuncName: "Print"},
	{PkgPath: "log", FuncName: "Printf"},
	{PkgPath: "log", FuncName: "Println"},
	{PkgPath: "log", FuncName: "Fatal"},
	{PkgPath: "log", FuncName: "Fatalf"},
	{PkgPath: "log", FuncName: "Fatalln"},
	{PkgPath: "log", FuncName: "Panic"},
	{PkgPath: "log", FuncName: "Panicf"},
	{PkgPath: "log", FuncName: "Panicln"},
	// *log.Logger methods
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Print"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Printf"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Println"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Fatal"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Fatalf"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Fatalln"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Panic"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Panicf"},
	{PkgPath: "log", RecvTypeName: "Logger", MethodName: "Panicln"},
}

var Analyzer = &analysis.Analyzer{
	Name:     "dekmaterialnolog",
	Doc:      "INV-CRYPTO-16: dek.Material MUST NOT be passed to log",
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
