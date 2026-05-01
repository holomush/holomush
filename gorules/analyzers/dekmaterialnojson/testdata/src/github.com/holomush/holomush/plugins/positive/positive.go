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
	return json.Marshal(m) // want `INV-27: dek.Material MUST NOT be passed to encoding/json`
}

func leakViaMarshalIndent(m *dek.Material) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ") // want `INV-27: dek.Material MUST NOT be passed to encoding/json`
}

func leakViaEncoder(m *dek.Material, w io.Writer) error {
	return json.NewEncoder(w).Encode(m) // want `INV-27: dek.Material MUST NOT be passed to encoding/json`
}
