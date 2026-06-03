// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestINV_SCENE_4_NoABACInGetPoseOrder pins INV-SCENE-4 / INV-S9 plugin-code-gate
// discipline: GetPoseOrder MUST NOT consult the ABAC engine. The participant
// gate is direct (IsParticipant store check), NOT via engine.Evaluate.
//
// This test uses go/parser to locate the GetPoseOrder method on
// *SceneServiceImpl and rg-asserts no engine.Evaluate / engine.CanPerformAction
// / engine.Allow / engine.Forbid call appears in the function body.
//
// T20 (scene RPC handler) closed at jj tto b3d with zero such calls;
// this test pins the invariant for CI to catch future regressions.
func TestINV_SCENE_4_NoABACInGetPoseOrder(t *testing.T) {
	t.Parallel()
	const sourcePath = "../../../plugins/core-scenes/service.go"

	src, err := os.ReadFile(sourcePath)
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, sourcePath, src, parser.AllErrors)
	require.NoError(t, err)

	// Find the GetPoseOrder method on *SceneServiceImpl.
	var getPoseOrderDecl *ast.FuncDecl
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "GetPoseOrder" {
			return true
		}
		// Verify receiver is *SceneServiceImpl.
		if fn.Recv == nil || len(fn.Recv.List) != 1 {
			return true
		}
		// Receiver type: *SceneServiceImpl.
		if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
			if ident, ok := star.X.(*ast.Ident); ok && ident.Name == "SceneServiceImpl" {
				getPoseOrderDecl = fn
				return false
			}
		}
		return true
	})
	require.NotNil(t, getPoseOrderDecl, "GetPoseOrder method not found on *SceneServiceImpl")
	require.NotNil(t, getPoseOrderDecl.Body, "GetPoseOrder has no body")

	// Extract body source bytes.
	bodyStart := fset.Position(getPoseOrderDecl.Body.Lbrace).Offset
	bodyEnd := fset.Position(getPoseOrderDecl.Body.Rbrace).Offset
	require.True(t, bodyEnd > bodyStart, "GetPoseOrder body bounds invalid")
	bodyText := string(src[bodyStart : bodyEnd+1])

	// Forbidden patterns: any direct call into the ABAC engine.
	forbidden := regexp.MustCompile(`\bengine\.(Evaluate|CanPerformAction|Allow|Forbid)\b`)

	// Also catch indirect call patterns (any *.Evaluate on something named
	// engine-ish: AccessPolicyEngine.Evaluate, accessEngine.Evaluate, etc.).
	// Be conservative: warn on common patterns but the strict gate is the
	// canonical engine.X pattern.
	indirectForbidden := regexp.MustCompile(`\b(accessEngine|abacEngine|policyEngine)\.(Evaluate|CanPerformAction|Allow|Forbid)\b`)

	if matches := forbidden.FindAllString(bodyText, -1); len(matches) > 0 {
		uniqueMatches := dedupeStrings(matches)
		t.Errorf("INV-SCENE-4 violated: GetPoseOrder body contains forbidden ABAC engine call(s): %s\n\nINV-S9 plugin-code gate forbids ABAC engine consultation. Use IsParticipant store check instead.",
			strings.Join(uniqueMatches, ", "))
	}

	if matches := indirectForbidden.FindAllString(bodyText, -1); len(matches) > 0 {
		uniqueMatches := dedupeStrings(matches)
		t.Errorf("INV-SCENE-4 violated: GetPoseOrder body contains indirect ABAC engine call(s): %s",
			strings.Join(uniqueMatches, ", "))
	}
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
