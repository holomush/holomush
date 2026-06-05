// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dekmaterialnoslog forbids passing dek.Material (or
// *dek.Material) to standard log/slog package sinks (Info, Debug, Warn,
// Error, Log, Any, Group; both package-level functions and *slog.Logger
// methods, except Any and Group which are not *Logger methods). Part
// of INV-CRYPTO-16 (Material non-leakage). See bead holomush-46ya for context.
package dekmaterialnoslog

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const sinkDescription = "log/slog"

var sinks = []holomushlint.Sink{
	// free functions (use the package-level default logger), alphabetized
	{PkgPath: "log/slog", FuncName: "Any"},
	{PkgPath: "log/slog", FuncName: "Debug"},
	{PkgPath: "log/slog", FuncName: "DebugContext"},
	{PkgPath: "log/slog", FuncName: "Error"},
	{PkgPath: "log/slog", FuncName: "ErrorContext"},
	{PkgPath: "log/slog", FuncName: "Group"},
	{PkgPath: "log/slog", FuncName: "Info"},
	{PkgPath: "log/slog", FuncName: "InfoContext"},
	{PkgPath: "log/slog", FuncName: "Log"},
	{PkgPath: "log/slog", FuncName: "LogAttrs"},
	{PkgPath: "log/slog", FuncName: "Warn"},
	{PkgPath: "log/slog", FuncName: "WarnContext"},
	{PkgPath: "log/slog", FuncName: "With"},
	// *slog.Logger methods (mirror set, but Any+Group are package-level
	// helpers, not *Logger methods), alphabetized
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Debug"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "DebugContext"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Error"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "ErrorContext"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Info"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "InfoContext"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Log"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "LogAttrs"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "Warn"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "WarnContext"},
	{PkgPath: "log/slog", RecvTypeName: "Logger", MethodName: "With"},
}

var Analyzer = &analysis.Analyzer{
	Name:     "dekmaterialnoslog",
	Doc:      "INV-CRYPTO-16: dek.Material MUST NOT be passed to log/slog",
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
