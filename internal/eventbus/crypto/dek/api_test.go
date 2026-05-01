// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// TestPackageHasNoExportedByteSlices guarantees the dek package never
// exposes an exported function/method returning []byte or an exported
// struct field of type []byte. This is the ground-truth defense for
// INV-27 — the ruleguard rules in gorules/ catch known sinks, but this
// test catches API drift (a future contributor adding a Bytes()
// accessor would bypass the ruleguards by introducing a new export).
func TestPackageHasNoExportedByteSlices(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
	}
	pkgs, err := packages.Load(cfg, "github.com/holomush/holomush/internal/eventbus/crypto/dek")
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	pkg := pkgs[0]
	require.NotNil(t, pkg.Types, "package types not loaded")

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch o := obj.(type) {
		case *types.Func:
			assertFuncDoesNotReturnByteSlice(t, o)
		case *types.TypeName:
			assertNamedTypeHasNoByteSliceFields(t, o)
			// Method set on the named type
			if named, ok := o.Type().(*types.Named); ok {
				for i := 0; i < named.NumMethods(); i++ {
					m := named.Method(i)
					if m.Exported() {
						assertFuncDoesNotReturnByteSlice(t, m)
					}
				}
				// Pointer method set
				ptrMethods := types.NewMethodSet(types.NewPointer(named))
				for i := 0; i < ptrMethods.Len(); i++ {
					sel := ptrMethods.At(i)
					if fn, ok := sel.Obj().(*types.Func); ok && fn.Exported() {
						assertFuncDoesNotReturnByteSlice(t, fn)
					}
				}
			}
		}
	}
}

func assertFuncDoesNotReturnByteSlice(t *testing.T, fn *types.Func) {
	t.Helper()
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return
	}
	results := sig.Results()
	for i := 0; i < results.Len(); i++ {
		r := results.At(i)
		if isByteSlice(r.Type()) {
			t.Fatalf("INV-27 violation: dek.%s returns []byte at result position %d. "+
				"If you need to expose key bytes, route through codec.Key (which is "+
				"lint-allowlisted via gorules/codec_key_bytes_allowlist.go).",
				fn.Name(), i)
		}
	}
}

func assertNamedTypeHasNoByteSliceFields(t *testing.T, tn *types.TypeName) {
	t.Helper()
	s, ok := tn.Type().Underlying().(*types.Struct)
	if !ok {
		return
	}
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		if !f.Exported() {
			continue
		}
		if isByteSlice(f.Type()) {
			t.Fatalf("INV-27 violation: dek.%s.%s is an exported []byte field. "+
				"Make it unexported and use codec.Key for the egress path.",
				tn.Name(), f.Name())
		}
	}
}

func isByteSlice(t types.Type) bool {
	sl, ok := t.(*types.Slice)
	if !ok {
		return false
	}
	basic, ok := sl.Elem().(*types.Basic)
	return ok && basic.Kind() == types.Uint8
}
