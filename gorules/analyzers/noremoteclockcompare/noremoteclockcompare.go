// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package noremoteclockcompare implements the lint rule that enforces
// INV-58 (Phase 3c grounding doc Decision 8): no cross-host wall-clock
// comparisons. The rule flags any subtraction or comparison between a
// time.Time value and a struct-field selector whose name is on the
// remote-sourced allowlist (PublishedAt, IssuedAt, StartedAt,
// LastHeartbeatAt). Remote-sourced timestamps MUST NOT feed protocol
// decisions; use sequence numbers instead.
//
// Detected forms:
//
//	time.Since(x.PublishedAt)
//	now.Sub(x.IssuedAt)
//	x.StartedAt.Sub(now)
//	now.Before(x.LastHeartbeatAt)
//	x.PublishedAt.After(now)
//
// The rule is syntactic: it matches by field name, not by knowing
// whether the field is genuinely sender-sourced. A field that happens
// to share a name on the allowlist but represents a local observation
// (e.g., cluster.Member.LastHeartbeatAt is the receiver's clock) MUST
// annotate the comparison with `//nolint:noremoteclockcompare //
// observability-only per Decision 8` (or a similar justification).
//
// Carved-out call sites:
//
//   - internal/cluster/heartbeat.go::recordSkew skew metric
//   - internal/eventbus/crypto/invalidation/coordinator.go latency histogram
//
// Both compute observability-only metrics with no protocol consequence.
package noremoteclockcompare

import (
	"go/ast"

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
	"LastHeartbeatAt": {},
}

// timeMethodAllowlist enumerates time.Time methods whose invocation
// constitutes a "comparison or subtraction" against the receiver. These
// are the verbs Decision 8 forbids when one operand is remote-sourced.
var timeMethodAllowlist = map[string]struct{}{
	"Sub":    {},
	"Before": {},
	"After":  {},
}

const message = "INV-58: no cross-host wall-clock comparison; remote-sourced timestamps must not feed protocol decisions. Use sequence numbers (Phase 3c grounding doc Decision 8). Annotate with //nolint:noremoteclockcompare with justification if this is an observability-only carve-out."

// Analyzer is the registered analyzer instance. Wired into golangci-lint
// via gorules/analyzers/noremoteclockcompare/plugin.go.
var Analyzer = &analysis.Analyzer{
	Name:     "noremoteclockcompare",
	Doc:      "INV-58: forbids wall-clock comparisons against remote-sourced struct fields (PublishedAt, IssuedAt, StartedAt, LastHeartbeatAt)",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		check(pass, call)
	})
	return nil, nil
}

// check inspects a call expression for the two flagged shapes:
//
//  1. `time.Since(<remote-selector>)` — package-qualified call.
//  2. `<recv>.<Method>(<arg>)` where Method is in timeMethodAllowlist
//     and either the receiver or any argument is a remote-selector.
//
// Reports at the position of the offending selector to give callers
// precise localisation.
func check(pass *analysis.Pass, call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	// Form 1: time.Since(...).
	if isTimeSince(sel) {
		for _, arg := range call.Args {
			if remoteFieldOf(arg) != "" {
				pass.Reportf(arg.Pos(), "%s", message)
				return
			}
		}
		return
	}

	// Form 2: <recv>.Sub|Before|After(<arg>).
	if _, ok := timeMethodAllowlist[sel.Sel.Name]; !ok {
		return
	}

	// Receiver side.
	if remoteFieldOf(sel.X) != "" {
		pass.Reportf(sel.X.Pos(), "%s", message)
		return
	}

	// Argument side.
	for _, arg := range call.Args {
		if remoteFieldOf(arg) != "" {
			pass.Reportf(arg.Pos(), "%s", message)
			return
		}
	}
}

// isTimeSince reports whether sel resolves to the package-level function
// time.Since. The check is type-aware via pass.TypesInfo would be ideal,
// but for a syntactic heuristic the package qualifier `time` is
// sufficient — gorules/analyzers/internal/holomushlint provides
// IsCallToFQSym for the precise variant; this analyzer keeps the check
// lightweight because the field-name allowlist already narrows scope.
func isTimeSince(sel *ast.SelectorExpr) bool {
	if sel.Sel.Name != "Since" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "time"
}

// remoteFieldOf returns the allowlist field name if expr is a selector
// whose Sel.Name is on the remoteFieldAllowlist, or "" otherwise.
func remoteFieldOf(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if _, ok := remoteFieldAllowlist[sel.Sel.Name]; !ok {
		return ""
	}
	return sel.Sel.Name
}
