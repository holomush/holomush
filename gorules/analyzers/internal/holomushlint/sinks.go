// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holomushlint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// Sink describes a forbidden function or method. Exactly one of
// (FuncName) or (RecvTypeName + MethodName) is populated:
//
//   - free function: PkgPath + FuncName, RecvTypeName = ""
//   - method on a type: PkgPath (where the type is defined) +
//     RecvTypeName + MethodName, FuncName = ""
type Sink struct {
	PkgPath      string // package containing the function or receiver type
	FuncName     string // free-function name (when RecvTypeName == "")
	RecvTypeName string // named-type receiver (when FuncName == "")
	MethodName   string // method name (when RecvTypeName != "")
}

// String returns a human-readable identifier for diagnostics.
func (s Sink) String() string {
	if s.RecvTypeName == "" {
		return s.PkgPath + "." + s.FuncName
	}
	return "(*" + s.PkgPath + "." + s.RecvTypeName + ")." + s.MethodName
}

// CallTargetsAnySink reports whether call's resolved callee matches any
// sink in the slice. Returns true on the first match.
func CallTargetsAnySink(pass *analysis.Pass, call *ast.CallExpr, sinks []Sink) bool {
	for _, s := range sinks {
		if s.RecvTypeName == "" {
			if IsCallToFQSym(pass, call, s.PkgPath, s.FuncName) {
				return true
			}
		} else {
			if IsCallToMethod(pass, call, s.PkgPath, s.RecvTypeName, s.MethodName) {
				return true
			}
		}
	}
	return false
}
