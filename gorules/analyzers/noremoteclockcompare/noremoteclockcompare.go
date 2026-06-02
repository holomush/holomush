// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package noremoteclockcompare implements the lint rule that enforces
// INV-CLUSTER-8 (Phase 3c grounding doc Decision 8): no cross-host wall-clock
// comparisons. The rule flags any subtraction or comparison between a
// time.Time value and a struct-field selector whose name is on the
// remote-sourced allowlist (PublishedAt, IssuedAt, StartedAt,
// LastPublishedAt). Remote-sourced timestamps MUST NOT feed protocol
// decisions; use sequence numbers instead.
//
// Detected forms:
//
//	time.Since(x.PublishedAt)
//	time.Until(x.PublishedAt)                // symmetric to Since
//	time.Since(x.PublishedAt.UTC())          // transformed operand
//	now.Sub(x.IssuedAt)
//	now.Sub(x.StartedAt.Add(skew))           // transformed operand
//	x.StartedAt.Sub(now)
//	now.Before(x.LastPublishedAt)
//	x.PublishedAt.After(now)
//	now.Compare(x.IssuedAt)                  // Go 1.20+ tri-state compare
//
// The rule combines a syntactic field-name allowlist with a type-aware
// guard via pass.TypesInfo: a selector is flagged only when its
// resolved type is time.Time. This rejects same-named fields on
// user-defined types (e.g., a custom Member with a PublishedAt int64)
// and same-named methods on user-defined types (e.g., a custom
// Ordered.After that is not time.Time.After).
//
// Carved-out call sites:
//
//   - internal/cluster/heartbeat.go::recordSkew skew metric
//   - internal/eventbus/crypto/invalidation/coordinator.go latency histogram
//
// Both compute observability-only metrics with no protocol consequence.
// Annotate carve-outs with `//nolint:noremoteclockcompare //
// observability-only per Decision 8` (or a similar justification).
package noremoteclockcompare

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// remoteFieldAllowlist enumerates struct field names whose values are
// presumed to be sender-sourced wall-clock timestamps. Comparisons or
// subtractions involving these selectors are flagged.
var remoteFieldAllowlist = map[string]struct{}{
	"PublishedAt":     {},
	"IssuedAt":        {},
	"StartedAt":       {},
	"LastPublishedAt": {},
}

// timeMethodAllowlist enumerates time.Time methods whose invocation
// constitutes a "comparison or subtraction" against the receiver. These
// are the verbs Decision 8 forbids when one operand is remote-sourced.
// Compare is the Go 1.20+ tri-state comparison (returns -1/0/+1) and
// is functionally equivalent to Before/After for protocol-decision
// purposes — also forbidden against remote operands.
var timeMethodAllowlist = map[string]struct{}{
	"Sub":     {},
	"Before":  {},
	"After":   {},
	"Compare": {},
}

// timePackageFuncAllowlist enumerates package-level time helpers that
// take a single time.Time argument and compare it against time.Now()
// internally. Both flag when their argument resolves to a remote-sourced
// time.Time selector.
//
//	time.Since(t)  ≡  time.Now().Sub(t)
//	time.Until(t)  ≡  t.Sub(time.Now())
var timePackageFuncAllowlist = map[string]struct{}{
	"Since": {},
	"Until": {},
}

const message = "INV-CLUSTER-8: no cross-host wall-clock comparison; remote-sourced timestamps must not feed protocol decisions. Use sequence numbers (Phase 3c grounding doc Decision 8). Annotate with //nolint:noremoteclockcompare with justification if this is an observability-only carve-out."

// Analyzer is the registered analyzer instance. Wired into golangci-lint
// via gorules/analyzers/noremoteclockcompare/plugin.go.
var Analyzer = &analysis.Analyzer{
	Name:     "noremoteclockcompare",
	Doc:      "INV-CLUSTER-8: forbids wall-clock comparisons against remote-sourced struct fields (PublishedAt, IssuedAt, StartedAt, LastPublishedAt) when the operand resolves to time.Time",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		check(pass, n.(*ast.CallExpr))
	})
	return nil, nil
}

