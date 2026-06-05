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
	. "log"
	"log"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaPrintf(m *dek.Material) {
	log.Printf("material: %v", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log`
}

func leakViaLoggerPrintf(m *dek.Material, l *log.Logger) {
	l.Printf("material: %v", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log`
}

// Dot-imported call: with `import . "log"`, the call's Fun is an
// *ast.Ident (no qualifying package selector). A naive
// `call.Fun.(*ast.SelectorExpr)` type assertion misses this entirely.
// CodeRabbit finding on PR #3457.
func leakViaDotImport(m *dek.Material) {
	Printf("material: %v", m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to log`
}
