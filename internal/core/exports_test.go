// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package core_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRegisterBuiltinTypesIsUnexported is INV-EVENTBUS-12. RegisterBuiltinTypes
// MUST NOT be a public symbol. BootstrapVerbRegistry is the only public
// path that returns a seeded registry.
func TestRegisterBuiltinTypesIsUnexported(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "builtins.go", nil, 0)
	require.NoError(t, err)

	foundBootstrapVerbRegistry := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fn.Name.Name
		// RegisterBuiltinTypes (uppercase) must not exist.
		if name == "RegisterBuiltinTypes" {
			t.Errorf("RegisterBuiltinTypes is exported but MUST be unexported (INV-EVENTBUS-12)")
		}
		// Public seeded constructor must exist.
		if name == "BootstrapVerbRegistry" {
			foundBootstrapVerbRegistry = true
			// Verify it's exported (uppercase).
			require.True(t, strings.ToUpper(name[:1]) == name[:1])
		}
	}
	require.True(t, foundBootstrapVerbRegistry,
		"BootstrapVerbRegistry must exist as a public seeded-registry constructor (INV-EVENTBUS-12)")
}
