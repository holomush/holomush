// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// plugins/ is OUTSIDE the dek package's internal-visibility boundary,
// but lives under github.com/holomush/holomush/ so that go/types'
// internal-package visibility rule lets us import
// internal/eventbus/crypto/dek at all (positive testdata cannot live
// under example.com/ because the typechecker would reject the import
// before the analyzer ever runs — see prior cursorpackageinternal
// precedent).
//
// This package also relies on the analysistest stub at
// testdata/src/google.golang.org/protobuf/proto/proto.go, whose
// Marshal/MarshalOptions.Marshal widen their parameter to `any` so
// that *dek.Material — which does not implement proto.Message in the
// real codebase — can be passed at all. The dekmaterialnoproto rule
// is forward-defensive: it fires only if Material ever gains the
// proto.Message method set.
package positive

import (
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaMarshal(m *dek.Material) ([]byte, error) {
	return proto.Marshal(m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto`
}

func leakViaOpts(m *dek.Material) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(m) // want `INV-CRYPTO-16: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto`
}
