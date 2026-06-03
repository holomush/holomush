// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"context"
	"go/types"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// TestPackageHasNoExportedByteSlices guarantees the dek package never
// exposes an exported function/method returning []byte or an exported
// struct field of type []byte. This is the ground-truth defense for
// INV-CRYPTO-16 — the dekmaterialno* and codeckeybytesallowlist analyzers in
// gorules/analyzers/ catch known sinks, but this test catches API drift
// (a future contributor adding a Bytes() accessor would bypass the
// analyzers by introducing a new export).
func TestPackageHasNoExportedByteSlices(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
	}
	pkgs, err := packages.Load(cfg, "github.com/holomush/holomush/internal/eventbus/crypto/dek")
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	// packages.Load surfaces parse/type-check failures via pkg.Errors,
	// not the top-level err. Without this assertion the test could pass
	// against a package that failed to type-check and miss the API drift
	// it is designed to catch. See cmd/holomush/gateway_imports_test.go.
	require.Equal(t, 0, packages.PrintErrors(pkgs),
		"package failed to load cleanly; static API surface check is unreliable")
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
			t.Fatalf("INV-CRYPTO-16 violation: dek.%s returns []byte at result position %d. "+
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
			t.Fatalf("INV-CRYPTO-16 violation: dek.%s.%s is an exported []byte field. "+
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

// stubAllowSet enumerates the bead IDs Phase 2 stubs MAY reference.
// Renaming or closing either bead without updating this list fails CI
// at task lint:test time, surfacing the rot before the stub error
// reaches a production log.
var stubAllowSet = map[string]struct{}{
	"holomush-fi0n": {}, // Phase 4: Add + Rotate lifecycle ops
	"holomush-jxo8": {}, // Phase 5: Rekey + AdminReadStream + OperatorAuth
}

func TestManagerStubsCarryTrackingBeadFromAllowSet(t *testing.T) {
	// Build a Manager skeleton and probe each stub. The Manager
	// constructor (NewManager) is independent of provider correctness;
	// tests inject a stub provider via NewManagerForUnitTest.
	m := dek.NewManagerForUnitTest()

	cases := []struct {
		name      string
		invoke    func() error
		wantBead  string
		wantPhase int
		wantCode  string
	}{
		{
			name: "Rekey",
			invoke: func() error {
				return m.Rekey(context.Background(), dek.ContextID{Type: "scene", ID: "x"}, "test", dek.OperatorFactors{})
			},
			wantBead:  "holomush-jxo8",
			wantPhase: 5,
			wantCode:  "DEK_REKEY_NOT_IMPLEMENTED",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.invoke()
			require.Error(t, err)

			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok, "stub error must be an oops error")

			// Code matches.
			require.Equal(t, tc.wantCode, oopsErr.Code())

			// tracking_bead present + matches expected.
			ctx := oopsErr.Context()
			require.Contains(t, ctx, "tracking_bead")
			require.Equal(t, tc.wantBead, ctx["tracking_bead"])

			// tracking_bead value is in the allow-set.
			beadStr, ok := ctx["tracking_bead"].(string)
			require.True(t, ok,
				"tracking_bead context value must be a string, got %T", ctx["tracking_bead"])
			_, allowed := stubAllowSet[beadStr]
			require.True(t, allowed,
				"tracking_bead %q is not in stubAllowSet — update stubAllowSet "+
					"in api_test.go or fix the stub", beadStr)

			// phase present + matches expected.
			require.Contains(t, ctx, "phase")
			require.Equal(t, tc.wantPhase, ctx["phase"])

			// tracking_bead value matches the holomush-<id> regex shape.
			require.Regexp(t, `^holomush-[a-z0-9]+$`, ctx["tracking_bead"])
		})
	}
}
