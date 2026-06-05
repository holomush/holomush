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
	"encoding/gob"
	"io"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaEncode(m *dek.Material, w io.Writer) error {
	return gob.NewEncoder(w).Encode(m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/gob`
}

func leakViaRegister(m *dek.Material) {
	gob.Register(m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to encoding/gob`
}
