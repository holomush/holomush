// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package ulidmakeforbidden implements the lint rule that forbids
// ulid.Make() in production code. ulid.Make() uses math/rand
// internally, violating the project-wide crypto/rand requirement.
// Use idgen.New() for entity IDs or core.NewULID() for event IDs.
//
// Test-file scope: this analyzer is excluded for `_test.go` via
// .golangci.yaml exclusions.rules; tests legitimately use ulid.Make()
// for fixture generation.
package ulidmakeforbidden

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

const (
	pkgPath  = "github.com/oklog/ulid/v2"
	funcName = "Make"
	message  = "use idgen.New() for entity IDs or core.NewULID() for event IDs; ulid.Make() uses math/rand"
)

var Analyzer = &analysis.Analyzer{
	Name:     "ulidmakeforbidden",
	Doc:      "forbids ulid.Make() (uses math/rand instead of crypto/rand)",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if holomushlint.IsCallToFQSym(pass, call, pkgPath, funcName) {
			pass.Reportf(call.Pos(), "%s", message)
		}
	})
	return nil, nil
}