// check inspects a call expression for the two flagged shapes:
//
//  1. `time.Since(<expr>)` where the package-level function resolves to
//     time.Since and any subtree of <expr> contains a remote-sourced
//     time.Time selector.
//  2. `<recv>.Sub|Before|After(<arg>)` where the method resolves to one
//     on time.Time AND either operand subtree contains a remote-sourced
//     time.Time selector.
//
// Reports at the position of the offending selector to give callers
// precise localisation.
func check(pass *analysis.Pass, call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	// Form 1: time.Since(...) or time.Until(...).
	if isTimePackageFunc(pass, sel) {
		for _, arg := range call.Args {
			if pos := findRemoteTimeSelector(pass, arg); pos.IsValid() {
				pass.Reportf(pos, "%s", message)
				return
			}
		}
		return
	}

	// Form 2: <recv>.Sub|Before|After(<arg>) on time.Time.
	if _, ok := timeMethodAllowlist[sel.Sel.Name]; !ok {
		return
	}
	if !isTimeMethod(pass, sel) {
		return
	}

	// Receiver side.
	if pos := findRemoteTimeSelector(pass, sel.X); pos.IsValid() {
		pass.Reportf(pos, "%s", message)
		return
	}
	// Argument side.
	for _, arg := range call.Args {
		if pos := findRemoteTimeSelector(pass, arg); pos.IsValid() {
			pass.Reportf(pos, "%s", message)
			return
		}
	}
}

// findRemoteTimeSelector walks expr looking for a SelectorExpr whose
// Sel.Name is on remoteFieldAllowlist AND whose resolved type is
// time.Time. Returns the matched selector's position or token.NoPos
// if none found.
//
// Descends through:
//   - *ast.ParenExpr — unwraps parenthesised expressions.
//   - *ast.CallExpr — descends into the method receiver to catch chains
//     like `s.PublishedAt.UTC()` or `s.StartedAt.Add(skew)` where a
//     time.Time-returning method hides the remote-sourced source.
//   - *ast.SelectorExpr — direct match check; on miss, recurses into
//     the receiver to catch deeper chains.
func findRemoteTimeSelector(pass *analysis.Pass, expr ast.Expr) token.Pos {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return findRemoteTimeSelector(pass, e.X)
	case *ast.CallExpr:
		if methodSel, ok := e.Fun.(*ast.SelectorExpr); ok {
			return findRemoteTimeSelector(pass, methodSel.X)
		}
		return token.NoPos
	case *ast.SelectorExpr:
		if _, allowed := remoteFieldAllowlist[e.Sel.Name]; allowed {
			if isTimeTimeType(pass, e) {
				return e.Sel.Pos()
			}
			return token.NoPos
		}
		// Non-allowlisted selector: recurse into the receiver in case
		// the chain hides a remote selector deeper (defensive).
		return findRemoteTimeSelector(pass, e.X)
	}
	return token.NoPos
}

// isTimeTimeType reports whether expr's type per pass.TypesInfo is
// the named type time.Time. Returns false if TypesInfo is unavailable
// or the type cannot be resolved.
func isTimeTimeType(pass *analysis.Pass, expr ast.Expr) bool {
	if pass.TypesInfo == nil {
		return false
	}
	t := pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	if named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "time" && named.Obj().Name() == "Time"
}

// isTimePackageFunc reports whether sel resolves to a package-level
// function in `time` whose name is on timePackageFuncAllowlist
// (currently Since and Until). Falls back to false when TypesInfo is
// unavailable, to avoid runaway false positives in degraded-analysis
// modes.
func isTimePackageFunc(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	if _, allowed := timePackageFuncAllowlist[sel.Sel.Name]; !allowed {
		return false
	}
	if pass.TypesInfo == nil {
		return false
	}
	obj := pass.TypesInfo.Uses[sel.Sel]
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
	return fn.Pkg().Path() == "time"
}

// isTimeMethod reports whether sel resolves to a method on time.Time
// (or *time.Time). Used to filter Form 2 calls so that user-defined
// types with same-named methods (e.g., a custom Ordered.After) are
// not flagged.
func isTimeMethod(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	if pass.TypesInfo == nil {
		return false
	}
	obj := pass.TypesInfo.Uses[sel.Sel]
	if obj == nil {
		return false
	}
	fn, ok := obj.(*types.Func)
	if !ok {
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
	return named.Obj().Pkg().Path() == "time" && named.Obj().Name() == "Time"
}
