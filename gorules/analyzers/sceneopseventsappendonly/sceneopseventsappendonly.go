// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package sceneopseventsappendonly implements the lint rule that
// forbids UPDATE/DELETE/TRUNCATE statements against the
// scene_ops_events table. The table is the append-only ops journal
// for the core-scenes plugin (Phase 3 design P3.D3, P3.D4).
//
// Targets: tx.Exec / tx.Query / tx.QueryRow calls (any receiver type;
// pgx receivers are plural). String-extraction supports literal,
// `+`-chain, and named-const shapes. Anything else (fmt.Sprintf,
// concat with a runtime variable, ...) is silently passed through.
package sceneopseventsappendonly

import (
	"go/ast"
	"regexp"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/holomush/holomush/gorules/analyzers/internal/holomushlint"
)

var forbiddenRegex = regexp.MustCompile(`(?i)(?:update\s+scene_ops_events|delete\s+from\s+scene_ops_events|truncate(?:\s+table)?\s+scene_ops_events)`)

const message = "scene_ops_events is append-only (Phase 3 design P3.D3/D4): use a new INSERT via recordOpsEventTx to record corrections instead of UPDATE/DELETE/TRUNCATE"

var methods = map[string]bool{"Exec": true, "Query": true, "QueryRow": true}

var Analyzer = &analysis.Analyzer{
	Name:     "sceneopseventsappendonly",
	Doc:      "forbids UPDATE/DELETE/TRUNCATE against scene_ops_events",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if !methods[sel.Sel.Name] {
			return
		}
		if len(call.Args) < 2 {
			return
		}
		// args[0] is ctx, args[1] is the SQL string.
		sql, ok := holomushlint.ExtractStringConst(pass, call.Args[1])
		if !ok {
			return
		}
		if forbiddenRegex.MatchString(sql) {
			pass.Reportf(call.Args[1].Pos(), "%s", message)
		}
	})
	return nil, nil
}
