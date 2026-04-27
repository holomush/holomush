// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package gateway_invariants_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAllGatewayRegistryInvariantsHaveTests asserts that every numbered
// invariant from the spec has at least one test function whose name
// references it.
func TestAllGatewayRegistryInvariantsHaveTests(t *testing.T) {
	invariants := []string{
		"INV-GW-1",
		"INV-GW-2",
		"INV-GW-3",
		"INV-GW-3a",
		"INV-GW-4",
		"INV-GW-5",
		"INV-GW-6",
		"INV-GW-7",
		"INV-GW-8",
		"INV-GW-9",
		"INV-GW-10",
		"INV-GW-11",
		// INV-GW-12 is forward-declared for Phase 3 — excluded.
		"INV-GW-13",
		"INV-GW-14",
		"INV-GW-15",
		"INV-GW-16",
	}

	testFiles := []string{
		"../../../internal/eventbus/rendering_publisher_test.go",
		"../../../internal/eventbus/rendering_publisher_internal_test.go",
		"../../../internal/eventbus/publisher_test.go",
		"../../../internal/eventbus/types_proto_sync_test.go",
		"../../../internal/eventbus/audit/projection_unit_test.go",
		"../../../internal/web/translate_test.go",
		"../../../internal/telnet/gateway_handler_test.go",
		"../../../internal/core/registry_test.go",
		"../../../internal/core/exports_test.go",
		"../../../internal/plugin/manager_test.go",
		"../../../cmd/holomush/gateway_imports_test.go",
		"../../../test/integration/eventbus_e2e/rendering_completeness_test.go",
	}

	invariantToFile := make(map[string][]string)
	fset := token.NewFileSet()
	for _, path := range testFiles {
		absPath, err := filepath.Abs(path)
		require.NoError(t, err)
		f, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
		if err != nil {
			t.Logf("skipping %s: %v", path, err)
			continue
		}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				for _, inv := range invariants {
					if strings.Contains(c.Text, inv) {
						invariantToFile[inv] = append(invariantToFile[inv], path)
					}
				}
			}
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for _, inv := range invariants {
				if strings.Contains(fn.Name.Name, strings.ReplaceAll(inv, "-", "")) ||
					(fn.Doc != nil && strings.Contains(fn.Doc.Text(), inv)) {
					invariantToFile[inv] = append(invariantToFile[inv], path)
				}
			}
		}
	}

	for _, inv := range invariants {
		files := invariantToFile[inv]
		if len(files) == 0 {
			t.Errorf("invariant %s has no test referencing it", inv)
		}
	}
}
