// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// plugins/ is OUTSIDE the dek package's internal-visibility boundary,
// but lives under github.com/holomush/holomush/ so that go/types'
// internal-package visibility rule lets us import
// internal/eventbus/crypto/dek at all (positive testdata cannot live
// under example.com/ because the typechecker would reject the import
// before the analyzer ever runs — see prior cursorpackageinternal
// precedent).
package positive

import (
	"encoding/json"
	"io"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaMarshal(m *dek.Material) ([]byte, error) {
	return json.Marshal(m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/json`
}

func leakViaMarshalIndent(m *dek.Material) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ") // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/json`
}

func leakViaEncoder(m *dek.Material, w io.Writer) error {
	return json.NewEncoder(w).Encode(m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/json`
}

// Conversion-wrapped bypass: any(m) wraps the *dek.Material into an
// interface, so pass.TypesInfo.TypeOf(arg) sees `any`, not `*dek.Material`.
// The analyzer must unwrap conversion-call expressions before the type
// check. CodeRabbit finding on PR #3457.
func leakViaAnyConversion(m *dek.Material) ([]byte, error) {
	return json.Marshal(any(m)) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/json`
}

// Parenthesized conversion: (any)(m) — unwrap ParenExpr around the
// type-conversion CallExpr too.
func leakViaParenConversion(m *dek.Material) ([]byte, error) {
	return json.Marshal((any)(m)) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/json`
}

// Alias-type bypass: `type AliasedMaterial = dek.Material` is a Go
// type alias. Without types.Unalias, the IsDEKMaterial check sees an
// *types.Alias instead of *types.Named and returns false.
// CodeRabbit finding on PR #3457.
type AliasedMaterial = dek.Material

func leakViaAlias(m *AliasedMaterial) ([]byte, error) {
	return json.Marshal(m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/json`
}
