// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoreWiresOneBlockListSubsystemIntoBothCompositionRoots pins the "one
// subsystem, one cache, both roots" property of the block-list transport
// (IDENT-07, 02-05) over core.go's ACTUAL wiring, by parsing it.
//
// A unit test cannot observe this: runCore needs a live database, and both
// config structs keep their fields private to the subsystems they build. A
// test that constructed the two configs itself would assert its own literals,
// not production wiring. So the assertion is structural, in the same spirit as
// 02-02's go/parser separation guards: read the composite literals core.go
// actually writes and require that every BlockList field is fed by the SAME
// identifier, and that identifier is constructed exactly once.
//
// Two constructions would give the gRPC root and the bootstrap root
// independently-polled lists that can disagree about which names are blocked —
// precisely the multi-root drift plan 02-06 exists to close.
func TestCoreWiresOneBlockListSubsystemIntoBothCompositionRoots(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "core.go", nil, parser.SkipObjectResolution)
	require.NoError(t, err, "core.go must parse")

	constructions := 0
	assigned := map[string][]string{} // composite-literal type name -> idents fed to BlockList

	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok &&
					pkg.Name == "blocklist" && sel.Sel.Name == "NewSubsystem" {
					constructions++
				}
			}
		}

		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		typeName := compositeLitTypeName(lit)
		if typeName == "" {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "BlockList" {
				continue
			}
			if val, ok := kv.Value.(*ast.Ident); ok {
				assigned[typeName] = append(assigned[typeName], val.Name)
			}
		}
		return true
	})

	require.Equal(t, 1, constructions,
		"core.go must construct exactly ONE blocklist.Subsystem; a second gives the two composition roots independently-polled lists")

	// Non-vacuity: each root must actually carry the field. A missing
	// assignment is RED here rather than an empty map quietly passing.
	for _, root := range []string{"grpcSubsystemConfig", "BootstrapSubsystemConfig", "productionSubsystemSet"} {
		require.Len(t, assigned[root], 1,
			"%s must be handed the block-list subsystem exactly once in core.go", root)
	}

	grpcIdent := assigned["grpcSubsystemConfig"][0]
	bootstrapIdent := assigned["BootstrapSubsystemConfig"][0]
	orchIdent := assigned["productionSubsystemSet"][0]

	assert.Equal(t, grpcIdent, bootstrapIdent,
		"the gRPC root and the bootstrap root must receive the SAME blocklist.Subsystem value")
	assert.Equal(t, grpcIdent, orchIdent,
		"the subsystem registered with the orchestrator must be the same one both composition roots hold — "+
			"otherwise the polled cache is not the cache the gate reads")
}

// compositeLitTypeName returns the bare type name of a composite literal,
// unwrapping a package qualifier (bootstrapsetup.BootstrapSubsystemConfig ->
// BootstrapSubsystemConfig). It returns "" for unnamed literal types.
func compositeLitTypeName(lit *ast.CompositeLit) string {
	switch t := lit.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}
