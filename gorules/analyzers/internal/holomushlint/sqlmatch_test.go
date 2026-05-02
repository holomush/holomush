// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint_test

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

func newPass(info *types.Info) *analysis.Pass {
	return &analysis.Pass{
		TypesInfo: info,
		Fset:      token.NewFileSet(),
	}
}

func TestExtractStringConstFromBasicLit(t *testing.T) {
	lit := &ast.BasicLit{Kind: token.STRING, Value: `"hello"`}
	pass := newPass(&types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
	})
	got, ok := holomushlint.ExtractStringConst(pass, lit)
	if !ok || got != "hello" {
		t.Fatalf(`got (%q, %v); want ("hello", true)`, got, ok)
	}
}

func TestExtractStringConstFromRawStringLit(t *testing.T) {
	lit := &ast.BasicLit{Kind: token.STRING, Value: "`raw\nstring`"}
	pass := newPass(&types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
	got, ok := holomushlint.ExtractStringConst(pass, lit)
	if !ok || got != "raw\nstring" {
		t.Fatalf("got (%q, %v)", got, ok)
	}
}

func TestExtractStringConstFromConcatChain(t *testing.T) {
	left := &ast.BasicLit{Kind: token.STRING, Value: `"UPDATE "`}
	mid := &ast.BasicLit{Kind: token.STRING, Value: `"scene_ops_events"`}
	right := &ast.BasicLit{Kind: token.STRING, Value: `" SET x = 1"`}
	expr := &ast.BinaryExpr{
		Op: token.ADD,
		X:  &ast.BinaryExpr{Op: token.ADD, X: left, Y: mid},
		Y:  right,
	}
	pass := newPass(&types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
	got, ok := holomushlint.ExtractStringConst(pass, expr)
	if !ok || got != "UPDATE scene_ops_events SET x = 1" {
		t.Fatalf("got (%q, %v)", got, ok)
	}
}

func TestExtractStringConstFromNamedConst(t *testing.T) {
	// Synthetic ident with a manually-attached constant value.
	ident := &ast.Ident{Name: "sql"}
	pass := newPass(&types.Info{
		Types: map[ast.Expr]types.TypeAndValue{
			ident: {Value: constant.MakeString("DELETE FROM scene_ops_events")},
		},
	})
	got, ok := holomushlint.ExtractStringConst(pass, ident)
	if !ok || got != "DELETE FROM scene_ops_events" {
		t.Fatalf("got (%q, %v)", got, ok)
	}
}

func TestExtractStringConstReturnsFalseForNonStringExpr(t *testing.T) {
	ident := &ast.Ident{Name: "x"}
	pass := newPass(&types.Info{
		Types: map[ast.Expr]types.TypeAndValue{
			ident: {Value: constant.MakeInt64(42)},
		},
	})
	if got, ok := holomushlint.ExtractStringConst(pass, ident); ok {
		t.Fatalf("expected false; got %q", got)
	}
}

func TestExtractStringConstReturnsFalseForUnknownExprKind(t *testing.T) {
	// CallExpr is not a supported shape.
	call := &ast.CallExpr{Fun: &ast.Ident{Name: "f"}}
	pass := newPass(&types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
	if _, ok := holomushlint.ExtractStringConst(pass, call); ok {
		t.Fatal("expected false for CallExpr")
	}
}

// keep the imports above honest in case go vet runs in isolation
var _ = types.NewPackage
