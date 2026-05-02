// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package holomushlint provides shared helpers for the HoloMUSH lint
// analyzers in gorules/analyzers/. Helpers in this package MUST NOT
// be exported beyond the gorules module; the internal/ position
// enforces that.
package holomushlint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// IsCallToFQSym reports whether call resolves to the package-level
// function (or constructor) named funcName in package pkgPath.
//
// Use this for free functions like fmt.Sprintf, json.Marshal,
// proto.Marshal, ulid.Make, log.Printf. For methods (where the
// receiver is significant), use IsCallToMethod.
//
// Handles both qualified calls (`fmt.Sprintf(...)` — *ast.SelectorExpr)
// and dot-imported calls (`Sprintf(...)` after `import . "fmt"` — bare
// *ast.Ident).
func IsCallToFQSym(pass *analysis.Pass, call *ast.CallExpr, pkgPath, funcName string) bool {
	obj := callee(pass, call)
	if obj == nil {
		return false
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	if fn.Pkg() == nil {
		return false
	}
	if fn.Pkg().Path() != pkgPath {
		return false
	}
	if fn.Name() != funcName {
		return false
	}
	// Free function has no receiver; method has one.
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	return sig.Recv() == nil
}

// callee resolves call.Fun to its referenced object via
// pass.TypesInfo.Uses, accepting either a SelectorExpr (qualified
// call: `pkg.Func()` / `recv.Method()`) or a bare Ident (dot-imported
// call: `Func()` after `import . "pkg"`). Returns nil if call.Fun is
// neither shape or has no resolved object.
func callee(pass *analysis.Pass, call *ast.CallExpr) types.Object {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return pass.TypesInfo.Uses[fun.Sel]
	case *ast.Ident:
		return pass.TypesInfo.Uses[fun]
	default:
		return nil
	}
}

// IsCallToMethod reports whether call resolves to a method named
// methodName on the named type recvTypeName in package recvPkgPath.
// The receiver may be either value or pointer type at the call site;
// IsCallToMethod normalizes via the named-type identity.
//
// Example: IsCallToMethod(pass, call, "encoding/json", "Encoder", "Encode")
// matches both `enc.Encode(v)` where enc is *json.Encoder and
// `(&json.Encoder{}).Encode(v)`. Also matches dot-imported method
// calls where the call expression is a bare *ast.Ident.
func IsCallToMethod(pass *analysis.Pass, call *ast.CallExpr, recvPkgPath, recvTypeName, methodName string) bool {
	obj := callee(pass, call)
	if obj == nil {
		return false
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	if fn.Name() != methodName {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	recv := sig.Recv()
	if recv == nil {
		return false
	}
	recvType := recv.Type()
	if ptr, ok := recvType.(*types.Pointer); ok {
		recvType = ptr.Elem()
	}
	named, ok := recvType.(*types.Named)
	if !ok {
		return false
	}
	if named.Obj().Pkg() == nil {
		return false
	}
	if named.Obj().Pkg().Path() != recvPkgPath {
		return false
	}
	if named.Obj().Name() != recvTypeName {
		return false
	}
	return true
}
