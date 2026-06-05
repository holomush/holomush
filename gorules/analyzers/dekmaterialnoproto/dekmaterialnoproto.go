// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dekmaterialnoproto forbids passing dek.Material (or
// *dek.Material) to google.golang.org/protobuf/proto sinks
// (proto.Marshal, (MarshalOptions).Marshal). Part of INV-CRYPTO-16 (Material
// non-leakage).
//
// Forward-defensive: dek.Material does not implement proto.Message
// today, so the rule cannot fire against current code (call sites
// would not type-check). The analyzer guards against accidental future
// satisfaction of the proto.Message interface — e.g., if Material ever
// gains Reset()/String()/ProtoMessage() methods, or if a generated
// stub aliases it. See bead holomush-46ya for context.
package dekmaterialnoproto

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const sinkDescription = "google.golang.org/protobuf/proto"

var sinks = []holomushlint.Sink{
	{PkgPath: "google.golang.org/protobuf/proto", FuncName: "Marshal"},
	{PkgPath: "google.golang.org/protobuf/proto", RecvTypeName: "MarshalOptions", MethodName: "Marshal"},
}

var Analyzer = &analysis.Analyzer{
	Name:     "dekmaterialnoproto",
	Doc:      "INV-CRYPTO-16: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto",
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
